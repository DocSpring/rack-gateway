// Mock Convox Rack API Server
//
// This mock server simulates the actual Convox rack API based on the real implementation
// found in the Convox source code at: ./reference/convox
//
// Key source files that define the API:
// - ./reference/convox/pkg/api/routes.go - Defines all API routes
// - ./reference/convox/pkg/api/controllers.go - Controller implementations
// - ./reference/convox/pkg/structs/process.go - Process struct definition
// - ./reference/convox/pkg/structs/system.go - System struct definition
// - ./reference/convox/sdk/sdk.go - SDK client that calls these endpoints
//
// Directory structure of Convox source:
// convox/
// ├── pkg/
// │   ├── api/
// │   │   ├── api.go - Main API server setup
// │   │   ├── routes.go - Route definitions (GET /apps, /system, /apps/{app}/processes, etc.)
// │   │   └── controllers.go - Handler implementations
// │   └── structs/
// │       ├── process.go - Process{Id, App, Command, Cpu, Memory, Status, etc.}
// │       └── system.go - System{Name, Provider, Region, Version, etc.}
// └── sdk/
//     └── sdk.go - Client that uses RACK_URL env var
//
// Important findings:
// 1. Convox uses Basic Auth with "convox" username and rack token as password
// 2. RACK_URL format: https://convox:token@api.domain.com (NO /v1/proxy suffix)
// 3. Main endpoints used by 'convox ps': GET /apps/{app}/processes
// 4. Main endpoint used by 'convox rack': GET /system
// 5. Process struct has specific field names (Id not ID, capitalized fields)
// 6. All responses are JSON arrays or objects
//
// Authentication:
// - Uses HTTP Basic Auth
// - Username is always "convox"
// - Password is the rack API token

package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
)

func main() {
	if shouldPrintHelp(os.Args) {
		printHelp()
		return
	}

	if err := runServer(); err != nil {
		log.Fatal(err)
	}
}

func shouldPrintHelp(args []string) bool {
	if len(args) <= 1 {
		return false
	}

	switch args[1] {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

func runServer() error {
	dir, err := os.MkdirTemp("", "mock-convox-objects-*")
	if err != nil {
		return fmt.Errorf("failed to create object storage directory: %w", err)
	}
	objectStorageDir = dir
	mclog.Infof("object storage directory: %s", objectStorageDir)

	router := newRouter()
	port := resolvePort()

	mclog.Infof("Mock Convox API server starting on port %s", port)
	auth := base64.StdEncoding.EncodeToString([]byte(mockUsername + ":" + mockPassword))
	mclog.DebugTopicf(mclog.TopicAuth, "expected auth: Basic %s", auth)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		return fmt.Errorf("server listen failed: %w", err)
	}
	return nil
}

func resolvePort() string {
	if port := os.Getenv("MOCK_CONVOX_PORT"); port != "" {
		return port
	}
	return "9090"
}

func newRouter() *mux.Router {
	router := mux.NewRouter()
	router.Use(requestLogger)
	router.Use(authMiddleware)

	registerAppRoutes(router)
	registerSystemRoutes(router)
	registerMiscRoutes(router)

	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mclog.Warnf("404 %s %s", r.Method, r.URL.String())
		http.NotFound(w, r)
	})

	return router
}

func registerAppRoutes(r *mux.Router) {
	r.HandleFunc("/apps", getApps).Methods("GET")
	r.HandleFunc("/apps/{app}", getApp).Methods("GET")
	r.HandleFunc("/apps", createApp).Methods("POST")
	r.HandleFunc("/apps/{app}", deleteApp).Methods("DELETE")

	r.HandleFunc("/apps/{app}/processes", getProcesses).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}", getProcess).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}/exec", execProcess).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}", deleteProcess).Methods("DELETE")

	r.HandleFunc("/apps/{app}/builds", getBuilds).Methods("GET")
	r.HandleFunc("/apps/{app}/builds/{id}", getBuild).Methods("GET")
	r.HandleFunc("/apps/{app}/builds", createBuild).Methods("POST")

	r.HandleFunc("/apps/{app}/releases", handleReleases).Methods("GET", "POST")
	r.HandleFunc("/apps/{app}/releases/{id}", getRelease).Methods("GET")
	r.HandleFunc("/apps/{app}/releases/{id}/promote", promoteRelease).Methods("POST")

	r.HandleFunc("/apps/{app}/environment", getEnvironment).Methods("GET")
	r.HandleFunc("/apps/{app}/environment", setEnvironment).Methods("POST", "PUT")

	r.HandleFunc("/apps/{app}/logs", appLogs).Methods("GET")
	r.HandleFunc("/apps/{app}/builds/{id}/logs", buildLogs).Methods("GET")

	r.HandleFunc("/apps/{app}/objects/tmp/{name}", uploadObject).Methods("POST")
	r.HandleFunc("/apps/{app}/objects/{key:.*}", downloadObject).Methods("GET")

	r.HandleFunc("/apps/{app}/restart", restartApp).Methods("POST")
	r.HandleFunc("/apps/{app}/services", listServices).Methods("GET")
	r.HandleFunc("/apps/{app}/services/{service}", updateService).Methods("PUT")
	r.HandleFunc("/apps/{app}/services/{service}/processes", serviceProcesses).Methods("POST", "GET")
	r.HandleFunc("/apps/{app}/services/{service}/restart", restartService).Methods("POST")
}

func registerSystemRoutes(r *mux.Router) {
	r.HandleFunc("/racks", listRacks).Methods("GET")
	r.HandleFunc("/instances", getInstances).Methods("GET")
	r.HandleFunc("/instances/{id}", getInstance).Methods("GET")
	r.HandleFunc("/system", getSystem).Methods("GET")
	r.HandleFunc("/system", putSystem).Methods("PUT")
	r.HandleFunc("/system/processes", getSystemProcesses).Methods("GET")
}

func registerMiscRoutes(r *mux.Router) {
	r.HandleFunc("/api/{path:.*}", handleAPI).Methods("GET", "POST", "PUT", "DELETE")
	r.HandleFunc("/health", health).Methods(http.MethodGet)
}

func health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]string{"status": "healthy", "server": "mock-convox"})
}

func printHelp() {
	fmt.Print(`Mock Convox Rack API Server

Starts an HTTP server that mimics a subset of the Convox Rack API used by our tests and E2E flows.

Usage:
  ./mock-convox                 # start server (port from $MOCK_CONVOX_PORT, default 9090)
  ./mock-convox help            # show this help

Auth:
  - HTTP Basic Auth required on all endpoints except /health
  - Username: "convox"
  - Password: mock-rack-token-12345

Endpoints:
  GET  /health                                  # server health
  GET  /system                                  # rack info

  GET  /apps                                    # list apps (rack-gateway, api, web)
  GET  /apps/{app}                              # app info

  GET    /apps/{app}/processes                  # list processes
  GET    /apps/{app}/processes/{id}             # process info
  GET    /apps/{app}/processes/{id}/exec        # mock exec (WebSocket)
  DELETE /apps/{app}/processes/{id}             # terminate process

  GET  /apps/{app}/builds                       # list builds
  GET  /apps/{app}/builds/{id}                  # build info

  GET  /apps/{app}/releases                     # list releases (newest first; ?limit=1 supported)
  POST /apps/{app}/releases                     # create release (returns single Release)
  GET  /apps/{app}/releases/{id}                # release info (includes Env string)
  POST /apps/{app}/releases/{id}/promote        # promote release (updates app current release)

  GET  /apps/{app}/logs                         # app logs (WebSocket; short stream then close)

  GET  /apps/{app}/environment                  # env (JSON map)
  POST /apps/{app}/environment                  # set/merge env (returns merged map)

  GET  /racks                                   # returns [] (some CLI flows call this)
`)
}
