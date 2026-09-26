package agent

import (
	"context"
	"testing"
	"time"
)

// Work that must be done however the night went gets time of its own once
// the night has run out, or is about to; a night with time left keeps its
// own deadline.
func TestTheLastWorkOfANightGetsTimeOfItsOwn(t *testing.T) {
	ample, cancelAmple := context.WithTimeout(context.Background(), time.Hour)
	defer cancelAmple()
	kept, stop := graceAfter(ample, time.Minute)
	stop()
	if kept != ample {
		t.Error("a night with an hour left was given another context")
	}

	spent, cancelSpent := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelSpent()
	<-spent.Done()
	graced, stopGraced := graceAfter(spent, time.Minute)
	defer stopGraced()
	if graced.Err() != nil {
		t.Fatalf("a night that ran out gave no grace: %v", graced.Err())
	}
	if deadline, _ := graced.Deadline(); time.Until(deadline) < 50*time.Second {
		t.Errorf("the grace ends in %s", time.Until(deadline))
	}

	nearly, cancelNearly := context.WithTimeout(context.Background(), time.Second)
	defer cancelNearly()
	extended, stopExtended := graceAfter(nearly, time.Minute)
	defer stopExtended()
	if deadline, _ := extended.Deadline(); time.Until(deadline) < 50*time.Second {
		t.Errorf("a night about to run out kept its second: %s", time.Until(deadline))
	}
}
