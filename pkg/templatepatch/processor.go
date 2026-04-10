// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package templatepatch

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/template"

	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/yaml"
)

// ParseMetadata parses raw bytes into a flat key-value map. It tries YAML/JSON
// first (flattening nested structures to dot-notation keys), then falls back to
// simple "key: value" line parsing. Both the original casing and a lowercase
// version of each key are stored for case-insensitive access.
func ParseMetadata(raw []byte) map[string]string {
	// Normalize Windows line endings before any parsing.
	normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))

	// Try structured parsing (YAML/JSON) first.
	if metadata, ok := parseStructured(normalized); ok && len(metadata) > 0 {
		return metadata
	}

	// Fallback: simple "key: value" line parsing.
	return parseLines(normalized)
}

// parseStructured attempts to parse raw bytes as YAML or JSON and flatten
// the result into a string map with dot-notation keys.
func parseStructured(raw []byte) (map[string]string, bool) {
	var parsed interface{}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, false
	}

	// Only accept map-like top-level structures.
	m, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, false
	}

	metadata := make(map[string]string)
	flattenMap("", m, metadata)

	return metadata, true
}

// setKey stores a key-value pair along with its lowercase variant.
func setKey(out map[string]string, key, value string) {
	out[key] = value
	out[strings.ToLower(key)] = value
}

// maxFlattenDepth limits recursion in flattenMap/flattenSlice to prevent
// stack overflow from deeply nested or malicious input.
const maxFlattenDepth = 20

// formatScalar converts an arbitrary YAML/JSON scalar to a string suitable for
// template consumption. It handles nil (→ empty string), float64 (avoiding
// scientific notation for whole numbers), bool, and everything else via Sprintf.
func formatScalar(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case float64:
		// Avoid scientific notation: 1e+08 → "100000000"
		if val == math.Trunc(val) && !math.IsInf(val, 0) && !math.IsNaN(val) &&
			val >= math.MinInt64 && val <= math.MaxInt64 {
			return strconv.FormatInt(int64(val), 10)
		}

		return strconv.FormatFloat(val, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// flattenMap recursively flattens a nested map into dot-notation string keys.
// Recursion is bounded by maxFlattenDepth.
func flattenMap(prefix string, m map[string]interface{}, out map[string]string) {
	flattenMapDepth(prefix, m, out, 0)
}

func flattenMapDepth(prefix string, m map[string]interface{}, out map[string]string, depth int) {
	if depth > maxFlattenDepth {
		return
	}

	for k, v := range m {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}

		switch val := v.(type) {
		case map[string]interface{}:
			flattenMapDepth(fullKey, val, out, depth+1)
		case []interface{}:
			flattenSliceDepth(fullKey, val, out, depth+1)
		default:
			setKey(out, fullKey, formatScalar(val))
		}
	}
}

func flattenSliceDepth(prefix string, s []interface{}, out map[string]string, depth int) {
	if depth > maxFlattenDepth {
		return
	}

	for i, v := range s {
		fullKey := fmt.Sprintf("%s[%d]", prefix, i)

		switch val := v.(type) {
		case map[string]interface{}:
			flattenMapDepth(fullKey, val, out, depth+1)
		case []interface{}:
			flattenSliceDepth(fullKey, val, out, depth+1)
		default:
			setKey(out, fullKey, formatScalar(val))
		}
	}
}

// parseLines parses simple "key: value" lines from raw bytes into a map.
// Each line is split on the first colon.
func parseLines(raw []byte) map[string]string {
	metadata := make(map[string]string)

	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}

		value := strings.TrimSpace(parts[1])

		metadata[key] = value
		metadata[strings.ToLower(key)] = value
	}

	return metadata
}

// EnrichWithCluster adds cluster.* keys to the metadata map from a CAPI Cluster resource.
func EnrichWithCluster(metadata map[string]string, cluster *capiv1.Cluster) {
	if metadata == nil || cluster == nil {
		return
	}

	set := func(key, value string) {
		setKey(metadata, key, value)
	}

	set("cluster.name", cluster.Name)
	set("cluster.namespace", cluster.Namespace)

	if cluster.Spec.ControlPlaneEndpoint.Host != "" {
		set("cluster.controlPlaneEndpoint.host", cluster.Spec.ControlPlaneEndpoint.Host)
		set("cluster.controlPlaneEndpoint.port", fmt.Sprintf("%d", cluster.Spec.ControlPlaneEndpoint.Port))
	}

	if cluster.Spec.ClusterNetwork.Pods.CIDRBlocks != nil {
		for i, cidr := range cluster.Spec.ClusterNetwork.Pods.CIDRBlocks {
			set(fmt.Sprintf("cluster.clusterNetwork.pods.cidrBlocks[%d]", i), cidr)
		}
	}

	if cluster.Spec.ClusterNetwork.Services.CIDRBlocks != nil {
		for i, cidr := range cluster.Spec.ClusterNetwork.Services.CIDRBlocks {
			set(fmt.Sprintf("cluster.clusterNetwork.services.cidrBlocks[%d]", i), cidr)
		}
	}

	if cluster.Spec.ClusterNetwork.ServiceDomain != "" {
		set("cluster.clusterNetwork.serviceDomain", cluster.Spec.ClusterNetwork.ServiceDomain)
	}

	if cluster.Spec.InfrastructureRef.Kind != "" {
		set("cluster.infrastructureRef.kind", cluster.Spec.InfrastructureRef.Kind)
		set("cluster.infrastructureRef.name", cluster.Spec.InfrastructureRef.Name)
	}
}

// EnrichWithMachine adds machine.* keys to the metadata map from CAPI Machine fields.
func EnrichWithMachine(metadata map[string]string, machineName, infraName string) {
	if metadata == nil {
		return
	}

	if machineName != "" {
		setKey(metadata, "machine.name", machineName)
	}

	if infraName != "" {
		setKey(metadata, "machine.infrastructureRef.name", infraName)
	}
}

// MergeVariables merges inline template variables into a metadata map.
// Variables take precedence over existing metadata for the same key.
// Empty keys are skipped. Both metadata and variables may be nil (no-op).
func MergeVariables(metadata map[string]string, variables map[string]string) {
	if metadata == nil || len(variables) == 0 {
		return
	}

	for k, v := range variables {
		if k == "" {
			continue
		}

		setKey(metadata, k, v)
	}
}

// ProcessTemplates renders Go templates against metadata, returning strategic patch strings.
// Each template has access to get(key), has(key), default(value, fallback), and
// required(value, errMsg) functions for flexible metadata lookup.
// A nil metadata map is treated as empty (all get calls return "", all has calls return false).
func ProcessTemplates(templates []string, metadata map[string]string) ([]string, error) {
	if len(templates) == 0 {
		return nil, nil
	}

	if metadata == nil {
		metadata = make(map[string]string)
	}

	funcMap := template.FuncMap{
		"get":      makeGetFunc(metadata),
		"has":      makeHasFunc(metadata),
		"default":  defaultFunc,
		"required": requiredFunc,
	}

	results := make([]string, 0, len(templates))

	for i, tmplStr := range templates {
		tmpl, err := template.New(fmt.Sprintf("templatepatch-%d", i)).Funcs(funcMap).Parse(tmplStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template patch %d (%s): %w", i, templateSnippet(tmplStr), err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, metadata); err != nil {
			return nil, fmt.Errorf("failed to execute template patch %d (%s): %w", i, templateSnippet(tmplStr), err)
		}

		rendered := strings.TrimSpace(buf.String())
		if rendered != "" {
			results = append(results, rendered)
		}
	}

	return results, nil
}

// templateSnippet returns the first line of a template (truncated to ~60 chars)
// for use in error messages. Truncation respects UTF-8 rune boundaries.
func templateSnippet(tmpl string) string {
	line, _, _ := strings.Cut(tmpl, "\n")

	runes := []rune(line)
	if len(runes) > 60 {
		return string(runes[:60]) + "..."
	}

	return line
}

// makeGetFunc creates a template function that returns the value for a key,
// trying exact match first, then case-insensitive fallback via the lowercase
// copy stored by ParseMetadata/EnrichWithCluster.
func makeGetFunc(metadata map[string]string) func(string) string {
	return func(key string) string {
		if value, ok := metadata[key]; ok {
			return value
		}

		if value, ok := metadata[strings.ToLower(key)]; ok {
			return value
		}

		return ""
	}
}

// makeHasFunc creates a template function that checks if a key exists,
// trying exact match first, then case-insensitive fallback via the lowercase
// copy stored by ParseMetadata/EnrichWithCluster.
func makeHasFunc(metadata map[string]string) func(string) bool {
	return func(key string) bool {
		if _, ok := metadata[key]; ok {
			return true
		}

		_, ok := metadata[strings.ToLower(key)]

		return ok
	}
}

// defaultFunc returns the value if non-empty, otherwise returns the fallback.
// Usage: {{get "key" | default "fallback"}}
func defaultFunc(fallback, value string) string {
	if value != "" {
		return value
	}

	return fallback
}

// requiredFunc returns the value if non-empty, otherwise returns an error.
// Usage: {{get "hostname" | required "hostname must be set in metadata"}}
func requiredFunc(errMsg, value string) (string, error) {
	if value != "" {
		return value, nil
	}

	return "", fmt.Errorf("required metadata: %s", errMsg)
}
