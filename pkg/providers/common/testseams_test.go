package common

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

// TransformToolDefinitions clones tool definitions and applies the configured
// schema transform to function parameter schemas. When the transform is off, the
// original slice is returned unchanged. For "compact" this uses the default
// CompactSchemaOptions; use TransformToolDefinitionsWithOptions to customize
// them (e.g. from a model's extra_body.compact_schema).
func TransformToolDefinitions(tools []ToolDefinition, transform string) ([]ToolDefinition, error) {
	return TransformToolDefinitionsWithOptions(tools, transform, CompactSchemaOptions{})
}
