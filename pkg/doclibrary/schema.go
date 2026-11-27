package doclibrary

import (
	"reflect"
	"strings"
)

// Schema describes exactly the exported request, config and response structures.
// Unknown JSON fields should be rejected by protocol adapters before Execute.
func Schema() map[string]any {
	request := schemaType(reflect.TypeOf(Request{}))
	props := request["properties"].(map[string]any)
	props["mode"].(map[string]any)["enum"] = []string{"search", "read", "view", "list", "find", "grep", "frequent", "index", "status", "schema"}
	props["match"].(map[string]any)["enum"] = []string{"any", "all", "phrase"}
	config := schemaType(reflect.TypeOf(Config{}))
	cp := config["properties"].(map[string]any)
	cp["cache_mode"].(map[string]any)["enum"] = []string{"readwrite", "off"}
	props["output_version"].(map[string]any)["enum"] = []int{0, 1, 2}
	props["output_version"].(map[string]any)["description"] = "1: legacy doc-library/1; 2: compact doc-library/2; omitted or 0 inherits config.output.version"
	props["include_bbox"].(map[string]any)["description"] = "Explicit true/false overrides config.output.include_bbox; omitted inherits it, then the effective protocol default (v1 true, v2 false). Returns only available PDF regions, never creates source coordinates or re-extracts missing frequent regions."
	output := cp["output"].(map[string]any)["properties"].(map[string]any)
	output["version"].(map[string]any)["enum"] = []int{0, 1, 2}
	output["version"].(map[string]any)["default"] = 1
	output["version"].(map[string]any)["description"] = "Output protocol: 1 legacy, 2 compact; omitted or 0 defaults to 1. Does not affect cache identity."
	output["include_bbox"].(map[string]any)["description"] = "Default for request.include_bbox; omitted uses the effective protocol default. V1 includes available point/pct/size; v2 includes only available bbox_pct."
	legacy := schemaType(reflect.TypeOf(Response{}))
	legacy["properties"].(map[string]any)["version"].(map[string]any)["const"] = Version
	legacy["description"] = "Legacy wire response. Geometry is optional: source and frequent results may have none, and include_bbox=false omits PDF region and page dimensions."
	compact := schemaType(reflect.TypeOf(compactResponse{}))
	compactProps := compact["properties"].(map[string]any)
	compactProps["version"].(map[string]any)["const"] = CompactVersion
	compactProps["cache"].(map[string]any)["description"] = "Diagnostic cache information, emitted only for status/index."
	compact["description"] = "Compact wire response: Results is the only body representation. No top-level text, point bbox, or page dimensions. PDF start_line/end_line block indexes remain in Go Results only. Optional bbox_pct contains available regions when requested; important extraction, truncation and continuation warnings remain."
	compactPDF := schemaType(reflect.TypeOf(compactResult{}))
	pdfProps := compactPDF["properties"].(map[string]any)
	delete(pdfProps, "start_line")
	delete(pdfProps, "end_line")
	pdfProps["kind"].(map[string]any)["const"] = "pdf"
	compactSource := schemaType(reflect.TypeOf(compactResult{}))
	compactSource["properties"].(map[string]any)["kind"].(map[string]any)["enum"] = []string{"source", "file", "directory"}
	compactProps["results"].(map[string]any)["items"] = map[string]any{"oneOf": []any{compactPDF, compactSource}}
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema", "version": Version,
		"versions": []string{Version, CompactVersion}, "request": request, "config": config,
		"response":    map[string]any{"oneOf": []any{legacy, compact}},
		"coordinates": map[string]any{"page": "1-based physical PDF page", "bbox": "PDF points, origin top-left; v1 only when available and enabled", "bbox_pct": "available PDF rectangle, percentages xyxy, origin top-left; optional", "region": "percentages, origin top-left", "source_line": "1-based text line", "pdf_line": "1-based canonical paragraph block within a page; Go and v1 only, not source lines"},
	}
}
func schemaType(t reflect.Type) map[string]any {
	if t.Kind() == reflect.Pointer {
		return schemaType(t.Elem())
	}
	switch t.Kind() {
	case reflect.Struct:
		properties := map[string]any{}
		var required []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			properties[name] = schemaType(f.Type)
			if !strings.Contains(tag, ",omitempty") {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaType(t.Elem())}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": true}
	default:
		return map[string]any{}
	}
}
