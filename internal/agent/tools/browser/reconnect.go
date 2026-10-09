package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/agent/tools"
)

var log = logging.MustGetLogger("agent")

// browserReconnectWait is how long a request whose connection to the
// person's browser dropped waits for it to come back. The extension puts
// its connection back within a second or two of losing it, and waits
// longer only after several attempts in a row have failed.
const browserReconnectWait = 10 * time.Second

// browserResendableActions are the actions on the person's tab that only
// read it, so that doing one twice is the same as doing it once: the ones
// sent again on a connection that came back. Anything else may have
// happened before the connection dropped, and doing it again could do it
// twice.
var browserResendableActions = map[string]bool{"snapshot": true, "screenshot": true, "tabs": true}

// askTab carries an action to the person's tab and, when the connection
// drops before the answer comes, waits for the extension to connect again:
// an action that only reads is then sent once more, and any other is not,
// since whether it happened is not known.
func askTab(ctx context.Context, run tools.Run, tab tools.Tab, action string, arguments any) (json.RawMessage, error) {
	sentAt := time.Now()
	data, err := tab.Ask(ctx, action, arguments)
	if err == nil || !errors.Is(err, tools.ErrDeviceDetached) {
		return data, err
	}
	droppedAfter := time.Since(sentAt)
	var reconnected tools.Tab
	waitStarted := time.Now()
	if browsing, ok := run.(tools.Browsing); ok {
		reconnected = browsing.ReconnectedTab(ctx, tab, browserReconnectWait)
	}
	waited := time.Since(waitStarted)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	isResendable := browserResendableActions[action]
	isRetried := reconnected != nil && isResendable
	log.Noticef("the browser connection of agent %s dropped during %s, %s after it was sent; waited %s for it to come back (came back: %t); sent again: %t",
		agentIdOf(run), action, droppedAfter.Round(time.Millisecond), waited.Round(time.Millisecond), reconnected != nil, isRetried)
	if !isResendable {
		return nil, droppedMidAction(action, reconnected != nil)
	}
	if reconnected == nil {
		return nil, droppedAndGone()
	}
	data, err = reconnected.Ask(ctx, action, arguments)
	if err != nil && errors.Is(err, tools.ErrDeviceDetached) {
		return nil, droppedAndGone()
	}
	return data, err
}

// droppedMidAction is what the model is told when the connection went
// while an action that changes things was under way.
func droppedMidAction(action string, hasReconnected bool) error {
	if hasReconnected {
		return fmt.Errorf("the connection to the person's browser dropped while %s was under way, so whether it happened is not known, and it was not sent again; the browser has connected again: read the page with snapshot, or tabs, to see whether it took effect before trying it again", action)
	}
	return fmt.Errorf("the connection to the person's browser dropped while %s was under way, so whether it happened is not known, and it was not sent again; it usually reconnects on its own within seconds: in a moment, read the page with snapshot, or tabs, to see whether it took effect before trying it again. Only if the browser stays away, ask the person to check the TeaNode extension and attach the tab again", action)
}

// droppedAndGone is what the model is told when the connection went and
// did not come back while the request waited.
func droppedAndGone() error {
	return fmt.Errorf("the connection to the person's browser dropped and did not come back within %s; it usually reconnects on its own within seconds: try again in a moment, starting with snapshot or tabs to see where things stand. Only if the browser stays away, ask the person to check the TeaNode extension and attach the tab again", browserReconnectWait)
}

// agentIdOf is the run's agent, for the log, or a dash.
func agentIdOf(run tools.Run) string {
	if agent := run.Agent(); agent != nil {
		return agent.ID
	}
	return "-"
}
