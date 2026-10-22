package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// The public keys this computer knows: one for each person, the first key it
// saw for them, from any source (sharing with them, an item they shared, a
// person waiting for an item). Sharing an item seals its key for the public key
// the server hands out for a person, and nobody is asked about it: the first key
// seen is recorded and used at once, and one line says so. When the server later
// hands out a different key for the same person, that key is accepted, recorded,
// and used from then on, and one line tells the person to compare safety codes
// with them. A person has one key for all their devices, so a changed key means
// they erased Passwords and set it up again, or a server put a key of its own in
// their place; only comparing codes can tell which, and nothing waits on it.
//
// Comparing codes is optional. 'teanode passwords trust' marks the key as
// compared when the code given matches; the mark is only shown and gates nothing,
// and a changed key clears it.
//
// The keys are public, so the file is not secret, but it is kept private to its
// owner because changing it is the same as changing which keys are used.

// passwordsKnownKey is what is known of one person.
type passwordsKnownKey struct {
	Username string `json:"username"`
	Name     string `json:"name"`

	// PublicKey is the key this computer uses for the person: the first key it
	// saw, or the one that replaced it when the server handed out another.
	PublicKey string `json:"publicKey,omitempty"`

	// FirstSeenAt is when this computer first saw a key for the person.
	FirstSeenAt *time.Time `json:"firstSeenAt,omitempty"`

	// ComparedAt is when the person marked this key as compared with the other
	// person (teanode passwords trust). It gates nothing, and is cleared when the
	// key changes.
	ComparedAt *time.Time `json:"comparedAt,omitempty"`

	// The fields an earlier version wrote, which kept a key the person confirmed
	// apart from one that was only seen. They are read into PublicKey and
	// ComparedAt and are not written again.
	EarlierConfirmedPublicKey string     `json:"confirmedPublicKey,omitempty"`
	EarlierConfirmedAt        *time.Time `json:"confirmedAt,omitempty"`
	EarlierSeenPublicKey      string     `json:"seenPublicKey,omitempty"`
	EarlierSeenAt             *time.Time `json:"seenAt,omitempty"`
}

// passwordsKnownItem is what this computer remembers of one item the person
// owns, so that a server cannot turn the item's sharing back, or widen it, by
// showing an older or a made up state. Everything here comes from what this
// computer saw signed by the owner's devices or did itself. Nothing is copied
// from the server's list of who holds a key or whether the agent has a copy: a
// computer that has never seen an item before cannot tell an old signed record
// from the newest one, so it never acts on its own on the strength of one.
type passwordsKnownItem struct {
	// HighestRecordVersion is the highest recordVersion of the access record
	// this computer has seen with a valid signature, and HighestRecordMac the
	// signature of that record (base64). A record with a lower version is an old
	// one the server showed again, and so is one with the same version and
	// another signature. Only a record seen on an item that was not marked as
	// needing a new key and not deleted is written here, or one this computer
	// signed itself.
	HighestRecordVersion int    `json:"highestRecordVersion,omitempty"`
	HighestRecordMac     string `json:"highestRecordMac,omitempty"`

	// SealedForUserIDs are the people this computer itself gave the item key to,
	// or shared the item with at the person's command while they had no key yet.
	// A re-key nobody asked for, and sharing with those who were waiting, seal
	// only for them.
	SealedForUserIDs []string `json:"sealedForUserIds,omitempty"`

	// HasMadeAgentCopy is whether this computer itself made the agent's copy of
	// the item key. A re-key nobody asked for makes one only if it did.
	HasMadeAgentCopy bool `json:"hasMadeAgentCopy,omitempty"`

	// LastAutomaticRekeyAt is when this computer last gave the item a new key
	// nobody asked for, for the wait between two of them.
	LastAutomaticRekeyAt *time.Time `json:"lastAutomaticRekeyAt,omitempty"`
}

// passwordsKnownKeysFile is what is in the file: the known keys of each server
// by that server's name, then by user id. Keeping every server in one file means
// a profile that is pointed at another server, and back, does not lose the keys
// it had written down for the first.
type passwordsKnownKeysFile struct {
	Servers map[string]map[string]*passwordsKnownKey `json:"servers"`

	// Items is what this computer remembers of each item the person owns, by
	// server and then item id (see passwordsKnownItem).
	Items map[string]map[string]*passwordsKnownItem `json:"items,omitempty"`

	// Server and People are the file as it was first written, for one server.
	// They are read and moved into Servers, and not written again.
	LegacyServer string                        `json:"server,omitempty"`
	LegacyPeople map[string]*passwordsKnownKey `json:"people,omitempty"`
}

// passwordsKnownKeys is the file, read into memory, for one server.
type passwordsKnownKeys struct {
	path   string
	server string
	file   passwordsKnownKeysFile
}

// passwordsKnownKeysPath is where the known keys of the profile this command
// talks to are kept, and the name of its server. They are kept in the
// configuration directory: they must outlast a login session.
func passwordsKnownKeysPath(command *cli.Command) (path, server string, err error) {
	name, server, err := passwordsProfile(command)
	if err != nil {
		return "", "", err
	}
	directory, err := passwordsConfigurationDirectory()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(directory, passwords.KnownKeysFileName(name)), server, nil
}

// readPasswordsKnownKeysFile reads the file as it is now. None is an empty
// file. A file others could have changed is an error. A file written before
// the keys were kept by server is moved to the server it names.
func readPasswordsKnownKeysFile(path string) (passwordsKnownKeysFile, error) {
	empty := passwordsKnownKeysFile{Servers: map[string]map[string]*passwordsKnownKey{}, Items: map[string]map[string]*passwordsKnownItem{}}
	content, err := readPrivateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var stored passwordsKnownKeysFile
	if err := json.Unmarshal(content, &stored); err != nil {
		return empty, fmt.Errorf("%s is not readable (%w); remove it to start the list of known keys again", path, err)
	}
	if stored.Servers == nil {
		stored.Servers = map[string]map[string]*passwordsKnownKey{}
	}
	if stored.Items == nil {
		stored.Items = map[string]map[string]*passwordsKnownItem{}
	}
	if stored.LegacyServer != "" && stored.LegacyPeople != nil && stored.Servers[stored.LegacyServer] == nil {
		stored.Servers[stored.LegacyServer] = stored.LegacyPeople
	}
	stored.LegacyServer, stored.LegacyPeople = "", nil
	for _, people := range stored.Servers {
		for _, known := range people {
			if known == nil {
				continue
			}
			// An earlier version kept a key the person confirmed apart from one
			// that was only seen. Now there is one key for each person: the one
			// that was confirmed, else the one that was seen. A confirmed key is
			// shown as compared.
			switch {
			case known.EarlierConfirmedPublicKey != "":
				known.PublicKey = known.EarlierConfirmedPublicKey
				moment := time.Now().Truncate(time.Second)
				if known.EarlierConfirmedAt != nil {
					moment = *known.EarlierConfirmedAt
				}
				known.ComparedAt, known.FirstSeenAt = &moment, &moment
				if known.EarlierSeenAt != nil && known.EarlierSeenAt.Before(moment) {
					known.FirstSeenAt = known.EarlierSeenAt
				}
			case known.EarlierSeenPublicKey != "":
				known.PublicKey = known.EarlierSeenPublicKey
				known.FirstSeenAt = known.EarlierSeenAt
			}
			known.EarlierConfirmedPublicKey, known.EarlierConfirmedAt = "", nil
			known.EarlierSeenPublicKey, known.EarlierSeenAt = "", nil
		}
	}
	return stored, nil
}

// loadPasswordsKnownKeys reads the known keys. None is an empty set, and so
// are the keys of another server, which stay in the file. A file others could
// have changed is an error.
func loadPasswordsKnownKeys(command *cli.Command) (*passwordsKnownKeys, error) {
	path, server, err := passwordsKnownKeysPath(command)
	if err != nil {
		return nil, err
	}
	file, err := readPasswordsKnownKeysFile(path)
	if err != nil {
		return nil, err
	}
	return &passwordsKnownKeys{path: path, server: server, file: file}, nil
}

// lookup is what is known of a person, nil if nothing.
func (self *passwordsKnownKeys) lookup(userID string) *passwordsKnownKey {
	return self.file.Servers[self.server][userID]
}

// knownKey is the key this computer uses for somebody, empty if it has seen
// none.
func (self *passwordsKnownKeys) knownKey(userID string) string {
	if known := self.lookup(userID); known != nil {
		return known.PublicKey
	}
	return ""
}

// isCompared is whether the person marked the key used for somebody as compared.
func (self *passwordsKnownKeys) isCompared(userID string) bool {
	known := self.lookup(userID)
	return known != nil && known.PublicKey != "" && known.ComparedAt != nil
}

// keyOutcome says what recording a key found.
type keyOutcome int

const (
	// keyUnchanged is a key this computer already used for the person.
	keyUnchanged keyOutcome = iota
	// keyFirstSeen is the first key this computer saw for the person.
	keyFirstSeen
	// keyChanged is a key that replaced another one.
	keyChanged
)

// rememberKey records the key the server hands out for somebody: the first one
// seen is kept, and a different one replaces it (and clears the mark that the
// old one was compared). It says which of the three happened.
func (self *passwordsKnownKeys) rememberKey(userID, username, name, publicKey string) (keyOutcome, error) {
	if self.knownKey(userID) == publicKey {
		return keyUnchanged, nil
	}
	outcome := keyFirstSeen
	err := self.update(userID, func(known *passwordsKnownKey) {
		moment := time.Now().Truncate(time.Second)
		outcome = keyFirstSeen
		if known.PublicKey != "" {
			outcome = keyChanged
		}
		if known.PublicKey == publicKey {
			outcome = keyUnchanged
			return
		}
		if username != "" {
			known.Username = username
		}
		known.Name = name
		known.PublicKey = publicKey
		known.ComparedAt = nil
		if outcome == keyFirstSeen || known.FirstSeenAt == nil {
			known.FirstSeenAt = &moment
		}
	})
	return outcome, err
}

// markCompared records that the person compared the safety code of this key
// with the other person. The key becomes the one used for them.
func (self *passwordsKnownKeys) markCompared(userID, username, name, publicKey string) error {
	return self.update(userID, func(known *passwordsKnownKey) {
		moment := time.Now().Truncate(time.Second)
		if username != "" {
			known.Username = username
		}
		known.Name = name
		if known.PublicKey != publicKey || known.FirstSeenAt == nil {
			known.FirstSeenAt = &moment
		}
		known.PublicKey = publicKey
		known.ComparedAt = &moment
	})
}

// item is what this computer remembers of an item the person owns, a copy
// that is empty if nothing is.
func (self *passwordsKnownKeys) item(itemID string) passwordsKnownItem {
	if known := self.file.Items[self.server][itemID]; known != nil {
		copied := *known
		copied.SealedForUserIDs = slices.Clone(known.SealedForUserIDs)
		return copied
	}
	return passwordsKnownItem{}
}

// updateItem changes what is remembered of one item. Nothing is written when
// the change leaves it as it was.
func (self *passwordsKnownKeys) updateItem(itemID string, change func(known *passwordsKnownItem)) error {
	before := self.item(itemID)
	after := self.item(itemID)
	change(&after)
	if reflect.DeepEqual(before, after) {
		return nil
	}
	return self.mutate(func(current *passwordsKnownKeysFile) {
		if current.Items[self.server] == nil {
			current.Items[self.server] = map[string]*passwordsKnownItem{}
		}
		known := current.Items[self.server][itemID]
		if known == nil {
			known = &passwordsKnownItem{}
			current.Items[self.server][itemID] = known
		}
		change(known)
	})
}

// update changes one person's record.
func (self *passwordsKnownKeys) update(userID string, change func(known *passwordsKnownKey)) error {
	return self.mutate(func(current *passwordsKnownKeysFile) {
		if current.Servers[self.server] == nil {
			current.Servers[self.server] = map[string]*passwordsKnownKey{}
		}
		known := current.Servers[self.server][userID]
		if known == nil {
			known = &passwordsKnownKey{}
			current.Servers[self.server][userID] = known
		}
		change(known)
	})
}

// mutate changes the file. The file is read again under a lock and written
// whole through a temporary file, so that another teanode command writing a
// key at the same moment does not lose this one, and the other servers' keys
// are kept.
func (self *passwordsKnownKeys) mutate(change func(current *passwordsKnownKeysFile)) error {
	release, err := lockPrivateFile(self.path)
	if err != nil {
		return err
	}
	defer release()
	current, err := readPasswordsKnownKeysFile(self.path)
	if err != nil {
		return err
	}
	change(&current)
	content, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(self.path, append(content, '\n')); err != nil {
		return err
	}
	self.file = current
	return nil
}

// knownKeys is the known keys, read when first needed.
func (self *passwordsSession) knownKeys() (*passwordsKnownKeys, error) {
	if self.known != nil {
		return self.known, nil
	}
	known, err := loadPasswordsKnownKeys(self.command)
	if err != nil {
		return nil, err
	}
	self.known = known
	return known, nil
}

// decodePublicKey reads a public key the server handed out and refuses what is
// not a point on the curve.
func decodePublicKey(text string) ([]byte, error) {
	publicKey, err := passwords.DecodeCanonicalBase64(text)
	if err != nil || passwords.CheckPublicKey(publicKey) != nil {
		return nil, errors.New("the public key the server gave is not a key")
	}
	return publicKey, nil
}

// keyNoticeOutput is where the lines about keys are printed (a first or a
// changed key, a circle member who got an item, something left for the person
// to do): the error output, so that --json stays JSON. A test replaces it.
var keyNoticeOutput io.Writer = os.Stderr

// keyNoticeLine prints one such line.
func keyNoticeLine(line string) {
	_, _ = fmt.Fprintln(keyNoticeOutput, line)
}

// keyNotice is the one line printed when a key is recorded for the first time
// or has changed, nothing for a key already used. userName is what to call the
// person.
func keyNotice(outcome keyOutcome, userName string) {
	switch outcome {
	case keyFirstSeen:
		_, _ = fmt.Fprintf(keyNoticeOutput, "first key for %s recorded\n", userName)
	case keyChanged:
		_, _ = fmt.Fprintf(keyNoticeOutput, "%s's key changed (they set Passwords up again): compare codes with them; your code: teanode passwords code\n", userName)
	}
}

// recordRecipientKey records the key the server hands out for a recipient, and
// says so when it is the first or has changed. Nobody is asked: the key is used
// for sealing from here on. userName is the one the person typed, where they
// typed one, and the server's otherwise.
func (self *passwordsSession) recordRecipientKey(recipient *client.PasswordRecipientView, userName string) error {
	known, err := self.knownKeys()
	if err != nil {
		return err
	}
	if _, err := decodePublicKey(recipient.PublicKey); err != nil {
		return fmt.Errorf("%s: %w", forTerminal(textFromServer(recipient.Username)), err)
	}
	if userName == "" {
		userName = textFromServer(recipient.Username)
	}
	outcome, err := known.rememberKey(recipient.UserID, recipient.Username, recipient.Name, recipient.PublicKey)
	if err != nil {
		return err
	}
	keyNotice(outcome, forTerminal(userName))
	return nil
}

// recordRecipientKeys records the key of each recipient before anything is
// sealed for them (see recordRecipientKey). A recipient with no key is skipped.
// typedUserNames, by user id, is what the person typed for each.
func (self *passwordsSession) recordRecipientKeys(recipients []*client.PasswordRecipientView, typedUserNames map[string]string) error {
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if recipient.PublicKey == "" || seen[recipient.UserID] {
			continue
		}
		seen[recipient.UserID] = true
		if err := self.recordRecipientKey(recipient, typedUserNames[recipient.UserID]); err != nil {
			return err
		}
	}
	return nil
}

// ownerKeyDecision is what was decided about an owner's key for the length of
// one command: the error that stops the item being opened, or nil, and what to
// write down once an item from that owner has opened with the key (nil if
// there is nothing to write).
type ownerKeyDecision struct {
	err    error
	record func() error
}

// checkedOwnerKey is the public key of the owner of an item shared with the
// person. A share opens only with the owner's key, so a share planted by
// anybody else does not open. The key the server gives for the owner is used
// whether this computer saw it before or not: the first one is to be recorded,
// and one that differs from the one used before replaces it, with a line telling
// the person to compare codes. Nobody is asked. Nothing is written down here:
// recordOwnerKey does it after an item from this owner has opened with the key.
func (self *passwordsSession) checkedOwnerKey(view *client.PasswordItemView) ([]byte, error) {
	publicKey, err := decodePublicKey(view.OwnerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("its owner's %w", err)
	}
	decisionKey := view.OwnerUserID + "\x00" + view.OwnerPublicKey
	if decision, isDecided := self.checkedOwnerKeys[decisionKey]; isDecided {
		return publicKey, decision.err
	}
	decide := func() *ownerKeyDecision {
		known, err := self.knownKeys()
		if err != nil {
			// Without the file the key cannot be written down, and a change of
			// it would not be noticed, but the item still opens with the key
			// the server gives. Sealing for anybody still refuses (see
			// sealItemKeyFor).
			keyNoticeLine(fmt.Sprintf("note: the file of known keys cannot be used, so the key of %s is not written down and a change of it would not be noticed; fix the file to have it written down: %s",
				forTerminal(textFromServer(view.OwnerName)), forTerminal(err.Error())))
			return &ownerKeyDecision{}
		}
		if known.knownKey(view.OwnerUserID) == view.OwnerPublicKey {
			return &ownerKeyDecision{}
		}
		return &ownerKeyDecision{record: func() error {
			outcome, err := known.rememberKey(view.OwnerUserID, "", view.OwnerName, view.OwnerPublicKey)
			if err != nil {
				return err
			}
			keyNotice(outcome, forTerminal(textFromServer(view.OwnerName)))
			return nil
		}}
	}
	if self.checkedOwnerKeys == nil {
		self.checkedOwnerKeys = map[string]*ownerKeyDecision{}
	}
	decision := decide()
	self.checkedOwnerKeys[decisionKey] = decision
	return publicKey, decision.err
}

// recordOwnerKey writes down the key of an owner whose item has just opened
// with it, if checkedOwnerKey found something to write. It is done once per
// key, after the item opened and not before: a key that opens nothing is not
// worth remembering. Reading an item does not depend on being able to write
// this down.
func (self *passwordsSession) recordOwnerKey(view *client.PasswordItemView) {
	decision := self.checkedOwnerKeys[view.OwnerUserID+"\x00"+view.OwnerPublicKey]
	if decision == nil || decision.record == nil {
		return
	}
	record := decision.record
	decision.record = nil
	if err := record(); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not write down the key of %s: %s\n", forTerminal(textFromServer(view.OwnerName)), err)
	}
}

// sealItemKeyFor seals an item key, as the owner, for a recipient's public key.
// The recipient must have been through recordRecipientKeys: a key that is not
// the one recorded for them is refused here too, so that no path to sealing
// skips recording it. sealedItemKey is the account-sealed text of the item
// key the device opened (the item view's itemKey), which the server compares
// with the item's current one when keys are added to an item; a re-key, whose
// keys are sealed from the new key in the same call, passes none.
func (self *passwordsSession) sealItemKeyFor(itemID string, itemKey []byte, sealedItemKey string, recipient *client.PasswordRecipientView) (*client.PasswordItemShareKeyInput, error) {
	known, err := self.knownKeys()
	if err != nil {
		return nil, err
	}
	if known.knownKey(recipient.UserID) != recipient.PublicKey {
		return nil, fmt.Errorf("the key of %s is not the one recorded on this computer, so nothing is sealed for it", forTerminal(textFromServer(recipient.Username)))
	}
	publicKey, err := decodePublicKey(recipient.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", recipient.Username, err)
	}
	ownerID, err := self.currentUserID()
	if err != nil {
		return nil, err
	}
	sealed, err := passwords.SealForKeyPair(self.keys.KeyPair, publicKey, itemKey, passwords.ShareData(itemID, ownerID, recipient.UserID))
	if err != nil {
		return nil, fmt.Errorf("cannot seal for %s: %w", recipient.Username, err)
	}
	return &client.PasswordItemShareKeyInput{
		ItemID: itemID, RecipientUserID: recipient.UserID, ItemKeyByRecipientKey: sealed.String(), RecipientPublicKey: recipient.PublicKey,
		ItemKey: sealedItemKey,
	}, nil
}

// runPasswordsTrust marks a person's key as compared, for the person who
// compared safety codes with them. It is optional and gates nothing: sharing
// uses the first key seen without it. It is still a comparison: the safety code
// the person gave (read from them in person or on a call you trust) must be the
// code of the key the server hands out for them right now. A code that does not
// match records nothing, and is not answered with the code that would have
// matched: a server could otherwise be asked for the answer. The server is also
// held to the person asked for: the answer must carry the user id it was asked
// about and the user name that was typed.
func runPasswordsTrust(ctx context.Context, command *cli.Command) error {
	if err := refuseAgentCommand(); err != nil {
		return err
	}
	const usageLine = "usage: teanode passwords trust <username> <safety code>  (or --code-stdin to read the code from a line of standard input)"
	arguments := command.Args().Len()
	if command.Bool("code-stdin") {
		if arguments != 1 {
			return usage(usageLine)
		}
	} else if arguments != 2 {
		return usage(usageLine)
	}
	typed := strings.TrimSpace(command.Args().Get(0))
	givenCode := ""
	if command.Bool("code-stdin") {
		line, err := readHiddenLine(command, "", true, "code-stdin")
		if err != nil {
			return err
		}
		givenCode = line
	} else {
		givenCode = command.Args().Get(1)
	}
	givenSafetyCode, err := passwords.NormalizeSafetyCode(givenCode)
	if err != nil {
		return usage("a safety code is eight groups of four digits, such as 0412 9930 0007 5121 8800 3341 0562 7719")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	granteeUserId, granteeCircleId, _, err := shareTarget(ctx, connection, typed)
	if err != nil {
		return describeError(command, err)
	}
	if granteeCircleId != "" || granteeUserId == "" {
		return usage("trust takes the user name of one person, not a circle")
	}
	recipients, err := client.ListPasswordRecipients(ctx, connection, granteeUserId, "")
	if err != nil {
		return describeNotFound(command, err, typed)
	}
	if len(recipients) != 1 {
		return fmt.Errorf("the server did not give one person for %q", typed)
	}
	recipient := recipients[0]
	if recipient.UserID != granteeUserId || !strings.EqualFold(recipient.Username, typed) {
		return fmt.Errorf("the server answered with a different person than %q, whose key you asked for; nothing was recorded", typed)
	}
	if recipient.PublicKey == "" {
		return fmt.Errorf("%q has not set up Passwords yet, so there is no key to compare", typed)
	}
	publicKey, err := decodePublicKey(recipient.PublicKey)
	if err != nil {
		return fmt.Errorf("%q: %w", typed, err)
	}
	actualCode := passwords.SafetyCode(publicKey)
	if actualCode != givenSafetyCode {
		return fmt.Errorf("the codes do not match: the key the server gives for %q is not the one they read you; nothing was recorded. "+
			"Ask them to read you the code on their own screen again, from Passwords settings, and tell them; "+
			"do not rely on this item's sharing until you know why", typed)
	}
	known, err := loadPasswordsKnownKeys(command)
	if err != nil {
		return err
	}
	if err := known.markCompared(recipient.UserID, recipient.Username, recipient.Name, recipient.PublicKey); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"username": typed, "safetyCode": actualCode, "isCompared": true})
	}
	fmt.Printf("marked the key of %s as compared (safety code %s)\n", forTerminal(typed), actualCode)
	return nil
}

// runPasswordsCode prints the person's own safety code, worked out on this
// computer from the public key of the key pair that unlocking opened, and not
// from anything the server reports about the key (the server is asked only for
// the user name to show beside it). It is what the people who share with them
// compare their copy with.
func runPasswordsCode(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage("usage: teanode passwords code")
	}
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	user, err := client.GetCurrentUser(ctx, session.connection)
	if err != nil {
		return describeError(command, err)
	}
	username := ""
	if user != nil {
		username = user.Username
	}
	safetyCode := session.keys.KeyPair.SafetyCode()
	if command.Bool("json") {
		return PrintJSON(map[string]any{"username": username, "safetyCode": safetyCode})
	}
	if username != "" {
		fmt.Printf("Your safety code (user name %s):\n", forTerminal(username))
	} else {
		fmt.Println("Your safety code:")
	}
	fmt.Println()
	fmt.Println("    " + safetyCode)
	fmt.Println()
	fmt.Println("Read it to a person who shares with you, in person or on a call you trust. They compare it with the code their device shows for you.")
	return nil
}
