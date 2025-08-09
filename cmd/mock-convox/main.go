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
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

const (
	mockUsername = "convox"
	mockPassword = "mock-rack-token-12345"
)

type App struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Generation int       `json:"generation"`
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

func main() {
	r := mux.NewRouter()

	// Add authentication middleware
	r.Use(authMiddleware)

	// Mock Convox API endpoints
	r.HandleFunc("/apps", getApps).Methods("GET")
	r.HandleFunc("/apps/{app}", getApp).Methods("GET")
	r.HandleFunc("/apps", createApp).Methods("POST")
	r.HandleFunc("/apps/{app}", deleteApp).Methods("DELETE")

	r.HandleFunc("/apps/{app}/processes", getProcesses).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}", getProcess).Methods("GET")
	r.HandleFunc("/apps/{app}/processes/{id}/stop", stopProcess).Methods("POST")

	r.HandleFunc("/apps/{app}/builds", getBuilds).Methods("GET")
	r.HandleFunc("/apps/{app}/builds/{id}", getBuild).Methods("GET")

	r.HandleFunc("/apps/{app}/releases", getReleases).Methods("GET")
	r.HandleFunc("/apps/{app}/releases/{id}", getRelease).Methods("GET")
	r.HandleFunc("/apps/{app}/releases/{id}/promote", promoteRelease).Methods("POST")

	r.HandleFunc("/apps/{app}/environment", getEnvironment).Methods("GET")
	r.HandleFunc("/apps/{app}/environment", setEnvironment).Methods("POST", "PUT")

	r.HandleFunc("/instances", getInstances).Methods("GET")
	r.HandleFunc("/instances/{id}", getInstance).Methods("GET")

	r.HandleFunc("/system", getSystem).Methods("GET")

	// Generic API endpoint for testing
	r.HandleFunc("/api/{path:.*}", handleAPI).Methods("GET", "POST", "PUT", "DELETE")

	// Health check (no auth required)
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "server": "mock-convox"})
	}).Methods("GET")

	port := os.Getenv("MOCK_CONVOX_PORT")
	if port == "" {
		port = "9090"
	}

	log.Printf("Mock Convox API server starting on port %s", port)
	log.Printf("Expected auth: Basic %s", base64.StdEncoding.EncodeToString([]byte(mockUsername+":"+mockPassword)))
	log.Fatal(http.ListenAndServe(":"+port, r))
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

func getApps(w http.ResponseWriter, r *http.Request) {
	apps := []App{
		{
			Name:       "api",
			Status:     "running",
			Generation: 3,
			Release:    "RAPI123456",
			CreatedAt:  time.Now().Add(-720 * time.Hour),
			UpdatedAt:  time.Now().Add(-24 * time.Hour),
		},
		{
			Name:       "web",
			Status:     "running",
			Generation: 3,
			Release:    "RWEB789012",
			CreatedAt:  time.Now().Add(-480 * time.Hour),
			UpdatedAt:  time.Now().Add(-2 * time.Hour),
		},
		{
			Name:       "worker",
			Status:     "updating",
			Generation: 3,
			Release:    "RWRK345678",
			CreatedAt:  time.Now().Add(-240 * time.Hour),
			UpdatedAt:  time.Now().Add(-5 * time.Minute),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apps)
}

func getApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := App{
		Name:       vars["app"],
		Status:     "running",
		Generation: 3,
		Release:    "R" + strings.ToUpper(vars["app"][:3]) + "123456",
		CreatedAt:  time.Now().Add(-720 * time.Hour),
		UpdatedAt:  time.Now().Add(-24 * time.Hour),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(app)
}

func createApp(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	json.NewDecoder(r.Body).Decode(&req)

	app := App{
		Name:       req["name"],
		Status:     "creating",
		Generation: 3,
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

func getReleases(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	releases := []Release{
		{
			ID:          "RAPI123456",
			App:         vars["app"],
			Build:       "BAPI123456",
			Description: "Deployed by mock",
			Version:     10,
			Created:     time.Now().Add(-24 * time.Hour),
		},
		{
			ID:          "RAPI123455",
			App:         vars["app"],
			Build:       "BAPI123455",
			Description: "Deployed by mock",
			Version:     9,
			Created:     time.Now().Add(-48 * time.Hour),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(releases)
}

func getRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	release := Release{
		ID:          vars["id"],
		App:         vars["app"],
		Build:       "B" + vars["id"][1:],
		Description: "Deployed by mock",
		Version:     10,
		Created:     time.Now().Add(-24 * time.Hour),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(release)
}

func promoteRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":     vars["id"],
		"status": "promoting",
	})
}

func getEnvironment(w http.ResponseWriter, r *http.Request) {
	env := map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"REDIS_URL":    "redis://localhost:6379",
		"SECRET_KEY":   "super-secret-key-12345",
		"NODE_ENV":     "production",
		"PORT":         "3000",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

func setEnvironment(w http.ResponseWriter, r *http.Request) {
	var env map[string]string
	json.NewDecoder(r.Body).Decode(&env)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
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
	log.Printf("Mock API: %s %s", r.Method, r.URL.Path)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
