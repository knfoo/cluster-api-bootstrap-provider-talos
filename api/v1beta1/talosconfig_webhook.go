// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package v1beta1

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util/topology"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (r *TalosConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).
		WithValidator(r).
		Complete()
}

//+kubebuilder:webhook:verbs=create;update,path=/validate-bootstrap-cluster-x-k8s-io-v1beta1-talosconfig,mutating=false,failurePolicy=fail,groups=bootstrap.cluster.x-k8s.io,resources=talosconfigs,versions=v1beta1,name=vtalosconfig.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Validator[*TalosConfig] = &TalosConfig{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (r *TalosConfig) ValidateCreate(ctx context.Context, obj *TalosConfig) (admission.Warnings, error) {
	r = obj
	return nil, r.validate()
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (r *TalosConfig) ValidateUpdate(ctx context.Context, oldObj *TalosConfig, newObj *TalosConfig) (admission.Warnings, error) {
	old := oldObj
	r = newObj

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Skip the immutability check if the request is a dry-run issued by the topology controller (#257)
	if topology.IsDryRunRequest(req, r) || cmp.Equal(r.Spec, old.Spec) {
		return nil, r.validate()
	}

	// The relaxed immutability check is only in effect while the InPlaceUpdates feature gate is
	// enabled (alpha, off by default). Without the gate no controller performs in-place updates, so
	// the whole spec stays immutable, exactly as before. Cluster API gates its in-place update code
	// the same way: KCP's canUpdateMachine returns false when the gate is off
	// (inplace_canupdatemachine.go:52).
	if !feature.Gates.Enabled(feature.InPlaceUpdates) {
		return nil, apierrors.NewBadRequest("TalosConfig.Spec is immutable")
	}

	// In-place updates: only the machine-config content (data, configPatches, strategicPatches)
	// may change. Everything else in the spec is immutable and needs a rollout.
	// The classification follows the CAPI bootstrap contract and KCP's CanUpdateMachine, which
	// patches only the bootstrap config fields that do not decide how the machine is generated
	// (inplace_canupdatemachine.go:44-87).
	if err := r.validateImmutableFields(old); err != nil {
		return nil, err
	}

	return nil, r.validate()
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (r *TalosConfig) ValidateDelete(ctx context.Context, obj *TalosConfig) (admission.Warnings, error) {
	return nil, nil
}

// validateImmutableFields rejects a spec change that touches an immutable field.
// Mutable fields (data, configPatches, strategicPatches) are allowed to change: the update
// extension reconciles them on the running machine.
func (r *TalosConfig) validateImmutableFields(old *TalosConfig) error {
	var allErrs field.ErrorList
	spec := field.NewPath("spec")

	if r.Spec.GenerateType != old.Spec.GenerateType {
		allErrs = append(allErrs, field.Invalid(spec.Child("generateType"), r.Spec.GenerateType,
			"generateType is immutable: changing it requires a machine rollout"))
	}

	if r.Spec.TalosVersion != old.Spec.TalosVersion {
		allErrs = append(allErrs, field.Invalid(spec.Child("talosVersion"), r.Spec.TalosVersion,
			"talosVersion is immutable: changing it requires a machine rollout"))
	}

	if r.Spec.Hostname.Source != old.Spec.Hostname.Source {
		allErrs = append(allErrs, field.Invalid(spec.Child("hostname").Child("source"), r.Spec.Hostname.Source,
			"hostname is immutable: changing it requires a machine rollout"))
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: GroupVersion.Group, Kind: "TalosConfig"},
		r.Name, allErrs)
}

func (r *TalosConfig) validate() error {
	var allErrs field.ErrorList

	switch r.Spec.Hostname.Source {
	case "":
	case HostnameSourceMachineName:
	case HostnameSourceInfrastructureName:
	default:
		allErrs = append(allErrs,
			field.Invalid(field.NewPath("spec").Child("hostname").Child("source"), r.Spec.Hostname.Source,
				fmt.Sprintf("valid values are: %q", []HostnameSource{HostnameSourceMachineName, HostnameSourceInfrastructureName}),
			),
		)
	}

	// metadataFrom can point at only one metadata source. The same rule is enforced
	// when the reconciler resolves the metadata (controllers/talosconfig_controller.go),
	// so rejecting it here fails bad specs at admission instead of at reconcile.
	if r.Spec.MetadataFrom != nil && r.Spec.MetadataFrom.SecretRef != nil && r.Spec.MetadataFrom.InfrastructureMachine != nil {
		allErrs = append(allErrs,
			field.Invalid(field.NewPath("spec").Child("metadataFrom"), r.Spec.MetadataFrom,
				"metadataFrom must specify either secretRef or infrastructureMachine, not both"),
		)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: GroupVersion.Group, Kind: "TalosConfig"},
		r.Name, allErrs)
}
