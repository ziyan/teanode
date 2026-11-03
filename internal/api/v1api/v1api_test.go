package v1api

import (
	"context"
	"testing"
	"time"
)

// The server finds these on the component by asserting an interface; without
// them it never runs the timers, and nothing fails to build.
func TestTheServerReachesTheTimers(t *testing.T) {
	var component any = &v1{}
	if _, ok := component.(interface {
		TellDueVisitNotices(context.Context, time.Time) error
	}); !ok {
		t.Errorf("the visit notices are not reachable")
	}
	if _, ok := component.(interface {
		TickChatCalls(context.Context, time.Time) error
		FailChatCallsLeftGoing(context.Context) error
	}); !ok {
		t.Errorf("the call timers are not reachable")
	}
}
