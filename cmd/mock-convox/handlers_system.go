package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
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

func putSystem(w http.ResponseWriter, r *http.Request) {
	body := readRequestBody(r)
	updates := extractSystemParameterUpdates(body, r.Header.Get("Content-Type"))
	applySystemParameterUpdates(updates)

	w.Header().Set("Content-Type", "application/json")
	getSystem(w, r)
}

func readRequestBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		mclog.Errorf("failed to read system body: %v", err)
		return nil
	}

	if err := r.Body.Close(); err != nil {
		mclog.Warnf("failed to close system body: %v", err)
	}
	return data
}

func extractSystemParameterUpdates(body []byte, contentType string) map[string]string {
	if len(body) == 0 {
		return nil
	}

	ct := strings.ToLower(contentType)
	if shouldTryJSON(ct, body) {
		if updates := parseJSONParameters(body); len(updates) > 0 {
			return updates
		}
	}

	if updates := parseFormParameters(body); len(updates) > 0 {
		return updates
	}

	return parseLegacyParameters(body)
}

func shouldTryJSON(contentType string, body []byte) bool {
	if strings.Contains(contentType, "application/json") {
		return true
	}
	if len(body) == 0 {
		return false
	}
	switch body[0] {
	case '{', '[':
		return true
	default:
		return false
	}
}

func parseJSONParameters(body []byte) map[string]string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	if nested := interfaceMapToStringMap(payload["parameters"]); len(nested) > 0 {
		return nested
	}
	return interfaceMapToStringMap(payload)
}

func interfaceMapToStringMap(value interface{}) map[string]string {
	raw, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = fmt.Sprintf("%v", v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseFormParameters(body []byte) map[string]string {
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values) == 0 {
		return nil
	}

	updates := make(map[string]string)
	mergeUpdates(updates, parseParametersField(values))
	mergeUpdates(updates, directParameterValues(values))
	mergeUpdates(updates, indexedParameterValues(values))

	if len(updates) == 0 {
		return nil
	}
	return updates
}

func directParameterValues(values url.Values) map[string]string {
	updates := make(map[string]string)
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		value := vals[len(vals)-1]
		switch {
		case key == "parameters":
			continue
		case strings.HasPrefix(key, "parameters[") && strings.HasSuffix(key, "]"):
			name := key[len("parameters[") : len(key)-1]
			if name != "" {
				updates[name] = value
			}
		case strings.HasPrefix(key, "params["):
			continue
		default:
			if key != "" {
				updates[key] = value
			}
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return updates
}

func indexedParameterValues(values url.Values) map[string]string {
	names := collectIndexedNames(values)
	vals := collectIndexedValues(values)
	return mergeIndexedPairs(names, vals)
}

func collectIndexedNames(values url.Values) map[string]string {
	result := make(map[string]string)
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		if idx := extractParamIndex(key, "][name]"); idx != "" {
			result[idx] = vals[len(vals)-1]
			continue
		}
		if idx := extractParamIndex(key, "][key]"); idx != "" {
			result[idx] = vals[len(vals)-1]
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func collectIndexedValues(values url.Values) map[string]string {
	result := make(map[string]string)
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		if idx := extractParamIndex(key, "][value]"); idx != "" {
			result[idx] = vals[len(vals)-1]
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func mergeIndexedPairs(names, values map[string]string) map[string]string {
	if len(names) == 0 && len(values) == 0 {
		return nil
	}
	updates := make(map[string]string)
	for idx, name := range names {
		if name == "" {
			continue
		}
		if val, ok := values[idx]; ok {
			updates[name] = val
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return updates
}

func parseParametersField(values url.Values) map[string]string {
	raw := lastValue(values, "parameters")
	if raw == "" {
		return nil
	}

	if updates := parseJSONParameters([]byte(raw)); len(updates) > 0 {
		return updates
	}

	if nested, err := url.ParseQuery(raw); err == nil {
		return valuesToMap(nested)
	}

	return nil
}

func extractParamIndex(key, suffix string) string {
	prefix := "params["
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return ""
	}
	core := key[len(prefix) : len(key)-len(suffix)]
	return core
}

func parseLegacyParameters(body []byte) map[string]string {
	raw := string(body)
	if raw == "" {
		return nil
	}

	if strings.HasPrefix(raw, "parameters=") {
		return valuesToMap(parseQueryString(strings.TrimPrefix(raw, "parameters=")))
	}

	parts := strings.Split(raw, "&")
	legacy := make(map[string]string, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, errKey := url.QueryUnescape(kv[0])
		value, errValue := url.QueryUnescape(kv[1])
		if errKey != nil || errValue != nil || key == "" {
			continue
		}
		legacy[key] = value
	}

	if len(legacy) == 0 {
		return nil
	}
	return legacy
}

func parseQueryString(s string) url.Values {
	values, err := url.ParseQuery(s)
	if err != nil {
		return url.Values{}
	}
	return values
}

func valuesToMap(values url.Values) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		out[key] = vals[len(vals)-1]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeUpdates(dst map[string]string, src map[string]string) {
	if len(src) == 0 {
		return
	}
	for k, v := range src {
		dst[k] = v
	}
}

func lastValue(values url.Values, key string) string {
	vals, ok := values[key]
	if !ok || len(vals) == 0 {
		return ""
	}
	return vals[len(vals)-1]
}

func applySystemParameterUpdates(updates map[string]string) {
	if len(updates) == 0 {
		return
	}
	for key, value := range updates {
		mockSystemParameters[key] = value
	}
}

func getSystemProcesses(w http.ResponseWriter, _ *http.Request) {
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
