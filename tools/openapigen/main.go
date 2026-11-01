// Command openapigen scans the sources for registered routes and writes a
// skeleton docs/openapi.yaml. Request and response schemas are left generic;
// refine them by hand and keep the route list in sync (see docs/openapi_test.go).
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var routePattern = regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE) (/[^"\s]*)"`)

type route struct {
	method, path string
	auth         bool
	file         string
}

func main() {
	routes := scanRoutes("internal")
	writeSpec(routes)
}

func scanRoutes(root string) map[string]route {
	routes := map[string]route{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		var lines []string
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		for i, line := range lines {
			for _, match := range routePattern.FindAllStringSubmatch(line, -1) {
				if !strings.HasPrefix(match[2], "/api/v1") {
					continue
				}
				window := strings.Join(lines[i:min(i+4, len(lines))], " ")
				key := match[1] + " " + match[2]
				routes[key] = route{match[1], match[2], strings.Contains(window, "requireAuth") || strings.Contains(window, "auth("), filepath.ToSlash(path)}
			}
		}
		return nil
	})
	return routes
}

func writeSpec(routes map[string]route) {
	keys := make([]string, 0, len(routes))
	for key := range routes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("openapi: 3.1.0\ninfo:\n  title: Voxhold API\n  version: 2.6.0\n  description: |\n    Generated route skeleton (tools/openapigen). Bodies and responses are still generic:\n    TODO refine schemas from the json-tagged structs next to each handler.\nservers:\n  - url: /\ncomponents:\n  securitySchemes:\n    bearer:\n      type: http\n      scheme: bearer\n  schemas:\n    Error:\n      type: object\n      properties:\n        error: {type: string}\npaths:\n")
	byPath := map[string][]route{}
	var paths []string
	for _, key := range keys {
		r := routes[key]
		if _, ok := byPath[r.path]; !ok {
			paths = append(paths, r.path)
		}
		byPath[r.path] = append(byPath[r.path], r)
	}
	for _, path := range paths {
		fmt.Fprintf(&out, "  %s:\n", path)
		for _, r := range byPath[path] {
			fmt.Fprintf(&out, "    %s:\n      operationId: %s\n      summary: %s %s\n      x-source: %s\n", strings.ToLower(r.method), operationID(r), r.method, r.path, r.file)
			if params := pathParams(path); len(params) > 0 {
				out.WriteString("      parameters:\n")
				for _, name := range params {
					fmt.Fprintf(&out, "        - {name: %s, in: path, required: true, schema: {type: string}}\n", name)
				}
			}
			if r.auth {
				out.WriteString("      security:\n        - bearer: []\n")
			}
			out.WriteString("      responses:\n        '200': {description: OK}\n        default:\n          description: Error\n          content:\n            application/json:\n              schema: {$ref: '#/components/schemas/Error'}\n")
		}
	}
	if err := os.WriteFile("docs/openapi.yaml", []byte(out.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote docs/openapi.yaml with %d operations\n", len(keys))
}

var nonWord = regexp.MustCompile(`[^A-Za-z0-9]+`)

func operationID(r route) string {
	return strings.ToLower(r.method) + "_" + strings.Trim(nonWord.ReplaceAllString(r.path, "_"), "_")
}

func pathParams(path string) []string {
	var names []string
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			names = append(names, strings.Trim(part, "{}"))
		}
	}
	return names
}
