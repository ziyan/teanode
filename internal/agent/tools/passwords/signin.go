package passwords

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/passwords"
)

// What the browser tool needs of Passwords to sign in with a login: open one
// the person let the agent use, and record that it was used. The browser tool
// puts the password, the one-time code or the passkey into a page; none of it
// is returned to the model, and the functions here hand the secrets only to
// the browser tool's code.

// CheckSignInHere is nil when the run may sign in with Passwords: the answer
// goes to the person alone, and the person started the run (a conversation
// with them, or a schedule they wrote). A chat with other people and Telegram
// or Discord are refused, as for every other action here, and so is a run
// that content started, such as an inbound mail, a webhook, a goal or a
// schedule the agent wrote for itself.
func CheckSignInHere(run tools.Run) error {
	if isOutsideOwnConversation(run) {
		return answerError(elsewhereWords)
	}
	if !tools.IsStartedByPerson(run) {
		return answerError(notTheirsAnswer(run))
	}
	return nil
}

// answerError is a sentence for the model as an error.
func answerError(words string) error {
	return errors.New(words)
}

// OpenLoginToSignIn opens a login of the person's own that they let the agent
// use. It refuses an item that is not on that list (turned off, somebody
// else's, deleted, or no item), a card and a note, and a login the server
// cannot open, each with words the model can act on.
func OpenLoginToSignIn(ctx context.Context, run tools.Run, itemId string) (*passwords.LoginFields, error) {
	itemId = strings.ToLower(strings.TrimSpace(itemId))
	if itemId == "" {
		return nil, errors.New("say which login: the item_id that passwords list gives")
	}
	var login *passwords.LoginFields
	var refusal string
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
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
		case models.PasswordItemKindLogin:
		case models.PasswordItemKindCard:
			refusal = "A card cannot be used by the agent."
			return nil
		default:
			refusal = "Only a login can sign in to a page."
			return nil
		}
		opener, err := openerFor(tx, run)
		if err != nil {
			return err
		}
		if opener == nil {
			return errors.New("the server has no agent key, so no login can be opened")
		}
		ownerPublicKey, err := ownerKeyOf(tx, run.Owner().ID)
		if err != nil {
			return err
		}
		// The error of opening names the item, never what is in it.
		if login, err = opener.OpenLogin(agentItemOf(found, ownerPublicKey)); errors.Is(err, passwords.ErrNotUsableByAgent) {
			refusal = notUsableWords
			return nil
		} else if err != nil {
			return fmt.Errorf("the login cannot be opened: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if refusal != "" {
		return nil, errors.New(refusal)
	}
	return login, nil
}

// RecordSignIn notes on the item, and in the audit log, that the agent used it
// on the host. The browser tool records the use before it puts a secret into
// a page: a use that cannot be recorded is not made.
func RecordSignIn(ctx context.Context, run tools.Run, itemId, host string) error {
	itemId = strings.ToLower(strings.TrimSpace(itemId))
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.RecordPasswordItemAgentUse(run.Owner().ID, itemId, host, time.Now())
	})
}
