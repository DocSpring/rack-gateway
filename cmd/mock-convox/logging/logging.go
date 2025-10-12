package logging

import base "github.com/DocSpring/rack-gateway/internal/logging"

var logger = base.NewLogger()

const (
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

func Reload() { logger.Reload() }

func DebugTopicf(topic, format string, args ...interface{}) {
	logger.DebugTopicf(topic, format, args...)
}
func Debugf(format string, args ...interface{}) { logger.Debugf(format, args...) }
func Infof(format string, args ...interface{})  { logger.Infof(format, args...) }
func Warnf(format string, args ...interface{})  { logger.Warnf(format, args...) }
func Errorf(format string, args ...interface{}) { logger.Errorf(format, args...) }
func TopicEnabled(topic string) bool            { return logger.TopicEnabled(topic) }
