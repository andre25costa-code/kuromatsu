// PicoClaw - Ultra-lightweight personal AI agent

package common

import "strings"

// CompactSchemaOptions controls how CompactToolSchema/CompactToolDescription
// reduce a tool definition for small/local models with tight context
// windows (ADR-014 point 4 / FR-015 AC-015-4).
type CompactSchemaOptions struct {
	// MaxEnum caps how many enum values are kept per property. <= 0 uses the
	// library default (8).
	MaxEnum int
	// PropertyDescriptions keeps a compacted (first-sentence, <=100 chars)
	// description on each property. Default false: only the top-level tool
	// description survives compaction; per-property descriptions are
	// dropped entirely to save tokens.
	PropertyDescriptions bool
	// RequiredOnly drops properties that are not listed in the schema's
	// "required" array. Default false — ADR-014 explicitly rejects
	// "required only" as the *default* behavior, because it would silently
	// remove usable optional parameters (e.g. exec's "command", which is
	// not required but is exactly what the tool is for). Left available as
	// an explicit opt-in for callers who want a more aggressive reduction.
	RequiredOnly bool
}

const defaultCompactMaxEnum = 8

// maxCompactDescriptionChars is the hard cap from AC-015-4/AC-015-5: tool
// and (when enabled) property descriptions are reduced to their first
// sentence, then hard-truncated to this many runes.
const maxCompactDescriptionChars = 100

func (o CompactSchemaOptions) withDefaults() CompactSchemaOptions {
	if o.MaxEnum <= 0 {
		o.MaxEnum = defaultCompactMaxEnum
	}
	return o
}

// CompactToolDescription reduces a description to its first sentence
// (cut at the first ". ", "! ", "? " or newline), then hard-truncates to
// maxCompactDescriptionChars runes (AC-015-4).
func CompactToolDescription(desc string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return ""
	}

	cut := len(desc)
	for _, sep := range []string{". ", "! ", "? ", "\n"} {
		if idx := strings.Index(desc, sep); idx >= 0 {
			end := idx + len(strings.TrimRight(sep, " \n"))
			if end < cut {
				cut = end
			}
		}
	}
	desc = strings.TrimSpace(desc[:cut])
	return truncateRunes(desc, maxCompactDescriptionChars)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// CompactToolSchema reduces a JSON Schema tool-parameter definition to the
// compact shape from ADR-014 point 4: the object's "properties" are kept
// (all of them, unless opts.RequiredOnly is set) but each property is
// reduced to only "type" (+ "enum" capped at opts.MaxEnum, and
// "items":{"type":...} for arrays) — no per-property description by
// default, and a nested object collapses to {"type":"object"} without
// recursing into its own properties. A nil schema returns nil.
func CompactToolSchema(schema map[string]any, opts CompactSchemaOptions) map[string]any {
	if schema == nil {
		return nil
	}
	return compactObjectSchema(schema, opts.withDefaults())
}

func compactObjectSchema(schema map[string]any, opts CompactSchemaOptions) map[string]any {
	out := make(map[string]any)

	propsRaw, hasProps := schema["properties"].(map[string]any)
	if t := geminiSchemaBranchType(schema["type"]); t != "" {
		out["type"] = t
	} else if hasProps {
		out["type"] = "object"
	}

	required := requiredStrings(schema["required"])

	if hasProps {
		requiredSet := make(map[string]struct{}, len(required))
		for _, name := range required {
			requiredSet[name] = struct{}{}
		}

		props := make(map[string]any, len(propsRaw))
		for name, raw := range propsRaw {
			if opts.RequiredOnly {
				if _, ok := requiredSet[name]; !ok {
					continue
				}
			}
			propSchema, ok := raw.(map[string]any)
			if !ok {
				props[name] = map[string]any{}
				continue
			}
			props[name] = compactPropertySchema(propSchema, opts)
		}
		out["properties"] = props
		out["type"] = "object"
	}

	if len(required) > 0 {
		filtered := required
		if opts.RequiredOnly && hasProps {
			filtered = nil
			for _, name := range required {
				if _, ok := propsRaw[name]; ok {
					filtered = append(filtered, name)
				}
			}
		}
		if len(filtered) > 0 {
			out["required"] = filtered
		}
	}

	return out
}

func compactPropertySchema(schema map[string]any, opts CompactSchemaOptions) map[string]any {
	out := make(map[string]any)

	propType := compactPropertyType(schema)
	if propType != "" {
		out["type"] = propType
	}

	if opts.PropertyDescriptions {
		if desc, ok := schema["description"].(string); ok {
			if compact := CompactToolDescription(desc); compact != "" {
				out["description"] = compact
			}
		}
	}

	if enumRaw, ok := schema["enum"]; ok {
		if values := compactEnumValues(enumRaw, opts.MaxEnum); len(values) > 0 {
			out["enum"] = values
		}
	}

	if propType == "array" {
		itemType := "string"
		if itemsRaw, ok := schema["items"].(map[string]any); ok {
			if t := compactPropertyType(itemsRaw); t != "" {
				itemType = t
			}
		}
		out["items"] = map[string]any{"type": itemType}
	}

	return out
}

// compactPropertyType derives a single scalar type name for a property,
// falling back to "object"/"array" heuristics when "type" is absent (some
// hand-written schemas omit it but still declare "properties"/"items").
func compactPropertyType(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	if t := geminiSchemaBranchType(schema["type"]); t != "" && t != "null" {
		return t
	}
	if _, ok := schema["properties"]; ok {
		return "object"
	}
	if _, ok := schema["items"]; ok {
		return "array"
	}
	return ""
}

func compactEnumValues(raw any, maxEnum int) []any {
	values := sanitizeGeminiEnum(raw)
	if len(values) == 0 {
		return nil
	}
	if maxEnum > 0 && len(values) > maxEnum {
		values = values[:maxEnum]
	}
	return values
}
