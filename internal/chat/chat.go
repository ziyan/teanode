// Package chat carries what happens in a chat to the people in it: the feed
// of each member's conversation, the other instances of the server, and the
// phones of those who are not looking.
//
// It exists whatever the agent configuration is. A household may run this
// server for mail, calendars and chats with no language model at all, and a
// chat between two people needs no agent to be live. So the feed, the relay
// and the notifications are not the agent's: the agent publishes into the
// same hub when it takes part in a chat, and its own turns keep their feed.
package chat

import (
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("chat")
