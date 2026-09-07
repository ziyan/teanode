package mailer

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// mediaSource finds a picture this server stores, referred to the way the
// editors write it: src="/media/<identifier>".
//
// Only that exact shape. A picture the operator pasted from somewhere else is
// theirs and points where it points; rewriting it would be taking over a link
// this server has nothing to do with.
var mediaSource = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc=")/media/([A-Za-z0-9]+)(")`)

// tokenBytes is how much randomness an address carries.
//
// Sixteen bytes, base32 without padding, which is twenty-six characters. These
// are reachable by anybody on the internet with no session: one that could be
// guessed from another would let a stranger fetch a picture meant for somebody
// else's message and mark it opened.
const tokenBytes = 16

func newToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mailer: cannot make an address: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)), nil
}

// rewriteMedia gives every picture in a message an address of its own, under
// the sending domain, and records what each one resolves to.
//
// Two things at once, and the second is why the first is done this way. The
// address is under the domain the message is sent from, so a recipient reading
// where the picture came from sees that domain and no other — the same reason
// its mail server name and its signing key are its own. And because the
// address is unique to this message, a fetch of it says this message was
// opened.
//
// A failure to record one is not a failure to send. The picture still resolves
// — the row is what an address means, so a missing row means a broken picture,
// which is worse than a message not sent only if the message matters less than
// the logo in it. It does not: the picture is dropped back to the address that
// works for everybody, and the send goes on.
func (self *mailer) rewriteMedia(envelopeId string, domain *models.Domain, domains []*models.Domain, html string) string {
	if html == "" || domain == nil {
		return html
	}
	host := self.config.Current().LinkHostFor(domain, domains)
	if host == "" {
		return html
	}

	return mediaSource.ReplaceAllStringFunc(html, func(match string) string {
		parts := mediaSource.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		prefix, mediaId, suffix := parts[1], parts[2], parts[3]

		// Only this domain's own pictures. A template naming another domain's
		// media is a mistake somebody made, and quietly serving it from here
		// would hide it.
		stored, err := self.database.GetMedia(mediaId)
		if err != nil || stored == nil || stored.DomainID != domain.ID {
			if err != nil {
				log.Warningf("failed to look up media %s while sending: %s", mediaId, err)
			}
			return match
		}

		token, err := newToken()
		if err != nil {
			log.Errorf("failed to make an address for media %s: %s", mediaId, err)
			return match
		}
		if _, err := self.database.CreateMediaLink(&models.MediaLink{
			Token:      token,
			MediaID:    stored.ID,
			EnvelopeID: envelopeId,
		}); err != nil {
			log.Errorf("failed to record the address for media %s: %s", mediaId, err)
			return match
		}

		return prefix + "https://" + host + api.MediaLinkPath(token) + suffix
	})
}
