package proxy

import (
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// What a client does again when the gateway couldn't record the step the rack carried out.
const (
	retryUpload  = "upload the archive again, to a new key"
	retryBuild   = "create the build again"
	retryProcess = "start a new process (this one can't be used under the approval)"
)

// mustTrackAnswer reports whether the gateway has to record what a successful answer (status) to r says the rack
// created: an uploaded object, which a build of it needs (object keys are write-once), and a build or process started
// under a deploy approval, which the approval's later steps find by that record. Such an answer has to name what was
// created, in JSON; one that doesn't is refused with a stepNotRecordedError (see unreadableTrackedAnswer).
func mustTrackAnswer(r *http.Request, path string, status int) bool {
	if status < 200 || status >= 300 {
		return false
	}
	if isObjectUpload(r, path) {
		return true
	}
	if !hasApproval(getDeployApprovalTracker(r.Context())) {
		return false
	}
	return isBuildCreate(r, path) || (r.Method == http.MethodPost && rbac.KeyMatch3(path, processStartRoute))
}

// unreadableTrackedAnswer is the error for a tracked answer (mustTrackAnswer) the gateway can't read.
func unreadableTrackedAnswer(r *http.Request, path string) error {
	switch {
	case isObjectUpload(r, path):
		return unreadableAnswer("the rack answered the upload with something other than a stored object", retryUpload)
	case isBuildCreate(r, path):
		return unreadableAnswer("the rack answered the build request with something other than a build", retryBuild)
	default:
		return unreadableAnswer("the rack answered the process start with something other than a process",
			retryProcess)
	}
}
