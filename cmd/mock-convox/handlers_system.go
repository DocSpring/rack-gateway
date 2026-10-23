package main

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

func getInstances(w http.ResponseWriter, _ *http.Request) {
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

func getSystem(w http.ResponseWriter, _ *http.Request) {
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

func getSystemProcesses(w http.ResponseWriter, _ *http.Request) {
	procs := []Process{
		{
			ID:       "api-677dbf86db-699qf",
			App:      "system",
			Command:  "api",
			CPU:      10.0,
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
			ID:       "resolver-7c445f959c-l8t5p",
			App:      "system",
			Command:  "resolver",
			CPU:      5.0,
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
			ID:       "ingress-nginx-6bcbb5dbb4-5xbxx",
			App:      "system",
			Command:  "/nginx-ingress-controller ...",
			CPU:      15.0,
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
