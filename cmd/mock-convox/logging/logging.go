package logging

import base "github.com/DocSpring/rack-gateway/internal/logging"

var logger = base.NewLogger()

// Topic constants define available logging topics for the mock Convox server
const (
	// TopicHTTP enables HTTP request/response logging
	TopicHTTP         = "http"
	TopicHTTPHeaders  = "http.headers"
	TopicHTTPRequest  = "http.request"
	TopicHTTPResponse = "http.response"
	TopicAuth         = "auth"
	TopicApp          = "app"
	TopicAppObjects   = "app.objects"
	TopicAppProcesses = "app.processes"
	TopicAppDeploy    = "app.deploy"
)

// Reload reloads the logger configuration
func Reload() { logger.Reload() }

// DebugTopicf logs a debug message for a specific topic
func DebugTopicf(topic, format string, args ...interface{}) {
	logger.DebugTopicf(topic, format, args...)
}

// Debugf logs a debug message
func Debugf(format string, args ...interface{}) { logger.Debugf(format, args...) }

// Infof logs an info message
func Infof(format string, args ...interface{}) { logger.Infof(format, args...) }

// Warnf logs a warning message
func Warnf(format string, args ...interface{}) { logger.Warnf(format, args...) }

// Errorf logs an error message
func Errorf(format string, args ...interface{}) { logger.Errorf(format, args...) }

// TopicEnabled checks if a logging topic is enabled
func TopicEnabled(topic string) bool { return logger.TopicEnabled(topic) }
