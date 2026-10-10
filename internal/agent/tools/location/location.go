// Package location asks the browser the person is chatting in where it
// is. Only a browser can say, and only while the person writes from the
// dashboard in one: anywhere else the agent is told there is no browser
// to ask, so it asks the person rather than guess.
package location

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name:        "location",
				Family:      tools.FamilyGeneral,
				Core:        true,
				BrowserOnly: true,
				Risk:        tools.RiskRead,
				Description: "Where the person is now: asks the browser they are chatting with you in for its location (latitude, longitude, accuracy). " +
					"Use it when the answer depends on where they are (weather here, what is near me, how long to get home) and they have not said. " +
					"It works only while they write to you from the dashboard in a browser, and the browser may ask them first; anywhere else it says the location is not available, and then you ask them where they are. " +
					"It gives coordinates, not a place name: look the place up when you need one.",
				Parameters: tools.Object(map[string]any{
					"reason": tools.StringProperty("a few words on why, for the line the drawer shows"),
				}),
				Run: runLocation,
			},
		}
	})
}

type locationArguments struct {
	Reason string `json:"reason"`
}

func runLocation(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[locationArguments](call)
	if err != nil {
		return nil, err
	}
	locating, ok := tools.MustRun(ctx).(tools.Locating)
	if !ok {
		return nil, fmt.Errorf("location_unavailable: there is no browser to ask; ask the person where they are")
	}
	found, err := locating.Locate(ctx, call.ID)
	if err != nil {
		return nil, err
	}
	answer := map[string]any{
		"latitudeDegrees":  roundTo(found.LatitudeDegrees, 5),
		"longitudeDegrees": roundTo(found.LongitudeDegrees, 5),
		"accuracyMeters":   math.Round(found.AccuracyMeters),
	}
	if !found.MeasuredAt.IsZero() {
		answer["measuredAt"] = found.MeasuredAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("Located within about %s", accuracyWords(found.AccuracyMeters))
	if reason := strings.TrimSpace(arguments.Reason); reason != "" {
		result.Note += ": " + reason
	}
	return result, nil
}

// roundTo keeps the digits that mean something: five places of a degree
// is about a meter, finer than any browser measures.
func roundTo(value float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(value*scale) / scale
}

// accuracyWords is how close the fix is, for the drawer's line.
func accuracyWords(accuracyMeters float64) string {
	if accuracyMeters >= 1000 {
		return fmt.Sprintf("%.0f km", accuracyMeters/1000)
	}
	return fmt.Sprintf("%.0f m", accuracyMeters)
}
