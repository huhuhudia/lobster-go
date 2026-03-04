package logging

import "log"

// Logger is a thin wrapper to allow swapping implementations later.
type Logger struct{}

func (Logger) Info(msg string, args ...interface{})  { log.Printf("[INFO] "+msg, args...) }
func (Logger) Warn(msg string, args ...interface{})  { log.Printf("[WARN] "+msg, args...) }
func (Logger) Error(msg string, args ...interface{}) { log.Printf("[ERROR] "+msg, args...) }

var Default = Logger{}
