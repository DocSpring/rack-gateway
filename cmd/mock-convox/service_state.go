package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	serviceTemplatesByApp = map[string][]Service{
		"rack-gateway": {
			{Name: "web", Count: 3, CPU: 256, Memory: 512},
			{Name: "worker", Count: 3, CPU: 256, Memory: 512},
			{Name: "worker-gj", Count: 1, CPU: 256, Memory: 512},
		},
		"api": {
			{Name: "web", Count: 2, CPU: 256, Memory: 512},
			{Name: "worker", Count: 1, CPU: 256, Memory: 512},
		},
		"web": {
			{Name: "web", Count: 2, CPU: 256, Memory: 512},
		},
	}
	servicesByApp  = cloneServiceState(serviceTemplatesByApp)
	processesByApp = buildInitialProcesses(servicesByApp)
)

func cloneServiceState(src map[string][]Service) map[string][]Service {
	out := make(map[string][]Service, len(src))
	for app, services := range src {
		out[app] = append([]Service(nil), services...)
	}
	return out
}

func buildInitialProcesses(services map[string][]Service) map[string][]Process {
	out := make(map[string][]Process, len(services))
	for app, appServices := range services {
		out[app] = buildProcessesForApp(app, appServices)
	}
	return out
}

func listServiceState(app string) []Service {
	serviceStateMu.Lock()
	defer serviceStateMu.Unlock()

	return cloneServicesForApp(app)
}

func listProcessState(app string) []Process {
	serviceStateMu.Lock()
	defer serviceStateMu.Unlock()

	return cloneProcessesForApp(app)
}

func updateServiceState(app, serviceName string, query map[string][]string) (Service, error) {
	serviceStateMu.Lock()
	defer serviceStateMu.Unlock()

	services := ensureAppServicesLocked(app)
	processes := ensureAppProcessesLocked(app)

	index := slices.IndexFunc(services, func(service Service) bool {
		return service.Name == serviceName
	})
	if index == -1 {
		return Service{}, fmt.Errorf("service %q not found", serviceName)
	}

	service := services[index]

	if count, ok, err := parseOptionalInt(query, "count"); err != nil {
		return Service{}, err
	} else if ok {
		service.Count = count
	}

	if cpu, ok, err := parseOptionalInt(query, "cpu"); err != nil {
		return Service{}, err
	} else if ok {
		service.CPU = cpu
	}

	if memory, ok, err := parseOptionalInt(query, "memory"); err != nil {
		return Service{}, err
	} else if ok {
		service.Memory = memory
	}

	services[index] = service
	servicesByApp[app] = services
	processesByApp[app] = resizeServiceProcesses(app, processes, service.Name, service.Count)

	return service, nil
}

func stopProcessState(app, processID string) bool {
	serviceStateMu.Lock()
	defer serviceStateMu.Unlock()

	processes := ensureAppProcessesLocked(app)
	index := slices.IndexFunc(processes, func(process Process) bool {
		return process.Id == processID
	})
	if index == -1 {
		return false
	}

	processesByApp[app] = append(processes[:index], processes[index+1:]...)
	return true
}

func ensureAppServicesLocked(app string) []Service {
	if services, ok := servicesByApp[app]; ok {
		return append([]Service(nil), services...)
	}

	defaultServices := []Service{
		{Name: "web", Count: 1, CPU: 256, Memory: 512},
		{Name: "worker", Count: 1, CPU: 256, Memory: 512},
	}
	servicesByApp[app] = append([]Service(nil), defaultServices...)
	return append([]Service(nil), defaultServices...)
}

func ensureAppProcessesLocked(app string) []Process {
	if processes, ok := processesByApp[app]; ok {
		return append([]Process(nil), processes...)
	}

	processes := buildProcessesForApp(app, ensureAppServicesLocked(app))
	processesByApp[app] = append([]Process(nil), processes...)
	return processes
}

func cloneServicesForApp(app string) []Service {
	return append([]Service(nil), ensureAppServicesLocked(app)...)
}

func cloneProcessesForApp(app string) []Process {
	return append([]Process(nil), ensureAppProcessesLocked(app)...)
}

func buildProcessesForApp(app string, services []Service) []Process {
	var processes []Process
	for _, service := range services {
		for ordinal := range service.Count {
			processes = append(processes, buildProcess(app, service.Name, ordinal+1))
		}
	}
	return processes
}

func resizeServiceProcesses(app string, processes []Process, serviceName string, desiredCount int) []Process {
	var (
		matching  []Process
		remaining []Process
		maxIndex  int
	)

	for _, process := range processes {
		if process.Name == serviceName {
			matching = append(matching, process)
			maxIndex = max(maxIndex, processOrdinal(process.Id))
			continue
		}
		remaining = append(remaining, process)
	}

	if len(matching) > desiredCount {
		matching = matching[:desiredCount]
	}

	for len(matching) < desiredCount {
		maxIndex++
		matching = append(matching, buildProcess(app, serviceName, maxIndex))
	}

	return append(remaining, matching...)
}

func buildProcess(app, serviceName string, ordinal int) Process {
	command := "bundle exec sidekiq"
	ports := []string{}
	if serviceName == "web" {
		command = "bundle exec rails server"
		ports = []string{"80:3000"}
	}

	return Process{
		Id:       fmt.Sprintf("p-%s-%d", sanitizeServiceName(serviceName), ordinal),
		App:      app,
		Command:  command,
		Cpu:      15.0,
		Host:     fmt.Sprintf("10.0.1.%02d", 10+ordinal),
		Image:    "registry.example.com/app:latest",
		Instance: fmt.Sprintf("i-%012d", ordinal),
		Memory:   256.0,
		Name:     serviceName,
		Ports:    ports,
		Release:  releaseForApp(app),
		Started:  time.Now().Add(-time.Duration(ordinal) * time.Hour),
		Status:   "running",
	}
}

func sanitizeServiceName(serviceName string) string {
	return strings.ReplaceAll(serviceName, "_", "-")
}

func processOrdinal(id string) int {
	lastDash := strings.LastIndexByte(id, '-')
	if lastDash == -1 {
		return 0
	}

	ordinal, err := strconv.Atoi(id[lastDash+1:])
	if err != nil {
		return 0
	}

	return ordinal
}

func parseOptionalInt(query map[string][]string, key string) (int, bool, error) {
	values, ok := query[key]
	if !ok || len(values) == 0 {
		return 0, false, nil
	}

	value := strings.TrimSpace(values[0])
	if value == "" {
		return 0, false, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false, fmt.Errorf("invalid %s value %q", key, value)
	}

	if parsed < 0 {
		return 0, false, fmt.Errorf("%s must be >= 0", key)
	}

	return parsed, true, nil
}

func releaseForApp(app string) string {
	if release := currentReleaseByApp[app]; release != "" {
		return release
	}

	return "RAPP123456"
}
