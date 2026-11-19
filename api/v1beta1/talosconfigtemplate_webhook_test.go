// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package v1beta1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/ptr"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func talosConfigTemplateWith(spec TalosConfigSpec) *TalosConfigTemplate {
	return &TalosConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "talosconfigtemplate"},
		Spec: TalosConfigTemplateSpec{
			Template: TalosConfigTemplateResource{Spec: spec},
		},
	}
}

func templateUpdateContext(dryRun bool) context.Context {
	return admission.NewContextWithRequest(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			DryRun:    ptr.To(dryRun),
		},
	})
}

// A TalosConfigTemplate is a template: Cluster API creates a new one per change and moves the worker
// Machines of a MachineDeployment to the MachineSet that references it
// (core/reconcilers/machinedeployment/machinedeployment_rollout_rollingupdate.go:399-468). Editing the
// spec of an existing template would make the in-place decision of the rollout planner compare a
// template against itself, so it stays rejected.
func TestTalosConfigTemplateValidateUpdateRejectsSpecChangeOfAnExistingTemplate(t *testing.T) {
	old := talosConfigTemplateWith(workerSpec())
	updated := talosConfigTemplateWith(func() TalosConfigSpec {
		spec := workerSpec()
		spec.StrategicPatches = []string{"- system: hostname: from-the-new-template"}

		return spec
	}())

	_, err := updated.ValidateUpdate(templateUpdateContext(false), old, updated)

	assert.Error(t, err, "expected the spec of an existing TalosConfigTemplate to be immutable")
	assert.Contains(t, err.Error(), "immutable", "expected the immutable guard to reject the template edit")
	assert.True(t, apierrors.IsBadRequest(err), "expected a bad request error for the template edit")
}

// The template stays immutable even while worker Machines are being updated in place: an in-place
// update never edits a template, it references a different one.
func TestTalosConfigTemplateValidateUpdateRejectsSpecChangeWithInPlaceUpdatesEnabled(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.InPlaceUpdates, true)

	old := talosConfigTemplateWith(workerSpec())
	updated := talosConfigTemplateWith(func() TalosConfigSpec {
		spec := workerSpec()
		spec.TalosVersion = "v1.15"

		return spec
	}())

	_, err := updated.ValidateUpdate(templateUpdateContext(false), old, updated)

	assert.Error(t, err, "expected a TalosConfigTemplate spec change to be rejected during in-place updates")
	assert.Contains(t, err.Error(), "immutable", "expected the immutable guard to reject the template edit")
}

func TestTalosConfigTemplateValidateUpdateAcceptsAnUnchangedTemplate(t *testing.T) {
	old := talosConfigTemplateWith(workerSpec())
	updated := talosConfigTemplateWith(workerSpec())

	_, err := updated.ValidateUpdate(templateUpdateContext(false), old, updated)

	assert.NoError(t, err, "expected an unchanged TalosConfigTemplate to be accepted")
}

// The topology controller dry-run-applies objects when it computes the MachineSet of a new template
// (core/reconcilers/machinedeployment/machinedeployment_canupdatemachineset.go:127-165), so the
// immutability check must not fail that request.
func TestTalosConfigTemplateValidateUpdateAllowsTopologyDryRun(t *testing.T) {
	old := talosConfigTemplateWith(workerSpec())
	updated := talosConfigTemplateWith(func() TalosConfigSpec {
		spec := workerSpec()
		spec.StrategicPatches = []string{"- system: hostname: from-the-new-template"}

		return spec
	}())
	updated.Annotations = map[string]string{capiv1.TopologyDryRunAnnotation: "true"}

	_, err := updated.ValidateUpdate(templateUpdateContext(true), old, updated)

	assert.NoError(t, err, "expected the topology dry run of a TalosConfigTemplate to be accepted")
}
