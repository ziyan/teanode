package tools

// Linking is a run that can hand out an address for a file which opens by
// itself: signed for that one file, expiring, and needing no sign-in.
//
// A conversation does not need one. The dashboard fetches a file with the
// person's own session, so a plain path is enough there. A program using the
// agent tools over MCP has neither the session nor, usually, a token that
// works outside the tools endpoint, so a plain path was a file it could see
// and not fetch. One program fell back to copying five megabytes through the
// shell in base64 chunks.
type Linking interface {
	// SharedLink is a full address that opens the file, or "" when the run
	// cannot make one; the caller keeps the plain path then.
	SharedLink(attachmentId string) string
}
