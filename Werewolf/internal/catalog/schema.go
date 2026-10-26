package catalog

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Schema derives wrapper field names and types from the strict YAML loader.
// Native recipe and edit values retain arbitrary YAML; pinned Melange and the
// guarded-edit engine validate their respective vocabularies.
func Schema() ([]byte, error) {
	defs := map[string]any{}
	var schema func(reflect.Type) map[string]any
	schema = func(t reflect.Type) map[string]any {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == reflect.TypeFor[yaml.Node]() {
			return map[string]any{}
		}
		switch t.Kind() {
		case reflect.String:
			return map[string]any{"type": "string"}
		case reflect.Bool:
			return map[string]any{"type": "boolean"}
		case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64:
			return map[string]any{"type": "integer"}
		case reflect.Slice:
			return map[string]any{"type": "array", "items": schema(t.Elem())}
		case reflect.Map:
			return map[string]any{"type": "object", "additionalProperties": schema(t.Elem())}
		case reflect.Struct:
			name := t.Name()
			if name == "" {
				sum := sha256.Sum256([]byte(t.String()))
				name = fmt.Sprintf("Fields%x", sum[:6])
			}
			if _, ok := defs[name]; ok {
				return map[string]any{"$ref": "#/$defs/" + name}
			}
			props := map[string]any{}
			obj := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
			defs[name] = obj
			var fields func(reflect.Type)
			fields = func(st reflect.Type) {
				for i := 0; i < st.NumField(); i++ {
					f := st.Field(i)
					if !f.IsExported() {
						continue
					}
					tag := f.Tag.Get("yaml")
					parts := strings.Split(tag, ",")
					key := parts[0]
					if key == "-" {
						continue
					}
					if strings.Contains(tag, ",inline") {
						fields(f.Type)
						continue
					}
					if key == "" {
						key = strings.ToLower(f.Name)
					}
					props[key] = schema(f.Type)
				}
			}
			fields(t)
			return map[string]any{"$ref": "#/$defs/" + name}
		}
		return map[string]any{}
	}
	schema(reflect.TypeFor[Definition]())
	root := defs["Definition"].(map[string]any)
	root["required"] = []string{"update", "changes"}
	root["oneOf"] = []any{map[string]any{"required": []string{"melange"}}, map[string]any{"required": []string{"wolfi"}}}
	props := root["properties"].(map[string]any)
	props["melange"] = map[string]any{"type": "object", "required": []string{"package", "pipeline"}, "description": "Native Melange configuration; validated by the repository's pinned Melange."}
	source := defs["Source"].(map[string]any)
	source["oneOf"] = []any{map[string]any{"required": []string{"github"}}, map[string]any{"required": []string{"source"}}}
	derivation := defs["Derivation"].(map[string]any)
	derivation["required"] = []string{"revision", "sources"}
	derivation["oneOf"] = []any{map[string]any{"required": []string{"recipe"}}, map[string]any{"required": []string{"family"}}}
	defs["Change"].(map[string]any)["required"] = []string{"what", "why", "evidence"}
	github := defs["GitHub"].(map[string]any)
	github["required"] = []string{"identifier"}
	return json.MarshalIndent(map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "Werewolf package definition", "$ref": "#/$defs/Definition", "$defs": defs}, "", "  ")
}
