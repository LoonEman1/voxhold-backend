package main

import (
	"os"
	"strings"
	"testing"
)

// Every route registered in internal/ must appear in docs/openapi.yaml.
// Regenerate with: go run ./tools/openapigen
func TestOpenAPIListsEveryRegisteredRoute(t *testing.T) {
	spec, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(spec)
	routes := scanRoutes("../../internal")
	if len(routes) < 100 {
		t.Fatalf("route scan found only %d routes; the scanner is broken", len(routes))
	}
	for key, r := range routes {
		if !strings.Contains(text, "  "+r.path+":\n") || !strings.Contains(text, "summary: "+key+"\n") {
			t.Errorf("docs/openapi.yaml is missing %s (run go run ./tools/openapigen)", key)
		}
	}
}
