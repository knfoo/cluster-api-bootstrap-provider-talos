// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package templatepatch renders strategicPatches through Go's text/template
// engine. The data a template reads is the nested value pkg/machineconfig
// builds from the metadata secret, the Cluster and the Machine; templates use
// plain Go syntax and no provider functions.
package templatepatch

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// ProcessTemplates renders each strategic patch template with data and returns
// the patches it produces. Templates are plain Go text/template: the dot is the
// data map, so a value is read as .key, a list element as
// index .cluster.clusterNetwork.pods.cidrBlocks 0 and a fallback as
// or .zone "default". There is no FuncMap, and missingkey=error makes a key that
// is not in data fail the render with its name instead of writing "<no value>"
// into the machine configuration.
func ProcessTemplates(templates []string, data map[string]any) ([]string, error) {
	if len(templates) == 0 {
		return nil, nil
	}

	if data == nil {
		data = map[string]any{}
	}

	results := make([]string, 0, len(templates))

	for i, tmplStr := range templates {
		tmpl, err := template.New(fmt.Sprintf("templatepatch-%d", i)).Option("missingkey=error").Parse(tmplStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template patch %d (%s): %w", i, templateSnippet(tmplStr), err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
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
