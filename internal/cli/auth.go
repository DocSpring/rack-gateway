package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

// errGatewayTooOld means the gateway predates the loopback login this CLI uses.
var errGatewayTooOld = errors.New(
	"the gateway is older than this CLI; upgrade the gateway or use an older rack-gateway CLI",
)

// StartLogin starts a loopback login and returns the identity provider URL to open.
func StartLogin(gatewayURL string, req LoginStartRequest) (*LoginStartResponse, error) {
	// Gateways from before the loopback login answer with their own state and code verifier, and
	// would never redirect the browser to this CLI.
	var result struct {
		AuthURL      string `json:"auth_url"`
		State        string `json:"state"`
		CodeVerifier string `json:"code_verifier"`
	}
	url := buildGatewayAPIURL(gatewayURL, "/api/v1/auth/cli/start")
	if err := postLoginJSON(url, req, "login start failed: ", &result); err != nil {
		return nil, err
	}
	if result.State != "" || result.CodeVerifier != "" {
		return nil, errGatewayTooOld
	}
	return &LoginStartResponse{AuthURL: result.AuthURL}, nil
}

// CompleteLogin redeems the single-use login code with the PKCE code verifier for a session token.
func CompleteLogin(gatewayURL, loginCode, codeVerifier string, device DeviceInfo) (*LoginResponse, error) {
	payload := map[string]string{
		"login_code":     loginCode,
		"code_verifier":  codeVerifier,
		"device_id":      device.ID,
		"device_name":    device.Name,
		"device_os":      device.OS,
		"client_version": device.ClientVersion,
	}
	var result LoginResponse
	url := buildGatewayAPIURL(gatewayURL, "/api/v1/auth/cli/complete")
	if err := postLoginJSON(url, payload, "", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// postLoginJSON POSTs payload as JSON to an unauthenticated login endpoint and decodes the response
// into out. Non-200 responses become errors carrying errPrefix and the gateway's error message.
func postLoginJSON(url string, payload interface{}, errPrefix string, out interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := sendGatewayRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s%s", errPrefix, RenderGatewayError(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
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

// GetGatewayInfo fetches gateway info from /api/v1/info (requires auth)
func GetGatewayInfo(gatewayURL, token string) (*GatewayInfoResponse, error) {
	url := buildGatewayAPIURL(gatewayURL, "/api/v1/info")

	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get gateway info: %s", string(body))
	}

	var result GatewayInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
