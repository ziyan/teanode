package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// Giving an item a new item key. When access to an item narrows (the agent is
// no longer allowed to use it, or somebody is taken off its shares), whoever
// kept an old sealed copy of the item key could still open every later version
// of the item, a changed password included. So this computer makes a new item
// key, seals the fields and the key again, and the old key opens nothing that
// is saved after.

// passwordsRekeyFailedWords is what the person is told when the access was
// narrowed but the item could not be given a new key. The narrowing stands;
// the same command again gives the new key.
const passwordsRekeyFailedWords = "Removed. The item could not be given a new key; run the command again"

// isChangedOnAnotherDevice is whether the server refused a save because the
// item moved on since it was read.
func isChangedOnAnotherDevice(err error) bool {
	return err != nil && strings.Contains(err.Error(), "changed on another device")
}

// sealItemKeyForAgent seals an item key for the agent key the server hands
// out, the key the agent opens the items it may use with.
func (self *passwordsSession) sealItemKeyForAgent(itemID string, itemKey []byte) (string, error) {
	agentKey, err := client.ReadPasswordsAgentKey(self.ctx, self.connection)
	if err != nil {
		return "", err
	}
	publicKey, err := decodePublicKey(agentKey)
	if err != nil {
		return "", errors.New("the agent key the server gave is not a key")
	}
	ownerID, err := self.currentUserID()
	if err != nil {
		return "", err
	}
	sealed, err := passwords.SealForKeyPair(self.keys.KeyPair, publicKey, itemKey, passwords.ShareData(itemID, ownerID, passwords.AgentRecipientID))
	if err != nil {
		return "", fmt.Errorf("cannot seal the item for the agent key: %w", err)
	}
	return sealed.String(), nil
}

// reloadItem reads one of the person's items again, at the revision it has now.
func (self *passwordsSession) reloadItem(itemID string) (*passwordsItem, error) {
	list, err := client.ListPasswordItems(self.ctx, self.connection, nil)
	if err != nil {
		return nil, err
	}
	for _, view := range list.Items {
		if view.ID == itemID {
			return self.openItem(view)
		}
	}
	return nil, fmt.Errorf("item %s is not there any more", itemID)
}

// rekeyKind is why an item is being given a new key, which decides how far
// the new key may reach.
type rekeyKind int

const (
	// rekeyAutomatic is a re-key the person did not ask for: the server marked
	// the item as needing one, and another command (edit, share, agent on, share
	// --waiting) does it first.
	rekeyAutomatic rekeyKind = iota
	// rekeyNarrowing is the second step of taking somebody, or the agent, off
	// the item (unshare, agent off).
	rekeyNarrowing
	// rekeyByPerson is `teanode passwords rekey`, which the person ran for this
	// item on purpose.
	rekeyByPerson
)

// automaticRekeyWait is how long an item is left alone after a re-key nobody
// asked for, and automaticRekeyLimit how many such re-keys one command makes. A
// server that keeps marking items as needing a new key cannot make this
// computer keep sealing them; the person is told to run rekey instead. A test
// changes the wait.
var automaticRekeyWait = 10 * time.Minute

const automaticRekeyLimit = 20

// rekeyItem gives an item the person owns a new item key. changeAccess, when
// not nil, makes the access record that is sealed with the new fields from the
// signed one the item has (narrowing it, for unshare and agent off); nil seals
// the record as it is, which needs a record that is signed and is not an old
// one shown again. Every recipient the signed record reaches keeps access, sealed
// for the public key recorded for them (the same rule as sharing: nobody is
// asked); the ones with no key yet wait, and are given back so that the command
// can say so. A person the server lists
// but the record does not reach is given no key. Unless the person ran rekey,
// the new key reaches only the people this computer last saw holding a key for
// the item, and the agent only if the record says it may use the item, the
// server agrees, and it held a copy when this computer last saw the item: a
// re-key the person did not ask for never widens who can open the item. When
// the item moved on since it was read the item is read again and the new key is
// made once more. The item is updated in place to the new key and revision.
func (self *passwordsSession) rekeyItem(item *passwordsItem, changeAccess func(*passwords.PasswordAccess) *passwords.PasswordAccess, kind rekeyKind) ([]*client.PasswordRecipientView, error) {
	if changeAccess == nil {
		if err := item.refuseWithoutVerifiedAccess("cannot give the item a new key"); err != nil {
			return nil, err
		}
	}
	if kind == rekeyAutomatic {
		if err := self.refuseTooManyAutomaticRekeys(item); err != nil {
			return nil, err
		}
	}
	waiting, err := self.rekeyItemOnce(item, changeAccess, kind)
	if isChangedOnAnotherDevice(err) {
		fresh, reloadErr := self.reloadItem(item.View.ID)
		if reloadErr != nil {
			return nil, err
		}
		*item = *fresh
		waiting, err = self.rekeyItemOnce(item, changeAccess, kind)
	}
	if err == nil && kind == rekeyAutomatic {
		self.automaticRekeyCount++
		self.rememberAutomaticRekey(item.View.ID)
	}
	return waiting, err
}

// refuseTooManyAutomaticRekeys is the error for an item that was given a new key
// nobody asked for a short while ago, or for a command that made too many of
// them, and says what to run instead.
func (self *passwordsSession) refuseTooManyAutomaticRekeys(item *passwordsItem) error {
	instead := "the item needs attention: run teanode passwords rekey " + item.View.ID + " to give it a new key now"
	if self.automaticRekeyCount >= automaticRekeyLimit {
		return fmt.Errorf("%d items were given a new key by this command already; %s", automaticRekeyLimit, instead)
	}
	known, err := self.knownKeys()
	if err != nil {
		return nil
	}
	if last := known.item(item.View.ID).LastAutomaticRekeyAt; last != nil && time.Since(*last) < automaticRekeyWait {
		return fmt.Errorf("it was given a new key without being asked less than %s ago and is marked again; %s", automaticRekeyWait, instead)
	}
	return nil
}

// rememberAutomaticRekey writes down when an item was last given a new key
// nobody asked for.
func (self *passwordsSession) rememberAutomaticRekey(itemID string) {
	known, err := self.knownKeys()
	if err != nil {
		return
	}
	moment := time.Now().Truncate(time.Second)
	if err := known.updateItem(itemID, func(remembered *passwordsKnownItem) { remembered.LastAutomaticRekeyAt = &moment }); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not write down what is known of %s: %s\n", forTerminal(itemID), forTerminal(err.Error()))
	}
}

func (self *passwordsSession) rekeyItemOnce(item *passwordsItem, changeAccess func(*passwords.PasswordAccess) *passwords.PasswordAccess, kind rekeyKind) ([]*client.PasswordRecipientView, error) {
	// Only a record the owner's devices signed is built on. Without one, a
	// narrowing starts from a record that shares with nobody and lets the agent
	// use nothing, so whatever the unchecked record said widens nothing.
	access := item.VerifiedAccess
	if changeAccess != nil {
		access = changeAccess(access)
	}
	signed, err := self.signedAccessFor(item, access)
	if err != nil {
		return nil, err
	}
	// The agent keeps a copy of the new key only when the owner's own signed
	// record lets it use the item and the server agrees. A record that says yes
	// where the server says no is left as it is: a device never cuts the
	// record on the server's word. Unless the person ran rekey, the agent also
	// needs to have held a copy when this computer last saw the item.
	isAgentKept := signed.IsUsableByAgent && item.View.IsUsableByAgent && (kind == rekeyByPerson || item.HadAgentCopyBefore)
	previousAccess := item.Access()
	item.setAccess(signed)
	waiting, err := self.rekeyItemWithAccess(item, isAgentKept, kind)
	if err != nil {
		item.setAccess(previousAccess)
		return waiting, err
	}
	self.recordAccessWritten(item, signed)
	self.afterAccessChange(item)
	return waiting, nil
}

func (self *passwordsSession) rekeyItemWithAccess(item *passwordsItem, isAgentKept bool, kind rekeyKind) ([]*client.PasswordRecipientView, error) {
	// The known keys are where the key of each person is recorded. When their
	// file cannot be read (a bad mode, say) nobody can be sealed for: the
	// narrowing still happens, the people the item is still shared with wait for
	// the new key, and the person is told how to fix the file.
	_, knownErr := self.knownKeys()
	if knownErr != nil {
		fmt.Fprintf(os.Stderr, "note: the file of known keys cannot be used, so nobody is given the new key until it is fixed: %s\n", forTerminal(knownErr.Error()))
	}
	circleIDs := circleIDsOfRecipients(item.View.Shares)
	var reached, waiting []*client.PasswordRecipientView
	seen := map[string]bool{}
	for _, recipient := range existingRecipients(item.View) {
		if seen[recipient.UserID] {
			continue
		}
		seen[recipient.UserID] = true
		switch {
		case !item.isReachedBySealedAccess(recipient, circleIDs):
			fmt.Fprintf(os.Stderr, "note: the server lists %s as having this item, but you did not share it with them, so they are not given the new key\n",
				forTerminal(textFromServer(recipient.Username)))
		case recipient.PublicKey == "":
			waiting = append(waiting, recipient)
		case kind != rekeyByPerson && !slices.Contains(item.PreviousKeyHolderUserIDs, recipient.UserID):
			// Somebody this computer did not see holding a key is not given
			// one here, and is not asked about either: the person gives them
			// the item with share --waiting.
			waiting = append(waiting, recipient)
		default:
			reached = append(reached, recipient)
		}
	}

	newItemKey, err := passwords.NewKey()
	if err != nil {
		return nil, err
	}
	shareKeys := []*client.PasswordItemShareKeyInput{}
	for _, recipient := range reached {
		// The key the server gives is recorded first, as in sharing. When the
		// file of known keys cannot be used, or the key is not a key, the
		// person waits for the new key.
		if knownErr != nil {
			waiting = append(waiting, recipient)
			continue
		}
		if err := self.recordRecipientKey(recipient, ""); err != nil {
			fmt.Fprintf(os.Stderr, "note: %s waits for the item's new key: %s\n", forTerminal(textFromServer(recipient.Username)), forTerminal(err.Error()))
			waiting = append(waiting, recipient)
			continue
		}
		shareKey, err := self.sealItemKeyFor(item.View.ID, newItemKey, "", recipient)
		if err != nil {
			return nil, err
		}
		shareKeys = append(shareKeys, shareKey)
	}
	encryptedFields, err := sealFields(item.View.ID, item.Kind, item.Fields(), newItemKey)
	if err != nil {
		return nil, err
	}
	sealedItemKey, err := passwords.Seal(self.keys.AccountKey, newItemKey, passwords.ItemKeyData(item.View.ID))
	if err != nil {
		return nil, err
	}
	input := client.PasswordItemRekeyInput{
		ID:              item.View.ID,
		BaseRevision:    item.View.Revision,
		EncryptedFields: encryptedFields,
		ItemKey:         sealedItemKey.String(),
		ShareKeys:       shareKeys,
	}
	if isAgentKept {
		itemKeyByAgentKey, err := self.sealItemKeyForAgent(item.View.ID, newItemKey)
		if err != nil {
			return nil, err
		}
		input.ItemKeyByAgentKey = &itemKeyByAgentKey
	}
	view, err := client.RekeyPasswordItem(self.ctx, self.connection, input)
	if err != nil {
		return nil, err
	}
	item.View = view
	item.ItemKey = newItemKey
	return waiting, nil
}

// printWaitingRecipients tells the person who is waiting for the new key of an
// item.
func printWaitingRecipients(waiting []*client.PasswordRecipientView) {
	for _, recipient := range waiting {
		fmt.Printf("%s (%s) cannot open it until they have the new key; run 'teanode passwords share --waiting' to give it to them\n",
			forTerminal(recipient.Name), forTerminal(recipient.Username))
	}
}
