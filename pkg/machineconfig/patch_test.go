// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"testing"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	data, err := TemplateData([]byte("image: ghcr.io/siderolabs/installer:v1.14.0\n"), nil, "", "", nil)
	require.NoError(t, err)

	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  install:\n    image: {{ .image }}\n"},
		TemplateData:     data,
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, out, "ghcr.io/siderolabs/installer:v1.14.0")
}

func TestApplyPatchesTemplateVariableOverridesMetadata(t *testing.T) {
	data, err := TemplateData(
		[]byte("image: ghcr.io/siderolabs/installer:from-metadata\n"),
		nil,
		"",
		"",
		map[string]string{"image": "ghcr.io/siderolabs/installer:from-variables"},
	)
	require.NoError(t, err)

	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  install:\n    image: {{ .image }}\n"},
		TemplateData:     data,
	})
	require.NoError(t, err)

	assert.Contains(t, out, "from-variables")
	assert.NotContains(t, out, "from-metadata")
}

func TestApplyPatchesTemplateReadsNestedMetadataAndLists(t *testing.T) {
	data, err := TemplateData(
		[]byte("network:\n  internal:\n    - 10.0.0.0/24\nrack: 2\n"),
		testCluster(),
		"worker-1",
		"worker-infra",
		nil,
	)
	require.NoError(t, err)

	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{
			"machine:\n  network:\n    hostname: {{ .machine.name }}-{{ index .network.internal 0 }}\n",
		},
		StdPatches:   []string{"cluster:\n  network:\n    podSubnets:\n      - {{ index .cluster.clusterNetwork.pods.cidrBlocks 0 }}\n"},
		TemplateData: data,
	})
	require.NoError(t, err)

	assert.Contains(t, out, "hostname: worker-1-10.0.0.0/24")
	assert.Contains(t, out, "10.244.0.0/16")
}

func TestApplyPatchesMissingTemplateKeyFails(t *testing.T) {
	_, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  network:\n    hostname: {{ .hostname }}\n"},
		TemplateData:     map[string]any{"zone": "a"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
	assert.NotContains(t, err.Error(), "<no value>")
}

func TestApplyPatchesMetadataPendingSkipsBraces(t *testing.T) {
	out, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  install:\n    image: {{ .image }}\n"},
		MetadataPending:  true,
	})
	require.NoError(t, err)

	assert.NotContains(t, out, "{{")
	assert.NotContains(t, out, "ghcr.io/siderolabs/installer:v1.14.0")
}

func TestApplyPatchesBraceWithoutContextFails(t *testing.T) {
	_, err := ApplyPatches(minimalConfig, PatchInput{
		StrategicPatches: []string{"machine:\n  install:\n    image: {{ .image }}\n"},
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
