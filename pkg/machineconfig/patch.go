// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
	"github.com/siderolabs/cluster-api-bootstrap-provider-talos/pkg/templatepatch"
)

// PatchInput is the patching half of a machine configuration: the RFC6902
// configPatches, the strategic patches (the user's plus the standard ones the
// generator produced), and the template context the strategic patches are
// rendered with. Like GenerateInput it is a plain value with no client: the
// caller reads the metadata secret and builds the context.
type PatchInput struct {
	// ConfigPatches are RFC6902 JSON patches applied to the machine config.
	ConfigPatches []bootstrapv1beta1.ConfigPatches

	// StrategicPatches are the user's strategic merge patches, as written in
	// the TalosConfig.
	StrategicPatches []string

	// StdPatches are the standard strategic patches returned by Generate.
	StdPatches []string

	// Metadata is the template context: infrastructure metadata (for example
	// the Metal3DataTemplate metadata secret of CAPM3) enriched with cluster
	// and machine keys. A nil context means no template rendering.
	Metadata map[string]string

	// TemplateVariables are the inline templateVariables of the TalosConfig.
	// They take precedence over metadata values for the same key.
	TemplateVariables map[string]string

	// MetadataPending is true when metadataFrom is configured but the metadata
	// is not available yet: patches that still contain template expressions are
	// skipped so the bootstrap data can be written without them, and they are
	// rendered on the next reconcile.
	MetadataPending bool
}

// ApplyPatches returns the machine configuration in base with the patches of
// in applied to it.
//
// This is the patching formerly done inline in
// controllers/talosconfig_controller.go:513-628 (reconcileGenerate), moved here
// so the bootstrap controller and the in-place update extension share one
// implementation.
func ApplyPatches(base string, in PatchInput) (string, error) {
	data := base

	// Handle JSON6902 patches to the machine config if they were specified.
	// Note this applies to both pre-generated and user-provided configs.
	if len(in.ConfigPatches) > 0 {
		marshalledPatches, err := json.Marshal(in.ConfigPatches)
		if err != nil {
			return "", fmt.Errorf("failure marshalling config patches: %w", err)
		}

		patch, err := jsonpatch.DecodePatch(marshalledPatches)
		if err != nil {
			return "", fmt.Errorf("failure decoding config patches from talosconfig to rfc6902 patch: %w", err)
		}

		patchedBytes, err := configpatcher.JSON6902([]byte(data), patch)
		if err != nil {
			return "", err
		}

		data = string(patchedBytes)
	}

	// Build the template context from metadata and the inline template
	// variables. Ported from
	// cabpt-b7fbe6a-template-rendering.patch:1331-1390.
	context := in.Metadata

	if context == nil && len(in.TemplateVariables) > 0 && !in.MetadataPending {
		context = make(map[string]string)
	}

	if context != nil {
		if len(in.TemplateVariables) > 0 {
			merged := make(map[string]string, len(context)+len(in.TemplateVariables))

			for key, value := range context {
				merged[key] = value
			}

			// Inline template variables take precedence over metadata values.
			templatepatch.MergeVariables(merged, in.TemplateVariables)

			context = merged
		}
	}

	// Handle strategic merge patches: user patches plus the standard patches
	// from the generator, rendered through the template engine when a context
	// exists. Plain strings without template syntax pass through unchanged.
	strategicPatches := slices.Concat(in.StrategicPatches, in.StdPatches)

	if context != nil && len(strategicPatches) > 0 {
		rendered, err := templatepatch.ProcessTemplates(strategicPatches, context)
		if err != nil {
			return "", fmt.Errorf("rendering strategicPatches templates: %w", err)
		}

		strategicPatches = rendered
	}

	if len(strategicPatches) > 0 {
		patches := make([]configpatcher.Patch, 0, len(strategicPatches))

		for _, strategicPatch := range strategicPatches {
			// Phase 1 skips patches that still contain unrendered template
			// expressions: they are not valid YAML. They are applied in Phase 2.
			// Ported from cabpt-b7fbe6a-template-rendering.patch:1396-1401.
			if in.MetadataPending && strings.Contains(strategicPatch, "{{") {
				continue
			}

			patch, err := configpatcher.LoadPatch([]byte(strategicPatch))
			if err != nil {
				return "", fmt.Errorf("failure loading StrategicPatch: %w", err)
			}

			patches = append(patches, patch)
		}

		out, err := configpatcher.Apply(configpatcher.WithBytes([]byte(data)), patches)
		if err != nil {
			return "", fmt.Errorf("failure applying StrategicPatches: %w", err)
		}

		outCfg, err := out.Config()
		if err != nil {
			return "", fmt.Errorf("failure converting result to bytes: %w", err)
		}

		data, err = outCfg.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
		if err != nil {
			return "", fmt.Errorf("failure converting config to string: %w", err)
		}
	}

	return data, nil
}
