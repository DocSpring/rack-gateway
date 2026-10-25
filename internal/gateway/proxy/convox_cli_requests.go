package proxy

import (
	"net/http"
	"strings"
)

// envMaskConfigName is the app config Convox's CLI keeps its env mask in (`convox env mask set`).
const envMaskConfigName = "cli-env-mask"

// isRoutineConvoxCLIRequest reports whether a request to a rack route the gateway doesn't forward is one every
// Convox CLI makes for itself and gets by fine without. The gateway still answers it with 404, but doesn't audit it
// as a denial: it says nothing about the caller, and every deploy, run and log stream would otherwise add one.
//
//   - GET /machines and GET /racks: before each websocket (build logs, run, exec, logs), the Convox SDK sends a GET
//     that starts a Convox Console session, and ignores the answer. A rack has neither route.
//   - GET /apps/{app}/configs/cli-env-mask: `convox env` and `convox releases info` read Convox's env mask before
//     printing to a terminal, and print the env as it came on a 404. The gateway has already masked secret values.
//
// Any other method or config name (a `convox env mask set`, a read of another config) is still a denial. path is
// the canonical rack path, already checked by isSafeRackPath.
func isRoutineConvoxCLIRequest(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	if path == "/machines" || path == "/racks" {
		return true
	}
	segments := strings.Split(path, "/")
	return len(segments) == 5 && segments[0] == "" && segments[1] == "apps" && segments[2] != "" &&
		segments[3] == "configs" && segments[4] == envMaskConfigName
}
