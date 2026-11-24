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
	return map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "version": Version, "request": request, "config": config, "response": schemaType(reflect.TypeOf(Response{})), "coordinates": map[string]any{"page": "1-based physical PDF page", "bbox": "PDF points, origin top-left", "region": "percentages, origin top-left", "source_line": "1-based text line", "pdf_line": "1-based canonical paragraph block within a page"}}
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
