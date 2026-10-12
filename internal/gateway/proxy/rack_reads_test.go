package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// rackReads answers the reads the gateway's build and promote checks make of a fake rack: an app's releases (the
// latest first) and builds. Without releases the app lists none, so a build is checked with an empty env.
type rackReads struct {
	mu       sync.Mutex
	releases []rackRelease
	builds   map[string]rackBuild
}

// addRelease makes release the app's latest.
func (rr *rackReads) addRelease(release rackRelease) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.releases = append([]rackRelease{release}, rr.releases...)
}

func (rr *rackReads) addBuild(build rackBuild) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if rr.builds == nil {
		rr.builds = map[string]rackBuild{}
	}
	rr.builds[build.ID] = build
}

// serve answers a release or build read and reports whether it did. Anything else is left to the caller.
func (rr *rackReads) serve(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.Method != http.MethodGet || len(parts) < 3 || parts[0] != "apps" {
		return false
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()
	switch {
	case len(parts) == 3 && parts[2] == "releases":
		list := rr.releases
		if r.URL.Query().Get("limit") == "1" && len(list) > 1 {
			list = list[:1]
		}
		writeTestJSON(w, append([]rackRelease{}, list...))
	case len(parts) == 4 && parts[2] == "releases":
		for _, release := range rr.releases {
			if release.ID == parts[3] {
				writeTestJSON(w, release)
				return true
			}
		}
		http.NotFound(w, r)
	case len(parts) == 4 && parts[2] == "builds":
		build, ok := rr.builds[parts[3]]
		if !ok {
			http.NotFound(w, r)
			return true
		}
		writeTestJSON(w, build)
	default:
		return false
	}
	return true
}

func writeTestJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
