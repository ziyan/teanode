package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

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

// rekeyItem gives an item the person owns a new item key. changeAccess, when
// not nil, makes the access record that is sealed with the new fields from the
// one the item has (narrowing it, for unshare and agent off); nil seals the
// record as it is. Every recipient the sealed record reaches keeps access if
// their public key is one the person confirmed (the same rule as sharing: the
// person is asked about the ones that are not, at a terminal); the others wait,
// and are given back so that the command can say so. A person the server lists
// but the record does not reach is given no key. The agent keeps access only
// when the record says it may use the item, and the server agrees. When the item
// moved on since it was read the item is read again and the new key is made
// once more. The item is updated in place to the new key and revision.
func (self *passwordsSession) rekeyItem(item *passwordsItem, changeAccess func(*passwords.PasswordAccess) *passwords.PasswordAccess) ([]*client.PasswordRecipientView, error) {
	waiting, err := self.rekeyItemOnce(item, changeAccess)
	if !isChangedOnAnotherDevice(err) {
		return waiting, err
	}
	fresh, reloadErr := self.reloadItem(item.View.ID)
	if reloadErr != nil {
		return nil, err
	}
	*item = *fresh
	return self.rekeyItemOnce(item, changeAccess)
}

func (self *passwordsSession) rekeyItemOnce(item *passwordsItem, changeAccess func(*passwords.PasswordAccess) *passwords.PasswordAccess) ([]*client.PasswordRecipientView, error) {
	access := item.Access()
	if changeAccess != nil {
		access = changeAccess(access)
	}
	// The agent keeps a copy of the new key only when the owner's own record
	// lets it use the item, and the server's record of it agrees. A record that
	// says yes where the server says no is an agent permission that was never
	// finished; it is sealed as no.
	isAgentKept := access != nil && access.IsUsableByAgent && item.View.IsUsableByAgent
	if access != nil && access.IsUsableByAgent && !item.View.IsUsableByAgent {
		access = passwords.AccessWithAgentUse(access, false)
	}
	previousAccess := item.Access()
	item.setAccess(access)
	waiting, err := self.rekeyItemWithAccess(item, isAgentKept)
	if err != nil {
		item.setAccess(previousAccess)
	}
	return waiting, err
}

func (self *passwordsSession) rekeyItemWithAccess(item *passwordsItem, isAgentKept bool) ([]*client.PasswordRecipientView, error) {
	// The known keys decide who can be sealed for. When their file cannot be
	// read (a bad mode, say) nobody counts as confirmed: the narrowing still
	// happens, the people the item is still shared with wait for the new key,
	// and the person is told how to fix the file.
	known, knownErr := self.knownKeys()
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
		default:
			reached = append(reached, recipient)
		}
	}

	// A recipient whose key was never confirmed, or changed since, is asked
	// about now. If the person is not there to answer, or says no, the person
	// waits for the new key instead of being sealed for a key nobody checked.
	isConfirmed := func(recipient *client.PasswordRecipientView) bool {
		return knownErr == nil && known.confirmedKey(recipient.UserID) == recipient.PublicKey
	}
	var unconfirmed []*client.PasswordRecipientView
	for _, recipient := range reached {
		if !isConfirmed(recipient) {
			unconfirmed = append(unconfirmed, recipient)
		}
	}
	if len(unconfirmed) > 0 && knownErr == nil {
		if err := self.confirmRecipientKeys(unconfirmed, nil); err != nil {
			var names []string
			for _, recipient := range unconfirmed {
				names = append(names, fmt.Sprintf("%q", textFromServer(recipient.Username)))
			}
			fmt.Fprintf(os.Stderr, "note: the key of %s is not confirmed, so they wait for the item's new key; "+
				"run 'teanode passwords share --waiting' at a terminal to check their safety codes, or 'teanode passwords trust <username> <code>' and then 'share --waiting'\n", forTerminal(strings.Join(names, ", ")))
		}
	}

	newItemKey, err := passwords.NewKey()
	if err != nil {
		return nil, err
	}
	shareKeys := []*client.PasswordItemShareKeyInput{}
	for _, recipient := range reached {
		if !isConfirmed(recipient) {
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
		fmt.Printf("%s (%s) cannot open it until they have the new key; run 'teanode passwords share --waiting' once their key is confirmed\n",
			forTerminal(recipient.Name), forTerminal(recipient.Username))
	}
}
