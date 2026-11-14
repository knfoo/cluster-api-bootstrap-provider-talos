// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"testing"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/stretchr/testify/assert"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const minimalConfig = `version: v1alpha1
machine:
  type: worker
  install:
    image: ghcr.io/siderolabs/installer:latest
`

func TestApplyPatchesConfigPatches(t *testing.T) {
	out, err := ApplyPatches(minimalConfig, PatchInput{
		ConfigPatches: []bootstrapv1beta1.ConfigPatches{
			{
				Op:   "replace",
				Path: "/machine/install/image",
				Value: apiextensions.JSON{
					Raw: []byte(`"ghcr.io/siderolabs/installer:v1.14.0"`),
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, out, "ghcr.io/siderolabs/installer:v1.14.0")
	assert.NotContains(t, out, "installer:latest")

	if _, err := configloader.NewFromBytes([]byte(out)); err != nil {
		t.Fatalf("patched config does not parse: %v", err)
	}
}

func TestApplyPatchesStrategic(t *testing.T) {
	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  install:\n    image: ghcr.io/siderolabs/installer:v1.13.0\n"},
		StdPatches:       []string{"cluster:\n  network:\n    podSubnets:\n      - 10.244.0.0/16\n"},
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, out, "ghcr.io/siderolabs/installer:v1.13.0")
	assert.Contains(t, out, "10.244.0.0/16")
}

func TestApplyPatchesTemplateWithMetadata(t *testing.T) {
	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  install:\n    image: {{ get \"image\" }}\n"},
		Metadata:         map[string]string{"image": "ghcr.io/siderolabs/installer:v1.14.0"},
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, out, "ghcr.io/siderolabs/installer:v1.14.0")
}

func TestApplyPatchesTemplateVariablesWin(t *testing.T) {
	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches:  []string{"machine:\n  install:\n    image: {{ get \"image\" }}\n"},
		Metadata:          map[string]string{"image": "ghcr.io/siderolabs/installer:from-metadata"},
		TemplateVariables: map[string]string{"image": "ghcr.io/siderolabs/installer:from-variables"},
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, out, "from-variables")
	assert.NotContains(t, out, "from-metadata")
}

func TestApplyPatchesMetadataPendingSkipsBraces(t *testing.T) {
	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches:  []string{"machine:\n  install:\n    image: {{ get \"image\" }}\n"},
		TemplateVariables: map[string]string{"image": "ghcr.io/siderolabs/installer:v1.14.0"},
		MetadataPending:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.NotContains(t, out, "{{")
	assert.NotContains(t, out, "ghcr.io/siderolabs/installer:v1.14.0")
}

func TestApplyPatchesBraceWithoutContextFails(t *testing.T) {
	_, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  install:\n    image: {{ get \"image\" }}\n"},
	})
	assert.Error(t, err)
}

func TestApplyPatchesConfigPatchesOnGeneratedConfig(t *testing.T) {
	base, _, err := Generate(GenerateInput{
		ClusterName:          "test-cluster",
		ControlPlaneEndpoint: "https://cp.example.com:6443",
		SecretsBundle:        testBundle(t),
		GenerateType:         "worker",
		TalosVersion:         "v1.7",
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := ApplyPatches(base, PatchInput{
		ConfigPatches: []bootstrapv1beta1.ConfigPatches{
			{
				Op:    "replace",
				Path:  "/machine/install/image",
				Value: apiextensions.JSON{Raw: []byte(`"ghcr.io/siderolabs/installer:v1.7.0"`)},
			},
		},
	})
	if err != nil {
		t.Fatalf("json6902 on generated config: %v", err)
	}

	assert.Contains(t, out, "ghcr.io/siderolabs/installer:v1.7.0")
}
