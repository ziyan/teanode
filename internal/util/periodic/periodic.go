// Package periodic provides a standard way to periodically run a handler at a specific time interval.
package periodic

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.MustGetLogger("periodic")

type Periodic interface {
	Start()
	Stop()
}

type Settings struct {
	Interval       time.Duration
	Name           string
	SkipInitialRun bool
}

type periodic struct {
	ctx    context.Context
	cancel context.CancelFunc

	waitGroup *sync.WaitGroup

	handler func(context.Context) error

	settings *Settings
}

func New(ctx context.Context, waitGroup *sync.WaitGroup, handler func(context.Context) error, settings *Settings) Periodic {
	self := &periodic{
		waitGroup: waitGroup,
		handler:   handler,
		settings:  settings,
	}
	self.ctx, self.cancel = context.WithCancel(ctx)
	return self
}

func (self *periodic) Start() {
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		if !self.settings.SkipInitialRun {
			self.run()
		}
		for {
			select {
			case <-self.ctx.Done():
				return
			case <-time.After(self.settings.Interval):
				self.run()
			}
		}
	}()
}

func (self *periodic) Stop() {
	self.cancel()
}

func (self *periodic) run() {
	defer func() {
		if message := recover(); message != nil {
			log.Fatalf("panic: %s: %s\n%s", self.settings.Name, message, string(debug.Stack()))
		}
	}()
	// The context Stop cancels, not a fresh background one. Handing every
	// handler a context nothing could cancel meant a job that blocked --
	// an external fetch with no deadline, say -- was still running when
	// Close came to wait for it, and the wait never ended. One caller
	// already reaches for its own context to work around this and says so
	// in a comment; it keeps working, because it ignores what it is given.
	if err := self.handler(self.ctx); err != nil {
		log.Errorf("failed while running periodically: %s: %s", self.settings.Name, err)
	}
}
