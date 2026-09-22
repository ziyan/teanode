package agent

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// deviceIngestCursor reads the existing persisted map without changing its keys.
// Missing pass metadata remains valid for cursors saved by older readers.
type deviceIngestCursor struct {
	After            string
	KnownID          string
	KnownSent        string
	PassStartedAt    time.Time
	PassSeenCount    int
	PassRefusedCount int
}

func readDeviceCursor(cursor map[string]any, now time.Time) (deviceIngestCursor, error) {
	position := deviceIngestCursor{}
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"after", &position.After}, {cursorKnownID, &position.KnownID}, {cursorKnownSent, &position.KnownSent},
	} {
		raw := cursor[field.key]
		if raw == nil {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			return deviceIngestCursor{}, fmt.Errorf("source cursor %s must be a string", field.key)
		}
		*field.target = text
	}
	if raw := cursor[cursorPassStarted]; raw != nil {
		text, ok := raw.(string)
		if !ok {
			return deviceIngestCursor{}, fmt.Errorf("source cursor pass start must be a string")
		}
		if text != "" {
			started, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return deviceIngestCursor{}, fmt.Errorf("invalid source pass start: %w", err)
			}
			if started.After(now) {
				return deviceIngestCursor{}, fmt.Errorf("source pass start is in the future")
			}
			position.PassStartedAt = started
		}
	}
	for _, field := range []struct {
		key    string
		target *int
	}{
		{cursorPassSeen, &position.PassSeenCount}, {cursorPassRefused, &position.PassRefusedCount},
	} {
		switch number := cursor[field.key].(type) {
		case nil:
		case int:
			if number < 0 {
				return deviceIngestCursor{}, fmt.Errorf("source cursor %s must not be negative", field.key)
			}
			*field.target = number
		case float64:
			if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || math.Trunc(number) != number || number >= math.Exp2(float64(strconv.IntSize-1)) {
				return deviceIngestCursor{}, fmt.Errorf("source cursor %s is not a supported count", field.key)
			}
			*field.target = int(number)
		default:
			return deviceIngestCursor{}, fmt.Errorf("source cursor %s must be a count", field.key)
		}
	}
	return position, nil
}
