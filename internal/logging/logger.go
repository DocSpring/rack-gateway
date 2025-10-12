package logging

import (
	"log"
	"os"
	"strings"
	"sync"
)

// Level represents the severity of a log message.
type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

// Options configure a logger instance.
type Options struct {
	Prefix        string
	LevelEnv      string
	TopicsEnv     string
	DefaultLevel  Level
	DefaultTopics []string
}

// Option configures a logger.
type Option func(*Options)

// WithLevelEnv sets the environment variable consulted for log level.
func WithLevelEnv(name string) Option {
	return func(o *Options) {
		if strings.TrimSpace(name) != "" {
			o.LevelEnv = name
		}
	}
}

// WithTopicsEnv sets the environment variable consulted for topics.
func WithTopicsEnv(name string) Option {
	return func(o *Options) {
		if strings.TrimSpace(name) != "" {
			o.TopicsEnv = name
		}
	}
}

// WithDefaultTopics pre-enables the supplied topics.
func WithDefaultTopics(topics ...string) Option {
	return func(o *Options) {
		o.DefaultTopics = append(o.DefaultTopics, topics...)
	}
}

// WithDefaultLevel sets the starting level before env overrides.
func WithDefaultLevel(level Level) Option {
	return func(o *Options) {
		o.DefaultLevel = level
	}
}

// Logger provides leveled, topic-aware logging controlled via environment variables.
type Logger struct {
	mu sync.RWMutex

	prefix string
	level  Level

	levelEnv  string
	topicsEnv string

	defaultTopics []string

	wildcard     bool
	explicit     map[string]struct{}
	prefixTopics []string
}

// NewLogger constructs a new logger.
func NewLogger(prefix string, opts ...Option) *Logger {
	options := Options{
		Prefix:       strings.TrimSpace(prefix),
		LevelEnv:     "LOG_LEVEL",
		TopicsEnv:    "DEBUG_TOPICS",
		DefaultLevel: Info,
	}
	for _, opt := range opts {
		opt(&options)
	}

	l := &Logger{
		prefix:        options.Prefix,
		levelEnv:      options.LevelEnv,
		topicsEnv:     options.TopicsEnv,
		defaultTopics: normalizeTopics(options.DefaultTopics),
	}
	l.reload()
	return l
}

// Reload refreshes level and topic configuration from the environment.
func (l *Logger) Reload() {
	l.reload()
}

// Level returns the current logging level.
func (l *Logger) Level() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// SetLevel overrides the current logging level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Enabled reports whether debug logging for a specific topic should emit.
func (l *Logger) Enabled(topic string) bool {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()
	return level <= Debug && l.TopicEnabled(topic)
}

// TopicEnabled reports whether a topic is currently active.
func (l *Logger) TopicEnabled(topic string) bool {
	n := normalizeTopic(topic)
	if n == "" {
		return false
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.wildcard {
		return true
	}
	if _, ok := l.explicit[n]; ok {
		return true
	}
	for _, prefix := range l.prefixTopics {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// DebugTopicf logs a message under a specific topic when enabled.
func (l *Logger) DebugTopicf(topic, format string, args ...interface{}) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()
	if level > Debug {
		return
	}
	if !l.TopicEnabled(topic) {
		return
	}
	l.logf("DEBUG", topic, format, args...)
}

// Debugf logs a debug-level message.
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()
	if level > Debug {
		return
	}
	l.logf("DEBUG", "", format, args...)
}

// Infof logs an info-level message.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()
	if level > Info {
		return
	}
	l.logf("INFO", "", format, args...)
}

// Warnf logs a warning-level message.
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()
	if level > Warn {
		return
	}
	l.logf("WARN", "", format, args...)
}

// Errorf logs an error-level message.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()
	if level > Error {
		return
	}
	l.logf("ERROR", "", format, args...)
}

func (l *Logger) logf(level, topic, format string, args ...interface{}) {
	prefix := "[" + level
	if l.prefix != "" {
		prefix += " " + l.prefix
	}
	if topic != "" {
		prefix += " " + topic
	}
	prefix += "]"
	log.Printf(prefix+" "+format, args...)
}

func (l *Logger) reload() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.level = parseLevel(os.Getenv(l.levelEnv), l.level)
	l.explicit = make(map[string]struct{})
	l.prefixTopics = nil
	l.wildcard = false

	l.addTopicsLocked(l.defaultTopics...)
	l.addTopicsLocked(parseTopicList(os.Getenv(l.topicsEnv))...)
}

func (l *Logger) addTopicsLocked(topics ...string) {
	for _, topic := range topics {
		n := normalizeTopic(topic)
		if n == "" {
			continue
		}
		if n == "*" || n == "all" {
			l.wildcard = true
			continue
		}
		if strings.HasSuffix(n, ".*") {
			prefix := strings.TrimSuffix(n, ".*")
			if prefix != "" {
				l.prefixTopics = append(l.prefixTopics, prefix+".")
			}
			continue
		}
		if strings.HasSuffix(n, "*") {
			prefix := strings.TrimSuffix(n, "*")
			if prefix != "" {
				l.prefixTopics = append(l.prefixTopics, prefix)
			}
			continue
		}
		l.explicit[n] = struct{}{}
	}
}

func parseLevel(raw string, fallback Level) Level {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "debug", "trace":
		return Debug
	case "info":
		return Info
	case "warn", "warning":
		return Warn
	case "error", "err":
		return Error
	default:
		return fallback
	}
}

func parseTopicList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';':
			return true
		default:
			return false
		}
	})
	return normalizeTopics(parts)
}

func normalizeTopics(topics []string) []string {
	result := make([]string, 0, len(topics))
	for _, topic := range topics {
	if n := normalizeTopic(topic); n != "" {
		result = append(result, n)
	}
	}
	return result
}

func normalizeTopic(topic string) string {
	return strings.Trim(strings.ToLower(topic), " \t\r\n")
}

