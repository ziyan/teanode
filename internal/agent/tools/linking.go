package tools

import "context"

// Linking is a run that can hand out an address for a file which opens by
// itself: signed for that one file, expiring, and needing no sign-in.
//
// The dashboard fetches a file with the person's own session, so a plain
// path is enough to show one there. A program using the agent tools over MCP
// has neither the session nor, usually, a token that works outside the tools
// endpoint, so a plain path was a file it could see and not fetch; one fell
// back to copying five megabytes through the shell in base64 chunks. And a
// person who asks for a link to open on their phone has no session there
// either: a conversation hands one out when asked.
type Linking interface {
	// SharedLink is a full address that opens the file, or "" when the run
	// cannot make one; the caller keeps the plain path then.
	SharedLink(attachmentId string) string

	// IsLinkInPlaceOfPath says every file this run hands out goes by its
	// shared link, because whoever reads the answer has no session here: a
	// program over MCP. A conversation hands one out only when asked.
	IsLinkInPlaceOfPath() bool
}

// MainChatSending is a run that can send a file to the person's main chat:
// the conversation the dashboard and the phone open on, whichever
// conversation the run is in, or none.
type MainChatSending interface {
	// SendToMainChat marks one of the agent's files as sent to the main chat,
	// with the line sent with it and who sent it, and posts it there now, or
	// once a turn running there ends.
	SendToMainChat(ctx context.Context, attachmentId, caption, sentBy string) error

	// UploadLink is a full address that takes one PUT of the bytes of a
	// file waiting for them, signed for that file and expiring; "" when the
	// run cannot make one.
	UploadLink(attachmentId string) string
}

type senderNameKey struct{}

// WithSenderName is the context of a call made by a program the person
// connected, under the name it was approved by, so that a file it sends to
// the main chat says who sent it.
func WithSenderName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, senderNameKey{}, name)
}

// SenderNameOf is the name WithSenderName gave, or "".
func SenderNameOf(ctx context.Context) string {
	name, _ := ctx.Value(senderNameKey{}).(string)
	return name
}
