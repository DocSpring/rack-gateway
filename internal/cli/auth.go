package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

// StartLogin initiates the OAuth login flow
func StartLogin(gatewayURL string) (*LoginStartResponse, error) {
	url := buildGatewayAPIURL(gatewayURL, "/api/v1/auth/cli/start")

	resp, err := sendGatewayRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login start failed: %s", string(body))
	}

	var result LoginStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// CompleteLogin polls the server to complete the OAuth login flow
func CompleteLogin(gatewayURL, state, codeVerifier string, device DeviceInfo) (*LoginResponse, error) {
	url := buildGatewayAPIURL(gatewayURL, "/api/v1/auth/cli/complete")

	payload := map[string]string{
		"state":          state,
		"code_verifier":  codeVerifier,
		"device_id":      device.ID,
		"device_name":    device.Name,
		"device_os":      device.OS,
		"client_version": device.ClientVersion,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := sendGatewayRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusAccepted {
		return nil, ErrLoginPending
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s", RenderGatewayError(body))
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DetermineDeviceInfo gathers information about the current CLI client device
func DetermineDeviceInfo() DeviceInfo {
	cfg, _, _ := LoadConfig()
	deviceID := ""
	if cfg != nil {
		deviceID = strings.TrimSpace(cfg.MachineID)
	}
	if deviceID == "" {
		deviceID = uuid.NewString()
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown-device"
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = fmt.Sprintf("gateway-cli-%s", runtime.GOOS)
	}

	clientVersion := strings.TrimSpace(Version)
	if clientVersion == "" {
		clientVersion = "dev"
	}

	return DeviceInfo{
		ID:            deviceID,
		Name:          hostname,
		OS:            runtime.GOOS,
		ClientVersion: clientVersion,
	}
}

func buildGatewayAPIURL(gatewayURL, path string) string {
	parsedURL := gatewayURL
	if !strings.HasPrefix(parsedURL, "http://") && !strings.HasPrefix(parsedURL, "https://") {
		parsedURL = "https://" + parsedURL
	}
	return fmt.Sprintf("%s%s", strings.TrimSuffix(parsedURL, "/"), path)
}

func sendGatewayRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	return HTTPClient.Do(req)
}
