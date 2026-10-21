package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// The public keys this computer knows. Sharing an item seals its key for a
// public key that the server hands out, so a server that swapped a person's
// key for its own could read what is shared with that person. The defence is
// trust on first use: the key a person's safety code was checked for (or that
// they shared with first) is written down here, a share is sealed only for a
// key that is written down, and a key that changes is shown with both safety
// codes and asked about, never accepted quietly. The same goes for the key of
// an owner whose item is opened. The keys are public, so the file is not
// secret, but it is kept private to its owner because changing it is the same
// as changing who is trusted.

// passwordsKnownKey is what is known of one person.
type passwordsKnownKey struct {
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"publicKey"`
	ConfirmedAt time.Time `json:"confirmedAt"`
}

type passwordsKnownKeysFile struct {
	Server string `json:"server"`
	// People are the known keys by user id.
	People map[string]*passwordsKnownKey `json:"people"`
}

// passwordsKnownKeys is the file, read into memory.
type passwordsKnownKeys struct {
	path string
	file passwordsKnownKeysFile
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
	return filepath.Join(directory, "passwords-keys-"+name+".json"), server, nil
}

// loadPasswordsKnownKeys reads the known keys. None, or the keys of another
// server, are an empty set. A file others could have changed is an error.
func loadPasswordsKnownKeys(command *cli.Command) (*passwordsKnownKeys, error) {
	path, server, err := passwordsKnownKeysPath(command)
	if err != nil {
		return nil, err
	}
	known := &passwordsKnownKeys{path: path, file: passwordsKnownKeysFile{Server: server, People: map[string]*passwordsKnownKey{}}}
	content, err := readPrivateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return known, nil
	}
	if err != nil {
		return nil, err
	}
	var stored passwordsKnownKeysFile
	if err := json.Unmarshal(content, &stored); err != nil {
		return nil, fmt.Errorf("%s is not readable (%w); remove it to start the list of known keys again", path, err)
	}
	if stored.Server == server && stored.People != nil {
		known.file = stored
	}
	return known, nil
}

// lookup is what is known of a person, nil if nothing.
func (self *passwordsKnownKeys) lookup(userID string) *passwordsKnownKey {
	return self.file.People[userID]
}

// remember writes down a person's key as the one to trust.
func (self *passwordsKnownKeys) remember(userID, username, name, publicKey string) error {
	self.file.People[userID] = &passwordsKnownKey{Username: username, Name: name, PublicKey: publicKey, ConfirmedAt: time.Now().Truncate(time.Second)}
	content, err := json.MarshalIndent(self.file, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(self.path, append(content, '\n'))
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

// confirmRecipientKeys makes sure every recipient's public key is one this
// computer knows, or that the person confirms now by its safety code. A person
// whose key is already known and unchanged is not asked about. A person met
// for the first time, and a person whose key changed, are shown together with
// their safety codes (both the known one and the new one on a change), and the
// command goes on only if the person says yes (or passes --force; without a
// terminal it refuses). Nothing is written down until they do.
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
		previous := known.lookup(recipient.UserID)
		switch {
		case previous != nil && previous.PublicKey == recipient.PublicKey:
			continue
		case previous == nil:
			lines = append(lines, fmt.Sprintf("  %s (%s)  safety code %s", recipient.Name, recipient.Username, passwords.SafetyCode(publicKey)))
		default:
			isChange = true
			previousKey, _ := passwords.DecodeCanonicalBase64(previous.PublicKey)
			lines = append(lines, fmt.Sprintf("  %s (%s)  KEY CHANGED  safety code before %s, now %s",
				recipient.Name, recipient.Username, passwords.SafetyCode(previousKey), passwords.SafetyCode(publicKey)))
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
	if err := confirm(self.command, warning); err != nil {
		return err
	}
	for _, recipient := range toWrite {
		if err := known.remember(recipient.UserID, recipient.Username, recipient.Name, recipient.PublicKey); err != nil {
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
// down and their safety code is told, to be checked when it can be. A key that
// differs from the one written down is shown with both safety codes and the
// item is not opened unless the person confirms (or passes --force).
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
		previous := known.lookup(view.OwnerUserID)
		switch {
		case previous != nil && previous.PublicKey == view.OwnerPublicKey:
			return nil
		case previous == nil:
			fmt.Fprintf(os.Stderr, "note: this is the first item %s shared with you; their safety code is %s, to check with them\n",
				forTerminal(view.OwnerName), passwords.SafetyCode(publicKey))
		default:
			previousKey, _ := passwords.DecodeCanonicalBase64(previous.PublicKey)
			if err := confirm(self.command, fmt.Sprintf("The key of %s, who shared this with you, changed. Their safety code was %s and is now %s. "+
				"Either they erased Passwords and set it up again, or the server put its own key in their place; go on only if they tell you the first.",
				forTerminal(view.OwnerName), passwords.SafetyCode(previousKey), passwords.SafetyCode(publicKey))); err != nil {
				return fmt.Errorf("the key of its owner changed and was not confirmed: %w", err)
			}
		}
		if err := known.remember(view.OwnerUserID, "", view.OwnerName, view.OwnerPublicKey); err != nil {
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
// The recipient must have been through confirmRecipientKeys.
func (self *passwordsSession) sealItemKeyFor(itemID string, itemKey []byte, recipient *client.PasswordRecipientView) (*client.PasswordItemShareKeyInput, error) {
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
