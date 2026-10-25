package proxy

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

// A script command reaches the rack as sent, and its audit entry holds the script readably, not its base64.
func TestScriptCommandIsForwardedAsSentAndRecordedReadably(t *testing.T) {
	f := newProtectedEnvFixture(t)
	text := "accounts = Account.where(plan: 'pro')\nputs accounts.count\n"
	command := scriptrun.Build("count.rb", []byte(text), "rails runner")

	rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t), map[string]string{"Command": command})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	starts := f.processStarts()
	require.Len(t, starts, 1)
	require.Equal(t, command, starts[0].Get("Command"))
	details := f.processStartDetails(t, "success")
	require.Equal(t, "rails runner count.rb", details["command"])
	require.Equal(t, text, details["script"])
	require.Equal(t, "count.rb", details["script_name"])
}
