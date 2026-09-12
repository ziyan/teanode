package agent

import (
	"crypto/hmac"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// A shared file: an address for one file of a conversation that needs no
// sign-in, because whoever opens it has no session here. A chat app is
// one such reader -- Telegram and Discord show an attached page as a file
// to download and never run its script, so a chart in it would be blank,
// while a link opens it in the browser, drawn. The drawer framed into
// another site is the other: it cannot put a header on a picture or a
// framed page. The address names one file, is signed with the server's
// secret, and expires; it grants nothing else, which is what makes it a
// better answer than putting the person's own token in an address.

// ShareFor is how long a shared artifact's address stays good: long
// enough to find in a chat's history, not forever.
const ShareFor = 30 * 24 * time.Hour

// ShareAttachment is the query value that opens the file until the time
// given, or "" when the server has no secret to sign with.
func (self *Agent) ShareAttachment(attachmentId string, until time.Time) string {
	secret := self.settings.Configuration().Secret()
	if len(secret) == 0 {
		return ""
	}
	expires := strconv.FormatInt(until.Unix(), 10)
	return expires + "." + hex.EncodeToString(security.SignString(attachmentId+"\n"+expires, secret))
}

// SharedAttachment says whether the query value opens the file now.
func (self *Agent) SharedAttachment(attachmentId, share string) bool {
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
