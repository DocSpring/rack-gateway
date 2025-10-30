package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	securemw "github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

type ctxKey string

// StyleNonceContextKey is the context key used to store and retrieve the CSP nonce for inline styles.
const StyleNonceContextKey ctxKey = "rgw-style-nonce"

var defaultStyleHashes = []string{}

// SecurityHeaders returns a Gin middleware that configures comprehensive security headers including
// Content Security Policy (CSP), HSTS, frame denial, and optional Sentry reporting integration.
func SecurityHeaders(cfg *config.Config) gin.HandlerFunc {
	sentryCfg := initializeSentryConfig(cfg)
	return func(c *gin.Context) {
		isProdLike := determineProductionMode(cfg)
		nonce := injectNonce(c)
		csp := buildCSP(nonce, isProdLike, sentryCfg)
		applySecurityHeaders(c, csp, isProdLike, sentryCfg)
	}
}

func initializeSentryConfig(cfg *config.Config) *sentrySecurityConfig {
	if cfg == nil || strings.TrimSpace(cfg.SentryJSDsn) == "" {
		return nil
	}
	parsed, err := buildSentrySecurityConfig(cfg)
	if err != nil {
		log.Printf("security: invalid SENTRY_JS_DSN: %v", err)
		return nil
	}
	return parsed
}

func determineProductionMode(cfg *config.Config) bool {
	if cfg != nil && cfg.DevMode && os.Getenv("FORCE_CSP_IN_DEV") != "true" {
		return false
	}
	return true
}

func injectNonce(c *gin.Context) string {
	nonce := generateNonce()
	if nonce != "" {
		c.Set(string(StyleNonceContextKey), nonce)
		ctx := context.WithValue(c.Request.Context(), StyleNonceContextKey, nonce)
		c.Request = c.Request.WithContext(ctx)
	}
	return nonce
}

func buildCSP(nonce string, isProdLike bool, sentryCfg *sentrySecurityConfig) string {
	connectSrc := buildConnectSrc(isProdLike, sentryCfg)
	imgSrc := buildImgSrc(isProdLike, sentryCfg)
	scriptSrc := buildScriptSrc(nonce, isProdLike)
	styleSrc := buildStyleSrc(nonce, isProdLike)

	cspParts := []string{
		"default-src 'self'",
		connectSrc,
		scriptSrc,
		styleSrc,
		imgSrc,
	}
	if sentryCfg != nil {
		cspParts = append(cspParts,
			fmt.Sprintf("report-uri %s", sentryCfg.ReportURL),
			fmt.Sprintf("report-to %s", sentryCfg.ReportGroup),
		)
	}
	return strings.Join(cspParts, "; ")
}

func buildConnectSrc(isProdLike bool, sentryCfg *sentrySecurityConfig) string {
	directives := []string{"'self'", "ws:", "wss:"}
	if !isProdLike {
		directives = append(directives, "http://localhost:*", "https://localhost:*")
	}
	if sentryCfg != nil && sentryCfg.ConnectOrigin != "" {
		directives = append(directives, sentryCfg.ConnectOrigin)
	}
	return "connect-src " + strings.Join(directives, " ")
}

func buildImgSrc(isProdLike bool, sentryCfg *sentrySecurityConfig) string {
	directives := []string{"'self'", "data:"}
	if !isProdLike {
		directives = append(directives, "http://localhost:*", "https://localhost:*")
	}
	if sentryCfg != nil && sentryCfg.ConnectOrigin != "" {
		directives = append(directives, sentryCfg.ConnectOrigin)
	}
	return "img-src " + strings.Join(directives, " ")
}

func buildScriptSrc(nonce string, isProdLike bool) string {
	directives := []string{"'self'"}
	if nonce != "" {
		directives = append(directives, fmt.Sprintf("'nonce-%s'", nonce))
	}
	if !isProdLike {
		directives = append(directives, "'unsafe-inline'")
	}
	return "script-src " + strings.Join(directives, " ")
}

func buildStyleSrc(nonce string, isProdLike bool) string {
	directives := []string{"'self'"}
	if nonce != "" {
		directives = append(directives, fmt.Sprintf("'nonce-%s'", nonce))
	}
	directives = append(directives, defaultStyleHashes...)
	if !isProdLike {
		directives = append(directives, "'unsafe-inline'")
	}
	return "style-src " + strings.Join(directives, " ")
}

func applySecurityHeaders(c *gin.Context, csp string, isProdLike bool, sentryCfg *sentrySecurityConfig) {
	secCfg := securemw.Config{
		SSLRedirect:           false,
		STSSeconds:            calculateSTSSeconds(isProdLike),
		STSIncludeSubdomains:  false,
		FrameDeny:             true,
		ContentTypeNosniff:    true,
		BrowserXssFilter:      true,
		ContentSecurityPolicy: csp,
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		SSLProxyHeaders:       map[string]string{"X-Forwarded-Proto": "https"},
		IsDevelopment:         !isProdLike,
		BadHostHandler: func(c *gin.Context) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host"})
			c.Abort()
		},
	}

	securemw.New(secCfg)(c)
	if sentryCfg != nil {
		c.Header("Report-To", sentryCfg.ReportToHeader)
		c.Header("Reporting-Endpoints", sentryCfg.ReportingEndpointsHeader)
	}
}

func calculateSTSSeconds(isProdLike bool) int64 {
	if !isProdLike {
		return 0
	}
	return 63072000
}

// StyleNonceFromContext retrieves the CSP nonce from the given context, or returns an empty string if not found.
func StyleNonceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value := ctx.Value(StyleNonceContextKey); value != nil {
		if nonce, ok := value.(string); ok {
			return nonce
		}
	}
	return ""
}

func generateNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(buf)
}

type sentrySecurityConfig struct {
	ReportURL                string
	ReportGroup              string
	ReportToHeader           string
	ReportingEndpointsHeader string
	ConnectOrigin            string
}

func buildSentrySecurityConfig(cfg *config.Config) (*sentrySecurityConfig, error) {
	dsn := strings.TrimSpace(cfg.SentryJSDsn)
	if dsn == "" {
		return nil, fmt.Errorf("empty DSN")
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	publicKey := strings.TrimSpace(parsed.User.Username())
	if publicKey == "" {
		return nil, fmt.Errorf("DSN missing public key")
	}

	trimmedPath := strings.Trim(parsed.Path, "/")
	if trimmedPath == "" {
		return nil, fmt.Errorf("DSN missing project identifier")
	}
	segments := strings.Split(trimmedPath, "/")
	projectID := segments[len(segments)-1]
	pathPrefix := ""
	if len(segments) > 1 {
		pathPrefix = strings.Join(segments[:len(segments)-1], "/")
	}

	var apiPath strings.Builder
	apiPath.WriteString("/")
	if pathPrefix != "" {
		apiPath.WriteString(pathPrefix)
		apiPath.WriteString("/")
	}
	apiPath.WriteString("api/")
	apiPath.WriteString(projectID)
	apiPath.WriteString("/security/")

	baseURL := fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, apiPath.String())
	query := url.Values{}
	query.Set("sentry_key", publicKey)
	if env := strings.TrimSpace(cfg.SentryEnvironment); env != "" {
		query.Set("sentry_environment", env)
	}
	if release := strings.TrimSpace(cfg.SentryRelease); release != "" {
		query.Set("sentry_release", release)
	}
	reportURL := baseURL + "?" + query.Encode()

	group := "rgw-sentry-csp"
	payload := map[string]any{
		"group":              group,
		"max_age":            10886400,
		"endpoints":          []map[string]string{{"url": reportURL}},
		"include_subdomains": true,
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal report-to payload: %w", err)
	}

	connectOrigin := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	return &sentrySecurityConfig{
		ReportURL:                reportURL,
		ReportGroup:              group,
		ReportToHeader:           string(serialized),
		ReportingEndpointsHeader: fmt.Sprintf("%s=\"%s\"", group, reportURL),
		ConnectOrigin:            connectOrigin,
	}, nil
}
