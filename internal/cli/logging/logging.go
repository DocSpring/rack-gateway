package logging

import gwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"

const (
	TopicHTTP     = gwlog.TopicHTTP
	TopicHTTPBody = gwlog.TopicHTTPResponseBody
)

var (
	Reload       = gwlog.Reload
	DebugTopicf  = gwlog.DebugTopicf
	Debugf       = gwlog.Debugf
	Infof        = gwlog.Infof
	Warnf        = gwlog.Warnf
	Errorf       = gwlog.Errorf
	TopicEnabled = gwlog.TopicEnabled
)
