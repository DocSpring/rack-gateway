package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/convox/convox/sdk"
	"github.com/convox/stdcli"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

// envChangeGateway is a fake gateway whose MFA status reports a TOTP method and a step-up window that is still
// open. It records the headers of every rack-proxy request.
type envChangeGateway struct {
	mu      sync.Mutex
	proxied []http.Header
}

func (g *envChangeGateway) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/v1/auth/mfa/status" {
		expiresAt := time.Now().Add(5 * time.Minute)
		_ = json.NewEncoder(w).Encode(MFAStatusResponse{
			Enrolled:              true,
			Required:              true,
			Methods:               []MFAMethodResponse{{ID: 1, Type: "totp", Label: "Phone"}},
			RecentStepUpExpiresAt: &expiresAt,
		})
		return
	}
	g.mu.Lock()
	g.proxied = append(g.proxied, r.Header.Clone())
	g.mu.Unlock()
	_, _ = w.Write([]byte(`[]`))
}

func (g *envChangeGateway) requests() []http.Header {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]http.Header(nil), g.proxied...)
}

func newEnvChangeGateway(t *testing.T) *envChangeGateway {
	t.Helper()
	gateway := &envChangeGateway{}
	srv := httptest.NewServer(http.HandlerFunc(gateway.handler))
	t.Cleanup(srv.Close)
	configureRackForTLSTest(t, srv.URL)

	previousCode, previousMethod := MFACodeFlag, MFAMethodFlag
	MFACodeFlag, MFAMethodFlag = "", ""
	t.Cleanup(func() { MFACodeFlag, MFAMethodFlag = previousCode, previousMethod })
	return gateway
}

// envChangeCommand returns cmd with --app set, and --allow-protected when allowProtected.
func envChangeCommand(t *testing.T, cmd *cobra.Command, allowProtected bool) *cobra.Command {
	t.Helper()
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Flags().Set("app", "myapp"))
	if allowProtected {
		require.NoError(t, cmd.Flags().Set(allowProtectedFlag, "true"))
	}
	return cmd
}

// typeTOTPCode answers the TOTP code prompt (which reads os.Stdin) with a code, and returns that code.
func typeTOTPCode(t *testing.T) string {
	t.Helper()
	const code = "654321"
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	_, err = writer.WriteString(code + "\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	// The prompt has no trailing newline; on the test's output it would hide the "--- PASS" line from go test.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	previousIn, previousErr := os.Stdin, os.Stderr
	os.Stdin, os.Stderr = reader, devNull
	t.Cleanup(func() {
		os.Stdin, os.Stderr = previousIn, previousErr
		_ = reader.Close()
		_ = devNull.Close()
	})
	return code
}

// listApps stands in for the convox env command: one request through the configured client.
func listApps(rack sdk.Interface, _ *stdcli.Context) error {
	_, err := rack.AppList()
	return err
}

func basicCredential(t *testing.T, header http.Header) string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header.Get("Authorization"), "Basic "))
	require.NoError(t, err)
	return string(decoded)
}

func TestEnvSetKeys(t *testing.T) {
	keys := envSetKeys([]string{
		"DATABASE_URL=postgres://db.internal/app?sslmode=require", "NOT_AN_ASSIGNMENT", "EMPTY=", "PORT=3000",
	})
	require.Equal(t, []string{"DATABASE_URL", "EMPTY", "PORT"}, keys)
}

// The gateway verifies MFA on every protected env change, so --allow-protected asks for a code even though the
// step-up window is still open, and names the unset keys for the gateway.
func TestEnvUnsetAllowProtectedNamesKeysAndAsksForMFA(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	code := typeTOTPCode(t)
	cmd := envChangeCommand(t, envUnsetCommand(), true)
	args := []string{"DATABASE_URL", "REDIS_URL"}

	require.NoError(t, runEnvChange(cmd, args, "env unset", args, listApps))

	proxied := gateway.requests()
	require.Len(t, proxied, 1)
	require.Equal(t, "DATABASE_URL,REDIS_URL", proxied[0].Get(envutil.AllowProtectedHeader))
	require.Equal(t, "convox:SESSION-TOKEN-SECRET.totp."+code, basicCredential(t, proxied[0]))
}

func TestEnvSetAllowProtectedNamesKeysAndUsesMFACodeFlag(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	MFACodeFlag = "123456"
	cmd := envChangeCommand(t, envSetCommand(), true)
	args := []string{"DATABASE_URL=postgres://new", "PORT=3000"}

	require.NoError(t, runEnvChange(cmd, args, "env set", envSetKeys(args), listApps))

	proxied := gateway.requests()
	require.Len(t, proxied, 1)
	require.Equal(t, "DATABASE_URL,PORT", proxied[0].Get(envutil.AllowProtectedHeader))
	require.Equal(t, "convox:SESSION-TOKEN-SECRET.totp.123456", basicCredential(t, proxied[0]))
}

// Without --allow-protected nothing changes: no opt-in header, and no MFA prompt while step-up is fresh.
func TestEnvSetWithoutAllowProtectedSendsNoOptIn(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	cmd := envChangeCommand(t, envSetCommand(), false)
	args := []string{"DATABASE_URL=postgres://new"}

	require.NoError(t, runEnvChange(cmd, args, "env set", envSetKeys(args), listApps))

	proxied := gateway.requests()
	require.Len(t, proxied, 1)
	require.Empty(t, proxied[0].Get(envutil.AllowProtectedHeader))
	require.Equal(t, "convox:SESSION-TOKEN-SECRET", basicCredential(t, proxied[0]))
}

func TestAllowProtectedRefusesAPITokens(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	t.Setenv("RACK_GATEWAY_API_TOKEN", "rgw_token")
	cmd := envChangeCommand(t, envUnsetCommand(), true)
	args := []string{"DATABASE_URL"}

	err := runEnvChange(cmd, args, "env unset", args, listApps)
	require.ErrorIs(t, err, errAPITokenCannotChangeProtected)
	require.Empty(t, gateway.requests())
}

// withoutTerminal makes stdin a pipe, as when an agent runs the CLI.
func withoutTerminal(t *testing.T) {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	previous := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = previous
		_ = reader.Close()
	})
}

// The PIN dialog, the touch prompt and the notification name the keys, the app and the rack, never the values.
func TestProtectedEnvPromptNamesKeysAppAndRack(t *testing.T) {
	prompt := protectedEnvPrompt("env unset", []string{"DATABASE_URL", "REDIS_URL\x1b[2J"}, "prod-web", "us")
	require.Equal(t, []string{
		"Unset env var DATABASE_URL on app prod-web (rack us)",
		"Unset env var REDIS_URL?[2J on app prod-web (rack us)",
	}, prompt.Summary)
	require.Equal(t, "unset env vars, protected ones included, on app prod-web (rack us)", prompt.Touch)

	set := protectedEnvPrompt("env set", []string{"DATABASE_URL"}, "web\u202eevil", "eu")
	require.Equal(t, []string{"Set env var DATABASE_URL on app web?evil (rack eu)"}, set.Summary)
	require.Equal(t, "set env vars, protected ones included, on app web?evil (rack eu)", set.Touch)
}

// The change is described on the terminal before MFA, with the app the command resolved and the selected rack.
func TestAllowProtectedShowsWhatItChangesBeforeMFA(t *testing.T) {
	newEnvChangeGateway(t)
	t.Setenv("RACK_GATEWAY_RACK", "")
	MFACodeFlag = "123456"
	cmd := envChangeCommand(t, envSetCommand(), true)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	args := []string{"DATABASE_URL=postgres://new-host/db", "PORT=3000"}

	require.NoError(t, runEnvChange(cmd, args, "env set", envSetKeys(args), listApps))
	require.Contains(t, stderr.String(), "Set env var DATABASE_URL on app myapp (rack us)\n")
	require.Contains(t, stderr.String(), "Set env var PORT on app myapp (rack us)\n")
	require.NotContains(t, stderr.String(), "new-host", "values are never shown")
}

func TestAllowProtectedWithoutTerminalNeedsExplicitApp(t *testing.T) {
	require.NoError(t, requireExplicitProtectedEnvApp(true, ""), "in a terminal the resolved app is shown first")
	require.NoError(t, requireExplicitProtectedEnvApp(false, "prod-web"))
	require.ErrorIs(t, requireExplicitProtectedEnvApp(false, ""), errProtectedEnvAppNotExplicit)
	require.ErrorIs(t, requireExplicitProtectedEnvApp(false, "  "), errProtectedEnvAppNotExplicit)
}

// Without a terminal the person at the PIN dialog sees the command line, so the app must be on it: one from
// CONVOX_APP (or .convox/app, or the directory name) isn't shown there.
func TestAllowProtectedWithoutTerminalRefusesAnImplicitApp(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	withoutTerminal(t)
	t.Setenv("CONVOX_APP", "prod-web")
	MFACodeFlag = "123456"
	cmd := envSetCommand()
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Flags().Set(allowProtectedFlag, "true"))
	args := []string{"DATABASE_URL=postgres://new"}

	err := runEnvChange(cmd, args, "env set", envSetKeys(args), listApps)
	require.ErrorIs(t, err, errProtectedEnvAppNotExplicit)
	require.Empty(t, gateway.requests())
}
