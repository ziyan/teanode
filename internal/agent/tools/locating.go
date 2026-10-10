package tools

import (
	"context"
	"time"
)

// Locating is a run that can ask the browser the person is writing in
// where it is. A run anywhere else -- a terminal, a chat app, a mail, a
// run with nobody present -- says so instead of guessing.
type Locating interface {
	// Locate asks the browser and waits for its answer. The error says
	// why there is none: no browser to ask, the person did not allow it,
	// or the browser did not answer in time.
	Locate(ctx context.Context, callId string) (*BrowserLocation, error)
}

// BrowserLocation is where the person's browser said it is, as the
// browser's geolocation reports it.
type BrowserLocation struct {
	LatitudeDegrees  float64   `json:"latitudeDegrees"`
	LongitudeDegrees float64   `json:"longitudeDegrees"`
	AccuracyMeters   float64   `json:"accuracyMeters"`
	MeasuredAt       time.Time `json:"measuredAt"`
}
