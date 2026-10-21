package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// The public keys this computer knows. Sharing an item seals its key for a
// public key that the server hands out, so a server that swapped a person's
// key for its own could read what is shared with that person. The defence is
// to write a key down only when it can be trusted, and to keep two kinds apart.
//
// A confirmed key is one the person checked: its safety code was shown, the
// person compared it with the other person, and said yes. Only a confirmed key
// is sealed for, whether sharing, sharing with the people who were waiting, or
// giving an item a new key. A key that changes after it was confirmed is shown
// with both safety codes and asked about, never accepted quietly.
//
// A seen key is the key an owner had on an item shared with the person. It is
// not trusted for anything: a server can make an account of its own, share one
// item "from" it, and so put its key in front of the person. It is kept only to
// warn when the owner's key later changes.
//
// The keys are public, so the file is not secret, but it is kept private to its
// owner because changing it is the same as changing who is trusted.

// passwordsKnownKey is what is known of one person.
type passwordsKnownKey struct {
	Username string `json:"username"`
	Name     string `json:"name"`

	// ConfirmedPublicKey is the key the person confirmed by its safety code, and
	// when. Empty until they have.
	ConfirmedPublicKey string     `json:"confirmedPublicKey,omitempty"`
	ConfirmedAt        *time.Time `json:"confirmedAt,omitempty"`

	// SeenPublicKey is the key the person had on the last item they shared with
	// this one, and when it was first seen. It is never sealed for.
	SeenPublicKey string     `json:"seenPublicKey,omitempty"`
	SeenAt        *time.Time `json:"seenAt,omitempty"`

	// LegacyPublicKey is a key written by an earlier version, which did not tell
	// a key that was confirmed from one that was only seen. It is read as seen.
	LegacyPublicKey string `json:"publicKey,omitempty"`
}

// passwordsKnownKeysFile is what is in the file: the known keys of each server
// by that server's name, then by user id. Keeping every server in one file means
// a profile that is pointed at another server, and back, does not lose the keys
// it had written down for the first.
type passwordsKnownKeysFile struct {
	Servers map[string]map[string]*passwordsKnownKey `json:"servers"`

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
	empty := passwordsKnownKeysFile{Servers: map[string]map[string]*passwordsKnownKey{}}
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
	if stored.LegacyServer != "" && stored.LegacyPeople != nil && stored.Servers[stored.LegacyServer] == nil {
		stored.Servers[stored.LegacyServer] = stored.LegacyPeople
	}
	stored.LegacyServer, stored.LegacyPeople = "", nil
	for _, people := range stored.Servers {
		for _, known := range people {
			if known == nil {
				continue
			}
			// An earlier version trusted any key it had written down, whether
			// the person had compared its safety code or it had only been seen
			// on an item. Which of the two it was is not known, so it is read as
			// seen, and the person is asked once more before it is sealed for.
			if known.LegacyPublicKey != "" {
				if known.SeenPublicKey == "" {
					known.SeenPublicKey = known.LegacyPublicKey
				}
				known.LegacyPublicKey = ""
				if known.ConfirmedPublicKey == "" {
					known.ConfirmedAt = nil
				}
			}
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

// confirmedKey is the key the person confirmed for somebody, empty if none.
func (self *passwordsKnownKeys) confirmedKey(userID string) string {
	if known := self.lookup(userID); known != nil {
		return known.ConfirmedPublicKey
	}
	return ""
}

// seenKey is the key somebody had on an item shared with the person, empty if
// none was seen.
func (self *passwordsKnownKeys) seenKey(userID string) string {
	if known := self.lookup(userID); known != nil {
		return known.SeenPublicKey
	}
	return ""
}

// rememberConfirmed writes down a key the person confirmed by its safety code,
// the only kind of key an item is sealed for.
func (self *passwordsKnownKeys) rememberConfirmed(userID, username, name, publicKey string) error {
	return self.update(userID, func(known *passwordsKnownKey) {
		moment := time.Now().Truncate(time.Second)
		if username != "" {
			known.Username = username
		}
		known.Name = name
		known.ConfirmedPublicKey, known.ConfirmedAt = publicKey, &moment
	})
}

// rememberSeen writes down the key an owner had on an item shared with the
// person. It never changes a confirmed key.
func (self *passwordsKnownKeys) rememberSeen(userID, name, publicKey string) error {
	return self.update(userID, func(known *passwordsKnownKey) {
		moment := time.Now().Truncate(time.Second)
		known.Name = name
		known.SeenPublicKey, known.SeenAt = publicKey, &moment
	})
}

// update changes one person's record. The file is read again under a lock and
// written whole through a temporary file, so that another teanode command
// writing a key at the same moment does not lose this one, and the other
// servers' keys are kept.
func (self *passwordsKnownKeys) update(userID string, change func(known *passwordsKnownKey)) error {
	release, err := lockPrivateFile(self.path)
	if err != nil {
		return err
	}
	defer release()
	current, err := readPasswordsKnownKeysFile(self.path)
	if err != nil {
		return err
	}
	if current.Servers[self.server] == nil {
		current.Servers[self.server] = map[string]*passwordsKnownKey{}
	}
	known := current.Servers[self.server][userID]
	if known == nil {
		known = &passwordsKnownKey{}
		current.Servers[self.server][userID] = known
	}
	change(known)
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

// askAboutKeys shows what is about to be trusted and asks the person at the
// terminal whether to go on. A person's key is confirmed only by comparing its
// safety code with them, which a script or --force (or TEANODE_FORCE) cannot
// do, so unlike confirm it does not skip the question: without a terminal it
// refuses. A test replaces it.
var askAboutKeys = func(warning string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return usage(warning + "\nRefusing: a key is confirmed by comparing its safety code with the person, at a terminal, and --force does not skip that; " +
			"without a terminal, compare the code with them and run 'teanode passwords trust <username> <code>'")
	}
	fmt.Printf("%s Type 'yes' to continue: ", warning)
	var answer string
	_, _ = fmt.Scanln(&answer)
	if answer != "yes" {
		return errors.New("canceled")
	}
	return nil
}

// confirmRecipientKeys makes sure every recipient's public key is one the
// person confirmed, or confirms it now by its safety code. A person whose
// confirmed key is unchanged is not asked about. A person whose key was never
// confirmed (even if it was seen on an item they shared) and a person whose
// key changed are shown together with their safety codes (both the confirmed
// one and the new one on a change), and the command goes on only if the person
// says yes at a terminal. Nothing is written down until they do.
//
// Each person is shown under their user name, then the safety code on a line
// of its own, then the display name the server gives, quoted. The user name is
// the one the person typed, where they typed one (typedUserNames, by user id),
// and the server's otherwise. The display name is the server's text and so the
// last thing shown: a name that looks like a safety code can only ever appear
// inside its own quoted line, never in the place the code is read from.
func (self *passwordsSession) confirmRecipientKeys(recipients []*client.PasswordRecipientView, typedUserNames map[string]string) error {
	known, err := self.knownKeys()
	if err != nil {
		return err
	}
	var toWrite []*client.PasswordRecipientView
	var lines, askedNames []string
	isChange := false
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if recipient.PublicKey == "" || seen[recipient.UserID] {
			continue
		}
		seen[recipient.UserID] = true
		publicKey, err := decodePublicKey(recipient.PublicKey)
		if err != nil {
			return fmt.Errorf("%s: %w", forTerminal(textFromServer(recipient.Username)), err)
		}
		userName := typedUserNames[recipient.UserID]
		if userName == "" {
			userName = textFromServer(recipient.Username)
		}
		askedNames = append(askedNames, fmt.Sprintf("%q", userName))
		displayNameLine := fmt.Sprintf("    display name the server gives: %q", textFromServer(recipient.Name))
		confirmed := known.confirmedKey(recipient.UserID)
		switch confirmed {
		case recipient.PublicKey:
			continue
		case "":
			lines = append(lines, fmt.Sprintf("  user name %q", userName),
				"    safety code: "+passwords.SafetyCode(recipient.UserID, publicKey),
				displayNameLine)
		default:
			isChange = true
			confirmedKey, _ := passwords.DecodeCanonicalBase64(confirmed)
			lines = append(lines, fmt.Sprintf("  user name %q  KEY CHANGED", userName),
				"    safety code now:    "+passwords.SafetyCode(recipient.UserID, publicKey),
				"    safety code before: "+passwords.SafetyCode(recipient.UserID, confirmedKey),
				displayNameLine)
		}
		toWrite = append(toWrite, recipient)
	}
	if len(toWrite) == 0 {
		return nil
	}
	whom := "each of them"
	if len(askedNames) == 1 {
		whom = askedNames[0]
	}
	warning := "This lets these people open what you share:\n" + forTerminal(strings.Join(lines, "\n")) +
		"\nAsk " + whom + " to open Passwords settings on their own device and read you their safety code, in person or on a call you trust. Confirm only if it matches."
	if isChange {
		warning += "\nA key that changed is either a person who erased Passwords and set it up again, or a server that put its own key in their place. Go on only if the person tells you they did the first."
	}
	if err := askAboutKeys(warning); err != nil {
		return err
	}
	for _, recipient := range toWrite {
		if err := known.rememberConfirmed(recipient.UserID, recipient.Username, recipient.Name, recipient.PublicKey); err != nil {
			return err
		}
	}
	return nil
}

// confirmOwnerKeyChange asks whether to open items from an owner whose key
// changed, and says whether the person answered the question (isAnswered), as
// opposed to --force skipping it, which confirms nothing. A test replaces it.
var confirmOwnerKeyChange = func(command *cli.Command, warning string) (isAnswered bool, err error) {
	if command.Bool("force") {
		return false, nil
	}
	return true, confirm(command, warning)
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
// person, once it is known to be the owner's. A share opens only with the
// owner's key, so a share planted by anybody else does not open; this guards
// the other half, that the key the server calls the owner's is the one this
// computer saw before. The first time an owner is seen, their key is to be
// written down as seen (never as confirmed: seeing a key on an item proves
// nothing about whose it is) and their safety code is told, to be checked when
// it can be. A key that differs from the confirmed or the seen one is shown
// with both safety codes and the item is not opened unless the person says yes
// at a terminal, which also confirms the new key (the person compared the codes
// to say yes), or passes --force, which only reading an item honors and which
// confirms nothing. Nothing is written down here: recordOwnerKey does it after
// an item from this owner has opened with the key.
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
			return &ownerKeyDecision{err: err}
		}
		ownerName := forTerminal(textFromServer(view.OwnerName))
		confirmed, seen := known.confirmedKey(view.OwnerUserID), known.seenKey(view.OwnerUserID)
		recordSeen := func() error {
			return known.rememberSeen(view.OwnerUserID, view.OwnerName, view.OwnerPublicKey)
		}
		switch {
		case view.OwnerPublicKey == confirmed || view.OwnerPublicKey == seen:
			return &ownerKeyDecision{}
		case confirmed == "" && seen == "":
			fmt.Fprintf(os.Stderr, "note: this is the first item shared with you by the person the server calls %q; their safety code, to check with them, is\n    %s\n",
				ownerName, passwords.SafetyCode(view.OwnerUserID, publicKey))
			return &ownerKeyDecision{record: recordSeen}
		}
		previous := confirmed
		if previous == "" {
			previous = seen
		}
		previousKey, _ := passwords.DecodeCanonicalBase64(previous)
		isAnswered, err := confirmOwnerKeyChange(self.command, fmt.Sprintf("The key of the person who shared this with you changed. The name the server gives them is %q.\n"+
			"    safety code now:    %s\n    safety code before: %s\n"+
			"Ask them to open Passwords settings on their own device and read you their safety code, in person or on a call you trust. "+
			"Confirm only if it matches the one now. Either they erased Passwords and set it up again, or the server put its own key in their place; go on only if they tell you the first. "+
			"Saying yes also lets you share with them.",
			ownerName, passwords.SafetyCode(view.OwnerUserID, publicKey), passwords.SafetyCode(view.OwnerUserID, previousKey)))
		if err != nil {
			return &ownerKeyDecision{err: fmt.Errorf("the key of its owner changed and was not confirmed: %w", err)}
		}
		if !isAnswered {
			return &ownerKeyDecision{record: recordSeen}
		}
		return &ownerKeyDecision{record: func() error {
			return known.rememberConfirmed(view.OwnerUserID, "", view.OwnerName, view.OwnerPublicKey)
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
// The recipient must have been through confirmRecipientKeys: a key that is not
// the one the person confirmed is refused here too, so that no path to sealing
// can skip the question. sealedItemKey is the account-sealed text of the item
// key the device opened (the item view's itemKey), which the server compares
// with the item's current one when keys are added to an item; a re-key, whose
// keys are sealed from the new key in the same call, passes none.
func (self *passwordsSession) sealItemKeyFor(itemID string, itemKey []byte, sealedItemKey string, recipient *client.PasswordRecipientView) (*client.PasswordItemShareKeyInput, error) {
	known, err := self.knownKeys()
	if err != nil {
		return nil, err
	}
	if known.confirmedKey(recipient.UserID) != recipient.PublicKey {
		return nil, fmt.Errorf("the key of %s is not one you confirmed by its safety code, so nothing is sealed for it", forTerminal(textFromServer(recipient.Username)))
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

// runPasswordsTrust confirms a person's key without the question at a
// terminal, for a script or a session without one, and is still a comparison:
// the safety code the person gave (read from them in person or on a call you
// trust) must be the code of the key the server hands out for them right now.
// A code that does not match records nothing, and is not answered with the code
// that would have matched: a server could otherwise be asked for the answer.
// The server is also held to the person asked for: the answer must carry the
// user id it was asked about and the user name that was typed.
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
	granteeUserID, granteeCircleID, _, err := shareTarget(ctx, connection, typed)
	if err != nil {
		return describeError(command, err)
	}
	if granteeCircleID != "" || granteeUserID == "" {
		return usage("trust takes the user name of one person, not a circle")
	}
	recipients, err := client.ListPasswordRecipients(ctx, connection, granteeUserID, "")
	if err != nil {
		return describeNotFound(command, err, typed)
	}
	if len(recipients) != 1 {
		return fmt.Errorf("the server did not give one person for %q", typed)
	}
	recipient := recipients[0]
	if recipient.UserID != granteeUserID || !strings.EqualFold(recipient.Username, typed) {
		return fmt.Errorf("the server answered with a different person than %q, whose key you asked for; nothing was recorded", typed)
	}
	if recipient.PublicKey == "" {
		return fmt.Errorf("%q has not set up Passwords yet, so there is no key to confirm", typed)
	}
	publicKey, err := decodePublicKey(recipient.PublicKey)
	if err != nil {
		return fmt.Errorf("%q: %w", typed, err)
	}
	actualCode := passwords.SafetyCode(recipient.UserID, publicKey)
	if actualCode != givenSafetyCode {
		return fmt.Errorf("that is not the safety code of the key the server gives for %q; nothing was recorded. "+
			"Ask them to read you the code on their own screen again, from Passwords settings: if it still differs from what you typed, do not share", typed)
	}
	known, err := loadPasswordsKnownKeys(command)
	if err != nil {
		return err
	}
	if err := known.rememberConfirmed(recipient.UserID, recipient.Username, recipient.Name, recipient.PublicKey); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"username": typed, "safetyCode": actualCode, "isConfirmed": true})
	}
	fmt.Printf("confirmed the key of %s (safety code %s)\n", forTerminal(typed), actualCode)
	return nil
}

// runPasswordsCode prints the person's own safety code, worked out on this
// computer from the public key of the key pair that unlocking opened and from
// the person's own user id, and not from anything the server reports about the
// key. It is what the people who share with them compare their copy with.
func runPasswordsCode(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage("usage: teanode passwords code")
	}
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	userID, err := session.currentUserID()
	if err != nil {
		return describeError(command, err)
	}
	safetyCode := session.keys.KeyPair.SafetyCode(userID)
	if command.Bool("json") {
		return PrintJSON(map[string]any{"userId": userID, "safetyCode": safetyCode})
	}
	fmt.Println("Your safety code:")
	fmt.Println()
	fmt.Println("    " + safetyCode)
	fmt.Println()
	fmt.Println("Read it to a person who shares with you, in person or on a call you trust. They confirm your key only if it matches the code their device shows for you.")
	return nil
}
