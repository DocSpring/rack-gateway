package logging

import base "github.com/DocSpring/rack-gateway/internal/logging"

var logger = base.NewLogger()

// Logging topics for structured debug output.
const (
	// TopicHTTP enables HTTP-level logging.
	TopicHTTP = "http"
	// TopicHTTPRequest enables HTTP request logging.
	TopicHTTPRequest = "http.request"
	// TopicHTTPRequestInfo enables detailed HTTP request info logging.
	TopicHTTPRequestInfo = "http.request.info"
	// TopicHTTPRequestHeaders enables HTTP request header logging.
	TopicHTTPRequestHeaders = "http.request.headers"
	// TopicHTTPRequestBody enables HTTP request body logging.
	TopicHTTPRequestBody = "http.request.body"
	// TopicHTTPResponse enables HTTP response logging.
	TopicHTTPResponse = "http.response"
	// TopicHTTPResponseHeaders enables HTTP response header logging.
	TopicHTTPResponseHeaders = "http.response.headers"
	// TopicHTTPResponseBody enables HTTP response body logging.
	TopicHTTPResponseBody = "http.response.body"
	// TopicSQL enables SQL query logging.
	TopicSQL = "sql"
	// TopicSQLTrace enables SQL trace logging.
	TopicSQLTrace = "sql/trace"
	// TopicAuth enables authentication logging.
	TopicAuth = "auth"
	// TopicMFA enables MFA logging.
	TopicMFA = "mfa"
	// TopicMFAStepUp enables MFA step-up logging.
	TopicMFAStepUp = "mfa.stepup"
	// TopicProxy enables proxy logging.
	TopicProxy = "proxy"
	// TopicEmail enables email logging.
	TopicEmail = "email"
	// TopicEmailSummary enables email summary logging.
	TopicEmailSummary = "email.summary"
	// TopicEmailBody enables email body logging.
	TopicEmailBody = "email.body"
)

// Reload reloads the logger configuration from environment variables.
func Reload() { logger.Reload() }

// DebugTopicf logs a debug message under the specified topic with formatting.
func DebugTopicf(topic, format string, args ...interface{}) {
	logger.DebugTopicf(topic, format, args...)
}

// Debugf logs a debug message with formatting.
func Debugf(format string, args ...interface{}) { logger.Debugf(format, args...) }

// Infof logs an info message with formatting.
func Infof(format string, args ...interface{}) { logger.Infof(format, args...) }

// Warnf logs a warning message with formatting.
func Warnf(format string, args ...interface{}) { logger.Warnf(format, args...) }

// Errorf logs an error message with formatting.
func Errorf(format string, args ...interface{}) { logger.Errorf(format, args...) }

// TopicEnabled checks if a given topic is enabled for logging.
func TopicEnabled(topic string) bool { return logger.TopicEnabled(topic) }
