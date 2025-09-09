package logging

import (
	"log"
	"os"
	"strings"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

var current Level = Info

func init() {
	setLevelFromEnv()
}

func setLevelFromEnv() {
	lvl := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	switch lvl {
	case "debug", "trace":
		current = Debug
	case "info", "":
		current = Info
	case "warn", "warning":
		current = Warn
	case "error", "err":
		current = Error
	default:
		current = Info
	}
}

func SetLevel(l Level) { current = l }
func GetLevel() Level  { return current }

func Debugf(format string, args ...interface{}) {
	if current <= Debug {
		log.Printf(format, args...)
	}
}
func Infof(format string, args ...interface{}) {
	if current <= Info {
		log.Printf(format, args...)
	}
}
func Warnf(format string, args ...interface{}) {
	if current <= Warn {
		log.Printf(format, args...)
	}
}
func Errorf(format string, args ...interface{}) {
	if current <= Error {
		log.Printf(format, args...)
	}
}
