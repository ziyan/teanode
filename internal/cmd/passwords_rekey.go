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

// rekeyItem gives an item the person owns a new item key. Every recipient the
// item's shares reach now keeps access if their public key is one the person
// confirmed (the same rule as sharing: the person is asked about the ones that
// are not, at a terminal); the others wait, and are given back so that the
// command can say so. The agent keeps access only when shouldKeepAgent is set
// and the item is still usable by it. When the item moved on since it was read
// the item is read again and the new key is made once more. The item is
// updated in place to the new key and revision.
func (self *passwordsSession) rekeyItem(item *passwordsItem, shouldKeepAgent bool) ([]*client.PasswordRecipientView, error) {
	waiting, err := self.rekeyItemOnce(item, shouldKeepAgent)
	if !isChangedOnAnotherDevice(err) {
		return waiting, err
	}
	fresh, reloadErr := self.reloadItem(item.View.ID)
	if reloadErr != nil {
		return nil, err
	}
	*item = *fresh
	return self.rekeyItemOnce(item, shouldKeepAgent)
}

func (self *passwordsSession) rekeyItemOnce(item *passwordsItem, shouldKeepAgent bool) ([]*client.PasswordRecipientView, error) {
	known, err := self.knownKeys()
	if err != nil {
		return nil, err
	}
	var reached, waiting []*client.PasswordRecipientView
	seen := map[string]bool{}
	for _, recipient := range existingRecipients(item.View) {
		if seen[recipient.UserID] {
			continue
		}
		seen[recipient.UserID] = true
		if recipient.PublicKey == "" {
			waiting = append(waiting, recipient)
			continue
		}
		reached = append(reached, recipient)
	}

	// A recipient whose key was never confirmed, or changed since, is asked
	// about now. If the person is not there to answer, or says no, the person
	// waits for the new key instead of being sealed for a key nobody checked.
	var unconfirmed []*client.PasswordRecipientView
	for _, recipient := range reached {
		if known.confirmedKey(recipient.UserID) != recipient.PublicKey {
			unconfirmed = append(unconfirmed, recipient)
		}
	}
	if len(unconfirmed) > 0 {
		if err := self.confirmRecipientKeys(unconfirmed); err != nil {
			var names []string
			for _, recipient := range unconfirmed {
				names = append(names, recipient.Name+" ("+recipient.Username+")")
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
		if known.confirmedKey(recipient.UserID) != recipient.PublicKey {
			waiting = append(waiting, recipient)
			continue
		}
		shareKey, err := self.sealItemKeyFor(item.View.ID, newItemKey, recipient)
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
		BaseRevision:    int(item.View.Revision),
		EncryptedFields: encryptedFields,
		ItemKey:         sealedItemKey.String(),
		ShareKeys:       shareKeys,
	}
	if shouldKeepAgent && item.View.IsUsableByAgent {
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
