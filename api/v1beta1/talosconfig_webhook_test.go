// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package v1beta1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func updateContext() context.Context {
	return admission.NewContextWithRequest(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
		},
	})
}

func patchValue(value string) apiextensions.JSON {
	return apiextensions.JSON{Raw: []byte(`"` + value + `"`)}
}

func baseSpec() TalosConfigSpec {
	return TalosConfigSpec{
		GenerateType:     "controlplane",
		TalosVersion:     "v1.14",
		Hostname:         HostnameSpec{Source: HostnameSourceMachineName},
		ConfigPatches:    []ConfigPatches{{Op: "add", Path: "/system/hostname", Value: patchValue("old")}},
		StrategicPatches: []string{"- system: hostname: old"},
	}
}

func talosConfigWith(spec TalosConfigSpec) *TalosConfig {
	return &TalosConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "talosconfig"},
		Spec:       spec,
	}
}

func TestTalosConfigValidateUpdateAllowsMutableFields(t *testing.T) {
	// The relaxed immutability check is only active while the InPlaceUpdates feature gate is on.
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.InPlaceUpdates, true)

	old := talosConfigWith(baseSpec())

	cases := map[string]TalosConfigSpec{
		"data": func() TalosConfigSpec {
			spec := baseSpec()
			spec.Data = "#!talos\napiVersion: v1\nkind: Machine\n"
			return spec
		}(),
		"configPatches": func() TalosConfigSpec {
			spec := baseSpec()
			spec.ConfigPatches = []ConfigPatches{{Op: "add", Path: "/system/hostname", Value: patchValue("new")}}
			return spec
		}(),
		"strategicPatches": func() TalosConfigSpec {
			spec := baseSpec()
			spec.StrategicPatches = []string{"- system: hostname: new"}
			return spec
		}(),
		"no change": baseSpec(),
	}

	for name, spec := range cases {
		newObj := talosConfigWith(spec)

		_, err := newObj.ValidateUpdate(updateContext(), old, newObj)

		assert.NoError(t, err, "expected a change to %s to be accepted", name)
	}
}

func TestTalosConfigValidateUpdateRejectsImmutableFields(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.InPlaceUpdates, true)

	old := talosConfigWith(baseSpec())

	cases := map[string]TalosConfigSpec{
		"generateType": func() TalosConfigSpec {
			spec := baseSpec()
			spec.GenerateType = "worker"
			return spec
		}(),
		"talosVersion": func() TalosConfigSpec {
			spec := baseSpec()
			spec.TalosVersion = "v1.15"
			return spec
		}(),
		"hostname": func() TalosConfigSpec {
			spec := baseSpec()
			spec.Hostname = HostnameSpec{Source: HostnameSourceInfrastructureName}
			return spec
		}(),
		"mutable and immutable together": func() TalosConfigSpec {
			spec := baseSpec()
			spec.ConfigPatches = nil
			spec.GenerateType = "worker"
			return spec
		}(),
	}

	for name, spec := range cases {
		newObj := talosConfigWith(spec)

		_, err := newObj.ValidateUpdate(updateContext(), old, newObj)

		assert.Error(t, err, "expected a change to %s to be rejected", name)
		assert.Contains(t, err.Error(), "immutable", "expected the immutable guard to reject a change to %s", name)
		assert.True(t, apierrors.IsInvalid(err), "expected an invalid-object error for a change to %s", name)
	}
}

// With the InPlaceUpdates feature gate off (the default), every spec change is rejected, exactly as
// before in-place support: the mutable-field relaxation only applies when the gate is enabled.
func TestTalosConfigValidateUpdateRejectsAnySpecChangeWhenGateDisabled(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.InPlaceUpdates, false)

	old := talosConfigWith(baseSpec())

	cases := map[string]TalosConfigSpec{
		"data": func() TalosConfigSpec {
			spec := baseSpec()
			spec.Data = "#!talos\napiVersion: v1\nkind: Machine\n"
			return spec
		}(),
		"configPatches": func() TalosConfigSpec {
			spec := baseSpec()
			spec.ConfigPatches = []ConfigPatches{{Op: "add", Path: "/system/hostname", Value: patchValue("new")}}
			return spec
		}(),
		"strategicPatches": func() TalosConfigSpec {
			spec := baseSpec()
			spec.StrategicPatches = []string{"- system: hostname: new"}
			return spec
		}(),
		"generateType": func() TalosConfigSpec {
			spec := baseSpec()
			spec.GenerateType = "worker"
			return spec
		}(),
		"talosVersion": func() TalosConfigSpec {
			spec := baseSpec()
			spec.TalosVersion = "v1.15"
			return spec
		}(),
		"hostname": func() TalosConfigSpec {
			spec := baseSpec()
			spec.Hostname = HostnameSpec{Source: HostnameSourceInfrastructureName}
			return spec
		}(),
		"no change": baseSpec(),
	}

	for name, spec := range cases {
		newObj := talosConfigWith(spec)

		_, err := newObj.ValidateUpdate(updateContext(), old, newObj)

		if name == "no change" {
			assert.NoError(t, err, "expected no spec change to be accepted with the gate off")
			continue
		}

		assert.Error(t, err, "expected a change to %s to be rejected while the gate is off", name)
		assert.Contains(t, err.Error(), "immutable", "expected the immutable guard to reject a change to %s while the gate is off", name)
		assert.True(t, apierrors.IsBadRequest(err), "expected a bad request error for a change to %s while the gate is off", name)
	}
}

// metadataFrom describes where the template metadata comes from, and it can point at
// only one source; both sources set is rejected on create and update instead of at
// reconcile time.
func TestTalosConfigValidateRejectsMetadataFromWithBothSources(t *testing.T) {
	spec := baseSpec()
	spec.MetadataFrom = &MetadataFrom{
		SecretRef: &MetadataSecretRef{Name: "node-metadata"},
		InfrastructureMachine: &InfrastructureMachineMetadata{
			SecretNameSuffix: "-metadata",
		},
	}

	obj := talosConfigWith(spec)

	_, err := obj.ValidateCreate(context.Background(), obj)
	assert.Error(t, err, "expected metadataFrom with both secretRef and infrastructureMachine to be rejected")
	assert.Contains(t, err.Error(), "metadataFrom must specify either secretRef or infrastructureMachine", "expected the metadataFrom guard to reject both sources")
	assert.True(t, apierrors.IsInvalid(err), "expected an invalid-object error for metadataFrom with both sources")

	_, err = obj.ValidateUpdate(updateContext(), obj, obj)
	assert.Error(t, err, "expected metadataFrom with both secretRef and infrastructureMachine to be rejected on update")
	assert.Contains(t, err.Error(), "metadataFrom must specify either secretRef or infrastructureMachine", "expected the metadataFrom guard to reject both sources")
}

func TestTalosConfigValidateAcceptsMetadataFromWithOneSource(t *testing.T) {
	cases := map[string]*MetadataFrom{
		"secretRef":               {SecretRef: &MetadataSecretRef{Name: "node-metadata", Key: "metaData"}},
		"infrastructureMachine":   {InfrastructureMachine: &InfrastructureMachineMetadata{SecretNameSuffix: "-metadata", DataKey: "metaData"}},
		"no metadataFrom (plain)": nil,
	}

	for name, metadataFrom := range cases {
		spec := baseSpec()
		spec.MetadataFrom = metadataFrom
		spec.TemplateVariables = map[string]string{"disk": "nvme0"}
		spec.DataSecretKey = "userData"

		obj := talosConfigWith(spec)

		_, err := obj.ValidateCreate(context.Background(), obj)
		assert.NoError(t, err, "expected metadataFrom with one source (%s) to be accepted", name)
	}
}

// Template rendering fields are mutable: with the InPlaceUpdates gate enabled a change to
// metadataFrom, templateVariables or dataSecretKey is accepted, like a change to the
// patches themselves.
func TestTalosConfigValidateUpdateAllowsTemplateRenderingFields(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.InPlaceUpdates, true)

	old := talosConfigWith(baseSpec())

	cases := map[string]TalosConfigSpec{
		"metadataFrom": func() TalosConfigSpec {
			spec := baseSpec()
			spec.MetadataFrom = &MetadataFrom{SecretRef: &MetadataSecretRef{Name: "node-metadata"}}
			return spec
		}(),
		"templateVariables": func() TalosConfigSpec {
			spec := baseSpec()
			spec.TemplateVariables = map[string]string{"disk": "nvme0"}
			return spec
		}(),
		"dataSecretKey": func() TalosConfigSpec {
			spec := baseSpec()
			spec.DataSecretKey = "userData"
			return spec
		}(),
		"strategicPatches with templates": func() TalosConfigSpec {
			spec := baseSpec()
			spec.StrategicPatches = []string{`- network: hostname: {{ get "hostname" }}`}
			return spec
		}(),
	}

	for name, spec := range cases {
		newObj := talosConfigWith(spec)

		_, err := newObj.ValidateUpdate(updateContext(), old, newObj)

		assert.NoError(t, err, "expected a change to %s to be accepted", name)
	}
}

// Without the gate the whole spec stays immutable, so the rendering fields are rejected too.
func TestTalosConfigValidateUpdateRejectsTemplateRenderingFieldsWhenGateDisabled(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.InPlaceUpdates, false)

	old := talosConfigWith(baseSpec())

	cases := map[string]TalosConfigSpec{
		"metadataFrom": func() TalosConfigSpec {
			spec := baseSpec()
			spec.MetadataFrom = &MetadataFrom{SecretRef: &MetadataSecretRef{Name: "node-metadata"}}
			return spec
		}(),
		"templateVariables": func() TalosConfigSpec {
			spec := baseSpec()
			spec.TemplateVariables = map[string]string{"disk": "nvme0"}
			return spec
		}(),
		"dataSecretKey": func() TalosConfigSpec {
			spec := baseSpec()
			spec.DataSecretKey = "userData"
			return spec
		}(),
	}

	for name, spec := range cases {
		newObj := talosConfigWith(spec)

		_, err := newObj.ValidateUpdate(updateContext(), old, newObj)

		assert.Error(t, err, "expected a change to %s to be rejected while the gate is off", name)
		assert.Contains(t, err.Error(), "immutable", "expected the immutable guard to reject a change to %s while the gate is off", name)
		assert.True(t, apierrors.IsBadRequest(err), "expected a bad request error for a change to %s while the gate is off", name)
	}
}
