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
		known.Username, known.Name = username, name
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
func (self *passwordsSession) confirmRecipientKeys(recipients []*client.PasswordRecipientView) error {
	known, err := self.knownKeys()
	if err != nil {
		return err
	}
	var toWrite []*client.PasswordRecipientView
	var lines []string
	isChange := false
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if recipient.PublicKey == "" || seen[recipient.UserID] {
			continue
		}
		seen[recipient.UserID] = true
		publicKey, err := decodePublicKey(recipient.PublicKey)
		if err != nil {
			return fmt.Errorf("%s: %w", recipient.Username, err)
		}
		confirmed := known.confirmedKey(recipient.UserID)
		switch {
		case confirmed == recipient.PublicKey:
			continue
		case confirmed == "":
			lines = append(lines, fmt.Sprintf("  %s (%s)  safety code %s", recipient.Name, recipient.Username, passwords.SafetyCode(publicKey)))
		default:
			isChange = true
			confirmedKey, _ := passwords.DecodeCanonicalBase64(confirmed)
			lines = append(lines, fmt.Sprintf("  %s (%s)  KEY CHANGED  safety code before %s, now %s",
				recipient.Name, recipient.Username, passwords.SafetyCode(confirmedKey), passwords.SafetyCode(publicKey)))
		}
		toWrite = append(toWrite, recipient)
	}
	if len(toWrite) == 0 {
		return nil
	}
	warning := "This lets these people open what you share:\n" + forTerminal(strings.Join(lines, "\n")) +
		"\nCheck each safety code with the person, in person or on a call you trust, before you go on: it shows the key they set up, and the same code must be on their screen."
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

// checkedOwnerKey is the public key of the owner of an item shared with the
// person, once it is known to be the owner's. A share opens only with the
// owner's key, so a share planted by anybody else does not open; this guards
// the other half, that the key the server calls the owner's is the one this
// computer saw before. The first time an owner is seen, their key is written
// down as seen (never as confirmed: seeing a key on an item proves nothing about
// whose it is) and their safety code is told, to be checked when it can be. A
// key that differs from the confirmed or the seen one is shown with both safety
// codes and the item is not opened unless the person confirms (or passes
// --force, which only reading an item honors).
func (self *passwordsSession) checkedOwnerKey(view *client.PasswordItemView) ([]byte, error) {
	publicKey, err := decodePublicKey(view.OwnerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("its owner's %w", err)
	}
	decisionKey := view.OwnerUserID + "\x00" + view.OwnerPublicKey
	if decision, isDecided := self.checkedOwnerKeys[decisionKey]; isDecided {
		return publicKey, decision
	}
	decide := func() error {
		known, err := self.knownKeys()
		if err != nil {
			return err
		}
		confirmed, seen := known.confirmedKey(view.OwnerUserID), known.seenKey(view.OwnerUserID)
		switch {
		case view.OwnerPublicKey == confirmed || view.OwnerPublicKey == seen:
			return nil
		case confirmed == "" && seen == "":
			fmt.Fprintf(os.Stderr, "note: this is the first item %s shared with you; their safety code is %s, to check with them\n",
				forTerminal(view.OwnerName), passwords.SafetyCode(publicKey))
		default:
			previous := confirmed
			if previous == "" {
				previous = seen
			}
			previousKey, _ := passwords.DecodeCanonicalBase64(previous)
			if err := confirm(self.command, fmt.Sprintf("The key of %s, who shared this with you, changed. Their safety code was %s and is now %s. "+
				"Either they erased Passwords and set it up again, or the server put its own key in their place; go on only if they tell you the first.",
				forTerminal(view.OwnerName), passwords.SafetyCode(previousKey), passwords.SafetyCode(publicKey))); err != nil {
				return fmt.Errorf("the key of its owner changed and was not confirmed: %w", err)
			}
		}
		if err := known.rememberSeen(view.OwnerUserID, view.OwnerName, view.OwnerPublicKey); err != nil {
			// Reading an item does not depend on being able to write this down.
			fmt.Fprintf(os.Stderr, "note: could not write down the key of %s: %s\n", forTerminal(view.OwnerName), err)
		}
		return nil
	}
	if self.checkedOwnerKeys == nil {
		self.checkedOwnerKeys = map[string]error{}
	}
	decision := decide()
	self.checkedOwnerKeys[decisionKey] = decision
	return publicKey, decision
}

// sealItemKeyFor seals an item key, as the owner, for a recipient's public key.
// The recipient must have been through confirmRecipientKeys: a key that is not
// the one the person confirmed is refused here too, so that no path to sealing
// can skip the question.
func (self *passwordsSession) sealItemKeyFor(itemID string, itemKey []byte, recipient *client.PasswordRecipientView) (*client.PasswordItemShareKeyInput, error) {
	known, err := self.knownKeys()
	if err != nil {
		return nil, err
	}
	if known.confirmedKey(recipient.UserID) != recipient.PublicKey {
		return nil, fmt.Errorf("the key of %s is not one you confirmed by its safety code, so nothing is sealed for it", recipient.Username)
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
	}, nil
}

// runPasswordsTrust confirms a person's key without the question at a
// terminal, for a script or a session without one, and is still a comparison:
// the safety code the person gave (read from them in person or on a call you
// trust) must be the code of the key the server hands out for them right now.
// A code that does not match records nothing.
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
	typed := command.Args().Get(0)
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
	givenDigits := strings.Join(strings.Fields(givenCode), "")
	if len(givenDigits) != 20 || strings.Trim(givenDigits, "0123456789") != "" {
		return usage("a safety code is five groups of four digits, such as 0412 9930 0007 5121 8800")
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
	if recipient.PublicKey == "" {
		return fmt.Errorf("%s has not set up Passwords yet, so there is no key to confirm", forTerminal(recipient.Username))
	}
	publicKey, err := decodePublicKey(recipient.PublicKey)
	if err != nil {
		return fmt.Errorf("%s: %w", forTerminal(recipient.Username), err)
	}
	actualCode := passwords.SafetyCode(publicKey)
	if strings.Join(strings.Fields(actualCode), "") != givenDigits {
		return fmt.Errorf("that is not the safety code of the key the server gives for %s (it is %s); nothing was recorded. "+
			"Ask them for the code on their screen again: if it differs from this one, do not share",
			forTerminal(recipient.Username), actualCode)
	}
	known, err := loadPasswordsKnownKeys(command)
	if err != nil {
		return err
	}
	if err := known.rememberConfirmed(recipient.UserID, recipient.Username, recipient.Name, recipient.PublicKey); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"username": recipient.Username, "safetyCode": actualCode, "isConfirmed": true})
	}
	fmt.Printf("confirmed the key of %s (safety code %s)\n", forTerminal(recipient.Username), actualCode)
	return nil
}
