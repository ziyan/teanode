package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type sentIngestCursor struct {
	ReceivedAt time.Time `json:"receivedAt"`
	ItemID     string    `json:"itemId"`
}

func readSentCursor(cursor map[string]any, now time.Time) (sentIngestCursor, error) {
	position := sentIngestCursor{ReceivedAt: now}
	raw, exists := cursor["before"]
	if !exists || raw == nil {
		return position, nil
	}
	encoded, ok := raw.(string)
	if !ok {
		return sentIngestCursor{}, fmt.Errorf("sent source cursor must be a string")
	}
	if encoded == "" {
		return position, nil
	}
	if !strings.HasPrefix(encoded, "v1:") {
		receivedAt, err := time.Parse(time.RFC3339Nano, encoded)
		if err != nil {
			return sentIngestCursor{}, fmt.Errorf("invalid legacy sent source cursor: %w", err)
		}
		return sentIngestCursor{ReceivedAt: receivedAt}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if err != nil {
		return sentIngestCursor{}, fmt.Errorf("invalid sent source cursor: %w", err)
	}
	position = sentIngestCursor{}
	if err := json.Unmarshal(decoded, &position); err != nil {
		return sentIngestCursor{}, fmt.Errorf("invalid sent source cursor: %w", err)
	}
	if position.ReceivedAt.IsZero() || position.ItemID == "" {
		return sentIngestCursor{}, fmt.Errorf("sent source cursor needs a time and item identifier")
	}
	return position, nil
}

func (self sentIngestCursor) encode() (string, error) {
	encoded, err := json.Marshal(self)
	if err != nil {
		return "", err
	}
	return "v1:" + base64.RawURLEncoding.EncodeToString(encoded), nil
}
