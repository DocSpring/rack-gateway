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
// - Also supports JWT tokens with username "jwt"

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const (
	mockUsername = "convox"
	mockPassword = "mock-rack-token-12345"
)

func writeJSON(w http.ResponseWriter, payload interface{}) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("mock-convox: failed to encode JSON response: %v", err)
	}
}

func decodeRequest(body io.ReadCloser, dest interface{}) error {
	defer func() {
		_ = body.Close()
	}()
	return json.NewDecoder(body).Decode(dest)
}

type App struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Generation string    `json:"generation"`
	Release    string    `json:"release"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Process struct {
	Id       string    `json:"id"`
	App      string    `json:"app"`
	Command  string    `json:"command"`
	Cpu      float64   `json:"cpu"`
	Host     string    `json:"host"`
	Image    string    `json:"image"`
	Instance string    `json:"instance"`
	Memory   float64   `json:"memory"`
	Name     string    `json:"name"`
	Ports    []string  `json:"ports"`
	Release  string    `json:"release"`
	Started  time.Time `json:"started"`
	Status   string    `json:"status"`
}

type Build struct {
	ID          string    `json:"id"`
	App         string    `json:"app"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Release     string    `json:"release"`
	Started     time.Time `json:"started"`
	Ended       time.Time `json:"ended"`
}

type Release struct {
	ID          string    `json:"id"`
	App         string    `json:"app"`
	Build       string    `json:"build"`
	Description string    `json:"description"`
	Version     int       `json:"version"`
	Created     time.Time `json:"created"`
	Env         string    `json:"env,omitempty"`
}

type Instance struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	PrivateIP    string    `json:"private_ip"`
	PublicIP     string    `json:"public_ip"`
	Started      time.Time `json:"started"`
	InstanceType string    `json:"instance_type"`
}

type System struct {
	Count      int               `json:"count"`
	Domain     string            `json:"domain"`
	Name       string            `json:"name"`
	Outputs    map[string]string `json:"outputs,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Provider   string            `json:"provider"`
	RackDomain string            `json:"rack-domain"`
	Region     string            `json:"region"`
	Status     string            `json:"status"`
	Type       string            `json:"type"`
	Version    string            `json:"version"`
}

// In-memory state for releases and current app release
var (
	idCounter           atomic.Uint64
	currentReleaseByApp = map[string]string{
		"rack-gateway": "RAPP123456",
	}
	releasesByApp = map[string][]Release{
		"rack-gateway": {
			{ID: "RAPI123456", App: "rack-gateway", Build: "BAPI123456", Description: "Deployed by mock", Version: 10, Created: time.Now().Add(-24 * time.Hour), Env: envString()},
			{ID: "RAPI123455", App: "rack-gateway", Build: "BAPI123455", Description: "Deployed by mock", Version: 9, Created: time.Now().Add(-48 * time.Hour), Env: envString()},
		},
	}

	// In-memory, mutable rack parameters for GET/PUT /system
	mockSystemParameters = map[string]string{
		"access_log_retention_in_days": "7",
		"availability_zones":           "us-east-1a,us-east-1b,us-east-1d,us-east-1e,us-east-1f",
		"cidr":                         "10.2.0.0/16",
		"internal_router":              "false",
		"node_capacity_type":           "on_demand",
		"node_type":                    "t3a.large",
		"proxy_protocol":               "true",
		"schedule_rack_scale_down":     "30 9 * * *",
		"schedule_rack_scale_up":       "30 18 * * MON-THU",
	}
)

// nextID appends an incrementing counter to a base ID to make it unique
func nextID(base string) string {
	return fmt.Sprintf("%s-%04d", base, idCounter.Add(1))
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "--help", "-h":
			printHelp()
			return
		}
	}
	r := mux.NewRouter()
	r.Use(requestLogger)

	// Add authentication middleware
	r.Use(authMiddleware)

	// Mock Convox API endpoints
	r.HandleFunc("/apps", getApps).Methods("GET")
	r.HandleFunc("/apps/{app}", getApp).Methods("GET")
	r.HandleFunc("/apps", createApp).Methods("POST")
	r.HandleFunc("/apps/{app}", deleteApp).Methods("DELETE")

	r.HandleFunc("/apps/{app}/processes", getProcesses).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}", getProcess).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}/exec", execProcess).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}/stop", stopProcess).Methods("POST")

	r.HandleFunc("/apps/{app}/builds", getBuilds).Methods("GET")
	r.HandleFunc("/apps/{app}/builds/{id}", getBuild).Methods("GET")
	// Build creation endpoint needed by deploy flows
	r.HandleFunc("/apps/{app}/builds", createBuild).Methods("POST")

	r.HandleFunc("/apps/{app}/releases", handleReleases).Methods("GET", "POST")
	r.HandleFunc("/apps/{app}/releases/{id}", getRelease).Methods("GET")
	r.HandleFunc("/apps/{app}/releases/{id}/promote", promoteRelease).Methods("POST")

	r.HandleFunc("/apps/{app}/environment", getEnvironment).Methods("GET")
	r.HandleFunc("/apps/{app}/environment", setEnvironment).Methods("POST", "PUT")

	// Logs streaming (WebSocket)
	r.HandleFunc("/apps/{app}/logs", appLogs).Methods("GET")
	r.HandleFunc("/apps/{app}/builds/{id}/logs", buildLogs).Methods("GET")

	// Object upload used by deploy: POST /apps/{app}/objects/tmp/<name>.tgz
	r.HandleFunc("/apps/{app}/objects/tmp/{name}", uploadObject).Methods("POST")

	// Optional racks listing used by CLI in some flows
	r.HandleFunc("/racks", listRacks).Methods("GET")

	r.HandleFunc("/instances", getInstances).Methods("GET")
	r.HandleFunc("/instances/{id}", getInstance).Methods("GET")

	r.HandleFunc("/system", getSystem).Methods("GET")
	r.HandleFunc("/system", putSystem).Methods("PUT")
	r.HandleFunc("/system/processes", getSystemProcesses).Methods("GET")

	// Services command processes (stub for convox run)
	r.HandleFunc("/apps/{app}/restart", restartApp).Methods("POST")
	r.HandleFunc("/apps/{app}/services", listServices).Methods("GET")
	r.HandleFunc("/apps/{app}/services/{service}/processes", serviceProcesses).Methods("POST", "GET")
	r.HandleFunc("/apps/{app}/services/{service}/restart", restartService).Methods("POST")

	// Generic API endpoint for testing
	r.HandleFunc("/api/{path:.*}", handleAPI).Methods("GET", "POST", "PUT", "DELETE")

	// Health check (no auth required)
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "healthy", "server": "mock-convox"})
	}).Methods("GET")

	// Log 404s explicitly so unexpected routes are visible in logs
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("404 %s %s", r.Method, r.URL.String())
		http.NotFound(w, r)
	})

	port := os.Getenv("MOCK_CONVOX_PORT")
	if port == "" {
		port = "9090"
	}

	log.Printf("Mock Convox API server starting on port %s", port)
	log.Printf("Expected auth: Basic %s", base64.StdEncoding.EncodeToString([]byte(mockUsername+":"+mockPassword)))
	log.Fatal(http.ListenAndServe(":"+port, r))
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

  GET  /apps/{app}/processes                    # list processes
  GET  /apps/{app}/processes/{id}               # process info
  GET  /apps/{app}/processes/{id}/exec          # mock exec (WebSocket)
  POST /apps/{app}/processes/{id}/stop          # stop process (mock)

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

// requestLogger logs method, path, status and duration for every request
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Request: %s %s rawQuery=%q", r.Method, r.URL.Path, r.URL.RawQuery)
		if getBoolEnv("MOCK_CONVOX_LOG_HEADERS", true) {
			for k, vs := range r.Header {
				for _, v := range vs {
					log.Printf("[Header] %s: %s", k, v)
				}
			}
		}
		// Log request body for write methods unless it's an object upload (huge tarball)
		if getBoolEnv("MOCK_CONVOX_LOG_REQUEST_BODY", false) && r.Body != nil && !isObjectUploadPath(r.URL.Path) && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			// Read and restore body
			buf, err := io.ReadAll(r.Body)
			if err != nil {
				log.Printf("mock-convox: failed to read request body: %v", err)
			} else {
				if err := r.Body.Close(); err != nil {
					log.Printf("mock-convox: failed to close request body: %v", err)
				}
				r.Body = io.NopCloser(strings.NewReader(string(buf)))
				max := 4096
				preview := string(buf)
				if len(preview) > max {
					preview = preview[:max] + "…(truncated)"
				}
				if preview != "" {
					log.Printf("[Request Body] %d bytes: %s", len(buf), preview)
				} else {
					log.Printf("[Request Body] 0 bytes")
				}
			}
		}
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		if getBoolEnv("MOCK_CONVOX_LOG_RESPONSE_BODY", false) && !isObjectUploadPath(r.URL.Path) {
			// Log response body preview
			max := 4096
			preview := string(sr.body)
			if len(preview) > max {
				preview = preview[:max] + "…(truncated)"
			}
			log.Printf("[Response Body] %d bytes: %s", len(sr.body), preview)
		}
		log.Printf("Response: %d %s %s in %s", sr.status, r.Method, r.URL.String(), time.Since(start))
	})
}

// isObjectUploadPath returns true for deploy tarball uploads
func isObjectUploadPath(p string) bool {
	return strings.Contains(p, "/objects/tmp/")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(p []byte) (int, error) {
	// Copy to buffer for optional logging
	if len(p) > 0 {
		sr.body = append(sr.body, p...)
	}
	return sr.ResponseWriter.Write(p)
}

// Ensure WebSocket upgrades can hijack the connection
func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := sr.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijacker not supported")
}

// Pass-through flushing if supported
func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health endpoint
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mock Convox"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		const prefix = "Basic "
		if !strings.HasPrefix(auth, prefix) {
			http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}

		decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
		if err != nil {
			http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 || parts[0] != mockUsername || parts[1] != mockPassword {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getBoolEnv returns true if the env var parses to a truthy value, else defaultVal.
func getBoolEnv(name string, defaultVal bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return defaultVal
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func getApps(w http.ResponseWriter, r *http.Request) {
	apps := []App{
		{
			Name:       "rack-gateway",
			Status:     "running",
			Generation: "3",
			Release:    currentReleaseByApp["rack-gateway"],
			CreatedAt:  time.Now().Add(-720 * time.Hour),
			UpdatedAt:  time.Now().Add(-24 * time.Hour),
		},
		{
			Name:       "api",
			Status:     "running",
			Generation: "3",
			Release:    "RAPI123456",
			CreatedAt:  time.Now().Add(-720 * time.Hour),
			UpdatedAt:  time.Now().Add(-24 * time.Hour),
		},
		{
			Name:       "web",
			Status:     "running",
			Generation: "3",
			Release:    "RWEB789012",
			CreatedAt:  time.Now().Add(-480 * time.Hour),
			UpdatedAt:  time.Now().Add(-2 * time.Hour),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, apps)
}

func getApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rel := currentReleaseByApp[vars["app"]]
	if rel == "" {
		rel = "RAPP123456"
	}
	app := App{
		Name:       vars["app"],
		Status:     "running",
		Generation: "3",
		Release:    rel,
		CreatedAt:  time.Now().Add(-720 * time.Hour),
		UpdatedAt:  time.Now().Add(-24 * time.Hour),
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, app)
}

func serviceProcesses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	service := vars["service"]
	// Log query and body for debugging
	rawBody := ""
	if r.Body != nil {
		if b, err := io.ReadAll(r.Body); err == nil {
			rawBody = string(b)
		}
	}
	_ = r.Body.Close()
	log.Printf("SERVICE processes start app=%s service=%s query=%q body=%q", app, service, r.URL.RawQuery, rawBody)

	// Stub: return 202 Accepted with a fake process id; echo method
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]interface{}{
		"status":  "started",
		"method":  r.Method,
		"app":     app,
		"service": service,
		"id":      nextID("proc-123456"),
	})
}

func listServices(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	log.Printf("SERVICES list app=%s", app)
	services := []map[string]interface{}{
		{
			"name":    "web",
			"process": "web",
			"status":  "running",
		},
		{
			"name":    "worker",
			"process": "worker",
			"status":  "running",
		},
	}
	writeJSON(w, services)
}

func restartService(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	service := vars["service"]
	log.Printf("SERVICE restart app=%s service=%s", app, service)
	writeJSON(w, map[string]string{"status": "restarting"})
}

func restartApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	log.Printf("APP restart app=%s", app)
	writeJSON(w, map[string]string{"status": "restarting"})
}

// execProcess upgrades to a WebSocket and streams a short mock session, then closes
func execProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	log.Printf("EXEC request for app=%s pid=%s query=%q", app, id, r.URL.RawQuery)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
		// Accept client requested subprotocols to mimic k8s exec behavior
		Subprotocols: parseSubprotocols(r.Header.Get("Sec-WebSocket-Protocol")),
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("exec upgrade error: %v", err)
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	// Send a tiny session transcript
	_ = conn.WriteMessage(websocket.TextMessage, []byte("Connected to mock exec for app="+app+" pid="+id+"\n"))
	// Convox passes exec options via headers; support both header and query param
	cmd := r.Header.Get("command")
	if cmd == "" {
		cmd = r.URL.Query().Get("command")
	}
	if cmd != "" {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("$ "+cmd+"\n"))
		// Basic echo emulation for `echo <text>`
		if strings.HasPrefix(cmd, "echo ") {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(strings.TrimPrefix(cmd, "echo ")+"\n"))
		} else {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("(mock output)\n"))
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("Exit code: 0\n"))
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte("Session closed.\n"))
}

func parseSubprotocols(h string) []string {
	if h == "" {
		return nil
	}
	parts := strings.Split(h, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func createApp(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := decodeRequest(r.Body, &req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	app := App{
		Name:       req["name"],
		Status:     "creating",
		Generation: "3",
		Release:    "",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, app)
}

func deleteApp(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func getProcesses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	processes := []Process{
		{
			Id:       "p-web-1",
			App:      vars["app"],
			Command:  "bundle exec rails server",
			Cpu:      25.5,
			Host:     "10.0.1.10",
			Image:    "registry.example.com/app:latest",
			Instance: "i-1234567890abcdef0",
			Memory:   512.0,
			Name:     "web",
			Ports:    []string{"80:3000"},
			Release:  "RAPI123456",
			Started:  time.Now().Add(-3 * time.Hour),
			Status:   "running",
		},
		{
			Id:       "p-worker-1",
			App:      vars["app"],
			Command:  "bundle exec sidekiq",
			Cpu:      15.0,
			Host:     "10.0.1.11",
			Image:    "registry.example.com/app:latest",
			Instance: "i-0987654321fedcba0",
			Memory:   256.0,
			Name:     "worker",
			Ports:    []string{},
			Release:  "RAPI123456",
			Started:  time.Now().Add(-2 * time.Hour),
			Status:   "running",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, processes)
}

func getProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	process := Process{
		Id:       vars["id"],
		App:      vars["app"],
		Command:  "bundle exec rails server",
		Cpu:      25.5,
		Host:     "10.0.1.10",
		Image:    "registry.example.com/app:latest",
		Instance: "i-1234567890abcdef0",
		Memory:   512.0,
		Name:     "web",
		Ports:    []string{"80:3000"},
		Release:  "RAPI123456",
		Started:  time.Now().Add(-3 * time.Hour),
		Status:   "running",
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, process)
}

func stopProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{
		"id":     vars["id"],
		"status": "stopping",
	})
}

func getBuilds(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// Create a recent sample build with a fixed elapsed time of 2m30s
	startRecent := time.Now().Add(-30 * time.Minute)
	endRecent := startRecent.Add(150 * time.Second) // 2m30s
	builds := []Build{
		{
			ID:          "BAPI123456",
			App:         vars["app"],
			Description: "git sha: abc123",
			Status:      "complete",
			Release:     "RAPI123456",
			Started:     startRecent,
			Ended:       endRecent,
		},
		{
			ID:          "BAPI123455",
			App:         vars["app"],
			Description: "git sha: def456",
			Status:      "complete",
			Release:     "RAPI123455",
			Started:     time.Now().Add(-49 * time.Hour),
			Ended:       time.Now().Add(-48 * time.Hour),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, builds)
}

func getBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// Match the sample elapsed of 2m30s for demonstration when looking up an individual build
	startRecent := time.Now().Add(-30 * time.Minute)
	endRecent := startRecent.Add(150 * time.Second)
	build := Build{
		ID:          vars["id"],
		App:         vars["app"],
		Description: "git sha: abc123",
		Status:      "complete",
		Release:     "R" + vars["id"][1:],
		Started:     startRecent,
		Ended:       endRecent,
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, build)
}

// createBuild simulates POST /apps/{app}/builds and returns a new Build
func createBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	baseID := fmt.Sprintf("BNEW%06d", time.Now().UnixNano()%1000000)
	id := nextID(baseID)
	build := Build{
		ID:          id,
		App:         app,
		Description: "created by mock",
		Status:      "running",
		Release:     "R" + id[1:],
		Started:     time.Now(),
	}
	// Optionally prepend to builds list to make it visible in GET /builds
	// (safe even if app key not present)
	builds := []Build{build}
	if existing := releasesByApp[app]; existing != nil {
		_ = existing // no-op; builds list is separate; keep minimal behavior
	}
	_ = builds // satisfy linters; currently not used elsewhere

	w.Header().Set("Content-Type", "application/json")
	// Real API often returns 201 Created
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, build)
}

// createBuild simulates creating a new build for an app
// (intentionally no build creation/import or build logs stubs; tests use app logs and releases)

func handleReleases(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	if r.Method == http.MethodPost {
		// Create a new release and append to store
		baseID := fmt.Sprintf("RAPI%06d", time.Now().UnixNano()%1000000)
		id := nextID(baseID)
		rel := Release{
			ID:          id,
			App:         app,
			Build:       nextID("BNEW123456"),
			Description: "Created by mock env set",
			Version:     42,
			Created:     time.Now(),
			Env:         envString(),
		}
		// Prepend so newest release is first
		releasesByApp[app] = append([]Release{rel}, releasesByApp[app]...)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, rel)
		return
	}
	// GET: list releases (support limit=1)
	list := releasesByApp[app]
	if list == nil {
		list = []Release{}
	}
	if lim := r.URL.Query().Get("limit"); lim == "1" && len(list) > 0 {
		// Return most recent (index 0)
		list = []Release{list[0]}
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, list)
}

func getRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	var rel Release
	found := false
	for _, rr := range releasesByApp[app] {
		if rr.ID == id {
			rel = rr
			found = true
			break
		}
	}
	if !found {
		rel = Release{ID: id, App: app, Build: "B" + id[1:], Description: "Deployed by mock", Version: 10, Created: time.Now().Add(-24 * time.Hour)}
	}
	if rel.Env == "" {
		rel.Env = envString()
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, rel)
}

func promoteRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	currentReleaseByApp[app] = id
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{"id": id, "status": "promoting"})
}

// listRacks returns an empty list to satisfy CLI calls
func listRacks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("[]")); err != nil {
		log.Printf("mock-convox: failed to write racks response: %v", err)
	}
}

// appLogs upgrades to WebSocket and streams a few log lines, then closes
func appLogs(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = c.Close()
	}()

	// Emit a couple of lines immediately like a real deploy/log stream
	_ = c.WriteMessage(websocket.TextMessage, []byte("Promoting release...\n"))
	time.Sleep(100 * time.Millisecond)
	_ = c.WriteMessage(websocket.TextMessage, []byte("Release promoted successfully.\n"))

	// Keep the connection open to mimic tail -f behavior.
	// Send periodic ping frames to prevent idle timeouts and wait until client disconnects.
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			// Read to detect client close; ignore content
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-ping.C:
			_ = c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
		}
	}
}

// buildLogs upgrades to WebSocket and streams build logs, then closes
func buildLogs(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = c.Close()
	}()

	_ = c.WriteMessage(websocket.TextMessage, []byte("Building app...\n"))
	time.Sleep(100 * time.Millisecond)
	_ = c.WriteMessage(websocket.TextMessage, []byte("Step 1/1: mock build step\n"))
	time.Sleep(100 * time.Millisecond)
	_ = c.WriteMessage(websocket.TextMessage, []byte("Build complete\n"))
	_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func getEnvironment(w http.ResponseWriter, r *http.Request) {
	env := defaultEnvMap()

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, env)
}

func setEnvironment(w http.ResponseWriter, r *http.Request) {
	var env map[string]string
	if err := decodeRequest(r.Body, &env); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if env == nil {
		env = map[string]string{}
	}
	// Merge with defaults for predictability
	merged := defaultEnvMap()
	for k, v := range env {
		merged[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, merged)
}

// uploadObject accepts a tarball upload for deploy and returns 200 OK
func uploadObject(w http.ResponseWriter, r *http.Request) {
	// Drain body to simulate upload and avoid connection reuse issues
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		log.Printf("mock-convox: failed to drain upload body: %v", err)
	}
	if err := r.Body.Close(); err != nil {
		log.Printf("mock-convox: failed to close upload body: %v", err)
	}
	// Return a minimal JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{"status": "uploaded"})
}

// Helpers
func defaultEnvMap() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"REDIS_URL":    "redis://localhost:6379",
		"SECRET_KEY":   "super-secret-key-12345",
		"NODE_ENV":     "production",
		"PORT":         "3000",
	}
}

func envString() string {
	// Return a deterministic order for tests
	env := defaultEnvMap()
	order := []string{"DATABASE_URL", "REDIS_URL", "SECRET_KEY", "NODE_ENV", "PORT"}
	var b strings.Builder
	for _, k := range order {
		fmt.Fprintf(&b, "%s=%s\n", k, env[k])
	}
	return b.String()
}

func getInstances(w http.ResponseWriter, r *http.Request) {
	instances := []Instance{
		{
			ID:           "i-1234567890abcdef0",
			Status:       "running",
			PrivateIP:    "10.0.1.10",
			PublicIP:     "54.123.45.67",
			Started:      time.Now().Add(-720 * time.Hour),
			InstanceType: "t3.medium",
		},
		{
			ID:           "i-0987654321fedcba0",
			Status:       "running",
			PrivateIP:    "10.0.1.11",
			PublicIP:     "54.123.45.68",
			Started:      time.Now().Add(-480 * time.Hour),
			InstanceType: "t3.medium",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, instances)
}

func getInstance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instance := Instance{
		ID:           vars["id"],
		Status:       "running",
		PrivateIP:    "10.0.1.10",
		PublicIP:     "54.123.45.67",
		Started:      time.Now().Add(-720 * time.Hour),
		InstanceType: "t3.medium",
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, instance)
}

func getSystem(w http.ResponseWriter, r *http.Request) {
	system := System{
		Count:      2,
		Domain:     "mock-rack.example.com",
		Name:       "mock-rack",
		Provider:   "aws",
		RackDomain: "rack.mock-rack.example.com",
		Region:     "us-east-1",
		Status:     "running",
		Type:       "production",
		Version:    "3.5.0",
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, system)
}

// putSystem accepts parameter updates similar to `convox rack params set`.
// It accepts either JSON bodies like {"parameters":{"k":"v"}} or a flat JSON map {"k":"v"},
// and also tolerates application/x-www-form-urlencoded such as "proxy_protocol=false".
func putSystem(w http.ResponseWriter, r *http.Request) {
	// Read full body (it can be small)
	var body []byte
	if r.Body != nil {
		if b, err := io.ReadAll(r.Body); err == nil {
			body = b
		} else {
			log.Printf("mock-convox: failed to read system body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			log.Printf("mock-convox: failed to close system body: %v", err)
		}
	}

	updated := 0
	ct := strings.ToLower(r.Header.Get("Content-Type"))

	// Try JSON first when content-type indicates JSON or looks like JSON
	tryJSON := strings.Contains(ct, "application/json") || (len(body) > 0 && (body[0] == '{' || body[0] == '['))
	if tryJSON && len(body) > 0 {
		var any map[string]interface{}
		if err := json.Unmarshal(body, &any); err == nil {
			// Prefer nested {parameters:{...}}
			if pv, ok := any["parameters"].(map[string]interface{}); ok {
				for k, v := range pv {
					sval := fmt.Sprintf("%v", v)
					mockSystemParameters[k] = sval
					updated++
				}
			} else {
				// Flat map
				for k, v := range any {
					sval := fmt.Sprintf("%v", v)
					mockSystemParameters[k] = sval
					updated++
				}
			}
		}
	}

	// If nothing updated yet and body exists, attempt to parse as form-encoded
	if updated == 0 && len(body) > 0 {
		if vals, err := url.ParseQuery(string(body)); err == nil {
			// Accept either direct keys (k=v) or a serialized parameters JSON in a field named "parameters"
			if pjson := vals.Get("parameters"); pjson != "" {
				var m map[string]interface{}
				if err := json.Unmarshal([]byte(pjson), &m); err == nil {
					for k, v := range m {
						mockSystemParameters[k] = fmt.Sprintf("%v", v)
						updated++
					}
				} else {
					// Not JSON; attempt to parse as querystring like "k=v&k2=v2"
					if pvals, err2 := url.ParseQuery(pjson); err2 == nil {
						for pk, pvs := range pvals {
							if len(pvs) == 0 {
								continue
							}
							mockSystemParameters[pk] = pvs[len(pvs)-1]
							updated++
						}
					}
				}
			}
			// Handle bracketed array pairs: params[0][name]=k & params[0][value]=v
			indexToName := map[string]string{}
			indexToValue := map[string]string{}
			for k, vs := range vals {
				if k == "parameters" { // handled above
					continue
				}
				if len(vs) == 0 {
					continue
				}
				v := vs[len(vs)-1]
				// parameters[key]=value form
				if strings.HasPrefix(k, "parameters[") && strings.HasSuffix(k, "]") {
					name := k[len("parameters[") : len(k)-1]
					if name != "" {
						mockSystemParameters[name] = v
						updated++
						continue
					}
				}
				// params[0][name] or params[0][key]
				if strings.HasPrefix(k, "params[") && strings.HasSuffix(k, "][name]") {
					idx := k[len("params[") : len(k)-len("][name]")]
					indexToName[idx] = v
					continue
				}
				if strings.HasPrefix(k, "params[") && strings.HasSuffix(k, "][key]") {
					idx := k[len("params[") : len(k)-len("][key]")]
					indexToName[idx] = v
					continue
				}
				if strings.HasPrefix(k, "params[") && strings.HasSuffix(k, "][value]") {
					idx := k[len("params[") : len(k)-len("][value]")]
					indexToValue[idx] = v
					continue
				}
				// Direct k=v
				mockSystemParameters[k] = v
				updated++
			}
			for idx, name := range indexToName {
				if val, ok := indexToValue[idx]; ok {
					mockSystemParameters[name] = val
					updated++
				}
			}
		}
	}

	// Last-resort fallback: parse simple ampersand-separated pairs without full encoding.
	// If present, also attempt to interpret a bare 'parameters=' payload as querystring.
	if updated == 0 && len(body) > 0 {
		raw := string(body)
		if strings.HasPrefix(raw, "parameters=") {
			pv := strings.TrimPrefix(raw, "parameters=")
			if pvals, err := url.ParseQuery(pv); err == nil {
				for pk, pvs := range pvals {
					if len(pvs) == 0 {
						continue
					}
					mockSystemParameters[pk] = pvs[len(pvs)-1]
					updated++
				}
			}
		} else {
			parts := strings.Split(raw, "&")
			for _, p := range parts {
				if p == "" {
					continue
				}
				kv := strings.SplitN(p, "=", 2)
				if len(kv) != 2 {
					continue
				}
				k, _ := url.QueryUnescape(kv[0])
				v, _ := url.QueryUnescape(kv[1])
				if k == "" {
					continue
				}
				mockSystemParameters[k] = v
				updated++
			}
		}
	}

	// Respond with updated system state
	w.Header().Set("Content-Type", "application/json")
	// Match real API behavior: 200 OK
	// Reuse getSystem composition
	getSystem(w, r)
}

func getSystemProcesses(w http.ResponseWriter, r *http.Request) {
	// Return a small set of system processes similar to real racks
	procs := []Process{
		{
			Id:       "api-677dbf86db-699qf",
			App:      "system",
			Command:  "api",
			Cpu:      10.0,
			Host:     "10.0.0.10",
			Image:    "convox/api:latest",
			Instance: "i-1234567890abcdef0",
			Memory:   256.0,
			Name:     "api",
			Ports:    []string{"5443:5443"},
			Release:  "3.21.3",
			Started:  time.Now().Add(-7 * 24 * time.Hour),
			Status:   "running",
		},
		{
			Id:       "resolver-7c445f959c-l8t5p",
			App:      "system",
			Command:  "resolver",
			Cpu:      5.0,
			Host:     "10.0.0.11",
			Image:    "convox/resolver:latest",
			Instance: "i-0987654321fedcba0",
			Memory:   128.0,
			Name:     "resolver",
			Ports:    []string{},
			Release:  "3.21.3",
			Started:  time.Now().Add(-7 * 24 * time.Hour),
			Status:   "running",
		},
		{
			Id:       "ingress-nginx-6bcbb5dbb4-5xbxx",
			App:      "system",
			Command:  "/nginx-ingress-controller ...",
			Cpu:      15.0,
			Host:     "10.0.0.12",
			Image:    "nginx/ingress-controller:latest",
			Instance: "i-0abcdeffedcba9876",
			Memory:   256.0,
			Name:     "ingress-nginx",
			Ports:    []string{"80:80", "443:443"},
			Release:  "3.21.3",
			Started:  time.Now().Add(-7 * 24 * time.Hour),
			Status:   "running",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, procs)
}

// parameters are embedded in GET /system in the mock

func handleAPI(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	response := map[string]interface{}{
		"path":      vars["path"],
		"method":    r.Method,
		"mock":      true,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Log the request for debugging
	log.Printf("%s %s", r.Method, r.URL.Path)

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, response)
}
