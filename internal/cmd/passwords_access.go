package cmd

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"
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

// isReachedBySealedAccess is whether the item's signed access record lets this
// recipient have the item, directly or as a member of a circle the record names.
// A record that is missing, not signed by the owner's devices, or an old one the
// server showed again reaches nobody.
func (self *passwordsItem) isReachedBySealedAccess(recipient *client.PasswordRecipientView, circleIDs map[string][]string) bool {
	return passwords.IsReachedByAccess(self.VerifiedAccess, recipient.UserID, circleIDs[recipient.UserID])
}

// accessGranteeOf is a grantee as the sealed record writes it.
func accessGranteeOf(granteeUserId, granteeCircleId string) passwords.PasswordGrantee {
	if granteeCircleId != "" {
		return passwords.PasswordGrantee{CircleID: granteeCircleId}
	}
	return passwords.PasswordGrantee{UserID: granteeUserId}
}

// sealedGrantees are the grantees in the item's signed access record, none if
// it has no record that can be believed.
func (self *passwordsItem) sealedGrantees() []passwords.PasswordGrantee {
	if self.VerifiedAccess == nil {
		return nil
	}
	return self.VerifiedAccess.Grantees
}

// isSameGrantee is whether a grantee of the sealed record is this person or
// circle.
func isSameGrantee(grantee passwords.PasswordGrantee, granteeUserId, granteeCircleId string) bool {
	if granteeCircleId != "" {
		return grantee.CircleID == granteeCircleId
	}
	return granteeUserId != "" && grantee.UserID == granteeUserId
}

// withoutGrantee is a record of access that no longer reaches this person or
// circle. The agent's permission is kept as it was.
func withoutGrantee(access *passwords.PasswordAccess, granteeUserId, granteeCircleId string) *passwords.PasswordAccess {
	remaining := []passwords.PasswordGrantee{}
	if access != nil {
		for _, grantee := range access.Grantees {
			if !isSameGrantee(grantee, granteeUserId, granteeCircleId) {
				remaining = append(remaining, grantee)
			}
		}
	}
	return passwords.AccessWithGrantees(access, remaining)
}

// withGrantee is a record of access that also reaches this person or circle.
func withGrantee(access *passwords.PasswordAccess, granteeUserId, granteeCircleId string) *passwords.PasswordAccess {
	var grantees []passwords.PasswordGrantee
	if access != nil {
		grantees = append(grantees, access.Grantees...)
	}
	for _, grantee := range grantees {
		if isSameGrantee(grantee, granteeUserId, granteeCircleId) {
			return passwords.AccessWithGrantees(access, grantees)
		}
	}
	return passwords.AccessWithGrantees(access, append(grantees, accessGranteeOf(granteeUserId, granteeCircleId)))
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

// passwordsAccessCheck is what opening an item the person owns found out about
// the access record sealed in its fields. Anyone who holds an item key (a person
// it is shared with, the server through the agent's copy) can seal new fields,
// so a record counts only if the owner's devices signed it, and only if it is
// not older than one this computer already saw.
type passwordsAccessCheck int

const (
	// accessCheckNotApplicable is an item shared with the person: they ignore
	// the record, which they cannot check.
	accessCheckNotApplicable passwordsAccessCheck = iota
	// accessCheckVerified is a record signed for this item by the owner's
	// devices, at the highest version this computer has seen.
	accessCheckVerified
	// accessCheckNotVerified is no record, an unsigned one, or one whose
	// signature is wrong. It reaches nobody and gives the agent nothing.
	accessCheckNotVerified
	// accessCheckReplayed is a record that is signed but has a lower version
	// than one this computer saw before: the server showed an old one again.
	accessCheckReplayed
)

// sharingCheckVerdict is the check as a word for --json, empty for an item
// shared with the person.
func (self *passwordsItem) sharingCheckVerdict() string {
	switch self.AccessCheck {
	case accessCheckNotApplicable:
		return ""
	case accessCheckVerified:
		return "checked"
	case accessCheckNotVerified:
		return "notChecked"
	case accessCheckReplayed:
		return "changedBack"
	}
	return ""
}

// sharingCheckWords say what is wrong with an item's sharing, empty when
// nothing is.
func (self *passwordsItem) sharingCheckWords() string {
	switch self.AccessCheck {
	case accessCheckNotVerified:
		return "sharing could not be checked: run teanode passwords share " + self.View.ID + " <username|circle> to set it again"
	case accessCheckReplayed:
		return "the server changed this item's sharing back: run teanode passwords share " + self.View.ID + " <username|circle> to set it again"
	}
	return ""
}

// refuseWithoutVerifiedAccess is the error for an automatic action on an item
// whose sharing cannot be believed, nil when it can.
func (self *passwordsItem) refuseWithoutVerifiedAccess(doing string) error {
	if self.AccessCheck != accessCheckNotVerified && self.AccessCheck != accessCheckReplayed {
		return nil
	}
	return fmt.Errorf("%s: %s", doing, self.sharingCheckWords())
}

// refuseWhileServerListsPeople is the error for setting sharing again on an
// item whose sharing cannot be believed while the server still lists people,
// the agent or a pending new key on it: those would stay as they are, outside
// the new record, so everyone is taken off first.
func (self *passwordsItem) refuseWhileServerListsPeople() error {
	if self.AccessCheck != accessCheckNotVerified && self.AccessCheck != accessCheckReplayed {
		return nil
	}
	if len(self.View.Shares) == 0 && !self.View.NeedsNewKey && !self.View.IsUsableByAgent {
		return nil
	}
	return fmt.Errorf("%s; the server lists people, the agent or a new key on it, so run teanode passwords unshare %s first to take everyone off, then share it again",
		self.sharingCheckWords(), self.View.ID)
}

// setFieldsAccess puts the access record in the fields of a new item.
func setFieldsAccess(fields any, access *passwords.PasswordAccess) {
	switch typed := fields.(type) {
	case *passwords.LoginFields:
		typed.Access = access
	case *passwords.CardFields:
		typed.Access = access
	case *passwords.NoteFields:
		typed.Access = access
	}
}

// isSameAccess is whether two records say the same thing: the same agent
// permission and the same grantees, in any order. Version and signature are not
// compared.
func isSameAccess(first, second *passwords.PasswordAccess) bool {
	if first == nil || second == nil {
		return first == second
	}
	if first.IsUsableByAgent != second.IsUsableByAgent || len(first.Grantees) != len(second.Grantees) {
		return false
	}
	for _, grantee := range first.Grantees {
		if !slices.Contains(second.Grantees, grantee) {
			return false
		}
	}
	return true
}

// checkSealedAccess decides what to believe of the access record sealed in the
// fields of an item the person owns, and remembers what it saw. The record is
// believed when the owner's devices signed it for this item (an account key is
// needed to do that, which the server does not have) and when it is not older
// than the newest this computer saw before: a lower version, or the same
// version with another signature, is an old record shown again. Otherwise
// nothing is derived from it: nobody is reached by it and the agent gets no
// copy.
//
// A computer that has never seen the item before cannot tell an old signed
// record from the newest one, so it trusts the newest one the server shows it
// when the person acts. What it writes down from a view is only the version and
// signature, and only from an item that is not marked as needing a new key and
// not deleted: the view of an item whose access narrowed or that was deleted is
// where an old record is most likely to be shown.
func (self *passwordsSession) checkSealedAccess(item *passwordsItem, plain []byte) {
	var remembered passwordsKnownItem
	if known, err := self.knownKeys(); err != nil {
		self.tellKnownKeysCannotBeUsed(err)
	} else {
		remembered = known.item(item.View.ID)
	}
	item.IsRemembered = remembered.HighestRecordVersion > 0
	item.HighestRecordVersionSeen = remembered.HighestRecordVersion
	item.SealedForUserIDs = remembered.SealedForUserIDs
	item.HasMadeAgentCopy = remembered.HasMadeAgentCopy
	verified, err := passwords.VerifyAccess(self.keys.AccountKey, item.View.ID, item.Kind, plain)
	switch {
	case err != nil:
		item.AccessCheck = accessCheckNotVerified
	case isOlderRecord(verified, remembered):
		item.AccessCheck = accessCheckReplayed
	default:
		item.AccessCheck = accessCheckVerified
		item.VerifiedAccess = verified
		item.HighestRecordVersionSeen = max(remembered.HighestRecordVersion, verified.RecordVersion)
		if item.View.DeletedAt == nil && !item.View.NeedsNewKey {
			self.rememberRecord(item.View.ID, verified)
		}
	}
}

// isOlderRecord is whether a signed record is one this computer should not
// believe because it saw a newer one: its version is lower, or it is the same
// version with another signature (two records of one version cannot both be the
// owner's). A computer that wrote down a version and no signature, as an earlier
// version of this command did, accepts the same version.
func isOlderRecord(record *passwords.PasswordAccess, remembered passwordsKnownItem) bool {
	if record.RecordVersion != remembered.HighestRecordVersion {
		return record.RecordVersion < remembered.HighestRecordVersion
	}
	return remembered.HighestRecordMac != "" && remembered.HighestRecordMac != base64.StdEncoding.EncodeToString(record.RecordMac)
}

// tellKnownKeysCannotBeUsed says once that the file this computer remembers
// things in cannot be used, so that replays are not noticed meanwhile.
func (self *passwordsSession) tellKnownKeysCannotBeUsed(err error) {
	if self.isKnownKeysFaultTold {
		return
	}
	self.isKnownKeysFaultTold = true
	fmt.Fprintf(os.Stderr, "note: the file of known keys cannot be used, so an old copy of an item's sharing would not be noticed: %s\n", forTerminal(err.Error()))
}

// updateRemembered changes what is written down of an item, and tells the
// person when it cannot be written. Failing to write it does not stop the
// command.
func (self *passwordsSession) updateRemembered(itemID string, change func(remembered *passwordsKnownItem)) {
	known, err := self.knownKeys()
	if err != nil {
		return
	}
	if err := known.updateItem(itemID, change); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not write down what is known of %s: %s\n", forTerminal(itemID), forTerminal(err.Error()))
	}
}

// rememberRecord writes down the version and signature of a signed access
// record, if it is newer than what is written down.
func (self *passwordsSession) rememberRecord(itemID string, record *passwords.PasswordAccess) {
	self.updateRemembered(itemID, func(remembered *passwordsKnownItem) {
		if record.RecordVersion > remembered.HighestRecordVersion || remembered.HighestRecordMac == "" && record.RecordVersion == remembered.HighestRecordVersion {
			remembered.HighestRecordVersion = record.RecordVersion
			remembered.HighestRecordMac = base64.StdEncoding.EncodeToString(record.RecordMac)
		}
	})
}

// rememberSealedFor writes down that this computer gave the item key to these
// people, or shared the item with them at the person's command (see
// passwordsKnownItem), and keeps the item in step.
func (self *passwordsSession) rememberSealedFor(item *passwordsItem, userIDs []string) {
	for _, userID := range userIDs {
		if !slices.Contains(item.SealedForUserIDs, userID) {
			item.SealedForUserIDs = append(slices.Clone(item.SealedForUserIDs), userID)
		}
	}
	self.setSealedFor(item, item.SealedForUserIDs)
}

// setSealedFor writes down exactly these people as the ones this computer
// sealed the item for.
func (self *passwordsSession) setSealedFor(item *passwordsItem, userIDs []string) {
	userIDs = slices.Clone(userIDs)
	slices.Sort(userIDs)
	userIDs = slices.Compact(userIDs)
	item.SealedForUserIDs = userIDs
	self.updateRemembered(item.View.ID, func(remembered *passwordsKnownItem) { remembered.SealedForUserIDs = userIDs })
}

// rememberAgentCopy writes down whether this computer itself made the agent's
// copy of the item key.
func (self *passwordsSession) rememberAgentCopy(item *passwordsItem, hasMadeAgentCopy bool) {
	item.HasMadeAgentCopy = hasMadeAgentCopy
	self.updateRemembered(item.View.ID, func(remembered *passwordsKnownItem) { remembered.HasMadeAgentCopy = hasMadeAgentCopy })
}

// signedAccessFor is the record to seal for the item: the signed one it has when
// it already says this, otherwise a new one signed with the owner's account key
// at one version more than the highest of what this computer remembers and the
// verified record the server showed. The version of a record that could not be
// believed is never counted from.
func (self *passwordsSession) signedAccessFor(item *passwordsItem, access *passwords.PasswordAccess) (*passwords.PasswordAccess, error) {
	if item.VerifiedAccess != nil && isSameAccess(item.VerifiedAccess, access) {
		return item.VerifiedAccess, nil
	}
	return passwords.SignAccess(self.keys.AccountKey, item.View.ID, item.Kind, access, item.HighestRecordVersionSeen)
}

// recordAccessWritten notes that the server now has this signed record for the
// item: it is the item's record, and its version the highest seen. This
// computer signed it, so it is written down whatever the item's view says.
func (self *passwordsSession) recordAccessWritten(item *passwordsItem, signed *passwords.PasswordAccess) {
	item.AccessCheck = accessCheckVerified
	item.VerifiedAccess = signed
	item.HighestRecordVersionSeen = max(item.HighestRecordVersionSeen, signed.RecordVersion)
	item.IsRemembered = true
	self.rememberRecord(item.View.ID, signed)
}

// errPasswordsNeedsYou marks what only the person's own command may do: the
// item is left as it is.
var errPasswordsNeedsYou = errors.New("needs you")

// needsYouWords is the line that tells the person which command finishes what
// was left alone, and why.
func needsYouWords(itemID, because string) string {
	return "needs you: teanode passwords rekey " + itemID + " (" + because + ")"
}

// namesOf are the names to show for people, joined for a line.
func namesOf(recipients []*client.PasswordRecipientView) string {
	names := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		name := recipient.Name
		if name == "" {
			name = recipient.Username
		}
		names = append(names, forTerminal(textFromServer(name)))
	}
	return strings.Join(names, ", ")
}

// circleNamesOfShares are the names of the circles the item is shared with, by
// circle id.
func circleNamesOfShares(shares []*client.PasswordShareView) map[string]string {
	names := map[string]string{}
	for _, share := range shares {
		if share.GranteeCircleID != nil {
			names[*share.GranteeCircleID] = share.GranteeName
		}
	}
	return names
}

// circleMemberLines are the lines that say who is given the item through a
// circle for the first time: a recipient the record reaches only as a member of
// a circle, whom this computer never sealed the item for. Who is in a circle is
// the server's word, so the person is told.
func (self *passwordsItem) circleMemberLines(record *passwords.PasswordAccess, recipients []*client.PasswordRecipientView, circleIDs map[string][]string, circleNames map[string]string) []string {
	var lines []string
	for _, recipient := range recipients {
		if slices.Contains(self.SealedForUserIDs, recipient.UserID) || passwords.IsReachedByAccess(record, recipient.UserID, nil) {
			continue
		}
		for _, circleID := range circleIDs[recipient.UserID] {
			if !passwords.IsReachedByAccess(record, recipient.UserID, []string{circleID}) {
				continue
			}
			name := recipient.Name
			if name == "" {
				name = recipient.Username
			}
			lines = append(lines, fmt.Sprintf("%s got %s through %s", forTerminal(textFromServer(name)), forTerminal(self.Name()), forTerminal(textFromServer(circleNames[circleID]))))
			break
		}
	}
	return lines
}
