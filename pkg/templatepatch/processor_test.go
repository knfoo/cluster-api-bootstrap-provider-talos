// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package templatepatch

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func TestParseMetadata(t *testing.T) {
	raw := []byte(`hostname: test-host
internalIP: 10.0.0.1
externalIP: 203.0.113.1
nodeSubnet: 10.0.0.0/24
leaf01nic: eth0
uuid: test-uuid-1234
CustomField: custom-value
url: https://example.com:8080/path
`)

	metadata := ParseMetadata(raw)

	tests := []struct {
		key      string
		expected string
	}{
		{"hostname", "test-host"},
		{"internalIP", "10.0.0.1"},
		{"internalip", "10.0.0.1"},
		{"externalIP", "203.0.113.1"},
		{"nodeSubnet", "10.0.0.0/24"},
		{"leaf01nic", "eth0"},
		{"uuid", "test-uuid-1234"},
		{"CustomField", "custom-value"},
		{"customfield", "custom-value"},
		{"url", "https://example.com:8080/path"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("ParseMetadata()[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestParseMetadataEmptyLines(t *testing.T) {
	raw := []byte(`
hostname: test-host

internalIP: 10.0.0.1

`)

	metadata := ParseMetadata(raw)
	if got := metadata["hostname"]; got != "test-host" {
		t.Errorf("got %q, want %q", got, "test-host")
	}
	if got := metadata["internalIP"]; got != "10.0.0.1" {
		t.Errorf("got %q, want %q", got, "10.0.0.1")
	}
}

func TestParseMetadataInvalidLines(t *testing.T) {
	// Mixed valid/invalid lines — not valid YAML, falls back to line parser.
	raw := []byte("hostname: test-host\ninvalid-no-colon\n: empty-key\n")

	// Test through the full ParseMetadata path (YAML fails, falls back to lines).
	metadata := ParseMetadata(raw)
	if got := metadata["hostname"]; got != "test-host" {
		t.Errorf("got %q, want %q", got, "test-host")
	}
	if len(metadata) != 1 { // hostname (lowercase is same)
		t.Errorf("expected 1 entry, got %d: %v", len(metadata), metadata)
	}
	if _, ok := metadata[""]; ok {
		t.Error("empty key should not be stored in metadata")
	}
}

func TestParseMetadataYAML(t *testing.T) {
	raw := []byte(`hostname: test-host
network:
  interfaces:
    - name: eth0
      addresses:
        - 10.0.0.1/24
    - name: eth1
      addresses:
        - 10.0.1.1/24
region: us-east-1
`)

	metadata := ParseMetadata(raw)

	tests := []struct {
		key      string
		expected string
	}{
		{"hostname", "test-host"},
		{"region", "us-east-1"},
		{"network.interfaces[0].name", "eth0"},
		{"network.interfaces[0].addresses[0]", "10.0.0.1/24"},
		{"network.interfaces[1].name", "eth1"},
		{"network.interfaces[1].addresses[0]", "10.0.1.1/24"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("ParseMetadata()[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestParseMetadataJSON(t *testing.T) {
	raw := []byte(`{"hostname": "test-host", "internalIP": "10.0.0.1", "tags": {"env": "prod"}}`)

	metadata := ParseMetadata(raw)

	tests := []struct {
		key      string
		expected string
	}{
		{"hostname", "test-host"},
		{"internalIP", "10.0.0.1"},
		{"internalip", "10.0.0.1"},
		{"tags.env", "prod"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("ParseMetadata()[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestParseMetadataYAMLQuotedValues(t *testing.T) {
	raw := []byte(`hostname: "test-host"
ip: '10.0.0.1'
plain: test-value
`)

	metadata := ParseMetadata(raw)

	// YAML parser strips quotes automatically
	tests := []struct {
		key      string
		expected string
	}{
		{"hostname", "test-host"},
		{"ip", "10.0.0.1"},
		{"plain", "test-value"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("ParseMetadata()[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestParseMetadataWindowsLineEndings(t *testing.T) {
	raw := []byte("hostname: test-host\r\ninternalIP: 10.0.0.1\r\n")

	metadata := ParseMetadata(raw)

	if got := metadata["hostname"]; got != "test-host" {
		t.Errorf("got %q, want %q", got, "test-host")
	}
	if got := metadata["internalIP"]; got != "10.0.0.1" {
		t.Errorf("got %q, want %q", got, "10.0.0.1")
	}
}

func TestParseMetadataNestedYAMLCaseInsensitive(t *testing.T) {
	raw := []byte(`Network:
  HostName: test-host
`)

	metadata := ParseMetadata(raw)

	// Original case
	if got := metadata["Network.HostName"]; got != "test-host" {
		t.Errorf("ParseMetadata()[%q] = %q, want %q", "Network.HostName", got, "test-host")
	}

	// Lowercase
	if got := metadata["network.hostname"]; got != "test-host" {
		t.Errorf("ParseMetadata()[%q] = %q, want %q", "network.hostname", got, "test-host")
	}

	// Via get function (mixed case)
	getFunc := makeGetFunc(metadata)
	if got := getFunc("NETWORK.HOSTNAME"); got != "test-host" {
		t.Errorf("get(%q) = %q, want %q", "NETWORK.HOSTNAME", got, "test-host")
	}
}

func TestParseMetadataYAMLNumericValues(t *testing.T) {
	raw := []byte(`port: 8080
enabled: true
ratio: 3.14
bignum: 100000000
`)

	metadata := ParseMetadata(raw)

	tests := []struct {
		key      string
		expected string
	}{
		{"port", "8080"},
		{"enabled", "true"},
		{"ratio", "3.14"},
		{"bignum", "100000000"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("ParseMetadata()[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestFormatScalarOverflow(t *testing.T) {
	// float64 values larger than MaxInt64 must not overflow when converting.
	huge := float64(1e20) // larger than math.MaxInt64
	result := formatScalar(huge)
	if result == "" || strings.Contains(result, "e+") {
		// Should use FormatFloat with 'f' format, not scientific notation
		t.Errorf("formatScalar(1e20) = %q, expected decimal notation", result)
	}
	// Verify it doesn't produce a garbled int from overflow
	if result != "100000000000000000000" {
		t.Errorf("formatScalar(1e20) = %q, want %q", result, "100000000000000000000")
	}
}

func TestProcessTemplatesNilMetadata(t *testing.T) {
	// nil metadata should not panic, should behave as empty.
	templates := []string{
		`val: "{{get "key"}}"`,
	}
	results, err := ProcessTemplates(templates, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != `val: ""` {
		t.Errorf("got %v, want [val: \"\"]", results)
	}
}

func TestParseMetadataYAMLNilValues(t *testing.T) {
	raw := []byte(`hostname: test-host
emptybare:
nullval: null
tildenull: ~
quoted_empty: ""
`)

	metadata := ParseMetadata(raw)

	tests := []struct {
		key      string
		expected string
	}{
		{"hostname", "test-host"},
		{"emptybare", ""},
		{"nullval", ""},
		{"tildenull", ""},
		{"quoted_empty", ""},
	}

	for _, tt := range tests {
		got := metadata[tt.key]
		if got != tt.expected {
			t.Errorf("ParseMetadata()[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}

	// Verify no "<nil>" strings leaked through
	for k, v := range metadata {
		if v == "<nil>" {
			t.Errorf("key %q has literal \"<nil>\" value — should be empty string", k)
		}
	}
}

func TestParseMetadataCAPM3RoundTrip(t *testing.T) {
	// This is the exact format that CAPM3's yaml.Marshal(map[string]string{...})
	// produces. Numeric-looking string values are YAML-quoted by the marshaler.
	raw := []byte(`Address-1: 192.168.0.14
Address-2: 192.168.0.14
Address-3: 192.168.1.14
Annotation-1: ""
Annotation-3: Metal3MachineAnnotation
Gateway-1: 192.168.0.1
Index-1: abc14def
Index-2: "2"
Label-1: ""
Label-3: Metal3MachineLabel
Mac-1: XX:XX:XX:XX:XX:YY
Namespace-1: default
ObjectName-1: machine-0
ObjectName-2: metal3machine-0
Prefix-1: "25"
String-1: String-1
providerid: default/kcm2/control-plane-template-84pvf
`)

	metadata := ParseMetadata(raw)

	tests := []struct {
		key      string
		expected string
	}{
		{"Address-1", "192.168.0.14"},
		{"Annotation-1", ""},
		{"Annotation-3", "Metal3MachineAnnotation"},
		{"Gateway-1", "192.168.0.1"},
		{"Index-1", "abc14def"},
		{"Index-2", "2"},
		{"Label-1", ""},
		{"Label-3", "Metal3MachineLabel"},
		{"Mac-1", "XX:XX:XX:XX:XX:YY"},
		{"Namespace-1", "default"},
		{"ObjectName-1", "machine-0"},
		{"Prefix-1", "25"},
		{"String-1", "String-1"},
		{"providerid", "default/kcm2/control-plane-template-84pvf"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("CAPM3 round-trip: key %q = %q, want %q", tt.key, got, tt.expected)
		}
	}

	// Verify case-insensitive access works for CAPM3 keys
	getFunc := makeGetFunc(metadata)
	if got := getFunc("address-1"); got != "192.168.0.14" {
		t.Errorf("case-insensitive get(address-1) = %q, want %q", got, "192.168.0.14")
	}
	if got := getFunc("MAC-1"); got != "XX:XX:XX:XX:XX:YY" {
		t.Errorf("case-insensitive get(MAC-1) = %q, want %q", got, "XX:XX:XX:XX:XX:YY")
	}
}

func TestProcessTemplatesErrorSnippet(t *testing.T) {
	metadata := map[string]string{}

	templates := []string{
		`machine:
  hostname: {{get "hostname"}}`,
		`{{invalid syntax starting here`,
	}

	_, err := ProcessTemplates(templates, metadata)
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}

	// Error message should contain the template snippet
	if !strings.Contains(err.Error(), "{{invalid syntax") {
		t.Errorf("error should contain template snippet, got: %v", err)
	}
	if !strings.Contains(err.Error(), "template patch 1") {
		t.Errorf("error should contain template index, got: %v", err)
	}
}

func TestEnrichWithCluster(t *testing.T) {
	metadata := make(map[string]string)

	cluster := &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: capiv1.ClusterSpec{
			ControlPlaneEndpoint: capiv1.APIEndpoint{
				Host: "10.0.0.100",
				Port: 6443,
			},
			ClusterNetwork: capiv1.ClusterNetwork{
				Pods: capiv1.NetworkRanges{
					CIDRBlocks: []string{"192.168.0.0/16"},
				},
				Services: capiv1.NetworkRanges{
					CIDRBlocks: []string{"10.96.0.0/12"},
				},
				ServiceDomain: "cluster.local",
			},
			InfrastructureRef: capiv1.ContractVersionedObjectReference{
				Kind: "Metal3Cluster",
				Name: "test-metal3-cluster",
			},
		},
	}

	EnrichWithCluster(metadata, cluster)

	tests := []struct {
		key      string
		expected string
	}{
		{"cluster.name", "test-cluster"},
		{"cluster.namespace", "default"},
		{"cluster.controlPlaneEndpoint.host", "10.0.0.100"},
		{"cluster.controlPlaneEndpoint.port", "6443"},
		{"cluster.clusterNetwork.pods.cidrBlocks[0]", "192.168.0.0/16"},
		{"cluster.clusterNetwork.services.cidrBlocks[0]", "10.96.0.0/12"},
		{"cluster.clusterNetwork.serviceDomain", "cluster.local"},
		{"cluster.infrastructureRef.kind", "Metal3Cluster"},
		{"cluster.infrastructureRef.name", "test-metal3-cluster"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("EnrichWithCluster key %q = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestEnrichWithClusterNil(t *testing.T) {
	metadata := make(map[string]string)
	EnrichWithCluster(metadata, nil)
	if len(metadata) != 0 {
		t.Errorf("expected empty metadata for nil cluster, got %d entries", len(metadata))
	}
}

func TestEnrichWithClusterCaseInsensitive(t *testing.T) {
	metadata := make(map[string]string)

	cluster := &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	EnrichWithCluster(metadata, cluster)

	// Verify lowercase copies are stored for case-insensitive access
	if got := metadata["cluster.name"]; got != "test-cluster" {
		t.Errorf("cluster.name = %q, want %q", got, "test-cluster")
	}
	if got := metadata["CLUSTER.NAME"]; got != "" {
		// Uppercase should not exist as a key, but lowercase lookup should work
		t.Errorf("CLUSTER.NAME should not be a key, got %q", got)
	}

	// The get function should find it case-insensitively
	getFunc := makeGetFunc(metadata)
	if got := getFunc("CLUSTER.NAME"); got != "test-cluster" {
		t.Errorf("get(CLUSTER.NAME) = %q, want %q", got, "test-cluster")
	}
	if got := getFunc("Cluster.Name"); got != "test-cluster" {
		t.Errorf("get(Cluster.Name) = %q, want %q", got, "test-cluster")
	}
}

func TestEnrichWithClusterMinimalSpec(t *testing.T) {
	metadata := make(map[string]string)

	// Cluster with no network config, no endpoint, no infra ref — should not panic
	cluster := &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minimal",
			Namespace: "ns",
		},
	}

	EnrichWithCluster(metadata, cluster)

	if got := metadata["cluster.name"]; got != "minimal" {
		t.Errorf("cluster.name = %q, want %q", got, "minimal")
	}
	if got := metadata["cluster.namespace"]; got != "ns" {
		t.Errorf("cluster.namespace = %q, want %q", got, "ns")
	}
}

func TestProcessTemplatesBasic(t *testing.T) {
	metadata := map[string]string{
		"hostname":   "test-host",
		"internalIP": "10.0.0.1",
		"internalip": "10.0.0.1",
		"nodeSubnet": "10.0.0.0/24",
		"nodesubnet": "10.0.0.0/24",
	}

	templates := []string{
		`machine:
  network:
    hostname: "{{get "hostname"}}"
    interfaces:
      - interface: eth0
        addresses:
          - {{get "internalIP"}}/32`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if expected := `machine:
  network:
    hostname: "test-host"
    interfaces:
      - interface: eth0
        addresses:
          - 10.0.0.1/32`; results[0] != expected {
		t.Errorf("got:\n%s\nwant:\n%s", results[0], expected)
	}
}

func TestProcessTemplatesCaseInsensitive(t *testing.T) {
	metadata := map[string]string{
		"InternalIP": "10.0.0.1",
		"internalip": "10.0.0.1",
	}

	templates := []string{
		`address: {{get "INTERNALIP"}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 || results[0] != "address: 10.0.0.1" {
		t.Errorf("got %v, want [address: 10.0.0.1]", results)
	}
}

func TestProcessTemplatesHasFunction(t *testing.T) {
	metadata := map[string]string{
		"externalIP": "203.0.113.1",
		"externalip": "203.0.113.1",
	}

	templates := []string{
		`{{if has "externalIP"}}external: {{get "externalIP"}}{{end}}`,
		`{{if has "missingField"}}should-not-appear{{end}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 non-empty result, got %d: %v", len(results), results)
	}

	if results[0] != "external: 203.0.113.1" {
		t.Errorf("got %q, want %q", results[0], "external: 203.0.113.1")
	}
}

func TestProcessTemplatesMissingKeyReturnsEmpty(t *testing.T) {
	metadata := map[string]string{}

	templates := []string{
		`value: "{{get "nonexistent"}}"`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 || results[0] != `value: ""` {
		t.Errorf("got %v, want [value: \"\"]", results)
	}
}

func TestProcessTemplatesInvalidSyntax(t *testing.T) {
	metadata := map[string]string{}

	templates := []string{
		`{{invalid syntax`,
	}

	_, err := ProcessTemplates(templates, metadata)
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}

func TestProcessTemplatesEmpty(t *testing.T) {
	results, err := ProcessTemplates(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestProcessTemplatesMultiple(t *testing.T) {
	metadata := map[string]string{
		"hostname":   "test-host",
		"internalIP": "10.0.0.1",
		"internalip": "10.0.0.1",
	}

	templates := []string{
		`machine:
  network:
    hostname: "{{get "hostname"}}"`,
		`machine:
  network:
    interfaces:
      - addresses:
          - {{get "internalIP"}}/32`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestProcessTemplatesEmptyResult(t *testing.T) {
	metadata := map[string]string{}

	templates := []string{
		`{{if has "missing"}}something{{end}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty rendered template, got %d", len(results))
	}
}

func TestProcessTemplatesDefaultFunction(t *testing.T) {
	metadata := map[string]string{
		"hostname": "real-host",
	}

	templates := []string{
		`host: {{get "hostname" | default "fallback"}}`,
		`region: {{get "missing" | default "us-east-1"}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0] != `host: real-host` {
		t.Errorf("got %q, want %q", results[0], "host: real-host")
	}
	if results[1] != `region: us-east-1` {
		t.Errorf("got %q, want %q", results[1], "region: us-east-1")
	}
}

func TestProcessTemplatesRequiredFunction(t *testing.T) {
	metadata := map[string]string{
		"hostname": "real-host",
	}

	// Should succeed when value exists
	templates := []string{
		`host: {{get "hostname" | required "hostname is mandatory"}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 || results[0] != "host: real-host" {
		t.Errorf("got %v, want [host: real-host]", results)
	}

	// Should fail when value is missing
	templates = []string{
		`host: {{get "missing" | required "hostname is mandatory"}}`,
	}

	_, err = ProcessTemplates(templates, metadata)
	if err == nil {
		t.Error("expected error for required missing key")
	}
	if !strings.Contains(err.Error(), "hostname is mandatory") {
		t.Errorf("error should contain the custom message, got: %v", err)
	}
}

func TestProcessTemplatesWithNestedYAMLMetadata(t *testing.T) {
	// Simulate metadata parsed from a YAML secret
	raw := []byte(`hostname: test-host
network:
  gateway: 10.0.0.1
  vlan: 100
`)

	metadata := ParseMetadata(raw)

	templates := []string{
		`machine:
  network:
    hostname: "{{get "hostname"}}"
    routes:
      - gateway: {{get "network.gateway"}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expected := `machine:
  network:
    hostname: "test-host"
    routes:
      - gateway: 10.0.0.1`
	if results[0] != expected {
		t.Errorf("got:\n%s\nwant:\n%s", results[0], expected)
	}
}

// Tests for native text/template features — the proposal (#239) endorses
// text/template as a standard library, so users should be able to use all
// built-in template syntax alongside our custom functions.

func TestProcessTemplatesDotNotation(t *testing.T) {
	// text/template natively supports map key access via {{.key}}.
	// This aligns with the proposal's {{ .Machine.Name }} style.
	metadata := map[string]string{
		"hostname":   "test-host",
		"internalIP": "10.0.0.1",
		"internalip": "10.0.0.1",
	}

	templates := []string{
		`host: {{.hostname}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 || results[0] != "host: test-host" {
		t.Errorf("got %v, want [host: test-host]", results)
	}
}

func TestProcessTemplatesIndexForDottedKeys(t *testing.T) {
	// Keys containing dots (e.g., "cluster.name") can't use {{.cluster.name}}
	// because text/template interprets dots as field access. Use the built-in
	// index function: {{index . "cluster.name"}} — or our get function.
	metadata := map[string]string{
		"cluster.name": "mycluster",
		"machine.name": "node-01",
	}

	templates := []string{
		`cluster: {{index . "cluster.name"}}, machine: {{index . "machine.name"}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "cluster: mycluster, machine: node-01"
	if len(results) != 1 || results[0] != expected {
		t.Errorf("got %v, want [%s]", results, expected)
	}
}

func TestProcessTemplatesRangeIteration(t *testing.T) {
	// text/template's range works on the metadata map.
	metadata := map[string]string{
		"hostname": "test-host",
	}

	templates := []string{
		`{{range $k, $v := .}}{{$k}}={{$v}}{{end}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 || results[0] != "hostname=test-host" {
		t.Errorf("got %v, want [hostname=test-host]", results)
	}
}

func TestEnrichWithMachine(t *testing.T) {
	metadata := make(map[string]string)

	EnrichWithMachine(metadata, "worker-01", "m3-worker-01")

	tests := []struct {
		key      string
		expected string
	}{
		{"machine.name", "worker-01"},
		{"machine.infrastructureRef.name", "m3-worker-01"},
	}

	for _, tt := range tests {
		if got := metadata[tt.key]; got != tt.expected {
			t.Errorf("key %q = %q, want %q", tt.key, got, tt.expected)
		}
	}

	// Case-insensitive
	getFunc := makeGetFunc(metadata)
	if got := getFunc("MACHINE.NAME"); got != "worker-01" {
		t.Errorf("get(MACHINE.NAME) = %q, want %q", got, "worker-01")
	}
}

func TestEnrichWithMachineEmpty(t *testing.T) {
	metadata := make(map[string]string)
	EnrichWithMachine(metadata, "", "")
	if len(metadata) != 0 {
		t.Errorf("expected empty metadata for empty machine info, got %d", len(metadata))
	}
}

func TestMergeVariablesEmptyKeySkipped(t *testing.T) {
	metadata := map[string]string{
		"existing": "val",
	}

	MergeVariables(metadata, map[string]string{
		"":     "should-be-skipped",
		"disk": "/dev/sda",
	})

	if _, ok := metadata[""]; ok {
		t.Error("empty key should not be stored in metadata")
	}
	if got := metadata["disk"]; got != "/dev/sda" {
		t.Errorf("disk = %q, want %q", got, "/dev/sda")
	}
}

func TestMergeVariablesNilSafe(t *testing.T) {
	// Should not panic with nil metadata or nil variables.
	MergeVariables(nil, map[string]string{"k": "v"})
	MergeVariables(map[string]string{}, nil)
	MergeVariables(nil, nil)
}

func TestHasWithEmptyValue(t *testing.T) {
	// CAPM3 produces empty values like Label-1: "". The key exists but
	// the value is empty. has() should return true, get() should return "".
	metadata := map[string]string{
		"Label-1": "",
		"label-1": "",
	}

	hasFunc := makeHasFunc(metadata)
	getFunc := makeGetFunc(metadata)

	if !hasFunc("Label-1") {
		t.Error("has(Label-1) should be true for empty value")
	}
	if got := getFunc("Label-1"); got != "" {
		t.Errorf("get(Label-1) = %q, want empty string", got)
	}
}

func TestFlattenDepthLimit(t *testing.T) {
	// Build a deeply nested structure that exceeds maxFlattenDepth.
	inner := map[string]interface{}{"leaf": "value"}
	for i := 0; i < maxFlattenDepth+5; i++ {
		inner = map[string]interface{}{"level": inner}
	}

	out := make(map[string]string)
	flattenMap("", inner, out)

	// The leaf should NOT appear because recursion was cut at maxFlattenDepth.
	for _, v := range out {
		if v == "value" {
			t.Error("deeply nested leaf should not be reachable beyond maxFlattenDepth")
		}
	}
}

func TestMergeVariables(t *testing.T) {
	metadata := map[string]string{
		"hostname":   "from-secret",
		"internalIP": "10.0.0.1",
		"internalip": "10.0.0.1",
	}

	vars := map[string]string{
		"hostname": "overridden",
		"disk":     "/dev/sda",
	}

	MergeVariables(metadata, vars)

	// Variables take precedence
	if got := metadata["hostname"]; got != "overridden" {
		t.Errorf("hostname = %q, want %q (variable should override metadata)", got, "overridden")
	}
	// New variable added
	if got := metadata["disk"]; got != "/dev/sda" {
		t.Errorf("disk = %q, want %q", got, "/dev/sda")
	}
	// Untouched metadata preserved
	if got := metadata["internalIP"]; got != "10.0.0.1" {
		t.Errorf("internalIP = %q, want %q", got, "10.0.0.1")
	}
	// Case-insensitive access for variables
	getFunc := makeGetFunc(metadata)
	if got := getFunc("DISK"); got != "/dev/sda" {
		t.Errorf("get(DISK) = %q, want %q", got, "/dev/sda")
	}
}

func TestProcessTemplatesStrategicPatchNoOp(t *testing.T) {
	// Plain strings without template syntax pass through unchanged.
	// This proves rendering strategicPatches through the template engine
	// is safe for existing users who don't use templating.
	metadata := map[string]string{
		"hostname": "test-host",
	}

	plainPatches := []string{
		`machine:
  install:
    disk: /dev/sda`,
		`cluster:
  network:
    cni:
      name: custom`,
	}

	results, err := ProcessTemplates(plainPatches, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Plain patches must come through unmodified
	for i, expected := range plainPatches {
		if results[i] != expected {
			t.Errorf("patch %d changed:\ngot:\n%s\nwant:\n%s", i, results[i], expected)
		}
	}
}

func TestProcessTemplatesStrategicPatchWithVariables(t *testing.T) {
	// The #239 proposal: users can template strategicPatches directly
	// using {{ .Vars.disk }} style (via get function or dot notation).
	metadata := map[string]string{
		"hostname": "worker-01",
		"disk":     "/dev/nvme0n1",
	}

	templates := []string{
		`machine:
  install:
    disk: {{get "disk"}}
  network:
    hostname: {{.hostname}}`,
	}

	results, err := ProcessTemplates(templates, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `machine:
  install:
    disk: /dev/nvme0n1
  network:
    hostname: worker-01`
	if len(results) != 1 || results[0] != expected {
		t.Errorf("got:\n%s\nwant:\n%s", results[0], expected)
	}
}

func TestFullCAPM3Scenario(t *testing.T) {
	// End-to-end scenario: CAPM3 metadata secret + cluster enrichment +
	// machine enrichment + template variables → template rendering.
	// This validates the original use case from the cloudinit-service.
	raw := []byte(`hostname: worker-01
internalIP: 10.0.0.5
externalIP: 203.0.113.5
nodeSubnet: 10.0.0.0/24
leaf01nic: ens2f1np1
providerid: default/bmh-01/m3m-worker-01
`)

	metadata := ParseMetadata(raw)

	// Enrich with cluster info (as the controller does).
	cluster := &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prod-cluster",
			Namespace: "metal3",
		},
		Spec: capiv1.ClusterSpec{
			ControlPlaneEndpoint: capiv1.APIEndpoint{
				Host: "10.0.0.100",
				Port: 6443,
			},
			ClusterNetwork: capiv1.ClusterNetwork{
				Pods: capiv1.NetworkRanges{
					CIDRBlocks: []string{"192.168.0.0/16"},
				},
				Services: capiv1.NetworkRanges{
					CIDRBlocks: []string{"10.96.0.0/12"},
				},
				ServiceDomain: "cluster.local",
			},
		},
	}
	EnrichWithCluster(metadata, cluster)

	// Enrich with machine info (as the controller does).
	EnrichWithMachine(metadata, "worker-01", "m3m-worker-01")

	// Merge inline variables (user-defined).
	MergeVariables(metadata, map[string]string{
		"disk": "/dev/nvme0n1",
	})

	// Template patches (the original CAPM3 use case).
	templatePatches := []string{
		`machine:
  network:
    hostname: "{{get "hostname"}}"
    interfaces:
      - interface: {{get "leaf01nic"}}
        addresses:
          - {{get "internalIP"}}/32
        routes:
          - network: 0.0.0.0/0
            gateway: {{get "nodeSubnet"}}
{{- if has "externalIP"}}
      - interface: lo
        addresses:
          - {{get "externalIP"}}/32
{{- end}}`,
	}

	results, err := ProcessTemplates(templatePatches, metadata)
	if err != nil {
		t.Fatalf("template patches failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify the rendered output contains expected values.
	r := results[0]
	for _, want := range []string{
		`hostname: "worker-01"`,
		"interface: ens2f1np1",
		"- 10.0.0.5/32",
		"gateway: 10.0.0.0/24",
		"- 203.0.113.5/32",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("rendered template missing %q:\n%s", want, r)
		}
	}

	// Strategic patches with template syntax (the #239 proposal).
	strategicPatches := []string{
		`machine:
  install:
    disk: {{get "disk"}}`,
	}

	results2, err := ProcessTemplates(strategicPatches, metadata)
	if err != nil {
		t.Fatalf("strategic patches failed: %v", err)
	}

	if len(results2) != 1 || !strings.Contains(results2[0], "/dev/nvme0n1") {
		t.Errorf("strategic patch template not rendered: %v", results2)
	}

	// Verify machine and cluster context is accessible.
	contextTemplates := []string{
		`machine: {{get "machine.name"}}, cluster: {{get "cluster.name"}}`,
	}

	results3, err := ProcessTemplates(contextTemplates, metadata)
	if err != nil {
		t.Fatalf("context templates failed: %v", err)
	}

	if len(results3) != 1 || results3[0] != "machine: worker-01, cluster: prod-cluster" {
		t.Errorf("got %v, want [machine: worker-01, cluster: prod-cluster]", results3)
	}
}
