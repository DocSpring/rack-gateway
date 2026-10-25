package audit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

// MaxRecordedCommandBytes is the longest command a request may run, measured as its audit entry stores it
// (redacted, then JSON-encoded). An entry's details are cut at db.AuditDetailsMaxChars, and a command cut short
// there would hide what actually ran, so a longer one is refused before it reaches the rack. 50 KB leaves room for
// a sizeable script while keeping audit entries a reasonable size.
const MaxRecordedCommandBytes = 50 << 10

// RequestCommand returns the command a rack request runs (its Command header): an exec's, or a process start's
// (`convox run`). ok is false for any other request.
func RequestCommand(r *http.Request) (string, bool) {
	path := "/" + strings.TrimLeft(r.URL.Path, "/")
	exec := rbac.KeyMatch3(path, "/apps/{app}/processes/{id}/exec")
	start := r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps/{app}/services/{service}/processes")
	if !exec && !start {
		return "", false
	}
	return r.Header.Get("Command"), true
}

// CheckCommandRecordable refuses a request whose command the audit log couldn't keep in full (see
// MaxRecordedCommandBytes).
func CheckCommandRecordable(r *http.Request) error {
	command, ok := RequestCommand(r)
	if !ok {
		return nil
	}
	return CheckCommandLength(command)
}

// CheckCommandLength refuses a command the audit log couldn't keep in full (see MaxRecordedCommandBytes): a
// script command (see scriptrun) is measured as its command and script are recorded, together. The CLI checks a
// command with it too, before it starts a process for the command to run in.
func CheckCommandLength(command string) error {
	details := commandDetails(command)
	recorded := 0
	for _, key := range []string{"command", "script"} {
		text, ok := details[key].(string)
		if !ok {
			continue
		}
		encoded, err := json.Marshal(text)
		if err != nil {
			return fmt.Errorf("the command can't be recorded: %w", err)
		}
		recorded += len(encoded)
	}
	if recorded > MaxRecordedCommandBytes {
		return fmt.Errorf("the command is too long for the audit log to keep in full (%d bytes as recorded, at most "+
			"%d, or %d KB): put a long script in a file in the image and run that",
			recorded, MaxRecordedCommandBytes, MaxRecordedCommandBytes>>10)
	}
	return nil
}

// commandDetails are the audit details that record command: the command, redacted (see RedactCommand). A script
// command (see scriptrun) is recorded as a person would run it locally (the command words, then the script's
// name), with the script's text (redacted too), its name and the SHA-256 of what was sent, which a copy of the
// script can be checked against.
func commandDetails(command string) map[string]interface{} {
	script, ok := scriptrun.Parse(command)
	if !ok {
		return map[string]interface{}{"command": RedactCommand(command)}
	}
	return map[string]interface{}{
		"command":       RedactCommand(script.Command()),
		"script":        RedactCommand(script.Text),
		"script_name":   script.Name,
		"script_sha256": script.SHA256,
	}
}
