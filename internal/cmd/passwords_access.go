package cmd

import (
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
func accessGranteeOf(granteeUserID, granteeCircleID string) passwords.PasswordGrantee {
	if granteeCircleID != "" {
		return passwords.PasswordGrantee{CircleID: granteeCircleID}
	}
	return passwords.PasswordGrantee{UserID: granteeUserID}
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

// keyHolderUserIDsOf are the people the server shows holding a key for the
// item, sorted, without repeats.
func keyHolderUserIDsOf(view *client.PasswordItemView) []string {
	var userIDs []string
	for _, recipient := range existingRecipients(view) {
		if recipient.HasKey && !slices.Contains(userIDs, recipient.UserID) {
			userIDs = append(userIDs, recipient.UserID)
		}
	}
	slices.Sort(userIDs)
	return userIDs
}

// checkSealedAccess decides what to believe of the access record sealed in the
// fields of an item the person owns, and remembers what it saw. The record is
// believed when the owner's devices signed it for this item (an account key is
// needed to do that, which the server does not have) and when its version is
// not lower than the highest this computer saw before. Otherwise nothing is
// derived from it: nobody is reached by it and the agent gets no copy.
func (self *passwordsSession) checkSealedAccess(item *passwordsItem, plain []byte) {
	var remembered passwordsKnownItem
	if known, err := self.knownKeys(); err != nil {
		self.tellKnownKeysCannotBeUsed(err)
	} else {
		remembered = known.item(item.View.ID)
	}
	item.HighestRecordVersionSeen = remembered.HighestRecordVersion
	item.PreviousKeyHolderUserIDs = remembered.KeyHolderUserIDs
	item.HadAgentCopyBefore = remembered.HasAgentCopy
	verified, err := passwords.VerifyAccess(self.keys.AccountKey, item.View.ID, item.Kind, plain)
	switch {
	case err != nil:
		item.AccessCheck = accessCheckNotVerified
	case verified.RecordVersion < remembered.HighestRecordVersion:
		item.AccessCheck = accessCheckReplayed
	default:
		item.AccessCheck = accessCheckVerified
		item.VerifiedAccess = verified
		item.HighestRecordVersionSeen = max(remembered.HighestRecordVersion, verified.RecordVersion)
		self.rememberItem(item)
	}
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

// rememberItem writes down what this computer now knows of an item whose access
// record is signed: the highest record version, who holds a key and whether the
// agent holds a copy. Failing to write it is told and does not stop the command.
func (self *passwordsSession) rememberItem(item *passwordsItem) {
	known, err := self.knownKeys()
	if err != nil {
		return
	}
	holders := keyHolderUserIDsOf(item.View)
	if err := known.updateItem(item.View.ID, func(remembered *passwordsKnownItem) {
		remembered.HighestRecordVersion = max(remembered.HighestRecordVersion, item.HighestRecordVersionSeen)
		remembered.KeyHolderUserIDs = holders
		remembered.HasAgentCopy = item.View.IsUsableByAgent
	}); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not write down what is known of %s: %s\n", forTerminal(item.View.ID), forTerminal(err.Error()))
	}
}

// rememberRecordVersion writes down the version of the first record of a new
// item.
func (self *passwordsSession) rememberRecordVersion(itemID string, version int) {
	known, err := self.knownKeys()
	if err != nil {
		return
	}
	if err := known.updateItem(itemID, func(remembered *passwordsKnownItem) {
		remembered.HighestRecordVersion = max(remembered.HighestRecordVersion, version)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not write down what is known of %s: %s\n", forTerminal(itemID), forTerminal(err.Error()))
	}
}

// signedAccessFor is the record to seal for the item: the signed one it has when
// it already says this, otherwise a new one signed with the owner's account key
// at one version more than the highest this computer has seen. The version the
// server showed is never counted from.
func (self *passwordsSession) signedAccessFor(item *passwordsItem, access *passwords.PasswordAccess) (*passwords.PasswordAccess, error) {
	if item.VerifiedAccess != nil && isSameAccess(item.VerifiedAccess, access) {
		return item.VerifiedAccess, nil
	}
	return passwords.SignAccess(self.keys.AccountKey, item.View.ID, item.Kind, access, item.HighestRecordVersionSeen)
}

// recordAccessWritten notes that the server now has this signed record for the
// item: it is the item's record, and its version the highest seen.
func (self *passwordsSession) recordAccessWritten(item *passwordsItem, signed *passwords.PasswordAccess) {
	item.AccessCheck = accessCheckVerified
	item.VerifiedAccess = signed
	item.HighestRecordVersionSeen = max(item.HighestRecordVersionSeen, signed.RecordVersion)
	self.rememberItem(item)
}

// afterAccessChange makes what the server now shows of the item (who holds a
// key, the agent's copy) what this computer last saw, once a command of the
// person's own changed it.
func (self *passwordsSession) afterAccessChange(item *passwordsItem) {
	item.PreviousKeyHolderUserIDs = keyHolderUserIDsOf(item.View)
	item.HadAgentCopyBefore = item.View.IsUsableByAgent
	if item.AccessCheck == accessCheckVerified {
		self.rememberItem(item)
	}
}
