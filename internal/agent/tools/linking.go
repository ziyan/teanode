package tools

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
