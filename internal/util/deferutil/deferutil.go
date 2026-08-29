// Package deferutil contains the guard every goroutine in this codebase starts
// with.
package deferutil

import (
	"runtime/debug"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("deferutil")

// Recover stops a panic in a goroutine from taking the process down.
//
// A panic on the main goroutine can be allowed to crash: something is
// fundamentally wrong and restarting is the right answer. A panic in one of the
// many goroutines this server runs is different. A single malformed message
// that trips a nil dereference while being parsed should cost that message, not
// every connection currently open, every delivery in flight, and the queue
// sweep. Mail servers are fed arbitrary input by strangers, so this happens.
//
// Use it as the first line of every goroutine:
//
//	go func() {
//		defer deferutil.Recover()
//		...
//	}()
//
// The stack is logged at ERROR, because a panic is always a bug and swallowing
// one silently is worse than crashing.
func Recover() {
	if recovered := recover(); recovered != nil {
		log.Errorf("recovered from a panic in a goroutine: %v\n%s", recovered, debug.Stack())
	}
}

// RecoverWith is Recover with a caller-supplied description of what the
// goroutine was doing, for when the stack alone does not say.
func RecoverWith(what string) {
	if recovered := recover(); recovered != nil {
		log.Errorf("recovered from a panic while %s: %v\n%s", what, recovered, debug.Stack())
	}
}
