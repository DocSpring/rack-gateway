package middleware

import (
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/gin-gonic/gin"
)

func HostValidator(cfg *config.Config) gin.HandlerFunc {
	allowedHost, isDev := validatorContext(cfg)
	return func(c *gin.Context) {
		ua := strings.ToLower(strings.TrimSpace(c.GetHeader("User-Agent")))
		if strings.Contains(ua, "kube-probe") {
			c.Next()
			return
		}

		host := canonicalHost(c.Request.Host)
		if host == "" && c.Request.URL != nil {
			host = canonicalHost(c.Request.URL.Host)
		}

		if host == "localhost" || host == "127.0.0.1" || host == "" {
			c.Next()
			return
		}

		if allowedHost == "" {
			c.Next()
			return
		}

		if strings.EqualFold(host, allowedHost) {
			c.Next()
			return
		}

		if isDev && isLocalHost(host) {
			c.Next()
			return
		}

		c.JSON(400, gin.H{"error": "invalid host"})
		c.Abort()
	}
}

func OriginValidator(cfg *config.Config) gin.HandlerFunc {
	allowedHost, isDev := validatorContext(cfg)
	return func(c *gin.Context) {
		if allowedHost == "" || (c.GetHeader("User-Agent") == "" && c.GetHeader("Sec-Fetch-Site") == "") {
			c.Next()
			return
		}

		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		originURL, err := url.Parse(origin)
		if err != nil {
			c.JSON(400, gin.H{"error": "invalid origin"})
			c.Abort()
			return
		}

		originHost := canonicalHost(originURL.Host)
		if isDev && isLocalHost(originHost) {
			c.Next()
			return
		}
		if originHost == "" || !strings.EqualFold(originHost, allowedHost) {
			c.JSON(400, gin.H{"error": "invalid origin"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func canonicalHost(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ".")
	raw = strings.ToLower(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "[") {
		if end := strings.Index(raw, "]"); end != -1 {
			inner := raw[1:end]
			return strings.TrimSpace(inner)
		}
	}
	if idx := strings.LastIndex(raw, ":"); idx != -1 && !strings.Contains(raw[idx+1:], ":") {
		raw = raw[:idx]
	}
	return raw
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1"
}

func validatorContext(cfg *config.Config) (allowedHost string, isDev bool) {
	isDev = gin.Mode() == gin.DebugMode
	if cfg != nil && cfg.DevMode {
		isDev = true
	}
	if cfg != nil {
		allowedHost = canonicalHost(cfg.Domain)
	}
	return allowedHost, isDev
}
