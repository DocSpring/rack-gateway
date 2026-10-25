package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

// errJustificationRequired refuses a command without a justification in an app that requires one.
var errJustificationRequired = errors.New("this app requires a justification for every command: run it with " +
	"--justification, saying why it's run and what it does")

// checkCommandJustification finds the justification of the command r runs (an exec's, or a process start's) and
// puts it on r, so the request's audit entries record it. A command under a deploy approval is justified by the
// approval's message. In an app that requires one (require_command_justification, the app's own or the
// gateway-wide one), a command without one is refused: `convox run`'s placeholder start too, so an attached run
// without one is refused before its process starts, with a reason the CLI can show (it can't show why an exec's
// WebSocket handshake was refused).
func (h *Handler) checkCommandJustification(r *http.Request, tracker *deployApprovalTracker) error {
	if _, ok := audit.RequestCommand(r); !ok {
		return nil
	}
	justification, err := audit.RequestJustification(r)
	if err != nil {
		return refuse(http.StatusBadRequest, err)
	}
	if justification == "" && hasApproval(tracker) {
		justification = strings.TrimSpace(tracker.request.Message)
	}
	if justification != "" {
		*r = *r.WithContext(audit.WithJustification(r.Context(), justification))
		return nil
	}
	if h.settingsService == nil {
		return nil
	}
	app := extractAppFromPath(r.URL.Path)
	required, err := h.settingsService.GetRequireCommandJustification(app)
	if err != nil {
		return fmt.Errorf("failed to read whether %s requires a justification: %w", app, err)
	}
	if required {
		return refuse(http.StatusBadRequest, errJustificationRequired)
	}
	return nil
}
