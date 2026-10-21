package passwords

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/passwords"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// turnOnWords is how the person lets the agent use an item, and so the answer
// to every request for an item the agent may not use.
const turnOnWords = "They can open the item in Passwords and turn on \"Let the agent use this\", then ask again."

// noItemsWords is the answer of list when the person has let the agent use
// nothing. It is an answer, not an error, so that the model tells the person
// how rather than trying again.
const noItemsWords = "The person has not let you use any item in Passwords. " + turnOnWords

// elsewhereWords is the whole answer of an action that reads an item in a run
// whose answer would leave the person's own conversation: over Telegram or
// Discord, or into a chat room with other members.
const elsewhereWords = "Passwords is used only in the TeaNode app and on the dashboard, and in the person's own scheduled runs, never in a chat with other people."

// notTheirsWords is the whole answer of an action that reads or uses an item
// in a run the person did not start: one an inbound mail, a chat app, a
// webhook, a goal, a dream, a command left running or a schedule the agent
// wrote for itself began. What such a run does is steered by content the
// person did not write, and a login is the last thing to hand to that.
const notTheirsWords = "Passwords is used only in a conversation with the person or in a schedule the person wrote themselves, not in a run that a mail, a message, a webhook, a goal, a dream or any other content started."

// notAllowedWords is the answer when the person's account no longer holds the
// permission to use Passwords.
const notAllowedWords = "The person's account is not allowed to use Passwords, so no item can be used."

// generateInSubagentWords is the answer of generate in a subagent, where a
// password made up would come back to its parent as a tool result.
const generateInSubagentWords = "A password made up here would appear in the conversation and go to the model's provider. Ask for one in a conversation with the person instead, and suggest they make it with the generator in Passwords and save it themselves, which no model sees."

// subagentSurface is a run whose answer comes back to its parent as a tool
// result, which the filing of conversations into memory cannot tell from any
// other. A note read there would be filed.
const subagentSurface = "subagent"

// isOutsideOwnConversation says the run's answer would be sent where somebody
// else can read it: out through a chat app's bot (Telegram, Discord), or into
// a chat room with other members.
func isOutsideOwnConversation(run tools.Run) bool {
	if tools.IsAnsweringInChat(run) {
		return true
	}
	for _, kind := range models.AgentChannelKinds {
		if run.Surface() == string(kind) {
			return true
		}
	}
	return false
}

// withGate runs an action only in a run whose answer the person alone reads
// and which the person started (a conversation with them, or a schedule they
// wrote). It does not ask whether anybody is watching, as Health does: a
// schedule the person wrote may sign in, which is why they turn an item on.
func withGate(run func(ctx context.Context, call *tools.Call, current tools.Run) (*tools.Result, error)) func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	return func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
		current, err := tools.RunFrom(ctx)
		if err != nil {
			return nil, err
		}
		if isOutsideOwnConversation(current) {
			return tools.TextResult("%s", elsewhereWords), nil
		}
		if !tools.IsStartedByPerson(current) {
			return tools.TextResult("%s", notTheirsWords), nil
		}
		return run(ctx, call, current)
	}
}

// ownerMayUse says whether the person still holds the permission to use
// Passwords, asked at the moment of use: a person whose role lost it, or whose
// account was disabled, since the run began no longer has an agent that can
// open an item.
func ownerMayUse(tx db.Transaction, run tools.Run) (bool, error) {
	permissions, err := tx.EffectivePermissions(run.Owner().ID)
	if err != nil {
		return false, err
	}
	return permissions != nil && permissions.Has(models.PermissionPasswordsUse), nil
}

// ownerKeyOf reads the public key the person's device published when they set
// up Passwords. A key sealed for the agent key opens only with the public key
// of the owner who sealed it.
func ownerKeyOf(tx db.Transaction, ownerUserId string) ([]byte, error) {
	setup, err := tx.GetPasswordsSetup(ownerUserId)
	if err != nil {
		return nil, err
	}
	if setup == nil {
		return nil, errors.New("the person has not set up Passwords")
	}
	return base64.StdEncoding.DecodeString(setup.PublicKey)
}

// agentItemOf is an item row with what opening it needs.
func agentItemOf(item *models.PasswordItem, ownerPublicKey []byte) passwords.AgentItem {
	return passwords.AgentItem{
		ID: item.ID, Kind: passwords.ItemKind(item.ItemKind), OwnerID: item.OwnerUserID, OwnerPublicKey: ownerPublicKey,
		EncryptedFields: item.EncryptedFields, ItemKeyByAgentKey: item.ItemKeyByAgentKey,
	}
}

// openerFor loads the server's agent key and opens it with the server's secret.
// It is nil, with no error, when the server has made no agent key yet: then no
// item can have been turned on.
func openerFor(tx db.Transaction, run tools.Run) (*passwords.AgentOpener, error) {
	key, err := tx.GetPasswordAgentKey()
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, nil
	}
	box, err := secretbox.New(run.Configuration().Secret(), models.PasswordAgentKeyLabel)
	if err != nil {
		return nil, fmt.Errorf("the agent key cannot be opened: %w", err)
	}
	privateKey, err := box.Open(key.PrivateKeySealed)
	if err != nil {
		return nil, fmt.Errorf("the agent key cannot be opened: %w", err)
	}
	return passwords.NewAgentOpener(privateKey)
}

// listedItem is one item as the model sees it in the list: nothing that signs
// in, nothing of a card.
type listedItem struct {
	ItemID          string   `json:"item_id"`
	ItemKind        string   `json:"item_kind"`
	ItemName        string   `json:"item_name,omitempty"`
	Sites           []string `json:"sites,omitempty"`
	LoginUserName   string   `json:"login_user_name,omitempty"`
	HasOneTimeCode  *bool    `json:"has_one_time_code,omitempty"`
	HasPasskeys     *bool    `json:"has_passkeys,omitempty"`
	IsUnreadable    bool     `json:"is_unreadable,omitempty"`
	UnreadableCause string   `json:"unreadable_cause,omitempty"`
}

const unreadableWords = "the server could not open this item; the person can turn \"Let the agent use this\" off and on again on it"

var runList = withGate(func(ctx context.Context, call *tools.Call, run tools.Run) (*tools.Result, error) {
	var listed []listedItem
	isRefused := false
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		isAllowed, err := ownerMayUse(tx, run)
		if err != nil {
			return err
		}
		if !isAllowed {
			isRefused = true
			return nil
		}
		items, err := tx.ListAgentUsablePasswordItems(run.Owner().ID)
		if err != nil || len(items) == 0 {
			return err
		}
		opener, err := openerFor(tx, run)
		if err != nil {
			return err
		}
		ownerPublicKey, err := ownerKeyOf(tx, run.Owner().ID)
		if err != nil {
			return err
		}
		for _, item := range items {
			listed = append(listed, describeItem(opener, item, ownerPublicKey))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if isRefused {
		return tools.TextResult("%s", notAllowedWords), nil
	}
	if len(listed) == 0 {
		return tools.TextResult("%s", noItemsWords), nil
	}
	return tools.JSONResult(map[string]any{"item_list": listed, "item_count": len(listed)})
})

// describeItem opens one item and keeps from it only what the list may say. An
// item that does not open is listed as unreadable, by id and kind only.
func describeItem(opener *passwords.AgentOpener, item *models.PasswordItem, ownerPublicKey []byte) listedItem {
	row := listedItem{ItemID: item.ID, ItemKind: string(item.ItemKind)}
	unreadable := func() listedItem {
		row.IsUnreadable, row.UnreadableCause = true, unreadableWords
		return row
	}
	if opener == nil || item.ItemKind == models.PasswordItemKindCard {
		return unreadable()
	}
	switch item.ItemKind {
	case models.PasswordItemKindLogin:
		login, err := opener.OpenLogin(agentItemOf(item, ownerPublicKey))
		if err != nil {
			return unreadable()
		}
		hasOneTimeCode, hasPasskeys := strings.TrimSpace(login.OneTimeCodeURI) != "", len(login.Passkeys) > 0
		row.ItemName, row.Sites, row.LoginUserName = login.Name, login.Sites, login.UserName
		row.HasOneTimeCode, row.HasPasskeys = &hasOneTimeCode, &hasPasskeys
	case models.PasswordItemKindNote:
		note, err := opener.OpenNote(agentItemOf(item, ownerPublicKey))
		if err != nil {
			return unreadable()
		}
		row.ItemName = note.Name
	default:
		return unreadable()
	}
	return row
}

type readNoteArguments struct {
	ItemID string `json:"item_id"`
}

var runReadNote = withGate(func(ctx context.Context, call *tools.Call, run tools.Run) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[readNoteArguments](call)
	if err != nil {
		return nil, err
	}
	itemId := strings.ToLower(strings.TrimSpace(arguments.ItemID))
	if itemId == "" {
		return nil, errors.New("say which note: the item_id that list gives")
	}
	// A note read in a subagent would come back to its parent as a tool
	// result that filing into memory cannot tell from any other.
	if run.Surface() == subagentSurface {
		return tools.TextResult("A note is read only in a conversation with the person or in their own scheduled run, not in a subagent, so ask for it from there."), nil
	}

	var note *passwords.NoteFields
	var refusal string
	err = run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		isAllowed, err := ownerMayUse(tx, run)
		if err != nil {
			return err
		}
		if !isAllowed {
			refusal = notAllowedWords
			return nil
		}
		items, err := tx.ListAgentUsablePasswordItems(run.Owner().ID)
		if err != nil {
			return err
		}
		var found *models.PasswordItem
		for _, item := range items {
			if item.ID == itemId {
				found = item
			}
		}
		if found == nil {
			refusal = "That is not an item the person let you use. " + turnOnWords
			return nil
		}
		switch found.ItemKind {
		case models.PasswordItemKindNote:
		case models.PasswordItemKindCard:
			refusal = "A card cannot be used by the agent."
			return nil
		default:
			refusal = "Only a note can be read. A login's password, one-time code and passkeys are never given to you."
			return nil
		}
		opener, err := openerFor(tx, run)
		if err != nil {
			return err
		}
		if opener == nil {
			return errors.New("the server has no agent key, so no note can be opened")
		}
		ownerPublicKey, err := ownerKeyOf(tx, run.Owner().ID)
		if err != nil {
			return err
		}
		if note, err = opener.OpenNote(agentItemOf(found, ownerPublicKey)); err != nil {
			return fmt.Errorf("the note cannot be opened: %w", err)
		}
		// The use is recorded before the text is given out: a note that
		// cannot be recorded as used is not read.
		return tx.RecordPasswordItemAgentUse(run.Owner().ID, found.ID, "", time.Now())
	})
	if err != nil {
		return nil, err
	}
	if refusal != "" {
		return tools.TextResult("%s", refusal), nil
	}
	result, err := tools.JSONResult(map[string]any{"item_id": itemId, "item_name": note.Name, "note_text": note.Text})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = "Read a note in Passwords"
	return result, nil
})

type generateArguments struct {
	LengthCount           *int  `json:"length_count"`
	ShouldIncludeLetters  *bool `json:"should_include_letters"`
	ShouldIncludeDigits   *bool `json:"should_include_digits"`
	ShouldIncludeSymbols  *bool `json:"should_include_symbols"`
	ShouldAvoidLookAlikes *bool `json:"should_avoid_look_alikes"`
}

// runGenerate makes up a password. It is not tied to who started the run (a
// random string is nobody's secret), but it is not made for a chat with other
// people, and not in a subagent, whose answer comes back to its parent as a
// tool result that is kept like any other.
func runGenerate(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	if isOutsideOwnConversation(run) {
		return tools.TextResult("%s", elsewhereWords), nil
	}
	if run.Surface() == subagentSurface {
		return tools.TextResult("%s", generateInSubagentWords), nil
	}
	arguments, err := tools.DecodeArguments[generateArguments](call)
	if err != nil {
		return nil, err
	}
	options := passwords.DefaultPasswordOptions()
	if arguments.LengthCount != nil {
		options.Length = *arguments.LengthCount
	}
	if arguments.ShouldIncludeLetters != nil {
		options.ShouldIncludeLetters = *arguments.ShouldIncludeLetters
	}
	if arguments.ShouldIncludeDigits != nil {
		options.ShouldIncludeDigits = *arguments.ShouldIncludeDigits
	}
	if arguments.ShouldIncludeSymbols != nil {
		options.ShouldIncludeSymbols = *arguments.ShouldIncludeSymbols
	}
	if arguments.ShouldAvoidLookAlikes != nil {
		options.ShouldAvoidLookAlikes = *arguments.ShouldAvoidLookAlikes
	}
	password, err := passwords.GeneratePassword(options)
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"generated_password": password, "length_count": len([]rune(password))})
}
