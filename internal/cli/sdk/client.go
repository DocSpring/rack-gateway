package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	clilog "github.com/DocSpring/rack-gateway/internal/cli/logging"
	"github.com/convox/convox/pkg/structs"
	"github.com/convox/stdapi"
	"github.com/convox/stdsdk"
)

const maxLoggedBodyBytes = 4096

// Client is a custom SDK client that talks to the rack-gateway API
type Client struct {
	gatewayURL string
	token      string
	httpClient *http.Client
}

// New creates a new gateway SDK client
func New(gatewayURL, token string) *Client {
	return &Client{
		gatewayURL: gatewayURL,
		token:      token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// request makes an authenticated HTTP request to the gateway API
func (c *Client) request(method, path string, body interface{}, out interface{}) error {
	fullURL := c.gatewayURL + "/api/v1/convox" + path

	var payload io.Reader
	var requestBody []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
		requestBody = data
	}

	req, err := http.NewRequest(method, fullURL, payload)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if clilog.TopicEnabled(clilog.TopicHTTP) {
		clilog.DebugTopicf(clilog.TopicHTTP, "%s %s", method, fullURL)
	}
	if clilog.TopicEnabled(clilog.TopicHTTPBody) && len(requestBody) > 0 {
		clilog.DebugTopicf(clilog.TopicHTTPBody, "payload=%s", truncateBody(requestBody))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	if clilog.TopicEnabled(clilog.TopicHTTP) {
		clilog.DebugTopicf(clilog.TopicHTTP, "<-- %d %s", resp.StatusCode, fullURL)
	}
	if clilog.TopicEnabled(clilog.TopicHTTPBody) && len(respBody) > 0 {
		clilog.DebugTopicf(clilog.TopicHTTPBody, "response=%s", truncateBody(respBody))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, string(respBody))
	}

	if out != nil {
		if len(respBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return err
		}
	}

	return nil
}

func truncateBody(body []byte) string {
	if len(body) <= maxLoggedBodyBytes {
		return string(body)
	}
	return string(body[:maxLoggedBodyBytes]) + "…(truncated)"
}

// ProcessList lists processes for an app
func (c *Client) ProcessList(app string, opts structs.ProcessListOptions) (structs.Processes, error) {
	var processes structs.Processes

	path := fmt.Sprintf("/apps/%s/processes", app)
	if err := c.request("GET", path, nil, &processes); err != nil {
		return nil, err
	}

	return processes, nil
}

// Get makes a raw GET request (required by sdk.Interface)
func (c *Client) Get(path string, opts stdsdk.RequestOptions, out interface{}) error {
	return c.request("GET", path, nil, out)
}

// Endpoint returns the gateway URL (required by sdk.Interface)
func (c *Client) Endpoint() (*url.URL, error) {
	return url.Parse(c.gatewayURL + "/api/v1/convox")
}

// ClientType returns the client type (required by sdk.Interface)
func (c *Client) ClientType() string {
	return "standard"
}

// MachineList lists machines (required by sdk.Interface)
func (c *Client) MachineList() (structs.Machines, error) {
	return nil, fmt.Errorf("not implemented")
}

// WithContext returns a copy of the client with the given context (required by sdk.Interface)
func (c *Client) WithContext(ctx context.Context) structs.Provider {
	// Create a copy of the client with updated context
	// For now, we'll just return the same client since we handle context per-request
	return c
}

// Workers returns an error (required by structs.Provider)
func (c *Client) Workers() error {
	return nil
}

// TODO: Implement remaining sdk.Interface methods as needed
// For now, return "not implemented" errors

func (c *Client) Initialize(opts structs.ProviderOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) Start() error {
	return fmt.Errorf("not implemented")
}

func (c *Client) AppCancel(name string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) AppCreate(name string, opts structs.AppCreateOptions) (*structs.App, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) AppConfigGet(app, name string) (*structs.AppConfig, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) AppConfigList(app string) ([]structs.AppConfig, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) AppConfigSet(app, name, valueBase64 string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) AppGet(name string) (*structs.App, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) AppDelete(name string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) AppList() (structs.Apps, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) AppLogs(name string, opts structs.LogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) AppMetrics(name string, opts structs.MetricsOptions) (structs.Metrics, error) {
	return structs.Metrics{}, fmt.Errorf("not implemented")
}

func (c *Client) AppUpdate(name string, opts structs.AppUpdateOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) BalancerList(app string) (structs.Balancers, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildCreate(app, url string, opts structs.BuildCreateOptions) (*structs.Build, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildExport(app, id string, w io.Writer) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) BuildGet(app, id string) (*structs.Build, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildImport(app string, r io.Reader) (*structs.Build, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildLogs(app, id string, opts structs.LogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildList(app string, opts structs.BuildListOptions) (structs.Builds, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildUpdate(app, id string, opts structs.BuildUpdateOptions) (*structs.Build, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) CapacityGet() (*structs.Capacity, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) CertificateApply(app, service string, port int, id string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) CertificateCreate(pub, key string, opts structs.CertificateCreateOptions) (*structs.Certificate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) CertificateDelete(id string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) CertificateRenew(id string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) CertificateGenerate(domains []string, opts structs.CertificateGenerateOptions) (*structs.Certificate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) CertificateList(opts structs.CertificateListOptions) (structs.Certificates, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) LetsEncryptConfigGet() (*structs.LetsEncryptConfig, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) LetsEncryptConfigApply(config structs.LetsEncryptConfig) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) EventSend(action string, opts structs.EventSendOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) FilesDelete(app, pid string, files []string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) FilesDownload(app, pid string, file string) (io.Reader, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) FilesUpload(app, pid string, r io.Reader, opts structs.FileTransterOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) InstanceKeyroll() (*structs.KeyPair, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) InstanceList() (structs.Instances, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) InstanceShell(id string, rw io.ReadWriter, opts structs.InstanceShellOptions) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (c *Client) InstanceTerminate(id string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ObjectDelete(app, key string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ObjectExists(app, key string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (c *Client) ObjectFetch(app, key string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ObjectList(app, prefix string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ObjectStore(app, key string, r io.Reader, opts structs.ObjectStoreOptions) (*structs.Object, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ProcessExec(app, pid, command string, rw io.ReadWriter, opts structs.ProcessExecOptions) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (c *Client) ProcessGet(app, pid string) (*structs.Process, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ProcessLogs(app, pid string, opts structs.LogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ProcessRun(app, service string, opts structs.ProcessRunOptions) (*structs.Process, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ProcessStop(app, pid string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) Proxy(host string, port int, rw io.ReadWriter, opts structs.ProxyOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) RegistryAdd(server, username, password string) (*structs.Registry, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) RegistryList() (structs.Registries, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) RegistryProxy(ctx *stdapi.Context) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) RegistryRemove(server string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ReleaseCreate(app string, opts structs.ReleaseCreateOptions) (*structs.Release, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ReleaseGet(app, id string) (*structs.Release, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ReleaseList(app string, opts structs.ReleaseListOptions) (structs.Releases, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ReleasePromote(app, id string, opts structs.ReleasePromoteOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ResourceConsole(app, name string, rw io.ReadWriter, opts structs.ResourceConsoleOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ResourceExport(app, name string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ResourceGet(app, name string) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ResourceImport(app, name string, r io.Reader) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ResourceList(app string) (structs.Resources, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ServiceList(app string) (structs.Services, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ServiceRestart(app, name string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ServiceUpdate(app, name string, opts structs.ServiceUpdateOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ServiceLogs(app, name string, opts structs.LogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemGet() (*structs.System, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemInstall(w io.Writer, opts structs.SystemInstallOptions) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (c *Client) SystemJwtSignKey() (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (c *Client) SystemJwtSignKeyRotate() (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (c *Client) SystemLogs(opts structs.LogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemMetrics(opts structs.MetricsOptions) (structs.Metrics, error) {
	return structs.Metrics{}, fmt.Errorf("not implemented")
}

func (c *Client) SystemProcesses(opts structs.SystemProcessesOptions) (structs.Processes, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemReleases() (structs.Releases, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemUninstall(name string, w io.Writer, opts structs.SystemUninstallOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) SystemUpdate(opts structs.SystemUpdateOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceCreate(kind string, opts structs.ResourceCreateOptions) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceDelete(name string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceGet(name string) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceLink(name, app string) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceList() (structs.Resources, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceTypes() (structs.ResourceTypes, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceUnlink(name, app string) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceUpdate(name string, opts structs.ResourceUpdateOptions) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

// Backward compatibility methods

func (c *Client) AppParametersGet(app string) (map[string]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) AppParametersSet(app string, params map[string]string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) BuildCreateUpload(app string, r io.Reader, opts structs.BuildCreateOptions) (*structs.Build, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildImportMultipart(app string, r io.Reader) (*structs.Build, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BuildImportUrl(app string, r io.Reader) (*structs.Build, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) CertificateCreateClassic(pub, key string, opts structs.CertificateCreateOptions) (*structs.Certificate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) EnvironmentSet(app string, data []byte) (*structs.Release, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) EnvironmentUnset(app, key string) (*structs.Release, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) FormationGet(app string) (structs.Services, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) FormationUpdate(app, service string, opts structs.ServiceUpdateOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) InstanceShellClassic(id string, rw io.ReadWriter, opts structs.InstanceShellOptions) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (c *Client) ProcessRunAttached(app, service string, rw io.ReadWriter, height int, opts structs.ProcessRunOptions) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (c *Client) ProcessRunDetached(app, service string, opts structs.ProcessRunOptions) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (c *Client) RegistryRemoveClassic(server string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) ResourceCreateClassic(kind string, opts structs.ResourceCreateOptions) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) ResourceUpdateClassic(name string, opts structs.ResourceUpdateOptions) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) RackHost(rackOrgSlug string) (structs.RackData, error) {
	return structs.RackData{}, fmt.Errorf("not implemented")
}

func (c *Client) Runtimes(rackOrgSlug string) (structs.Runtimes, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) RuntimeAttach(rackOrgSlug string, opts structs.RuntimeAttachOptions) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) SystemJwtToken(opts structs.SystemJwtOptions) (*structs.SystemJwt, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceCreateClassic(kind string, opts structs.ResourceCreateOptions) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceDeleteClassic(name string) error {
	return fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceGetClassic(name string) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceLinkClassic(name, app string) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceListClassic() (structs.Resources, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceTypesClassic() (structs.ResourceTypes, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceUnlinkClassic(name, app string) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) SystemResourceUpdateClassic(name string, opts structs.ResourceUpdateOptions) (*structs.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) WorkflowList(rackOrgSlug string) (structs.WorkflowListResp, error) {
	return structs.WorkflowListResp{}, fmt.Errorf("not implemented")
}

func (c *Client) WorkflowCustomRun(rackOrgSlug, workflowId string, opts structs.WorkflowCustomRunOptions) (*structs.WorkflowCustomRunResp, error) {
	return nil, fmt.Errorf("not implemented")
}
