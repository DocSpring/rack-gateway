package logging

import gwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"

// HTTP logging topics re-exported from gateway logging.
const (
	// TopicHTTP is the logging topic for HTTP requests.
	TopicHTTP     = gwlog.TopicHTTP
	TopicHTTPBody = gwlog.TopicHTTPResponseBody
)

// Logging functions re-exported from gateway logging.
var (
	// Reload reloads the logging configuration.
	Reload       = gwlog.Reload
	DebugTopicf  = gwlog.DebugTopicf
	Debugf       = gwlog.Debugf
	Infof        = gwlog.Infof
	Warnf        = gwlog.Warnf
	Errorf       = gwlog.Errorf
	TopicEnabled = gwlog.TopicEnabled
)
