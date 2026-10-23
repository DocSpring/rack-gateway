package audit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// MaxRecordedCommandBytes is the longest command a request may run, measured as its audit entry stores it
// (redacted, then JSON-encoded). An entry's details are cut at db.AuditDetailsMaxChars, and a command cut short
// there would hide what actually ran, so a longer one is refused before it reaches the rack.
const MaxRecordedCommandBytes = 4000

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
	encoded, err := json.Marshal(RedactCommand(command))
	if err != nil {
		return fmt.Errorf("the command can't be recorded: %w", err)
	}
	if len(encoded) > MaxRecordedCommandBytes {
		return fmt.Errorf("the command is too long for the audit log to keep in full (%d bytes as recorded, at most "+
			"%d): put a long script in a file in the image and run that", len(encoded), MaxRecordedCommandBytes)
	}
	return nil
}
