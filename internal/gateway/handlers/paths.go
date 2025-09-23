package handlers

const (
	apiRootPath = "/.gateway/api"
	webRootPath = "/.gateway/web"

	// DefaultWebRoute is the landing page for the SPA after login.
	DefaultWebRoute = webRootPath + "/rack"
)

// APIRoute builds a fully-qualified gateway API route from a relative path.
func APIRoute(path string) string {
	return joinRoute(apiRootPath, path)
}

// WebRoute builds a fully-qualified web route under the gateway prefix.
func WebRoute(path string) string {
	return joinRoute(webRootPath, path)
}

func joinRoute(base, path string) string {
	if path == "" {
		return base
	}
	if path == "/" {
		return base + "/"
	}
	if path[0] == '/' {
		return base + path
	}
	return base + "/" + path
}
