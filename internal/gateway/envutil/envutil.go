package envutil

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/httpclient"
)

const MaskedSecret = "********************"

// IsSecretKey returns true if key looks secret-like (by suffix/pattern) or is in explicit list.
func IsSecretKey(key string, explicit []string) bool {
	upper := strings.ToUpper(key)
	if strings.Contains(upper, "SECRET") || strings.Contains(upper, "TOKEN") {
		return true
	}
	if strings.HasSuffix(upper, "_KEY") || strings.HasSuffix(upper, "_KEY_ID") {
		return true
	}
	for _, k := range explicit {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return true
		}
	}
	return false
}

// FetchLatestEnvMap pulls the latest release then returns its env as a map.
func FetchLatestEnvMap(rack config.RackConfig, app string, tlsConfig *tls.Config) (map[string]string, error) {
	base := strings.TrimRight(rack.URL, "/")
	// Disable TLS verification for Convox API (internal/self-signed certs)
	client := httpclient.NewRackClient(10*time.Second, tlsConfig)
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))
	// List releases
	req1, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/apps/%s/releases?limit=1", base, app), nil)
	req1.Header.Set("Authorization", authHeader)
	resp1, err := client.Do(req1)
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close()
	var list []map[string]interface{}
	if err := json.NewDecoder(resp1.Body).Decode(&list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return map[string]string{}, nil
	}
	id, _ := list[0]["id"].(string)
	if id == "" {
		return map[string]string{}, nil
	}
	// Get release
	req2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/apps/%s/releases/%s", base, app, id), nil)
	req2.Header.Set("Authorization", authHeader)
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	var rel map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&rel); err != nil {
		return nil, err
	}
	envStr, _ := rel["env"].(string)
	out := map[string]string{}
	for _, ln := range strings.Split(envStr, "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		k := parts[0]
		v := ""
		if len(parts) == 2 {
			v = parts[1]
		}
		out[k] = v
	}
	return out, nil
}
