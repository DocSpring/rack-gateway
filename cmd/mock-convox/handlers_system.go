package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
	"github.com/gorilla/mux"
)

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

func putSystem(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if b, err := io.ReadAll(r.Body); err == nil {
			body = b
		} else {
			mclog.Errorf("failed to read system body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			mclog.Warnf("failed to close system body: %v", err)
		}
	}

	updated := 0
	ct := strings.ToLower(r.Header.Get("Content-Type"))

	tryJSON := strings.Contains(ct, "application/json") || (len(body) > 0 && (body[0] == '{' || body[0] == '['))
	if tryJSON && len(body) > 0 {
		var any map[string]interface{}
		if err := json.Unmarshal(body, &any); err == nil {
			if pv, ok := any["parameters"].(map[string]interface{}); ok {
				for k, v := range pv {
					sval := fmt.Sprintf("%v", v)
					mockSystemParameters[k] = sval
					updated++
				}
			} else {
				for k, v := range any {
					sval := fmt.Sprintf("%v", v)
					mockSystemParameters[k] = sval
					updated++
				}
			}
		}
	}

	if updated == 0 && len(body) > 0 {
		if vals, err := url.ParseQuery(string(body)); err == nil {
			if pjson := vals.Get("parameters"); pjson != "" {
				var m map[string]interface{}
				if err := json.Unmarshal([]byte(pjson), &m); err == nil {
					for k, v := range m {
						mockSystemParameters[k] = fmt.Sprintf("%v", v)
						updated++
					}
				} else {
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
			indexToName := map[string]string{}
			indexToValue := map[string]string{}
			for k, vs := range vals {
				if k == "parameters" {
					continue
				}
				if len(vs) == 0 {
					continue
				}
				v := vs[len(vs)-1]
				if strings.HasPrefix(k, "parameters[") && strings.HasSuffix(k, "]") {
					name := k[len("parameters[") : len(k)-1]
					if name != "" {
						mockSystemParameters[name] = v
						updated++
						continue
					}
				}
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

	w.Header().Set("Content-Type", "application/json")
	getSystem(w, r)
}

func getSystemProcesses(w http.ResponseWriter, r *http.Request) {
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
