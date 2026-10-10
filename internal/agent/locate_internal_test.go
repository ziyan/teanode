package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// answerLocationsWith answers every location call the run asks with what
// answer returns, as the drawer that sent the turn does.
func answerLocationsWith(t *testing.T, run *AskRun, answering *AskRun, answer func() LocationAnswer) {
	t.Helper()
	events, unsubscribe := run.Subscribe()
	go func() {
		defer unsubscribe()
		for event := range events {
			if event.Kind != EventLocate {
				continue
			}
			said, err := json.Marshal(answer())
			if err != nil {
				t.Error(err)
				return
			}
			if !answering.AnswerLocation(event.CallID, string(said)) {
				t.Errorf("nothing was waiting for location call %s", event.CallID)
			}
			return
		}
	}()
}

// A turn written in the dashboard asks the browser, naming the drawer that
// sent it, and is told where it is.
func TestLocateAsksTheBrowserTheTurnWasWrittenIn(t *testing.T) {
	run := batchRun()
	defer run.cancel()
	run.settings.DrawerID = "drawer-1"
	events, unsubscribe := run.Subscribe()
	defer unsubscribe()
	asked := make(chan string, 1)
	go func() {
		for event := range events {
			if event.Kind == EventLocate {
				asked <- event.DrawerID
				return
			}
		}
	}()
	answerLocationsWith(t, run, run, func() LocationAnswer {
		return LocationAnswer{LatitudeDegrees: 23.45, LongitudeDegrees: -67.89, AccuracyMeters: 20, MeasuredAt: time.Now()}
	})
	found, err := run.Locate(context.Background(), "call-1")
	if err != nil {
		t.Fatal(err)
	}
	if found.LatitudeDegrees != 23.45 || found.LongitudeDegrees != -67.89 || found.AccuracyMeters != 20 {
		t.Errorf("located at %+v", found)
	}
	if drawerId := <-asked; drawerId != "drawer-1" {
		t.Errorf("the locate event named drawer %q", drawerId)
	}
}

// Anywhere but the dashboard in a browser there is nobody's browser to
// ask, and the turn is told so rather than kept waiting.
func TestLocateSaysThereIsNoBrowserOffTheDashboard(t *testing.T) {
	for _, surface := range []string{"cli", "telegram", "mail", "extension", ""} {
		run := batchRun()
		run.settings.Surface = surface
		_, err := run.Locate(context.Background(), "call-1")
		run.cancel()
		if err == nil || !strings.Contains(err.Error(), "location_unavailable") {
			t.Errorf("on %q: %v", surface, err)
		}
	}
	run := batchRun()
	defer run.cancel()
	run.settings.Headless = true
	if _, err := run.Locate(context.Background(), "call-1"); err == nil || !strings.Contains(err.Error(), "nobody is writing") {
		t.Errorf("headless: %v", err)
	}
}

// The browser saying why it cannot is passed on, and so is a place that
// is not on the earth refused.
func TestLocateTellsWhyTheBrowserCannotSay(t *testing.T) {
	run := batchRun()
	defer run.cancel()
	answerLocationsWith(t, run, run, func() LocationAnswer {
		return LocationAnswer{ErrorMessage: "the person has not allowed the dashboard to know their location"}
	})
	if _, err := run.Locate(context.Background(), "call-1"); err == nil || !strings.Contains(err.Error(), "has not allowed") {
		t.Errorf("refused: %v", err)
	}
	if _, err := locationFrom(`{"latitudeDegrees": 91, "longitudeDegrees": 0}`); err == nil {
		t.Error("a latitude of 91 degrees was taken")
	}
	if run.AnswerLocation("call-unknown", "{}") {
		t.Error("an answer nothing waited for was taken")
	}
}

// A message handed to a running turn follows that turn's events under its
// own id; the drawer answers it there, and the answer reaches the turn
// that asked.
func TestALocationAnswerToASteeredMessageReachesTheTurnThatAsked(t *testing.T) {
	running := batchRun()
	defer running.cancel()
	steered := batchRun()
	defer steered.cancel()
	steered.ID = "run-steered"
	steered.steeredInto = running
	answerLocationsWith(t, running, steered, func() LocationAnswer {
		return LocationAnswer{LatitudeDegrees: -21.09, LongitudeDegrees: 87.65, AccuracyMeters: 1500}
	})
	found, err := running.Locate(context.Background(), "call-1")
	if err != nil {
		t.Fatal(err)
	}
	if found.LongitudeDegrees != 87.65 {
		t.Errorf("located at %+v", found)
	}
}
