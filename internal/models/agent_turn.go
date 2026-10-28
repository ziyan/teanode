package models

import "time"

// AgentTurn is a turn of the agent running on some instance of the server:
// what lets every instance tell whether a conversation has a turn in flight,
// reach it with a word (stop, an answer, a card), and carry it on when the
// instance running it went away.
type AgentTurn struct {
	RunID          string `json:"runId"`
	ConversationID string `json:"conversationId"`

	// Instance is the name of the instance running it.
	Instance string `json:"instance"`

	StartedAt time.Time `json:"startedAt"`

	// HeartbeatAt is when its instance last said it was still running it. A
	// turn whose instance has not said so for a while is one that instance
	// lost when it stopped without saying so.
	HeartbeatAt time.Time `json:"heartbeatAt"`

	// CutAt is when its instance stopped with the turn still in flight, nil
	// while it runs: the turn waits for whichever instance carries it on.
	CutAt *time.Time `json:"cutAt,omitempty"`
}
