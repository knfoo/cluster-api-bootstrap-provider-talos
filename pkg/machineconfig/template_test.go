// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func testCluster() *capiv1.Cluster {
	return &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "metal3-cluster", Namespace: "default"},
		Spec: capiv1.ClusterSpec{
			ControlPlaneEndpoint: capiv1.APIEndpoint{Host: "192.168.122.100", Port: 6443},
			ClusterNetwork: capiv1.ClusterNetwork{
				Pods:          capiv1.NetworkRanges{CIDRBlocks: []string{"10.244.0.0/16"}},
				Services:      capiv1.NetworkRanges{CIDRBlocks: []string{"10.96.0.0/12"}},
				ServiceDomain: "cluster.local",
			},
			InfrastructureRef: capiv1.ContractVersionedObjectReference{
				Kind:     "Metal3Cluster",
				Name:     "metal3-cluster",
				APIGroup: "infrastructure.cluster.x-k8s.io",
			},
		},
	}
}

// The metadata of a Metal3DataTemplate stays nested, and its integers stay integers.
func TestTemplateDataKeepsMetadataNested(t *testing.T) {
	metadata := []byte(`
hostname: metal3-worker-1
providerID: metal3://b3a4c1d2-0000-0000-0000-000000000001
rack: 3
nicNames:
  - ens2f1np1
  - ens3f1np1
network:
  internal:
    - 10.0.0.0/24
  gateway:
    address: 10.0.0.1
`)

	data, err := TemplateData(metadata, testCluster(), "worker-1-abcde", "metal3-worker-1", nil)
	require.NoError(t, err)

	assert.Equal(t, "metal3-worker-1", data["hostname"])
	assert.Equal(t, 3, data["rack"])
	assert.Equal(t, []any{"ens2f1np1", "ens3f1np1"}, data["nicNames"])

	network, ok := data["network"].(map[string]any)
	require.True(t, ok, "network stays a nested map")
	assert.Equal(t, []any{"10.0.0.0/24"}, network["internal"])

	gateway, ok := network["gateway"].(map[string]any)
	require.True(t, ok, "gateway stays a nested map")
	assert.Equal(t, "10.0.0.1", gateway["address"])
}

// templateVariables are top-level keys and override metadata keys of the same name.
func TestTemplateDataVariablesOverrideMetadata(t *testing.T) {
	data, err := TemplateData([]byte("installerImage: from-metadata\n"), nil, "", "", map[string]string{
		"installerImage": "from-variables",
		"disk":           "nvme0",
		"":               "ignored",
	})
	require.NoError(t, err)

	assert.Equal(t, "from-variables", data["installerImage"])
	assert.Equal(t, "nvme0", data["disk"])
	assert.NotContains(t, data, "")
}

// The cluster and the machine are nested under .cluster and .machine.
func TestTemplateDataClusterAndMachineContext(t *testing.T) {
	data, err := TemplateData(nil, testCluster(), "worker-1-abcde", "metal3-worker-1", nil)
	require.NoError(t, err)

	cluster, ok := data["cluster"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "metal3-cluster", cluster["name"])
	assert.Equal(t, "default", cluster["namespace"])

	endpoint, ok := cluster["controlPlaneEndpoint"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "192.168.122.100", endpoint["host"])
	assert.Equal(t, 6443, endpoint["port"])

	network, ok := cluster["clusterNetwork"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []string{"10.244.0.0/16"}, network["pods"].(map[string]any)["cidrBlocks"])
	assert.Equal(t, []string{"10.96.0.0/12"}, network["services"].(map[string]any)["cidrBlocks"])
	assert.Equal(t, "cluster.local", network["serviceDomain"])

	ref, ok := cluster["infrastructureRef"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Metal3Cluster", ref["kind"])
	assert.Equal(t, "metal3-cluster", ref["name"])

	machine, ok := data["machine"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "worker-1-abcde", machine["name"])
	assert.Equal(t, "metal3-worker-1", machine["infrastructureRef"].(map[string]any)["name"])
}

// A MachinePool has no single machine identity, so there is no .machine key.
func TestTemplateDataWithoutMachineIdentity(t *testing.T) {
	data, err := TemplateData(nil, testCluster(), "", "", nil)
	require.NoError(t, err)
	assert.NotContains(t, data, "machine")
}

// Empty metadata is an empty context; metadata that is not an object is an error.
func TestTemplateDataMetadataThatIsNotAnObject(t *testing.T) {
	data, err := TemplateData([]byte("  \n"), nil, "", "", nil)
	require.NoError(t, err)
	assert.Empty(t, data)

	_, err = TemplateData([]byte("- a\n- b\n"), nil, "", "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "YAML object")
}
