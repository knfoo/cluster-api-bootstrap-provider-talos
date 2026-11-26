// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package templatepatch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessTemplatesEmpty(t *testing.T) {
	out, err := ProcessTemplates(nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, out)

	out, err = ProcessTemplates([]string{}, map[string]any{"hostname": "node-1"})
	assert.NoError(t, err)
	assert.Len(t, out, 0)
}

func TestProcessTemplatesPlainPatchPassesThrough(t *testing.T) {
	patch := "machine:\n  install:\n    image: ghcr.io/siderolabs/installer:v1.14.0"

	out, err := ProcessTemplates([]string{patch}, map[string]any{"hostname": "node-1"})
	assert.NoError(t, err)
	assert.Equal(t, []string{patch}, out)
}

func TestProcessTemplatesNilData(t *testing.T) {
	out, err := ProcessTemplates([]string{"machine:\n  install:\n    image: ghcr.io/siderolabs/installer:v1.14.0"}, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"machine:\n  install:\n    image: ghcr.io/siderolabs/installer:v1.14.0"}, out)

	_, err = ProcessTemplates([]string{`hostname: {{ or .hostname "node-1" }}`}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
}

func TestProcessTemplatesNestedMetadata(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{"b": "leaf"},
		"network": map[string]any{
			"gateway": map[string]any{"address": "10.0.0.1"},
		},
	}

	out, err := ProcessTemplates([]string{
		"address: {{ .a.b }}",
		"gateway: {{ .network.gateway.address }}",
	}, data)
	assert.NoError(t, err)
	assert.Equal(t, []string{"address: leaf", "gateway: 10.0.0.1"}, out)
}

func TestProcessTemplatesListIndex(t *testing.T) {
	data := map[string]any{
		"cluster": map[string]any{
			"clusterNetwork": map[string]any{
				"pods":     map[string]any{"cidrBlocks": []string{"10.244.0.0/16"}},
				"services": map[string]any{"cidrBlocks": []string{"10.96.0.0/12"}},
			},
		},
		"nicNames": []any{"ens2f1np1", "ens3f1np1"},
	}

	out, err := ProcessTemplates([]string{
		"podSubnet: {{ index .cluster.clusterNetwork.pods.cidrBlocks 0 }}",
		"serviceSubnet: {{ index .cluster.clusterNetwork.services.cidrBlocks 0 }}",
		"nic: {{ index .nicNames 1 }}",
		"nics: {{ range .nicNames }}{{ . }},{{ end }}",
	}, data)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"podSubnet: 10.244.0.0/16",
		"serviceSubnet: 10.96.0.0/12",
		"nic: ens3f1np1",
		"nics: ens2f1np1,ens3f1np1,",
	}, out)
}

func TestProcessTemplatesIntegerStaysInteger(t *testing.T) {
	out, err := ProcessTemplates([]string{"rack: {{ .rack }}"}, map[string]any{"rack": 42})
	assert.NoError(t, err)
	assert.Equal(t, []string{"rack: 42"}, out)
}

func TestProcessTemplatesExactKeyCase(t *testing.T) {
	data := map[string]any{"HostName": "node-1"}

	out, err := ProcessTemplates([]string{"hostname: {{ .HostName }}"}, data)
	assert.NoError(t, err)
	assert.Equal(t, []string{"hostname: node-1"}, out)

	_, err = ProcessTemplates([]string{"hostname: {{ .hostname }}"}, data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
}

func TestProcessTemplatesMissingKeyFailsWithItsName(t *testing.T) {
	_, err := ProcessTemplates([]string{"hostname: {{ .internalIP }}"}, map[string]any{"hostname": "node-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "internalIP")
	assert.NotContains(t, err.Error(), "<no value>")
}

func TestProcessTemplatesFallbackWithOr(t *testing.T) {
	data := map[string]any{"zone": "", "nilValue": nil, "region": "eu-west"}

	out, err := ProcessTemplates([]string{
		`zone: {{ or .zone "default" }}`,
		`nilValue: {{ or .nilValue "fallback" }}`,
		`region: {{ or .region "us-east-1" }}`,
		"enabled: {{ not .zone }}",
		`same: {{ eq .region "eu-west" }}`,
		`host: {{ printf "%s.example.com" .region }}`,
	}, data)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"zone: default",
		"nilValue: fallback",
		"region: eu-west",
		"enabled: true",
		"same: true",
		"host: eu-west.example.com",
	}, out)
}

func TestProcessTemplatesNoAddedFunctions(t *testing.T) {
	for _, patch := range []string{
		`hostname: {{ get "hostname" }}`,
		`hostname: {{ has "hostname" }}`,
		`hostname: {{ default "node-1" .hostname }}`,
		`hostname: {{ required "hostname is mandatory" .hostname }}`,
	} {
		_, err := ProcessTemplates([]string{patch}, map[string]any{"hostname": "node-1"})
		assert.Error(t, err, "%q must not parse: the engine adds no functions", patch)
		assert.Contains(t, err.Error(), "failed to parse template patch")
	}
}

func TestProcessTemplatesInvalidSyntaxFailsWithSnippet(t *testing.T) {
	_, err := ProcessTemplates([]string{"machine: {{ .hostname"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse template patch 0")
}

func TestProcessTemplatesRenderErrorNamesThePatch(t *testing.T) {
	_, err := ProcessTemplates([]string{"machine:\n  network:\n    hostname: {{ .missing }}"}, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute template patch 0")
	assert.Contains(t, err.Error(), "machine:")
}

func TestProcessTemplatesBlankResultIsDropped(t *testing.T) {
	out, err := ProcessTemplates([]string{"", "   ", "hostname: {{ .hostname }}"}, map[string]any{"hostname": "node-1"})
	assert.NoError(t, err)
	assert.Equal(t, []string{"hostname: node-1"}, out)
}

func TestProcessTemplatesCAPM3Metadata(t *testing.T) {
	// The metadata a CAPM3 Metal3DataTemplate writes, plus the cluster and machine
	// context the bootstrap provider adds to it.
	data := map[string]any{
		"hostname":   "metal3-worker-1",
		"providerID": "metal3://b3a4c1d2-0000-0000-0000-000000000001",
		"ip":         "192.168.122.10",
		"rack":       3,
		"nicNames":   []any{"ens2f1np1"},
		"network": map[string]any{
			"internal": []any{"10.0.0.0/24"},
			"external": []any{"192.168.122.0/24"},
		},
		"cluster": map[string]any{
			"name": "metal3-cluster",
			"clusterNetwork": map[string]any{
				"pods":          map[string]any{"cidrBlocks": []string{"10.244.0.0/16"}},
				"serviceDomain": "cluster.local",
			},
		},
		"machine": map[string]any{
			"name":              "metal3-worker-1-abcde",
			"infrastructureRef": map[string]any{"name": "metal3-worker-1"},
		},
		"installerImage": "ghcr.io/siderolabs/installer:v1.14.0",
	}

	out, err := ProcessTemplates([]string{
		"machine:\n  network:\n    hostname: {{ .hostname }}",
		"node:\n  labels:\n    rack: {{ .rack }}\n    providerID: {{ .providerID }}",
		"machine:\n  install:\n    image: {{ or .installerImage \"ghcr.io/siderolabs/installer:latest\" }}",
		"network:\n  nicNames: [{{ index .nicNames 0 }}]\n  external: {{ index .network.external 0 }}",
		"cluster:\n  name: {{ .cluster.name }}\n  podSubnets:\n    - {{ index .cluster.clusterNetwork.pods.cidrBlocks 0 }}",
		"machine:\n  hostname: {{ .machine.name }}-{{ .machine.infrastructureRef.name }}",
		"cluster:\n  domain: {{ .cluster.clusterNetwork.serviceDomain }}",
	}, data)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"machine:\n  network:\n    hostname: metal3-worker-1",
		"node:\n  labels:\n    rack: 3\n    providerID: metal3://b3a4c1d2-0000-0000-0000-000000000001",
		"machine:\n  install:\n    image: ghcr.io/siderolabs/installer:v1.14.0",
		"network:\n  nicNames: [ens2f1np1]\n  external: 192.168.122.0/24",
		"cluster:\n  name: metal3-cluster\n  podSubnets:\n    - 10.244.0.0/16",
		"machine:\n  hostname: metal3-worker-1-abcde-metal3-worker-1",
		"cluster:\n  domain: cluster.local",
	}, out)
}

func TestTemplateSnippetTruncatesLongFirstLine(t *testing.T) {
	long := strings.Repeat("a", 100)

	assert.Equal(t, strings.Repeat("a", 60)+"...", templateSnippet(long))
	assert.Equal(t, "first line", templateSnippet("first line\nsecond line"))
}
