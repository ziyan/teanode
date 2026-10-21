package cmd

import (
	"strings"
	"unicode"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// Who may have an item is written down twice. The server keeps a list of
// shares, which it can change on its own. The owner's device also seals a
// record of the same thing inside the item's fields (see
// passwords.PasswordAccess), which the server cannot read or change. A key is
// sealed for somebody only when that sealed record reaches them, so a server
// that invents a share, a waiting person or the agent's permission gets
// nothing sealed for it. The server's list is used for two things only: to
// know who is a member of a circle, and who is waiting for a key.

// circleIDsOfRecipients are the circles the server lists each person as a
// member of, by user id, read from the recipients of the item's circle shares.
func circleIDsOfRecipients(shares []*client.PasswordShareView) map[string][]string {
	circleIDs := map[string][]string{}
	for _, share := range shares {
		if share.GranteeCircleID == nil {
			continue
		}
		for _, recipient := range share.Recipients {
			circleIDs[recipient.UserID] = append(circleIDs[recipient.UserID], *share.GranteeCircleID)
		}
	}
	return circleIDs
}

// isReachedBySealedAccess is whether the item's sealed access record lets this
// recipient have the item, directly or as a member of a circle the record names.
func (self *passwordsItem) isReachedBySealedAccess(recipient *client.PasswordRecipientView, circleIDs map[string][]string) bool {
	return passwords.IsReachedByAccess(self.Access(), recipient.UserID, circleIDs[recipient.UserID])
}

// accessGranteeOf is a grantee as the sealed record writes it.
func accessGranteeOf(granteeUserID, granteeCircleID string) passwords.PasswordGrantee {
	if granteeCircleID != "" {
		return passwords.PasswordGrantee{CircleID: granteeCircleID}
	}
	return passwords.PasswordGrantee{UserID: granteeUserID}
}

// sealedGrantees are the grantees in the item's sealed access record.
func (self *passwordsItem) sealedGrantees() []passwords.PasswordGrantee {
	access := self.Access()
	if access == nil {
		return nil
	}
	return access.Grantees
}

// isSameGrantee is whether a grantee of the sealed record is this person or
// circle.
func isSameGrantee(grantee passwords.PasswordGrantee, granteeUserID, granteeCircleID string) bool {
	if granteeCircleID != "" {
		return grantee.CircleID == granteeCircleID
	}
	return granteeUserID != "" && grantee.UserID == granteeUserID
}

// withoutGrantee is a record of access that no longer reaches this person or
// circle. The agent's permission is kept as it was.
func withoutGrantee(access *passwords.PasswordAccess, granteeUserID, granteeCircleID string) *passwords.PasswordAccess {
	remaining := []passwords.PasswordGrantee{}
	if access != nil {
		for _, grantee := range access.Grantees {
			if !isSameGrantee(grantee, granteeUserID, granteeCircleID) {
				remaining = append(remaining, grantee)
			}
		}
	}
	return passwords.AccessWithGrantees(access, remaining)
}

// withGrantee is a record of access that also reaches this person or circle.
func withGrantee(access *passwords.PasswordAccess, granteeUserID, granteeCircleID string) *passwords.PasswordAccess {
	var grantees []passwords.PasswordGrantee
	if access != nil {
		grantees = append(grantees, access.Grantees...)
	}
	for _, grantee := range grantees {
		if isSameGrantee(grantee, granteeUserID, granteeCircleID) {
			return passwords.AccessWithGrantees(access, grantees)
		}
	}
	return passwords.AccessWithGrantees(access, append(grantees, accessGranteeOf(granteeUserID, granteeCircleID)))
}

// textFromServer makes a name or other text the server supplied fit on one
// line of a question the person answers: it drops line breaks and every
// Unicode format character (zero width characters, and the marks that reverse
// the direction of text), which are the ways a name can pass for something
// else, such as the line that carries a safety code.
func textFromServer(text string) string {
	return strings.Map(func(character rune) rune {
		switch {
		case character == '\n' || character == '\r':
			return ' '
		case unicode.Is(unicode.Cf, character), unicode.Is(unicode.Zl, character), unicode.Is(unicode.Zp, character):
			return -1
		}
		return character
	}, text)
}
