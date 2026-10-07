package common

import (
	"fmt"
	"strings"
)

const (
	ToolSchemaTransformOff     = ""
	ToolSchemaTransformSimple  = "simple"
	ToolSchemaTransformCompact = "compact"
)

// NormalizeToolSchemaTransform resolves user-facing aliases to a canonical
// transform mode. Empty values and explicit "off"-style values disable schema
// transformation.
func NormalizeToolSchemaTransform(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off", "none", "native":
		return ToolSchemaTransformOff, nil
	case "simple", "basic", "strict", "flat":
		return ToolSchemaTransformSimple, nil
	case "compact", "minimal", "tiny":
		return ToolSchemaTransformCompact, nil
	default:
		return "", fmt.Errorf("unsupported tool_schema_transform %q (supported: off, simple, compact)", raw)
	}
}

// TransformToolDefinitionsWithOptions is TransformToolDefinitions with
// explicit CompactSchemaOptions (ignored for every transform except
// "compact").
func TransformToolDefinitionsWithOptions(
	tools []ToolDefinition,
	transform string,
	compactOpts CompactSchemaOptions,
) ([]ToolDefinition, error) {
	transform, err := NormalizeToolSchemaTransform(transform)
	if err != nil {
		return nil, err
	}
	if transform == ToolSchemaTransformOff || len(tools) == 0 {
		return tools, nil
	}

	out := make([]ToolDefinition, len(tools))
	for i, tool := range tools {
		out[i] = tool
		if tool.Type != "function" {
			continue
		}
		out[i].Function = tool.Function
		out[i].Function.Parameters = transformToolSchema(tool.Function.Parameters, transform, compactOpts)
		if transform == ToolSchemaTransformCompact {
			out[i].Function.Description = CompactToolDescription(tool.Function.Description)
		}
	}

	return out, nil
}

func transformToolSchema(schema map[string]any, transform string, compactOpts CompactSchemaOptions) map[string]any {
	switch transform {
	case ToolSchemaTransformSimple:
		return SanitizeSchemaForGoogle(schema)
	case ToolSchemaTransformCompact:
		return CompactToolSchema(schema, compactOpts)
	default:
		return cloneGeminiSchemaMap(schema)
	}
}
