package main

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

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

	_ = c.WriteMessage(websocket.TextMessage, []byte("Promoting release...\n"))
	time.Sleep(100 * time.Millisecond)
	_ = c.WriteMessage(websocket.TextMessage, []byte("Release promoted successfully.\n"))

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
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
