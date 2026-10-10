package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
)

var _ tools.Locating = (*AskRun)(nil)

// locationWait is how long a location call waits for the browser. The
// first time, the browser asks the person whether the dashboard may know
// where they are, and that question waits on them.
const locationWait = time.Minute

// locationLine tells a turn written anywhere but the dashboard in a
// browser that it has no way to know where the person is, so the agent
// asks instead of guessing. The location tool is not offered there.
const locationLine = "Their location is not available here: it comes only from their browser while they chat with you in the dashboard. Ask them where they are when it matters."

// LocationAnswer is what the browser says to a location call: where it
// is, or, in ErrorMessage, why it cannot say.
type LocationAnswer struct {
	LatitudeDegrees  float64   `json:"latitudeDegrees"`
	LongitudeDegrees float64   `json:"longitudeDegrees"`
	AccuracyMeters   float64   `json:"accuracyMeters"`
	MeasuredAt       time.Time `json:"measuredAt"`
	ErrorMessage     string    `json:"errorMessage,omitempty"`
}

// Locate asks the browser the person is writing in where it is
// (tools.Locating): the drawer that sent the turn answers the event with
// AnswerLocation. A turn not written in the dashboard in a browser has
// none to ask, and says so; the tool is not offered there (BrowserOnly),
// and a subagent's turn is not one.
func (self *AskRun) Locate(ctx context.Context, callId string) (*tools.BrowserLocation, error) {
	if self.settings.Headless || !surfaceOf(self.settings.Surface).canLocate {
		where := strings.TrimSpace(self.settings.Surface)
		if self.settings.Headless || where == "" || where == backgroundSurface {
			where = "nobody is writing to you now"
		} else {
			where = "this turn came through the " + where
		}
		return nil, fmt.Errorf("location_unavailable: the location comes from the browser the person chats with you in, and they are not chatting in the dashboard in a browser (%s). Ask them where they are if it matters", where)
	}
	channel := make(chan string, 1)
	self.mutex.Lock()
	if self.locations == nil {
		self.locations = map[string]chan string{}
	}
	self.locations[callId] = channel
	self.mutex.Unlock()
	defer func() {
		self.mutex.Lock()
		delete(self.locations, callId)
		self.mutex.Unlock()
	}()
	self.emit(Event{Kind: EventLocate, CallID: callId, Tool: "location", DrawerID: self.settings.DrawerID})
	timer := time.NewTimer(locationWait)
	defer timer.Stop()
	select {
	case said, ok := <-channel:
		if !ok {
			return nil, fmt.Errorf("location_unavailable: the turn ended before the browser answered")
		}
		return locationFrom(said)
	case <-timer.C:
		return nil, fmt.Errorf("location_unavailable: the browser did not answer within %s; the dashboard may have been closed, or the person has not answered the browser's question whether to share their location. Ask them where they are if it matters", locationWait)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// AnswerLocation is the browser's answer to a location call, as
// LocationAnswer's JSON. It says whether a call was waiting for it. A
// message handed to a running turn follows that turn's events under its
// own id, so its answer goes on to the turn that asked.
func (self *AskRun) AnswerLocation(callId, said string) bool {
	self.mutex.Lock()
	channel, ok := self.locations[callId]
	if ok {
		delete(self.locations, callId)
	}
	self.mutex.Unlock()
	if !ok {
		if into := self.steeredInto; into != nil {
			return into.AnswerLocation(callId, said)
		}
		return false
	}
	channel <- said
	return true
}

// locationFrom reads the browser's answer, refusing one that is not a
// place on the earth.
func locationFrom(said string) (*tools.BrowserLocation, error) {
	var answer LocationAnswer
	if err := json.Unmarshal([]byte(said), &answer); err != nil {
		return nil, fmt.Errorf("location_unavailable: the browser's answer could not be read")
	}
	if message := strings.TrimSpace(answer.ErrorMessage); message != "" {
		return nil, fmt.Errorf("location_unavailable: the browser could not say where it is (%s). Ask the person where they are if it matters", cutMarked(message, 200))
	}
	if !isOnEarth(answer.LatitudeDegrees, answer.LongitudeDegrees) || answer.AccuracyMeters < 0 || math.IsNaN(answer.AccuracyMeters) {
		return nil, fmt.Errorf("location_unavailable: the browser answered with a place that is not on the earth")
	}
	return &tools.BrowserLocation{
		LatitudeDegrees:  answer.LatitudeDegrees,
		LongitudeDegrees: answer.LongitudeDegrees,
		AccuracyMeters:   answer.AccuracyMeters,
		MeasuredAt:       answer.MeasuredAt,
	}, nil
}

func isOnEarth(latitudeDegrees, longitudeDegrees float64) bool {
	return latitudeDegrees >= -90 && latitudeDegrees <= 90 && longitudeDegrees >= -180 && longitudeDegrees <= 180
}
