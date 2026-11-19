package control

import "testing"

// Every arg type becomes a JSON Schema "type" for MCP (/v1/_verbs); a client
// drops the whole tool on an unknown one ("bool"/"int" hid `activity`).
func TestVerbArgTypesAreJSONSchema(t *testing.T) {
	ok := map[string]bool{"string": true, "integer": true, "number": true, "boolean": true, "array": true, "object": true}
	for _, v := range New(Deps{}).Verbs() {
		for _, a := range v.Args {
			if a.Schema == nil && !ok[a.Type] {
				t.Errorf("verb %s arg %s: type %q is not a JSON Schema type", v.Name, a.Name, a.Type)
			}
		}
	}
}
