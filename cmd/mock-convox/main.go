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
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const (
	mockUsername = "convox"
	mockPassword = "mock-rack-token-12345"
)

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
	currentReleaseByApp = map[string]string{
		"convox-gateway": "RAPP123456",
	}
	releasesByApp = map[string][]Release{
		"convox-gateway": {
			{ID: "RAPI123456", App: "convox-gateway", Build: "BAPI123456", Description: "Deployed by mock", Version: 10, Created: time.Now().Add(-24 * time.Hour), Env: envString()},
			{ID: "RAPI123455", App: "convox-gateway", Build: "BAPI123455", Description: "Deployed by mock", Version: 9, Created: time.Now().Add(-48 * time.Hour), Env: envString()},
		},
	}
)

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

	r.HandleFunc("/apps/{app}/releases", handleReleases).Methods("GET", "POST")
	r.HandleFunc("/apps/{app}/releases/{id}", getRelease).Methods("GET")
	r.HandleFunc("/apps/{app}/releases/{id}/promote", promoteRelease).Methods("POST")

	r.HandleFunc("/apps/{app}/environment", getEnvironment).Methods("GET")
	r.HandleFunc("/apps/{app}/environment", setEnvironment).Methods("POST", "PUT")

	// Logs streaming (WebSocket)
	r.HandleFunc("/apps/{app}/logs", appLogs).Methods("GET")

	// Optional racks listing used by CLI in some flows
	r.HandleFunc("/racks", listRacks).Methods("GET")

	r.HandleFunc("/instances", getInstances).Methods("GET")
	r.HandleFunc("/instances/{id}", getInstance).Methods("GET")

	r.HandleFunc("/system", getSystem).Methods("GET")

	// Services command processes (stub for convox run)
	r.HandleFunc("/apps/{app}/services/{service}/processes", serviceProcesses).Methods("POST", "GET")

	// Generic API endpoint for testing
	r.HandleFunc("/api/{path:.*}", handleAPI).Methods("GET", "POST", "PUT", "DELETE")

	// Health check (no auth required)
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "server": "mock-convox"})
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
  
  GET  /apps                                    # list apps (convox-gateway, api, web)
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
		// Log request body for write methods, but cap size to avoid noise
		if getBoolEnv("MOCK_CONVOX_LOG_REQUEST_BODY", false) && r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			// Read and restore body
			buf, _ := io.ReadAll(r.Body)
			r.Body.Close()
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
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		if getBoolEnv("MOCK_CONVOX_LOG_RESPONSE_BODY", false) {
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
			Name:       "convox-gateway",
			Status:     "running",
			Generation: "3",
			Release:    currentReleaseByApp["convox-gateway"],
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
	json.NewEncoder(w).Encode(apps)
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
	json.NewEncoder(w).Encode(app)
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "started",
		"method":  r.Method,
		"app":     app,
		"service": service,
		"id":      "proc-123456",
	})
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
	defer conn.Close()

	// Send a tiny session transcript
	_ = conn.WriteMessage(websocket.TextMessage, []byte("Connected to mock exec for app="+app+" pid="+id+"\n"))
	// If a command was provided on query, echo it (some clients may do this)
	if cmd := r.URL.Query().Get("command"); cmd != "" {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("$ "+cmd+"\n"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("(mock output)\n"))
	}

	_ = conn.WriteMessage(websocket.TextMessage, []byte("Session closing.\n"))
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
	json.NewDecoder(r.Body).Decode(&req)

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
	json.NewEncoder(w).Encode(app)
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
	json.NewEncoder(w).Encode(processes)
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
	json.NewEncoder(w).Encode(process)
}

func stopProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":     vars["id"],
		"status": "stopping",
	})
}

func getBuilds(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	builds := []Build{
		{
			ID:          "BAPI123456",
			App:         vars["app"],
			Description: "git sha: abc123",
			Status:      "complete",
			Release:     "RAPI123456",
			Started:     time.Now().Add(-25 * time.Hour),
			Ended:       time.Now().Add(-24 * time.Hour),
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
	json.NewEncoder(w).Encode(builds)
}

func getBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	build := Build{
		ID:          vars["id"],
		App:         vars["app"],
		Description: "git sha: abc123",
		Status:      "complete",
		Release:     "R" + vars["id"][1:],
		Started:     time.Now().Add(-25 * time.Hour),
		Ended:       time.Now().Add(-24 * time.Hour),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(build)
}

func handleReleases(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	if r.Method == http.MethodPost {
		// Create a new release and append to store
		id := fmt.Sprintf("RAPI%06d", time.Now().UnixNano()%1000000)
		rel := Release{
			ID:          id,
			App:         app,
			Build:       "BNEW123456",
			Description: "Created by mock env set",
			Version:     42,
			Created:     time.Now(),
			Env:         envString(),
		}
		// Prepend so newest release is first
		releasesByApp[app] = append([]Release{rel}, releasesByApp[app]...)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rel)
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
	json.NewEncoder(w).Encode(list)
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
	json.NewEncoder(w).Encode(rel)
}

func createRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// In real Convox, creating a release returns a structs.Release object
	rel := Release{
		ID:          fmt.Sprintf("RAPI%06d", time.Now().Unix()%1000000),
		App:         vars["app"],
		Build:       "BNEW123456",
		Description: "Created by mock env set",
		Version:     42,
		Created:     time.Now(),
		Env:         envString(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rel)
}

func promoteRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	currentReleaseByApp[app] = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "promoting"})
}

// listRacks returns an empty list to satisfy CLI calls
func listRacks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("[]"))
}

// appLogs upgrades to WebSocket and streams a few log lines, then closes
func appLogs(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer c.Close()
	// Stream a couple of lines quickly then close
	_ = c.WriteMessage(websocket.TextMessage, []byte("Promoting release..."))
	time.Sleep(100 * time.Millisecond)
	_ = c.WriteMessage(websocket.TextMessage, []byte("Release promoted successfully."))
	// Close the WS cleanly
	_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func getEnvironment(w http.ResponseWriter, r *http.Request) {
	env := defaultEnvMap()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

func setEnvironment(w http.ResponseWriter, r *http.Request) {
	var env map[string]string
	json.NewDecoder(r.Body).Decode(&env)
	if env == nil {
		env = map[string]string{}
	}
	// Merge with defaults for predictability
	merged := defaultEnvMap()
	for k, v := range env {
		merged[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(merged)
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
	json.NewEncoder(w).Encode(instances)
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
	json.NewEncoder(w).Encode(instance)
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
	json.NewEncoder(w).Encode(system)
}

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
	json.NewEncoder(w).Encode(response)
}
