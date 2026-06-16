package logger

import (
	"fmt"
	"log"
)

// LogBroadcaster is a callback function set by the router or controller package
// to broadcast log messages to WebSocket clients.
var LogBroadcaster func(level string, message string)

func Log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)

	// Print to console
	log.Printf("[%s] %s", level, msg)

	// Delegate to broadcaster if registered
	if LogBroadcaster != nil {
		LogBroadcaster(level, msg)
	}
}

func Info(format string, args ...interface{}) {
	Log("INFO", format, args...)
}

func Warn(format string, args ...interface{}) {
	Log("WARN", format, args...)
}

func Error(format string, args ...interface{}) {
	Log("ERROR", format, args...)
}
