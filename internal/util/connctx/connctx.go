// Package connctx provides helpers to set connection deadlines and watch for cancellation.
package connctx

import (
	"context"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.MustGetLogger("connctx")

type Conn interface {
	SetDeadline(time.Time) error
	Close() error
}

func SetDeadlineAndWatchForCancel(ctx context.Context, conn Conn) func() {
	// set deadlines
	var deadlineSet bool
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			log.Warningf("failed to set deadline: %s", err)
		} else {
			deadlineSet = true
		}
	}

	// also watch for cancel
	done := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer waitGroup.Done()
		select {
		case <-ctx.Done():
			switch ctx.Err() {
			case context.Canceled:
				log.Warningf("closing connection %v because context is canceled", conn)
				_ = conn.Close()
			case context.DeadlineExceeded:
				if !deadlineSet {
					log.Warningf("closing connection %v because deadline has been exceeded", conn)
					_ = conn.Close()
				}
			default:
				log.Warningf("got unknown error %v", ctx.Err())
			}
		case <-done:
		}
	}()

	// return a clean up function
	return func() {
		// signal the goroutine to quit
		close(done)

		// wait for it to quit
		waitGroup.Wait()

		// unset the deadline if we set one
		if deadlineSet {
			_ = conn.SetDeadline(time.Time{})
		}
	}
}
