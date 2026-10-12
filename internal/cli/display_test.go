package cli

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDisplayTextNeutralizesControlAndBidiCharacters(t *testing.T) {
	require.Equal(t, "Deploy 36a302d to staging", displayText("Deploy 36a302d to staging"))
	require.Equal(t, "?[2J?[31mevil", displayText("\x1b[2J\x1b[31mevil"))
	require.Equal(t, "line?break", displayText("line\nbreak"))
	require.Equal(t, "?gnp.exe", displayText("\u202egnp.exe"))
	require.Equal(t, "?31m", displayText("\u009b31m"), "C1 CSI")
	require.Equal(t, "a?b?c", displayText("a b c"), "line and paragraph separators")
}

// Errors are printed by main; they can quote the gateway, so escape sequences go but lines and tabs stay.
func TestDisplayErrorKeepsLinesButNotEscapeSequences(t *testing.T) {
	err := errors.New("unknown command \"x\"\n\nDid you mean this?\n\tapps\x1b[2J\u009b31m\u202e\r")
	require.Equal(t, "unknown command \"x\"\n\nDid you mean this?\n\tapps?[2J?31m??", DisplayError(err))
}

// -o json stays valid JSON that decodes to the gateway's exact strings, but a terminal can't act on what it prints:
// encoding/json leaves DEL, C1 controls and bidirectional overrides raw, so printJSON escapes them.
func TestPrintJSONEscapesWhatTheTerminalWouldActOn(t *testing.T) {
	request := shownRequest("p-web", "web", "pending")
	request.Message = "Deploy\u009b2J\u202eweb\x7f\x1b[31m"
	output := &lockedBuffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(output)

	require.NoError(t, printJSON(cmd, request))

	printed := output.String()
	require.Contains(t, printed, `"message": "Deploy\u009b2J\u202eweb\u007f\u001b[31m"`)
	for _, raw := range []string{"\u009b", "\u202e", "\x7f", "\x1b"} {
		require.NotContains(t, printed, raw)
	}
	var decoded deployApprovalRequest
	require.NoError(t, json.Unmarshal([]byte(printed), &decoded))
	require.Equal(t, request.Message, decoded.Message)
}
