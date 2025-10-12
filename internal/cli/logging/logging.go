package logging

import base "github.com/DocSpring/rack-gateway/internal/logging"

var logger = base.NewLogger("cli")

const (
	TopicHTTP = "http"
	TopicHTTPBody = "http.body"
)

func Reload() { logger.Reload() }

func DebugTopicf(topic, format string, args ...interface{}) { logger.DebugTopicf(topic, format, args...) }
func Debugf(format string, args ...interface{})            { logger.Debugf(format, args...) }
func Infof(format string, args ...interface{})             { logger.Infof(format, args...) }
func Warnf(format string, args ...interface{})             { logger.Warnf(format, args...) }
func Errorf(format string, args ...interface{})            { logger.Errorf(format, args...) }
func TopicEnabled(topic string) bool                       { return logger.TopicEnabled(topic) }

