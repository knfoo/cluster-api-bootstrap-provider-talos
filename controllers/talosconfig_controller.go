// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/go-logr/logr"
	"github.com/siderolabs/go-pointer"
	"github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/types/network"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"gopkg.in/yaml.v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	v1beta1conditions "sigs.k8s.io/cluster-api/util/conditions/deprecated/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
	"github.com/siderolabs/cluster-api-bootstrap-provider-talos/pkg/templatepatch"
	// +kubebuilder:scaffold:imports
)

const (
	controllerName = "cabpt-controller"
)

var (
	defaultVersionContract = config.TalosVersionCurrent
)

// TalosConfigReconciler reconciles a TalosConfig object
type TalosConfigReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	WatchFilterValue string
}

type TalosConfigScope struct {
	Config      *bootstrapv1beta1.TalosConfig
	ConfigOwner *bsutil.ConfigOwner
	Cluster     *capiv1.Cluster
}

type TalosConfigBundle struct {
	BootstrapData string
	TalosConfig   string
}

func (r *TalosConfigReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	r.Scheme = mgr.GetScheme()

	b := ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1beta1.TalosConfig{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(r.Scheme, ctrl.LoggerFrom(ctx), r.WatchFilterValue)).
		Watches(
			&capiv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.MachineToBootstrapMapFunc),
		)

	if feature.Gates.Enabled(feature.MachinePool) {
		b = b.Watches(
			&capiv1.MachinePool{},
			handler.EnqueueRequestsFromMapFunc(r.MachinePoolToBootstrapMapFunc),
		).WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(r.Scheme, ctrl.LoggerFrom(ctx), r.WatchFilterValue))
	}

	b = b.Watches(
		&capiv1.Cluster{},
		handler.EnqueueRequestsFromMapFunc(r.ClusterToTalosConfigs),
		builder.WithPredicates(
			predicates.All(r.Scheme, ctrl.LoggerFrom(ctx),
				predicates.ClusterPausedTransitionsOrInfrastructureProvisioned(r.Scheme, ctrl.LoggerFrom(ctx)),
				predicates.ResourceHasFilterLabel(r.Scheme, ctrl.LoggerFrom(ctx), r.WatchFilterValue),
			),
		),
	)

	// Watch Secrets so that Phase 2 triggers immediately when a metadata secret
	// appears, rather than waiting for a requeue timer. This is critical to win
	// the race against CAPM3's setHostSpec (which sets image + userData on the BMH).
	b = b.Watches(
		&corev1.Secret{},
		handler.EnqueueRequestsFromMapFunc(r.MetadataSecretToTalosConfigs),
		builder.WithPredicates(predicate.Funcs{
			CreateFunc:  func(e event.CreateEvent) bool { return true },
			UpdateFunc:  func(e event.UpdateEvent) bool { return true },
			DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			GenericFunc: func(e event.GenericEvent) bool { return false },
		}),
	)

	if err := b.Complete(r); err != nil {
		return fmt.Errorf("failed setting up with a controller manager: %w", err)
	}

	return nil

}

// MetadataSecretToTalosConfigs maps a Secret to TalosConfigs that may need Phase 2
// reconciliation. When a metadata secret appears, this triggers immediate
// reconciliation so CABPT can update the bootstrap secret before CAPM3's
// setHostSpec sets the image on the BMH.
func (r *TalosConfigReconciler) MetadataSecretToTalosConfigs(ctx context.Context, o client.Object) []reconcile.Request {
	secret, ok := o.(*corev1.Secret)
	if !ok {
		return nil
	}

	// Only process secrets that look like metadata secrets (created by Metal3Data controller).
	if secret.Type != "" && secret.Type != "infrastructure.cluster.x-k8s.io/secret" && secret.Type != "Opaque" {
		return nil
	}
	if !strings.HasSuffix(secret.Name, "-metadata") {
		return nil
	}

	// Find TalosConfigs in the same namespace that have pending template patches.
	talosConfigList := &bootstrapv1beta1.TalosConfigList{}
	if err := r.Client.List(ctx, talosConfigList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, tc := range talosConfigList.Items {
		if tc.Spec.MetadataFrom == nil {
			continue
		}
		// Enqueue TalosConfigs that have pending template patches or where
		// the bootstrap secret doesn't yet have the final key.
		if ptr.Deref(tc.Status.TemplatePatchesPending, false) ||
			(ptr.Deref(tc.Status.Initialization.DataSecretCreated, false) && tc.Spec.DataSecretKey != "") {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&tc),
			})
		}
	}

	return requests
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=talosconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=talosconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *TalosConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := r.Log.WithName(controllerName).
		WithName(fmt.Sprintf("namespace=%s", req.Namespace)).
		WithName(fmt.Sprintf("talosconfig=%s", req.Name))

	// Lookup the talosconfig config
	config := &bootstrapv1beta1.TalosConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get config")
		return ctrl.Result{}, err
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the TalosConfig object and status after each reconciliation.
	defer func() {
		// always update the readyCondition; the summary is represented using the "1 of x completed" notation.
		conditions.SetSummaryCondition(config, config, bootstrapv1beta1.DataSecretAvailableCondition)
		v1beta1conditions.SetSummary(config,
			v1beta1conditions.WithConditions(
				bootstrapv1beta1.DataSecretAvailableV1Beta1Condition,
			),
		)

		patchOpts := []patch.Option{
			patch.WithOwnedConditions{
				Conditions: []string{
					bootstrapv1beta1.DataSecretAvailableCondition,
				},
			},
			patch.WithOwnedV1Beta1Conditions{
				Conditions: []capiv1.ConditionType{
					bootstrapv1beta1.DataSecretAvailableV1Beta1Condition,
				},
			},
		}

		// Patch ObservedGeneration only if the reconciliation completed successfully
		if rerr == nil {
			patchOpts = append(patchOpts, patch.WithStatusObservedGeneration{})
		}

		if err := patchHelper.Patch(ctx, config, patchOpts...); err != nil {
			log.Error(err, "failed to patch config")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Handle deleted talosconfigs
	// We no longer set finalizers on talosconfigs, but we have to remove previously set finalizers
	if !config.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(config)
	}

	// Look up the resource that owns this talosconfig if there is one
	owner, err := bsutil.GetConfigOwner(ctx, r.Client, config)
	if err != nil {
		log.Error(err, "could not get owner resource")
		return ctrl.Result{}, err
	}

	if owner == nil {
		log.Info("Waiting for OwnerRef on the talosconfig")
		return ctrl.Result{}, nil
	}

	log = log.WithName(fmt.Sprintf("owner-name=%s", owner.GetName()))

	// Lookup the cluster the machine is associated with
	cluster, err := util.GetClusterByName(ctx, r.Client, owner.GetNamespace(), owner.ClusterName())
	if err != nil {
		if errors.Is(err, util.ErrNoCluster) {
			log.Info(fmt.Sprintf("%s does not belong to a cluster yet, waiting until it's part of a cluster", owner.GetKind()))
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(err) {
			log.Info("Cluster does not exist yet, waiting until it is created")
			return ctrl.Result{}, nil
		}

		log.Error(err, "could not get cluster by machine metadata")

		return ctrl.Result{}, err
	}

	if annotations.IsPaused(cluster, config) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	tcScope := &TalosConfigScope{
		Config:      config,
		ConfigOwner: owner,
		Cluster:     cluster,
	}

	// bail super early if it's already ready, unless template patches are still pending
	if ptr.Deref(config.Status.Initialization.DataSecretCreated, false) {
		needsPhase2 := ptr.Deref(config.Status.TemplatePatchesPending, false)

		// Self-healing: detect if Phase 2 was never applied. This handles the case
		// where templatePatchesPending was set by the controller but stripped by the
		// API server (e.g. CRD not yet updated). If dataSecretKey is configured
		// (e.g. "userData") but the bootstrap secret only has "value", Phase 2 is needed.
		if !needsPhase2 && config.Spec.DataSecretKey != "" && config.Spec.MetadataFrom != nil {
			if secretName := config.Status.DataSecretName; secretName != "" {
				if secret, fetchErr := r.fetchSecret(ctx, config, secretName); fetchErr == nil {
					if _, hasKey := secret.Data[config.Spec.DataSecretKey]; !hasKey {
						log.Info("bootstrap secret missing final key, triggering Phase 2", "key", config.Spec.DataSecretKey)
						needsPhase2 = true
					}
				}
			}
		}

		if !needsPhase2 {
			log.Info("ignoring an already ready config")
			v1beta1conditions.MarkTrue(config, bootstrapv1beta1.DataSecretAvailableV1Beta1Condition)
			conditions.Set(config, metav1.Condition{
				Type:   bootstrapv1beta1.DataSecretAvailableCondition,
				Reason: bootstrapv1beta1.DataSecretAvailableReason,
				Status: metav1.ConditionTrue,
			})

			// reconcile cluster-wide talosconfig
			err = r.reconcileClientConfig(ctx, log, tcScope)

			if err == nil {
				v1beta1conditions.MarkTrue(config, bootstrapv1beta1.ClientConfigAvailableV1Beta1Condition)
				conditions.Set(config, metav1.Condition{
					Type:   bootstrapv1beta1.ClientConfigAvailableCondition,
					Reason: bootstrapv1beta1.ClientConfigAvailableReason,
					Status: metav1.ConditionTrue,
				})
			} else {
				v1beta1conditions.MarkFalse(
					config,
					bootstrapv1beta1.ClientConfigAvailableV1Beta1Condition,
					bootstrapv1beta1.ClientConfigGenerationFailedV1Beta1Reason,
					capiv1.ConditionSeverityError,
					"talosconfig generation failure: %s",
					err.Error(),
				)
				conditions.Set(config, metav1.Condition{
					Type:    bootstrapv1beta1.ClientConfigAvailableCondition,
					Status:  metav1.ConditionFalse,
					Reason:  bootstrapv1beta1.ClientConfigAvailableInternalErrorReason,
					Message: fmt.Sprintf("talosconfig generation failure: %s", err),
				})
			}

			return ctrl.Result{}, err
		}

		// Template patches are pending — re-reconcile to apply them now that metadata may be available.
		log.Info("re-reconciling to apply pending template patches")
	}

	// Wait patiently for the infrastructure to be ready
	if !ptr.Deref(cluster.Status.Initialization.InfrastructureProvisioned, false) {
		log.Info("Infrastructure is not ready, waiting until ready.")

		v1beta1conditions.MarkFalse(
			config,
			bootstrapv1beta1.DataSecretAvailableV1Beta1Condition,
			bootstrapv1beta1.WaitingForClusterInfrastructureV1Beta1Reason,
			capiv1.ConditionSeverityInfo,
			"",
		)
		conditions.Set(config, metav1.Condition{
			Type:    bootstrapv1beta1.DataSecretAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  bootstrapv1beta1.DataSecretNotAvailableReason,
			Message: "Waiting for the cluster infrastructure to be ready",
		})

		return ctrl.Result{}, nil
	}

	// Reconcile status for machines that already have a secret reference, but our status isn't up to date.
	// This case solves the pivoting scenario (or a backup restore) which doesn't preserve the status subresource on objects.
	if owner.DataSecretName() != nil && (!ptr.Deref(config.Status.Initialization.DataSecretCreated, false) || config.Status.DataSecretName == "") {
		config.Status.Initialization.DataSecretCreated = ptr.To(true)
		config.Status.DataSecretName = *owner.DataSecretName()

		v1beta1conditions.MarkTrue(config, bootstrapv1beta1.DataSecretAvailableV1Beta1Condition)
		conditions.Set(config, metav1.Condition{
			Type:   bootstrapv1beta1.DataSecretAvailableCondition,
			Reason: bootstrapv1beta1.DataSecretAvailableReason,
			Status: metav1.ConditionTrue,
		})

		return ctrl.Result{}, nil
	}

	metadataPending, err := r.reconcileGenerate(ctx, tcScope)
	if err != nil {
		v1beta1conditions.MarkFalse(
			config,
			bootstrapv1beta1.DataSecretAvailableV1Beta1Condition,
			bootstrapv1beta1.DataSecretGenerationFailedV1Beta1Reason,
			capiv1.ConditionSeverityError,
			"%s",
			err.Error(),
		)
		conditions.Set(config, metav1.Condition{
			Type:    bootstrapv1beta1.DataSecretAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  bootstrapv1beta1.DataSecretNotAvailableInternalErrorReason,
			Message: fmt.Sprintf("Data secret generation failed: %s", err),
		})

		return ctrl.Result{}, err
	}

	config.Status.Initialization.DataSecretCreated = ptr.To(true)

	if metadataPending {
		// Phase 1: bootstrap secret created with key "value" (without template patches).
		// Requeue to apply patches once the metadata secret appears.
		config.Status.TemplatePatchesPending = ptr.To(true)

		v1beta1conditions.MarkTrue(config, bootstrapv1beta1.DataSecretAvailableV1Beta1Condition)
		conditions.Set(config, metav1.Condition{
			Type:   bootstrapv1beta1.DataSecretAvailableCondition,
			Reason: bootstrapv1beta1.DataSecretAvailableReason,
			Status: metav1.ConditionTrue,
		})

		log.Info("metadata not yet available, bootstrap secret created without template patches (Phase 1)")

		// The Secret watch will trigger Phase 2 immediately when the metadata
		// secret appears. The 30s fallback is a safety net that exceeds CAPM3's
		// WaitForM3Metadata requeue interval.
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Phase 2 complete or single-phase (no metadataFrom): all patches applied.
	config.Status.TemplatePatchesPending = ptr.To(false)

	v1beta1conditions.MarkTrue(config, bootstrapv1beta1.DataSecretAvailableV1Beta1Condition)
	conditions.Set(config, metav1.Condition{
		Type:   bootstrapv1beta1.DataSecretAvailableCondition,
		Reason: bootstrapv1beta1.DataSecretAvailableReason,
		Status: metav1.ConditionTrue,
	})

	return ctrl.Result{}, nil
}

// reconcileGenerate generates the Talos config and writes the bootstrap secret.
// It returns metadataPending=true when metadataFrom is configured but the metadata
// secret does not yet exist — in that case bootstrap data is written without template
// patches (Phase 1). The caller should requeue so Phase 2 can apply patches once
// the metadata secret appears.
func (r *TalosConfigReconciler) reconcileGenerate(ctx context.Context, tcScope *TalosConfigScope) (metadataPending bool, retErr error) {
	var (
		retData *TalosConfigBundle
		err     error
	)

	config := tcScope.Config

	machineType, _ := machine.ParseType(config.Spec.GenerateType) //nolint:errcheck // handle errors later

	multiConfigPatches := []string{}

	switch {
	// Slurp and use user-supplied configs
	case config.Spec.GenerateType == "none":
		if config.Spec.Data == "" {
			return false, errors.New("failed to specify config data with none generate type")
		}

		retData, err = r.userConfigs(ctx, tcScope)
		if err != nil {
			return false, err
		}

	// Generate configs on the fly
	case machineType != machine.TypeUnknown:
		retData, multiConfigPatches, err = r.genConfigs(ctx, tcScope)
		if err != nil {
			return false, err
		}

	default:
		return false, fmt.Errorf("unknown generate type specified: %q", config.Spec.GenerateType)
	}

	// Handle JSON6902 patches to the machine config if they were specified
	// Note this will patch both pre-generated and user-provided configs.
	if len(config.Spec.ConfigPatches) > 0 {
		marshalledPatches, err := json.Marshal(config.Spec.ConfigPatches)
		if err != nil {
			return false, fmt.Errorf("failure marshalling config patches: %w", err)
		}

		patch, err := jsonpatch.DecodePatch(marshalledPatches)
		if err != nil {
			return false, fmt.Errorf("failure decoding config patches from talosconfig to rfc6902 patch: %w", err)
		}

		patchedBytes, err := configpatcher.JSON6902([]byte(retData.BootstrapData), patch)
		if err != nil {
			return false, err
		}

		retData.BootstrapData = string(patchedBytes)
	}

	// Build template context from metadata secrets + inline variables + machine/cluster info.
	// When metadataFrom or templateVariables is configured, strategicPatches are rendered
	// through Go's text/template engine. Plain strings without template syntax pass through
	// unchanged (text/template is a no-op for literal text).
	var metadata map[string]string

	hasMetadata := config.Spec.MetadataFrom != nil
	hasVariables := len(config.Spec.TemplateVariables) > 0

	if hasMetadata || hasVariables {
		// Start with metadata from secret (if configured).
		if hasMetadata {
			metadata, err = r.resolveMetadata(ctx, tcScope)
			if err != nil {
				if isMetadataNotYetAvailable(err) {
					// Two-phase approach: metadata secret doesn't exist yet (e.g., CAPM3
					// hasn't created the Metal3DataClaim because it waits for BootstrapReady).
					// Proceed without template rendering — Phase 1 creates the bootstrap
					// secret so CAPM3 can progress, then Phase 2 renders templates.
					r.Log.Info("metadata secret not yet available, proceeding without template rendering (Phase 1)")
					metadataPending = true
					metadata = nil
				} else {
					return false, fmt.Errorf("resolving metadata: %w", err)
				}
			}
		}

		if !metadataPending {
			if metadata == nil {
				metadata = make(map[string]string)
			}

			// Merge inline template variables (take precedence over secret metadata).
			templatepatch.MergeVariables(metadata, config.Spec.TemplateVariables)

			// Add machine context: machine.name, machine.infrastructureRef.name.
			// This enables {{ get "machine.name" }} as proposed in #239.
			if !tcScope.ConfigOwner.IsMachinePool() {
				machine := &capiv1.Machine{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(tcScope.ConfigOwner.Object, machine); err != nil {
					r.Log.Info("could not extract Machine from config owner for template context, machine.* keys will be unavailable", "error", err)
				} else {
					templatepatch.EnrichWithMachine(metadata, machine.Name, machine.Spec.InfrastructureRef.Name)
				}
			}
		}
	}

	// Collect all strategic merge patches. When a template context is available,
	// render them through the template engine. Plain strings without template
	// syntax pass through unchanged.
	userStrategicPatches := slices.Concat(config.Spec.StrategicPatches, multiConfigPatches)

	if metadata != nil && len(userStrategicPatches) > 0 {
		userStrategicPatches, err = templatepatch.ProcessTemplates(userStrategicPatches, metadata)
		if err != nil {
			return false, fmt.Errorf("rendering strategicPatches templates: %w", err)
		}
	}

	if strategicPatches := userStrategicPatches; len(strategicPatches) > 0 {
		patches := make([]configpatcher.Patch, 0, len(strategicPatches))

		for _, strategicPatch := range strategicPatches {
			// In Phase 1 (metadata pending), skip patches containing unrendered
			// template expressions — they produce invalid YAML. These will be
			// rendered and applied in Phase 2 once metadata is available.
			if metadataPending && strings.Contains(strategicPatch, "{{") {
				continue
			}

			patch, err := configpatcher.LoadPatch([]byte(strategicPatch))
			if err != nil {
				return false, fmt.Errorf("failure loading StrategicPatch: %w", err)
			}

			patches = append(patches, patch)
		}

		out, err := configpatcher.Apply(configpatcher.WithBytes([]byte(retData.BootstrapData)), patches)
		if err != nil {
			return false, fmt.Errorf("failure applying StrategicPatches: %w", err)
		}

		outCfg, err := out.Config()
		if err != nil {
			return false, fmt.Errorf("failure converting result to bytes: %w", err)
		}

		retData.BootstrapData, err = outCfg.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
		if err != nil {
			return false, fmt.Errorf("failure converting config to string: %w", err)
		}
	}

	// Packet acts a fool if you don't prepend #!talos to the userdata
	// so we try to suss out if that's the type of machine/machinePool getting created.
	if tcScope.ConfigOwner.IsMachinePool() {
		mp := &capiv1.MachinePool{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(tcScope.ConfigOwner.Object, mp); err != nil {
			return false, err
		}

		if mp.Spec.Template.Spec.InfrastructureRef.Kind == "PacketMachinePool" {
			retData.BootstrapData = "#!talos\n" + retData.BootstrapData
		}
	} else {
		machine := &capiv1.Machine{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(tcScope.ConfigOwner.Object, machine); err != nil {
			return false, err
		}

		if machine.Spec.InfrastructureRef.Kind == "PacketMachine" {
			retData.BootstrapData = "#!talos\n" + retData.BootstrapData
		}
	}

	var dataSecretName string

	dataSecretName, err = r.writeBootstrapData(ctx, tcScope, []byte(retData.BootstrapData), metadataPending)
	if err != nil {
		return false, err
	}

	config.Status.DataSecretName = dataSecretName

	return metadataPending, nil
}

func (r *TalosConfigReconciler) reconcileDelete(config *bootstrapv1beta1.TalosConfig) (ctrl.Result, error) {
	controllerutil.RemoveFinalizer(config, bootstrapv1beta1.ConfigFinalizer)

	return ctrl.Result{}, nil
}

// isMetadataNotYetAvailable returns true if the error indicates the metadata
// secret does not exist yet (transient condition that resolves when the
// infrastructure provider creates the Metal3DataClaim).
func isMetadataNotYetAvailable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not yet available")
}

// resolveMetadata reads infrastructure metadata from the configured source and
// enriches it with cluster data. Returns nil if MetadataFrom is not configured.
func (r *TalosConfigReconciler) resolveMetadata(ctx context.Context, scope *TalosConfigScope) (map[string]string, error) {
	config := scope.Config

	if config.Spec.MetadataFrom == nil {
		return nil, nil
	}

	var (
		secretName string
		dataKey    string
	)

	if config.Spec.MetadataFrom.SecretRef != nil && config.Spec.MetadataFrom.InfrastructureMachine != nil {
		return nil, fmt.Errorf("metadataFrom must specify either secretRef or infrastructureMachine, not both")
	}

	switch {
	case config.Spec.MetadataFrom.SecretRef != nil:
		secretName = config.Spec.MetadataFrom.SecretRef.Name
		if secretName == "" {
			return nil, fmt.Errorf("metadataFrom.secretRef.name must not be empty")
		}

		dataKey = config.Spec.MetadataFrom.SecretRef.Key
		if dataKey == "" {
			dataKey = "metaData"
		}

	case config.Spec.MetadataFrom.InfrastructureMachine != nil:
		infraMeta := config.Spec.MetadataFrom.InfrastructureMachine

		suffix := infraMeta.SecretNameSuffix
		if suffix == "" {
			suffix = "-metadata"
		}

		dataKey = infraMeta.DataKey
		if dataKey == "" {
			dataKey = "metaData"
		}

		// Resolve the infrastructure machine name from the owning Machine.
		machine := &capiv1.Machine{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(scope.ConfigOwner.Object, machine); err != nil {
			return nil, fmt.Errorf("failed to convert config owner to Machine: %w", err)
		}

		infraName := machine.Spec.InfrastructureRef.Name
		if infraName == "" {
			return nil, fmt.Errorf("machine %s has no infrastructure reference name", machine.Name)
		}

		secretName = infraName + suffix

	default:
		return nil, fmt.Errorf("metadataFrom must specify either secretRef or infrastructureMachine")
	}

	secret, err := r.fetchSecret(ctx, config, secretName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("metadata secret %q not yet available: %w", secretName, err)
		}

		return nil, fmt.Errorf("failed to fetch metadata secret %q: %w", secretName, err)
	}

	raw, ok := secret.Data[dataKey]
	if !ok {
		return nil, fmt.Errorf("metadata secret %q does not contain key %q", secretName, dataKey)
	}

	metadata := templatepatch.ParseMetadata(raw)
	templatepatch.EnrichWithCluster(metadata, scope.Cluster)

	return metadata, nil
}

func genTalosConfigFile(clusterName string, bundle *secrets.Bundle, endpoints []string) (string, error) {
	in, err := generate.NewInput(clusterName, "https://localhost", "", generate.WithSecretsBundle(bundle), generate.WithEndpointList(endpoints))
	if err != nil {
		return "", err
	}

	talosConfig, err := in.Talosconfig()
	if err != nil {
		return "", err
	}

	talosConfigBytes, err := yaml.Marshal(talosConfig)
	if err != nil {
		return "", err
	}

	return string(talosConfigBytes), nil
}

// userConfigs will fetch and make use of user-supplied bootstrap configs to return
func (r *TalosConfigReconciler) userConfigs(ctx context.Context, scope *TalosConfigScope) (*TalosConfigBundle, error) {
	retBundle := &TalosConfigBundle{}

	userConfig, err := configloader.NewFromBytes([]byte(scope.Config.Spec.Data))
	if err != nil {
		return retBundle, err
	}

	// Create the secret with kubernetes certs so a kubeconfig can be generated
	// but do this only when machineconfig contains full Kubernetes CA secret (controlplane nodes)
	if userConfig.Cluster().IssuingCA() != nil && len(userConfig.Cluster().IssuingCA().Crt) > 0 && len(userConfig.Cluster().IssuingCA().Key) > 0 {
		if err = r.writeK8sCASecret(ctx, scope, userConfig.Cluster().IssuingCA()); err != nil {
			return retBundle, err
		}
	}

	userConfigStr, err := userConfig.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return retBundle, err
	}

	retBundle.BootstrapData = userConfigStr

	if userConfig.Machine().Security().IssuingCA() != nil && len(userConfig.Machine().Security().IssuingCA().Crt) > 0 && len(userConfig.Machine().Security().IssuingCA().Key) > 0 {
		bundle := secrets.NewBundleFromConfig(secrets.NewFixedClock(time.Now()), userConfig)

		retBundle.TalosConfig, err = genTalosConfigFile(userConfig.Cluster().Name(), bundle, nil)
		if err != nil {
			r.Log.Error(err, "failed generating talosconfig for user-supplied machine configuration")
		}
	}

	return retBundle, nil
}

// genConfigs will generate a bootstrap config and a talosconfig to return
func (r *TalosConfigReconciler) genConfigs(ctx context.Context, scope *TalosConfigScope) (*TalosConfigBundle, []string, error) {
	retBundle := &TalosConfigBundle{}

	// Determine what type of node this is
	machineType, err := machine.ParseType(scope.Config.Spec.GenerateType)
	if err != nil {
		machineType = machine.TypeWorker
	}

	patches := []string{}

	// Allow user to override default kube version.
	// This also handles version being formatted like "vX.Y.Z" instead of without leading 'v'
	// TrimPrefix returns the string unchanged if the prefix isn't present.
	k8sVersion := constants.DefaultKubernetesVersion
	if scope.ConfigOwner.IsMachinePool() {
		mp := &capiv1.MachinePool{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(scope.ConfigOwner.Object, mp); err != nil {
			return retBundle, patches, err
		}
		if mp.Spec.Template.Spec.Version != "" {
			k8sVersion = strings.TrimPrefix(mp.Spec.Template.Spec.Version, "v")
		}
	} else {
		machine := &capiv1.Machine{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(scope.ConfigOwner.Object, machine); err != nil {
			return retBundle, patches, err
		}
		if machine.Spec.Version != "" {
			k8sVersion = strings.TrimPrefix(machine.Spec.Version, "v")
		}
	}

	clusterDNS := constants.DefaultDNSDomain
	if scope.Cluster.Spec.ClusterNetwork.ServiceDomain != "" {
		clusterDNS = scope.Cluster.Spec.ClusterNetwork.ServiceDomain
	}

	genOptions := []generate.Option{generate.WithDNSDomain(clusterDNS)}

	versionContract := defaultVersionContract

	if scope.Config.Spec.TalosVersion != "" {
		var err error
		versionContract, err = config.ParseContractFromVersion(scope.Config.Spec.TalosVersion)
		if err != nil {
			return retBundle, patches, fmt.Errorf("invalid talos-version: %w", err)
		}
	}

	genOptions = append(genOptions, generate.WithVersionContract(versionContract))

	secretBundle, err := r.getSecretsBundle(ctx, scope, true, versionContract)
	if err != nil {
		return retBundle, patches, err
	}

	genOptions = append(genOptions, generate.WithSecretsBundle(secretBundle))

	// Talos dropped support for version contracts <= 0.14, but we still need to support old secret bundles
	if versionContract != nil && versionContract.Major < 1 && versionContract.Minor < 14 {
		genOptions = append(genOptions, generate.WithClusterDiscovery(false))
	}

	APIEndpointPort := strconv.Itoa(int(scope.Cluster.Spec.ControlPlaneEndpoint.Port))

	input, err := generate.NewInput(
		scope.Cluster.Name,
		"https://"+net.JoinHostPort(scope.Cluster.Spec.ControlPlaneEndpoint.Host, APIEndpointPort),
		k8sVersion,
		genOptions...,
	)
	if err != nil {
		return retBundle, patches, err
	}

	// Create the secret with kubernetes certs so a kubeconfig can be generated
	if err = r.writeK8sCASecret(ctx, scope, secretBundle.Certs.K8s); err != nil {
		return retBundle, patches, err
	}

	tcString, err := genTalosConfigFile(input.ClusterName, secretBundle, nil)
	if err != nil {
		return retBundle, patches, err
	}

	retBundle.TalosConfig = tcString

	data, err := input.Config(machineType)
	if err != nil {
		return retBundle, patches, err
	}

	if scope.Cluster.Spec.ClusterNetwork.Pods.CIDRBlocks != nil {
		data.RawV1Alpha1().ClusterConfig.ClusterNetwork.PodSubnet = scope.Cluster.Spec.ClusterNetwork.Pods.CIDRBlocks
	}
	if scope.Cluster.Spec.ClusterNetwork.Services.CIDRBlocks != nil {
		data.RawV1Alpha1().ClusterConfig.ClusterNetwork.ServiceSubnet = scope.Cluster.Spec.ClusterNetwork.Services.CIDRBlocks
	}

	if !scope.ConfigOwner.IsMachinePool() && scope.Config.Spec.Hostname.Source != "" {
		if data.RawV1Alpha1().MachineConfig.MachineNetwork == nil {
			data.RawV1Alpha1().MachineConfig.MachineNetwork = &v1alpha1.NetworkConfig{}
		}

		talosVersion, parseErr := semver.NewVersion(strings.TrimLeft(scope.Config.Spec.TalosVersion, "v"))

		if scope.Config.Spec.Hostname.Source == bootstrapv1beta1.HostnameSourceMachineName {
			if parseErr == nil && talosVersion.GreaterThanEqual(semver.MustParse("1.12.0-beta.0")) {
				hostnameCfg, err := newHostnameConfig(scope.ConfigOwner.GetName())
				if err != nil {
					return retBundle, patches, err
				}

				patches = append(patches, hostnameCfg)
			} else {
				data.RawV1Alpha1().MachineConfig.MachineNetwork.NetworkHostname = scope.ConfigOwner.GetName()
			}
		}

		if scope.Config.Spec.Hostname.Source == bootstrapv1beta1.HostnameSourceInfrastructureName {
			machine := &capiv1.Machine{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(scope.ConfigOwner.Object, machine); err != nil {
				return retBundle, patches, err
			}

			if parseErr == nil && talosVersion.GreaterThanEqual(semver.MustParse("1.12.0-beta.0")) {
				hostnameCfg, err := newHostnameConfig(machine.Spec.InfrastructureRef.Name)
				if err != nil {
					return retBundle, patches, err
				}

				patches = append(patches, hostnameCfg)
			} else {
				data.RawV1Alpha1().MachineConfig.MachineNetwork.NetworkHostname = machine.Spec.InfrastructureRef.Name
			}
		}
	}

	dataOut, err := data.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return retBundle, patches, err
	}

	retBundle.BootstrapData = dataOut

	return retBundle, patches, nil
}

// MachineToBootstrapMapFunc is a handler.ToRequestsFunc to be used to enqueue
// request for reconciliation of TalosConfig.
func (r *TalosConfigReconciler) MachineToBootstrapMapFunc(_ context.Context, o client.Object) []ctrl.Request {
	m, ok := o.(*capiv1.Machine)
	if !ok {
		panic(fmt.Sprintf("Expected a Machine but got a %T", o))
	}

	result := []ctrl.Request{}
	if m.Spec.Bootstrap.ConfigRef.IsDefined() && m.Spec.Bootstrap.ConfigRef.GroupKind() == bootstrapv1beta1.GroupVersion.WithKind("TalosConfig").GroupKind() {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// MachinePoolToBootstrapMapFunc is a handler.ToRequestsFunc to be used to enqueue
// request for reconciliation of TalosConfig.
func (r *TalosConfigReconciler) MachinePoolToBootstrapMapFunc(_ context.Context, o client.Object) []ctrl.Request {
	m, ok := o.(*capiv1.MachinePool)
	if !ok {
		panic(fmt.Sprintf("Expected a MachinePool but got a %T", o))
	}

	result := []ctrl.Request{}
	configRef := m.Spec.Template.Spec.Bootstrap.ConfigRef
	if configRef.IsDefined() && configRef.GroupKind() == bootstrapv1beta1.GroupVersion.WithKind("TalosConfig").GroupKind() {
		name := client.ObjectKey{Namespace: m.Namespace, Name: configRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// ClusterToTalosConfigs is a handler.ToRequestsFunc to be used to enqeue
// requests for reconciliation of TalosConfigs.
func (r *TalosConfigReconciler) ClusterToTalosConfigs(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}

	c, ok := o.(*capiv1.Cluster)
	if !ok {
		panic(fmt.Sprintf("Expected a Cluster but got a %T", o))
	}

	selectors := []client.ListOption{
		client.InNamespace(c.Namespace),
		client.MatchingLabels{
			capiv1.ClusterNameLabel: c.Name,
		},
	}

	machineList := &capiv1.MachineList{}
	if err := r.Client.List(ctx, machineList, selectors...); err != nil {
		return nil
	}

	for _, m := range machineList.Items {
		if m.Spec.Bootstrap.ConfigRef.IsDefined() &&
			m.Spec.Bootstrap.ConfigRef.GroupKind() == bootstrapv1beta1.GroupVersion.WithKind("TalosConfig").GroupKind() {
			name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
			result = append(result, ctrl.Request{NamespacedName: name})
		}
	}

	if feature.Gates.Enabled(feature.MachinePool) {
		machinePoolList := &capiv1.MachinePoolList{}
		if err := r.Client.List(ctx, machinePoolList, selectors...); err != nil {
			return nil
		}

		for _, mp := range machinePoolList.Items {
			if mp.Spec.Template.Spec.Bootstrap.ConfigRef.IsDefined() &&
				mp.Spec.Template.Spec.Bootstrap.ConfigRef.GroupKind() == bootstrapv1beta1.GroupVersion.WithKind("TalosConfig").GroupKind() {
				name := client.ObjectKey{Namespace: mp.Namespace, Name: mp.Spec.Template.Spec.Bootstrap.ConfigRef.Name}
				result = append(result, ctrl.Request{NamespacedName: name})
			}
		}
	}

	return result
}

func newHostnameConfig(hostname string) (string, error) {
	hostnameConfig := network.NewHostnameConfigV1Alpha1()
	hostnameConfig.ConfigAuto = pointer.To(nethelpers.AutoHostnameKindOff)
	hostnameConfig.ConfigHostname = hostname

	buf := new(bytes.Buffer)

	err := yaml.NewEncoder(buf).Encode(hostnameConfig)

	return buf.String(), err
}
