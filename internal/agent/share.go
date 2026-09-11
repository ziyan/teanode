package agent

import (
	"crypto/hmac"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// A shared artifact: an address for a page the agent made that needs no
// sign-in, for a chat app to open in a browser that has no session here.
// Telegram and Discord show an attached page as a file to download and
// never run its script, so a chart in it would be blank; a link opens it
// in the browser, drawn. The address names one artifact, is signed with
// the server's secret, and expires; it grants nothing else.

// ShareFor is how long a shared artifact's address stays good: long
// enough to find in a chat's history, not forever.
const ShareFor = 30 * 24 * time.Hour

// ShareArtifact is the query value that opens the artifact until the
// time given, or "" when the server has no secret to sign with.
func (self *Agent) ShareArtifact(attachmentId string, until time.Time) string {
	secret := self.settings.Configuration().Secret()
	if len(secret) == 0 {
		return ""
	}
	expires := strconv.FormatInt(until.Unix(), 10)
	return expires + "." + hex.EncodeToString(security.SignString(attachmentId+"\n"+expires, secret))
}

// SharedArtifact says whether the query value opens the artifact now.
func (self *Agent) SharedArtifact(attachmentId, share string) bool {
	secret := self.settings.Configuration().Secret()
	expires, signature, ok := strings.Cut(share, ".")
	if !ok || len(secret) == 0 {
		return false
	}
	until, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || time.Now().Unix() > until {
		return false
	}
	given, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(given, security.SignString(attachmentId+"\n"+expires, secret))
}
