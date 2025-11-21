package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

type mfaRoute struct {
	Method      string
	Pattern     string
	Permissions []string
	Level       string
}

func main() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("unable to resolve generator path")
	}

	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	outputPath := filepath.Join(projectRoot, "web", "src", "lib", "generated", "mfa-requirements.ts")

	routes := buildRoutes()

	if err := writeTypeScript(outputPath, routes); err != nil {
		log.Fatalf("failed to write mfa requirements: %v", err)
	}
}

func buildRoutes() []mfaRoute {
	specs := rbac.HTTPRouteSpecs()
	routes := make([]mfaRoute, 0, len(specs))

	for _, spec := range specs {
		perms := spec.PermissionStrings()
		level := rbac.GetMFALevel(perms).String()

		routes = append(routes, mfaRoute{
			Method:      strings.ToUpper(spec.Method),
			Pattern:     spec.Pattern,
			Permissions: append([]string(nil), perms...),
			Level:       level,
		})
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Method == routes[j].Method {
			return routes[i].Pattern < routes[j].Pattern
		}
		return routes[i].Method < routes[j].Method
	})

	return routes
}

func writeTypeScript(path string, routes []mfaRoute) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	var builder strings.Builder

	builder.WriteString("/* eslint-disable */\n")
	builder.WriteString("/* biome-ignore lint -- generated file */\n")
	builder.WriteString("/* biome-ignore format -- generated file */\n\n")

	builder.WriteString("export const MFALevels = ['none', 'step_up', 'always'] as const;\n")
	builder.WriteString("export type MFALevel = (typeof MFALevels)[number];\n")
	builder.WriteString("export interface HttpRouteMfaRequirement {\n")
	builder.WriteString("  method: string;\n")
	builder.WriteString("  pattern: string;\n")
	builder.WriteString("  permissions: string[];\n")
	builder.WriteString("  mfaLevel: MFALevel;\n")
	builder.WriteString("}\n\n")

	builder.WriteString("export const HTTP_ROUTE_MFA_REQUIREMENTS: HttpRouteMfaRequirement[] = [\n")
	for _, route := range routes {
		builder.WriteString("  {\n")
		builder.WriteString(fmt.Sprintf("    method: %s,\n", quote(route.Method)))
		builder.WriteString(fmt.Sprintf("    pattern: %s,\n", quote(route.Pattern)))

		builder.WriteString("    permissions: [")
		for idx, perm := range route.Permissions {
			if idx > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(quote(perm))
		}
		builder.WriteString("],\n")

		builder.WriteString(fmt.Sprintf("    mfaLevel: %s,\n", quote(route.Level)))
		builder.WriteString("  },\n")
	}
	builder.WriteString("];\n")

	tmpFile := path + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(builder.String()), 0o600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, path); err != nil {
		return fmt.Errorf("rename output: %w", err)
	}

	return nil
}

func quote(value string) string {
	return fmt.Sprintf("%q", value)
}
