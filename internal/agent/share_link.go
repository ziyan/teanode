package agent

import (
	"net/url"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
)

var _ tools.Linking = (*AskRun)(nil)

// SharedLink is an address that opens one file with no sign-in, on the
// dashboard the turn came through (tools.Linking), for a person who asked
// for a link to open elsewhere; empty when the turn did not come through
// the dashboard.
func (self *AskRun) SharedLink(attachmentId string) string {
	if self.settings.Origin == "" {
		return ""
	}
	share := self.agent.ShareAttachment(attachmentId, time.Now().Add(ShareToOpenFor))
	if share == "" {
		return ""
	}
	return self.settings.Origin + "/api/v1/agent/attachments/" + url.PathEscape(attachmentId) + "?share=" + url.QueryEscape(share)
}

// IsLinkInPlaceOfPath is false: the dashboard shows a file by its path with
// the person's session, and a link is for when they ask for one.
func (self *AskRun) IsLinkInPlaceOfPath() bool { return false }
