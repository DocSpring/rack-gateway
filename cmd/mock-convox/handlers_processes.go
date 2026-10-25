package main

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
	"github.com/DocSpring/rack-gateway/internal/cmdenv"
)

func getProcesses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	processes := listProcessState(vars["app"])
	slices.SortFunc(processes, func(a, b Process) int {
		if a.Name == b.Name {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Name, b.Name)
	})

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, processes)
}

func getProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	process := Process{
		ID:       vars["id"],
		App:      vars["app"],
		Command:  "bundle exec rails server",
		CPU:      25.5,
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

func deleteProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	mclog.DebugTopicf(mclog.TopicAppProcesses, "delete process app=%s id=%s", vars["app"], vars["id"])
	if !stopProcessState(vars["app"], vars["id"]) {
		http.Error(w, "process not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{
		"id":     vars["id"],
		"status": "stopping",
	})
}

func serviceProcesses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	service := vars["service"]
	rawBody := ""
	if r.Body != nil {
		if b, err := io.ReadAll(r.Body); err == nil {
			rawBody = string(b)
		}
	}
	_ = r.Body.Close()
	mclog.DebugTopicf(
		mclog.TopicAppProcesses,
		"start app=%s service=%s query=%q body=%q",
		app,
		service,
		r.URL.RawQuery,
		rawBody,
	)

	// A rack runs whatever command it gets. This one refuses a command the gateway didn't wrap (cmdenv), so the
	// CLI E2E tests catch a command reaching the rack with every env var.
	if _, _, ok := cmdenv.Parse(r.Header.Get("Command")); !ok {
		http.Error(w, "mock rack: the gateway didn't wrap the command", http.StatusBadRequest)
		return
	}

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

func execProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	mclog.DebugTopicf(mclog.TopicAppProcesses, "exec request app=%s pid=%s query=%q", app, id, r.URL.RawQuery)

	upgrader := websocket.Upgrader{
		CheckOrigin:  func(_ *http.Request) bool { return true },
		Subprotocols: parseSubprotocols(r.Header.Get("Sec-WebSocket-Protocol")),
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		mclog.Errorf("exec upgrade error: %v", err)
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	_ = conn.WriteMessage(websocket.TextMessage, []byte("Connected to mock exec for app="+app+" pid="+id+"\n"))
	// The rack runs the command the gateway wrapped (cmdenv): this one prints the command and the env vars it gets.
	// A command the gateway didn't wrap fails, so the CLI E2E tests catch it.
	plan, cmd, ok := cmdenv.Parse(r.Header.Get("Command"))
	if !ok {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("mock rack: the gateway didn't wrap the command\n"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(execExitMarker+"1\n"))
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte("$ "+cmd+"\n"))
	_ = conn.WriteMessage(websocket.TextMessage, []byte("env: "+plan.Describe()+"\n"))
	if strings.HasPrefix(cmd, "echo ") {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(strings.TrimPrefix(cmd, "echo ")+"\n"))
	} else {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("(mock output)\n"))
	}
	// A rack ends every exec stream with this marker and the exit code. The CLI strips the line, exits with the
	// code, and fails when the marker is missing.
	_ = conn.WriteMessage(websocket.TextMessage, []byte(execExitMarker+"0\n"))
}
