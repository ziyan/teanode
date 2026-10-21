package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

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
	// the item (unshare, agent off), which the person asked for: the new key
	// goes to everyone the signed record still reaches.
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
// but the record does not reach is given no key. The agent keeps a copy of the
// new key if the record still lets it use the item and the server agrees.
// When the person ran the command (rekey, unshare, agent off) that is all. A
// re-key the person did not ask for (rekeyAutomatic) never widens who can open
// the item: it reaches only the people this computer itself sealed the item
// for before, and the agent only if this computer itself made the agent's
// copy. Whoever is left out is told in one line that names the command
// (rekey) that gives them the key. When the item moved on since it was read
// the item is read again and the new key is made once more. The item is
// updated in place to the new key and revision.
func (self *passwordsSession) rekeyItem(item *passwordsItem, changeAccess func(*passwords.PasswordAccess) *passwords.PasswordAccess, kind rekeyKind) ([]*client.PasswordRecipientView, error) {
	if changeAccess == nil {
		if err := item.refuseWithoutVerifiedAccess("cannot give the item a new key"); err != nil {
			return nil, err
		}
	}
	if kind == rekeyAutomatic {
		// A computer that has never seen the item before, or never acted on it,
		// cannot tell the newest record from an old one the server shows again,
		// so it leaves the re-key to the person.
		if !item.IsRemembered {
			return nil, fmt.Errorf("%w: teanode passwords rekey %s (this computer has not seen this item before, so it does not give it a new key on its own)", errPasswordsNeedsYou, item.View.ID)
		}
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
	// record on the server's word. Only a re-key the person did not ask for
	// also needs the agent to have held a copy when this computer last saw the
	// item: unshare and agent off are the person's own narrowing, and keep the
	// agent's copy when the record still allows it, as the dashboard and the
	// iPhone do.
	isAgentWanted := signed.IsUsableByAgent && item.View.IsUsableByAgent
	isAgentKept := isAgentWanted && (kind != rekeyAutomatic || item.HasMadeAgentCopy)
	previousAccess := item.Access()
	item.setAccess(signed)
	waiting, err := self.rekeyItemWithAccess(item, isAgentWanted, isAgentKept, kind)
	if err != nil {
		item.setAccess(previousAccess)
		return waiting, err
	}
	self.recordAccessWritten(item, signed)
	return waiting, nil
}

func (self *passwordsSession) rekeyItemWithAccess(item *passwordsItem, isAgentWanted, isAgentKept bool, kind rekeyKind) ([]*client.PasswordRecipientView, error) {
	// The known keys are where the key of each person is recorded. When their
	// file cannot be read (a bad mode, say) nobody can be sealed for: the
	// narrowing still happens, the people the item is still shared with wait for
	// the new key, and the person is told how to fix the file.
	_, knownErr := self.knownKeys()
	if knownErr != nil {
		fmt.Fprintf(os.Stderr, "note: the file of known keys cannot be used, so nobody is given the new key until it is fixed: %s\n", forTerminal(knownErr.Error()))
	}
	circleIDs := circleIDsOfRecipients(item.View.Shares)
	var reached, waiting, notSealedFor []*client.PasswordRecipientView
	listedUserIDs := []string{}
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
			continue
		case recipient.PublicKey == "":
			waiting = append(waiting, recipient)
		case kind == rekeyAutomatic && !slices.Contains(item.SealedForUserIDs, recipient.UserID):
			// A re-key nobody asked for does not give the item key to somebody
			// this computer never gave it to: the person gives it to them with
			// rekey.
			notSealedFor = append(notSealedFor, recipient)
		default:
			reached = append(reached, recipient)
		}
		listedUserIDs = append(listedUserIDs, recipient.UserID)
	}

	newItemKey, err := passwords.NewKey()
	if err != nil {
		return nil, err
	}
	shareKeys := []*client.PasswordItemShareKeyInput{}
	var sealedFor []*client.PasswordRecipientView
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
		sealedFor = append(sealedFor, recipient)
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
	if err := refuseAnswerAboutAnotherItem(item.View.ID, view); err != nil {
		return nil, err
	}
	item.View = view
	item.ItemKey = newItemKey
	// What this computer now did itself: the people it sealed the new key for
	// (and the ones it sealed for before who are still listed), and the agent's
	// copy.
	circleNotices := item.circleMemberLines(item.Access(), sealedFor, circleIDs, circleNamesOfShares(item.View.Shares))
	var sealedUserIDs []string
	for _, recipient := range sealedFor {
		sealedUserIDs = append(sealedUserIDs, recipient.UserID)
	}
	for _, userID := range item.SealedForUserIDs {
		if slices.Contains(listedUserIDs, userID) {
			sealedUserIDs = append(sealedUserIDs, userID)
		}
	}
	self.setSealedFor(item, sealedUserIDs)
	self.rememberAgentCopy(item, isAgentKept)
	for _, line := range circleNotices {
		keyNoticeLine(line)
	}
	if len(notSealedFor) > 0 {
		keyNoticeLine(needsYouWords(item.View.ID, namesOf(notSealedFor)+" did not get the new key, because this computer never gave them this item's key"))
	}
	if isAgentWanted && !isAgentKept {
		keyNoticeLine(needsYouWords(item.View.ID, "the agent did not get a copy of the new key, because this computer did not make its earlier copy"))
	}
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

// passwordsRekeyPlan is who a new key for an item the person owns is sealed
// for, worked out from the owner's signed record and the server's list the way
// rekeyItemWithAccess does, without sealing anything. `rekey` says it before it
// seals, and `list` and `show` say it under an item that needs one, so the
// person knows who the command they are told to run gives the item to.
type passwordsRekeyPlan struct {
	// SealedForUserNames are the people who get the new key now.
	SealedForUserNames []string

	// WaitingUserNames are the people the record reaches who have not set up
	// Passwords, so they wait for the key.
	WaitingUserNames []string

	// IsAgentCopyMade says the agent gets a copy of the new key.
	IsAgentCopyMade bool
}

// rekeyAccessChange is how `rekey` changes the record before it seals: an item
// whose record lets the agent use it while the server has no copy of its key
// for the agent is given a record that says what the server has, because
// rekey never widens access. Nil leaves the record as it is.
func (self *passwordsItem) rekeyAccessChange() func(*passwords.PasswordAccess) *passwords.PasswordAccess {
	if !self.hasLostAgentCopy() {
		return nil
	}
	return func(access *passwords.PasswordAccess) *passwords.PasswordAccess {
		return passwords.AccessWithAgentUse(access, false)
	}
}

func (self *passwordsItem) rekeyPlan(changeAccess func(*passwords.PasswordAccess) *passwords.PasswordAccess) passwordsRekeyPlan {
	plan := passwordsRekeyPlan{SealedForUserNames: []string{}, WaitingUserNames: []string{}}
	access := self.VerifiedAccess
	if changeAccess != nil && access != nil {
		access = changeAccess(access)
	}
	plan.IsAgentCopyMade = access != nil && access.IsUsableByAgent && self.View.IsUsableByAgent
	circleIDs := circleIDsOfRecipients(self.View.Shares)
	seen := map[string]bool{}
	for _, recipient := range existingRecipients(self.View) {
		if seen[recipient.UserID] {
			continue
		}
		seen[recipient.UserID] = true
		if !self.isReachedBySealedAccess(recipient, circleIDs) {
			continue
		}
		name := forTerminal(textFromServer(recipient.Username))
		if recipient.PublicKey == "" {
			plan.WaitingUserNames = append(plan.WaitingUserNames, name)
		} else {
			plan.SealedForUserNames = append(plan.SealedForUserNames, name)
		}
	}
	return plan
}

// words say who gets the new key, in one sentence.
func (self passwordsRekeyPlan) words() string {
	sealedFor := "nobody but you"
	if len(self.SealedForUserNames) > 0 {
		sealedFor = strings.Join(self.SealedForUserNames, ", ")
	}
	words := "the new key goes to " + sealedFor
	if self.IsAgentCopyMade {
		words += "; the agent gets a copy"
	} else {
		words += "; the agent gets none"
	}
	if len(self.WaitingUserNames) > 0 {
		words += "; waiting for them to set up Passwords: " + strings.Join(self.WaitingUserNames, ", ")
	}
	return words
}

// confirmRekey says who the new key goes to before anything is sealed, and
// asks for a yes: `rekey` seals for everyone the record reaches, including
// people this computer never gave the item to, and a computer that never saw
// the item can be shown an older record. Without a terminal to ask on it
// refuses, unless --yes was given. With --json the words go to standard error
// so that standard output stays JSON.
func confirmRekey(command *cli.Command, itemName string, plan passwordsRekeyPlan) error {
	words := "Giving " + forTerminal(itemName) + " a new key: " + plan.words() + "."
	output := os.Stdout
	if command.Bool("json") {
		output = os.Stderr
	}
	if command.Bool("yes") {
		_, _ = fmt.Fprintln(output, words)
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return usage(words + "\nRefusing without confirmation, and standard input is not a terminal to ask on; pass --yes to proceed")
	}
	_, _ = fmt.Fprintf(output, "%s Type 'yes' to continue: ", words)
	var answer string
	_, _ = fmt.Scanln(&answer)
	if answer != "yes" {
		return errors.New("canceled")
	}
	return nil
}
