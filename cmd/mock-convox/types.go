package main

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	mockUsername = "convox"
	mockPassword = "mock-rack-token-12345" //nolint:gosec // G101: Mock credential for testing only
)

type App struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Generation string    `json:"generation"`
	Release    string    `json:"release"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Service struct {
	Name   string        `json:"name"`
	Count  int           `json:"count"`
	CPU    int           `json:"cpu"`
	Domain string        `json:"domain"`
	Memory int           `json:"memory"`
	Ports  []ServicePort `json:"ports"`
}

type ServicePort struct {
	Balancer    int    `json:"balancer"`
	Certificate string `json:"certificate"`
	Container   int    `json:"container"`
}

type Process struct {
	Id       string    `json:"id"` //nolint:staticcheck // Convox API contract
	App      string    `json:"app"`
	Command  string    `json:"command"`
	Cpu      float64   `json:"cpu"` //nolint:staticcheck // Convox API contract
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

var (
	idCounter           atomic.Uint64
	serviceStateMu      sync.Mutex
	currentReleaseByApp = map[string]string{
		"rack-gateway": "RAPP123456",
	}
	releasesByApp = map[string][]Release{
		"rack-gateway": {
			{
				ID:          "RAPI123456",
				App:         "rack-gateway",
				Build:       "BAPI123456",
				Description: "Deployed by mock",
				Version:     10,
				Created:     time.Now().Add(-24 * time.Hour),
				Env:         envString(),
			},
			{
				ID:          "RAPI123455",
				App:         "rack-gateway",
				Build:       "BAPI123455",
				Description: "Deployed by mock",
				Version:     9,
				Created:     time.Now().Add(-48 * time.Hour),
				Env:         envString(),
			},
		},
	}
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
	objectStorageDir string
)
