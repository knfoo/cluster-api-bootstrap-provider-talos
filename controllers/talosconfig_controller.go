// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	v1beta1conditions "sigs.k8s.io/cluster-api/util/conditions/deprecated/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
	"github.com/siderolabs/cluster-api-bootstrap-provider-talos/pkg/machineconfig"
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
	ConfigOwner *ConfigOwner
	Cluster     *capiv1.Cluster
}

type TalosConfigBundle struct {
	BootstrapData string
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

	// Watch Secrets so that Phase 2 (rendering the strategic patches that read
	// infrastructure metadata) starts as soon as the metadata secret appears,
	// instead of waiting for the fallback requeue. Metal3/CAPM3 creates the
	// Metal3DataClaim - and therefore the metadata secret - only after the
	// bootstrap config is ready, so this watch is what wins that race.
	// Ported from cabpt-b7fbe6a-template-rendering.patch:1090-1104.
	b = b.Watches(
		&corev1.Secret{},
		handler.EnqueueRequestsFromMapFunc(r.MetadataSecretToTalosConfigs),
		builder.WithPredicates(predicate.Funcs{
			CreateFunc:  func(_ event.CreateEvent) bool { return true },
			UpdateFunc:  func(_ event.UpdateEvent) bool { return true },
			DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
			GenericFunc: func(_ event.GenericEvent) bool { return false },
		}),
	)

	if err := b.Complete(r); err != nil {
		return fmt.Errorf("failed setting up with a controller manager: %w", err)
	}

	return nil

}

// MetadataSecretToTalosConfigs maps a metadata Secret to the TalosConfigs that may
// need Phase 2 reconciliation. Ported from
// cabpt-b7fbe6a-template-rendering.patch:1140-1181.
func (r *TalosConfigReconciler) MetadataSecretToTalosConfigs(ctx context.Context, o client.Object) []ctrl.Request {
	secret, ok := o.(*corev1.Secret)
	if !ok {
		return nil
	}

	// Only secrets that look like infrastructure metadata secrets (Metal3Data /
	// CAPM3 Metal3DataTemplate metadata) can trigger Phase 2.
	if secret.Type != "" && secret.Type != "infrastructure.cluster.x-k8s.io/secret" && secret.Type != "Opaque" {
		return nil
	}

	if !strings.HasSuffix(secret.Name, "-metadata") {
		return nil
	}

	talosConfigList := &bootstrapv1beta1.TalosConfigList{}
	if err := r.Client.List(ctx, talosConfigList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}

	var requests []ctrl.Request
	for _, talosConfig := range talosConfigList.Items {
		if talosConfig.Spec.MetadataFrom == nil {
			continue
		}

		// Enqueue configs with pending template patches, and configs whose bootstrap
		// secret was created without the configured dataSecretKey (self-healing).
		if ptr.Deref(talosConfig.Status.TemplatePatchesPending, false) ||
			(ptr.Deref(talosConfig.Status.Initialization.DataSecretCreated, false) && talosConfig.Spec.DataSecretKey != "") {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(&talosConfig),
			})
		}
	}

	return requests
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=talosconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=talosconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=talosconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/finalizers,verbs=update
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
		// always update the readyCondition, summarizing the DataSecretAvailable condition.
		// ClientConfigAvailable is excluded on purpose: the cluster-wide talosconfig is not needed to bootstrap
		// a Machine, and Cluster API mirrors this condition into the Machine's BootstrapConfigReady condition.
		if err := conditions.SetSummaryCondition(config, config, bootstrapv1beta1.TalosConfigReadyCondition,
			conditions.ForConditionTypes{
				bootstrapv1beta1.DataSecretAvailableCondition,
			},
		); err != nil {
			rerr = errors.Join(rerr, err)
		}
		v1beta1conditions.SetSummary(config,
			v1beta1conditions.WithConditions(
				bootstrapv1beta1.DataSecretAvailableV1Beta1Condition,
			),
		)

		patchOpts := []patch.Option{
			patch.WithOwnedConditions{
				Conditions: []string{
					bootstrapv1beta1.TalosConfigReadyCondition,
					bootstrapv1beta1.DataSecretAvailableCondition,
					bootstrapv1beta1.ClientConfigAvailableCondition,
				},
			},
			patch.WithOwnedV1Beta1Conditions{
				Conditions: []capiv1.ConditionType{
					capiv1.ReadyV1Beta1Condition,
					bootstrapv1beta1.DataSecretAvailableV1Beta1Condition,
					bootstrapv1beta1.ClientConfigAvailableV1Beta1Condition,
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
	owner, err := GetConfigOwner(ctx, r.Client, config)
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

	// bail super early if it's already ready, unless template patches are still pending.
	// Ported from cabpt-b7fbe6a-template-rendering.patch:1131-1221.
	if ptr.Deref(config.Status.Initialization.DataSecretCreated, false) {
		needsPhase2 := ptr.Deref(config.Status.TemplatePatchesPending, false)

		// Self-healing: templatePatchesPending may have been dropped (for example by
		// a CRD that does not yet carry the field, or a status restore). If the
		// bootstrap secret does not have the configured dataSecretKey, Phase 2 never
		// ran and the config is not actually ready.
		if !needsPhase2 && config.Spec.MetadataFrom != nil && config.Spec.DataSecretKey != "" {
			if secretName := config.Status.DataSecretName; secretName != "" {
				if secret, fetchErr := r.fetchSecret(ctx, config, secretName); fetchErr == nil {
					if _, hasKey := secret.Data[config.Spec.DataSecretKey]; !hasKey {
						log.Info("bootstrap secret is missing the configured dataSecretKey, re-running template rendering", "key", config.Spec.DataSecretKey)
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

		// Template patches are pending: re-reconcile so they can be applied now
		// that the metadata may be available.
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

	// In-place updates: while the update extension is updating the Machine in place, the bootstrap
	// provider must not regenerate the bootstrap data for the changed TalosConfig
	// (CAPI bootstrap contract, bootstrap-config.md:364-365). The trigger that sets this annotation
	// on the bootstrap config is KCP's triggerInPlaceUpdate (inplace_trigger.go:108-111).
	if _, inPlaceUpdate := config.GetAnnotations()[capiv1.UpdateInProgressAnnotation]; inPlaceUpdate {
		log.Info("TalosConfig is being updated in place, not regenerating the bootstrap data")

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
		// Phase 1: the bootstrap secret exists (so the Machine becomes bootstrap
		// ready and the infrastructure provider can progress), but the patches that
		// read metadata are not rendered yet. Requeue as a safety net; the Secret
		// watch normally triggers Phase 2 as soon as the metadata secret appears.
		config.Status.TemplatePatchesPending = ptr.To(true)

		v1beta1conditions.MarkTrue(config, bootstrapv1beta1.DataSecretAvailableV1Beta1Condition)
		conditions.Set(config, metav1.Condition{
			Type:   bootstrapv1beta1.DataSecretAvailableCondition,
			Reason: bootstrapv1beta1.DataSecretAvailableReason,
			Status: metav1.ConditionTrue,
		})

		log.Info("metadata not yet available, bootstrap secret created without the rendered template patches (Phase 1)")

		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Phase 2 complete, or single phase: metadataFrom is unset or the metadata is
	// available, so every strategic patch is applied.
	config.Status.TemplatePatchesPending = ptr.To(false)

	v1beta1conditions.MarkTrue(config, bootstrapv1beta1.DataSecretAvailableV1Beta1Condition)
	conditions.Set(config, metav1.Condition{
		Type:   bootstrapv1beta1.DataSecretAvailableCondition,
		Reason: bootstrapv1beta1.DataSecretAvailableReason,
		Status: metav1.ConditionTrue,
	})

	return ctrl.Result{}, nil
}

// reconcileGenerate generates the Talos machine configuration and writes the
// bootstrap data secret. It returns metadataPending=true when metadataFrom is
// configured but the metadata secret does not exist yet: in that case the
// bootstrap data is written without the template patches (Phase 1) and the
// caller requeues so Phase 2 can render them once the metadata appears.
func (r *TalosConfigReconciler) reconcileGenerate(ctx context.Context, tcScope *TalosConfigScope) (metadataPending bool, retErr error) {
	var (
		bootstrapData string
		stdPatches    []string
		err           error
	)

	config := tcScope.Config

	machineType, _ := machine.ParseType(config.Spec.GenerateType) //nolint:errcheck // handle errors later

	switch {
	// Slurp and use user-supplied configs
	case config.Spec.GenerateType == "none":
		if config.Spec.Data == "" {
			return false, errors.New("failed to specify config data with none generate type")
		}

		userBundle, err := r.userConfigs(ctx, tcScope)
		if err != nil {
			return false, err
		}

		bootstrapData = userBundle.BootstrapData

	// Generate configs on the fly. The generation itself lives in
	// pkg/machineconfig so the bootstrap controller and the in-place update
	// extension share one implementation.
	case machineType != machine.TypeUnknown:
		bootstrapData, stdPatches, err = r.generateConfigs(ctx, tcScope)
		if err != nil {
			return false, err
		}

	default:
		return false, fmt.Errorf("unknown generate type specified: %q", config.Spec.GenerateType)
	}

	// Build the template context from metadata secrets, inline template variables
	// and machine context. Ported from
	// cabpt-b7fbe6a-template-rendering.patch:1331-1390.
	var metadata map[string]string

	hasMetadata := config.Spec.MetadataFrom != nil
	hasVariables := len(config.Spec.TemplateVariables) > 0

	if hasMetadata || hasVariables {
		if hasMetadata {
			metadata, err = r.resolveMetadata(ctx, tcScope)
			if err != nil {
				if isMetadataNotYetAvailable(err) {
					// Two-phase bootstrap: the infrastructure provider (Metal3/CAPM3)
					// creates the metadata claim only after the bootstrap config is
					// ready, so the metadata secret cannot exist yet. Write the
					// bootstrap secret without the rendered patches (Phase 1) and let
					// the metadata secret trigger Phase 2.
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

			// Inline template variables take precedence over metadata values.
			templatepatch.MergeVariables(metadata, config.Spec.TemplateVariables)

			// Machine context, so patches can use {{ get "machine.name" }} and
			// {{ get "machine.infrastructureRef.name" }}.
			if !tcScope.ConfigOwner.IsMachinePool() {
				machine := &capiv1.Machine{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(tcScope.ConfigOwner.Object, machine); err != nil {
					r.Log.Info("could not extract Machine from config owner for the template context, machine.* keys are unavailable", "error", err)
				} else {
					templatepatch.EnrichWithMachine(metadata, machine.Name, machine.Spec.InfrastructureRef.Name)
				}
			}
		}
	}

	// Apply the RFC6902 configPatches and the strategic patches (the user's plus
	// the standard patches from the generator), rendering the templates that
	// read the metadata context. This is pkg/machineconfig's job, so the
	// controller and the in-place extension patch a config the same way.
	bootstrapData, err = machineconfig.ApplyPatches(bootstrapData, machineconfig.PatchInput{
		ConfigPatches:     config.Spec.ConfigPatches,
		StrategicPatches:  config.Spec.StrategicPatches,
		StdPatches:        stdPatches,
		Metadata:          metadata,
		MetadataPending:   metadataPending,
	})
	if err != nil {
		return false, err
	}

	// Packet acts a fool if you don't prepend #!talos to the userdata
	// so we try to suss out if that's the type of machine/machinePool getting created.
	if tcScope.ConfigOwner.IsMachinePool() {
		mp := &capiv1.MachinePool{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(tcScope.ConfigOwner.Object, mp); err != nil {
			return false, err
		}

		if mp.Spec.Template.Spec.InfrastructureRef.Kind == "PacketMachinePool" {
			bootstrapData = "#!talos\n" + bootstrapData
		}
	} else {
		machine := &capiv1.Machine{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(tcScope.ConfigOwner.Object, machine); err != nil {
			return false, err
		}

		if machine.Spec.InfrastructureRef.Kind == "PacketMachine" {
			bootstrapData = "#!talos\n" + bootstrapData
		}
	}

	var dataSecretName string

	dataSecretName, err = r.writeBootstrapData(ctx, tcScope, []byte(bootstrapData), metadataPending)
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

// isMetadataNotYetAvailable reports whether the error means the metadata secret
// does not exist yet — a transient state that clears once the infrastructure
// provider (Metal3/CAPM3) creates its metadata claim.
func isMetadataNotYetAvailable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not yet available")
}

// resolveMetadata reads infrastructure metadata from the configured source and
// enriches it with cluster data. Returns nil when metadataFrom is not set.
// Ported from cabpt-b7fbe6a-template-rendering.patch:1480-1556.
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

		// The metadata secret is named after the infrastructure machine, matching
		// the Metal3DataTemplate metadata secret of CAPM3.
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
	in, err := generate.NewInput(clusterName, "https://localhost", constants.DefaultKubernetesVersion, generate.WithSecretsBundle(bundle), generate.WithEndpointList(endpoints))
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
	if userConfig.K8sAPIServerCAConfig() != nil && userConfig.K8sAPIServerCAConfig().IssuingCA() != nil && len(userConfig.K8sAPIServerCAConfig().IssuingCA().Crt) > 0 && len(userConfig.K8sAPIServerCAConfig().IssuingCA().Key) > 0 {
		if err = r.writeK8sCASecret(ctx, scope, userConfig.K8sAPIServerCAConfig().IssuingCA()); err != nil {
			return retBundle, err
		}
	}

	userConfigStr, err := userConfig.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return retBundle, err
	}

	retBundle.BootstrapData = userConfigStr

	return retBundle, nil
}

// generateConfigs resolves the parts of the generation that touch the API
// server (the secrets bundle, the Kubernetes CA secret, the hostname source and
// the cluster network) and delegates the machine-config generation itself to
// pkg/machineconfig. It returns the generated configuration and the standard
// patches the generator produced.
func (r *TalosConfigReconciler) generateConfigs(ctx context.Context, scope *TalosConfigScope) (string, []string, error) {
	// Allow user to override default kube version.
	// This also handles version being formatted like "vX.Y.Z" instead of without leading 'v'
	// TrimPrefix returns the string unchanged if the prefix isn't present.
	k8sVersion := constants.DefaultKubernetesVersion
	hostname := ""

	if scope.ConfigOwner.IsMachinePool() {
		mp := &capiv1.MachinePool{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(scope.ConfigOwner.Object, mp); err != nil {
			return "", nil, err
		}

		if mp.Spec.Template.Spec.Version != "" {
			k8sVersion = strings.TrimPrefix(mp.Spec.Template.Spec.Version, "v")
		}
	} else {
		machine := &capiv1.Machine{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(scope.ConfigOwner.Object, machine); err != nil {
			return "", nil, err
		}

		if machine.Spec.Version != "" {
			k8sVersion = strings.TrimPrefix(machine.Spec.Version, "v")
		}

		// The hostname is only set for machines, not machine pools: a pool has
		// no single machine identity.
		if scope.Config.Spec.Hostname.Source != "" {
			switch scope.Config.Spec.Hostname.Source {
			case bootstrapv1beta1.HostnameSourceMachineName:
				hostname = scope.ConfigOwner.GetName()
			case bootstrapv1beta1.HostnameSourceInfrastructureName:
				hostname = machine.Spec.InfrastructureRef.Name
			}
		}
	}

	versionContract, err := machineconfig.VersionContract(scope.Config.Spec.TalosVersion)
	if err != nil {
		return "", nil, err
	}

	secretBundle, err := r.getSecretsBundle(ctx, scope, true, versionContract)
	if err != nil {
		return "", nil, err
	}

	// Create the secret with kubernetes certs so a kubeconfig can be generated
	if err = r.writeK8sCASecret(ctx, scope, secretBundle.Certs.K8s); err != nil {
		return "", nil, err
	}

	data, patches, err := machineconfig.Generate(machineconfig.GenerateInput{
		ClusterName:          scope.Cluster.Name,
		ControlPlaneEndpoint: "https://" + net.JoinHostPort(scope.Cluster.Spec.ControlPlaneEndpoint.Host, strconv.Itoa(int(scope.Cluster.Spec.ControlPlaneEndpoint.Port))),
		KubernetesVersion:    k8sVersion,
		TalosVersion:         scope.Config.Spec.TalosVersion,
		SecretsBundle:        secretBundle,
		GenerateType:         scope.Config.Spec.GenerateType,
		Hostname:             hostname,
		DNSDomain:            scope.Cluster.Spec.ClusterNetwork.ServiceDomain,
		PodSubnets:           scope.Cluster.Spec.ClusterNetwork.Pods.CIDRBlocks,
		ServiceSubnets:       scope.Cluster.Spec.ClusterNetwork.Services.CIDRBlocks,
	})
	if err != nil {
		return "", nil, err
	}

	r.Log.Info("patches are", "patches", patches)

	return data, patches, nil
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
