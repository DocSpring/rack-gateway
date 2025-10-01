package main

import (
	"log"
	"path/filepath"
	"runtime"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/handlers"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/tkrajina/typescriptify-golang-structs/typescriptify"
)

func main() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("unable to resolve generator path")
	}

	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	output := filepath.Join(projectRoot, "web", "src", "lib", "generated", "gateway-types.ts")

	converter := typescriptify.New().WithInterface(true).WithConstructor(false).WithCreateFromMethod(false)
	converter.BackupDir = ""
	converter.WithCustomCodeBefore("/* biome-ignore lint -- generated file */\n/* biome-ignore format -- generated file */")

	converter.Add(db.APIToken{})
	converter.Add(token.APITokenResponse{})
	converter.Add(handlers.DeployApprovalRequestResponse{})

	if err := converter.ConvertToFile(output); err != nil {
		log.Fatalf("failed to generate types: %v", err)
	}
}
