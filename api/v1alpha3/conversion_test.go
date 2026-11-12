// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package v1alpha3

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeserializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/utils/ptr"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/randfill"

	bsv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
)

func TestFuzzyConversion(t *testing.T) {
	t.Run("for TalosConfig", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:         &bsv1beta1.TalosConfig{},
		Spoke:       &TalosConfig{},
		FuzzerFuncs: []fuzzer.FuzzerFuncs{talosConfigFuzzFuncs},
	}))

	t.Run("for TalosConfigTemplate", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:         &bsv1beta1.TalosConfigTemplate{},
		Spoke:       &TalosConfigTemplate{},
		FuzzerFuncs: []fuzzer.FuzzerFuncs{talosConfigTemplateFuzzFuncs},
	}))
}

func talosConfigFuzzFuncs(_ runtimeserializer.CodecFactory) []any {
	return []any{
		hubConfigPatches,
		spokeConfigPatches,
		hubTalosConfigStatus,
		spokeTalosConfigStatus,
	}
}

func talosConfigTemplateFuzzFuncs(_ runtimeserializer.CodecFactory) []any {
	return []any{
		hubConfigPatches,
		spokeConfigPatches,
	}
}

// Patch values are raw JSON, so they have to be valid JSON to survive a round trip.
func fuzzJSON(c randfill.Continue) apiextensions.JSON {
	return apiextensions.JSON{Raw: []byte(strconv.Quote(c.String(0)))}
}

func hubConfigPatches(in *bsv1beta1.ConfigPatches, c randfill.Continue) {
	c.FillNoCustom(in)

	in.Value = fuzzJSON(c)
}

func spokeConfigPatches(in *ConfigPatches, c randfill.Continue) {
	c.FillNoCustom(in)

	in.Value = fuzzJSON(c)
}

func hubTalosConfigStatus(in *bsv1beta1.TalosConfigStatus, c randfill.Continue) {
	c.FillNoCustom(in)

	// Up-conversion always creates the deprecated v1beta1 status struct.
	if in.Deprecated == nil {
		in.Deprecated = &bsv1beta1.TalosConfigDeprecatedStatus{}
	}

	if in.Deprecated.V1Beta1 == nil {
		in.Deprecated.V1Beta1 = &bsv1beta1.TalosConfigV1Beta1DeprecatedStatus{}
	}
}

func spokeTalosConfigStatus(in *TalosConfigStatus, c randfill.Continue) {
	c.FillNoCustom(in)

	// TalosConfig has no equivalent in the hub version and is dropped on up-conversion.
	in.TalosConfig = "" //nolint:staticcheck // deprecated, kept for conversion only

	// DataSecretName is a string in the hub version, so an empty value is down-converted to nil.
	if ptr.Deref(in.DataSecretName, "") == "" {
		in.DataSecretName = nil
	}

	// An empty v1beta2 status is dropped on up-conversion.
	if in.V1Beta2 != nil && len(in.V1Beta2.Conditions) == 0 {
		in.V1Beta2 = nil
	}
}

// The template rendering fields (metadataFrom, templateVariables, dataSecretKey and
// status templatePatchesPending) are structurally identical in v1alpha3 and v1beta1, so
// they are converted mechanically and survive a round trip unchanged.
func TestTalosConfigConversionRoundTripTemplateFields(t *testing.T) {
	cases := map[string]*MetadataFrom{
		"secretRef": {
			SecretRef: &MetadataSecretRef{Name: "node-metadata", Key: "metaData"},
		},
		"infrastructureMachine": {
			InfrastructureMachine: &InfrastructureMachineMetadata{
				SecretNameSuffix: "-metadata",
				DataKey:          "metaData",
			},
		},
	}

	for name, metadataFrom := range cases {
		spoke := &TalosConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "talosconfig"},
			Spec: TalosConfigSpec{
				GenerateType:     "controlplane",
				TalosVersion:     "v1.14",
				StrategicPatches: []string{`- network: hostname: {{ get "hostname" }}`},
				MetadataFrom:     metadataFrom,
				DataSecretKey:    "userData",
				TemplateVariables: map[string]string{
					"disk":    "nvme0",
					"cluster": "prod",
				},
			},
			Status: TalosConfigStatus{
				TemplatePatchesPending: ptr.To(true),
			},
		}

		hub := &bsv1beta1.TalosConfig{}
		if err := Convert_v1alpha3_TalosConfig_To_v1beta1_TalosConfig(spoke, hub, nil); err != nil {
			t.Fatalf("failed to convert v1alpha3 to v1beta1 for %s: %v", name, err)
		}

		assert.Equal(t, metadataFrom.SecretRef == nil, hub.Spec.MetadataFrom.SecretRef == nil, "metadataFrom.secretRef lost on up-conversion for %s", name)
		if metadataFrom.SecretRef != nil {
			assert.Equal(t, metadataFrom.SecretRef.Name, hub.Spec.MetadataFrom.SecretRef.Name, "metadataFrom.secretRef.name lost on up-conversion for %s", name)
			assert.Equal(t, metadataFrom.SecretRef.Key, hub.Spec.MetadataFrom.SecretRef.Key, "metadataFrom.secretRef.key lost on up-conversion for %s", name)
		}
		assert.Equal(t, metadataFrom.InfrastructureMachine == nil, hub.Spec.MetadataFrom.InfrastructureMachine == nil, "metadataFrom.infrastructureMachine lost on up-conversion for %s", name)
		if metadataFrom.InfrastructureMachine != nil {
			assert.Equal(t, metadataFrom.InfrastructureMachine.SecretNameSuffix, hub.Spec.MetadataFrom.InfrastructureMachine.SecretNameSuffix, "metadataFrom.infrastructureMachine.secretNameSuffix lost on up-conversion for %s", name)
			assert.Equal(t, metadataFrom.InfrastructureMachine.DataKey, hub.Spec.MetadataFrom.InfrastructureMachine.DataKey, "metadataFrom.infrastructureMachine.dataKey lost on up-conversion for %s", name)
		}
		assert.Equal(t, spoke.Spec.DataSecretKey, hub.Spec.DataSecretKey, "dataSecretKey lost on up-conversion for %s", name)
		assert.Equal(t, spoke.Spec.TemplateVariables, hub.Spec.TemplateVariables, "templateVariables lost on up-conversion for %s", name)
		assert.Equal(t, spoke.Status.TemplatePatchesPending, hub.Status.TemplatePatchesPending, "templatePatchesPending lost on up-conversion for %s", name)

		back := &TalosConfig{}
		if err := Convert_v1beta1_TalosConfig_To_v1alpha3_TalosConfig(hub, back, nil); err != nil {
			t.Fatalf("failed to convert v1beta1 to v1alpha3 for %s: %v", name, err)
		}

		assert.Equal(t, spoke.Spec.MetadataFrom, back.Spec.MetadataFrom, "metadataFrom lost on down-conversion for %s", name)
		assert.Equal(t, spoke.Spec.DataSecretKey, back.Spec.DataSecretKey, "dataSecretKey lost on down-conversion for %s", name)
		assert.Equal(t, spoke.Spec.TemplateVariables, back.Spec.TemplateVariables, "templateVariables lost on down-conversion for %s", name)
		assert.Equal(t, spoke.Status.TemplatePatchesPending, back.Status.TemplatePatchesPending, "templatePatchesPending lost on down-conversion for %s", name)
	}
}

func TestTalosConfigTemplateConversionRoundTripTemplateFields(t *testing.T) {
	template := &TalosConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "talosconfigtemplate"},
		Spec: TalosConfigTemplateSpec{
			Template: TalosConfigTemplateResource{
				Spec: TalosConfigSpec{
					GenerateType:      "worker",
					MetadataFrom:      &MetadataFrom{SecretRef: &MetadataSecretRef{Name: "node-metadata"}},
					DataSecretKey:     "userData",
					TemplateVariables: map[string]string{"disk": "nvme0"},
				},
			},
		},
	}

	hub := &bsv1beta1.TalosConfigTemplate{}
	if err := Convert_v1alpha3_TalosConfigTemplate_To_v1beta1_TalosConfigTemplate(template, hub, nil); err != nil {
		t.Fatalf("failed to convert the TalosConfigTemplate to v1beta1: %v", err)
	}

	assert.Equal(t, template.Spec.Template.Spec.MetadataFrom.SecretRef.Name, hub.Spec.Template.Spec.MetadataFrom.SecretRef.Name, "metadataFrom.secretRef lost on the TalosConfigTemplate up-conversion")
	assert.Equal(t, template.Spec.Template.Spec.MetadataFrom.SecretRef.Key, hub.Spec.Template.Spec.MetadataFrom.SecretRef.Key, "metadataFrom.secretRef.key lost on the TalosConfigTemplate up-conversion")
	assert.Equal(t, template.Spec.Template.Spec.DataSecretKey, hub.Spec.Template.Spec.DataSecretKey, "dataSecretKey lost on the TalosConfigTemplate up-conversion")
	assert.Equal(t, template.Spec.Template.Spec.TemplateVariables, hub.Spec.Template.Spec.TemplateVariables, "templateVariables lost on the TalosConfigTemplate up-conversion")

	back := &TalosConfigTemplate{}
	if err := Convert_v1beta1_TalosConfigTemplate_To_v1alpha3_TalosConfigTemplate(hub, back, nil); err != nil {
		t.Fatalf("failed to convert the TalosConfigTemplate back to v1alpha3: %v", err)
	}

	assert.Equal(t, template.Spec.Template.Spec.MetadataFrom, back.Spec.Template.Spec.MetadataFrom, "metadataFrom lost on the TalosConfigTemplate down-conversion")
	assert.Equal(t, template.Spec.Template.Spec.TemplateVariables, back.Spec.Template.Spec.TemplateVariables, "templateVariables lost on the TalosConfigTemplate down-conversion")
}
