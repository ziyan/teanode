package apigraph

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/strainer"
)

// Teaching the built-in spam filter.
//
// The classifier is the part of a spam filter that does most of the work, and
// it cannot learn anything without somebody saying which messages were spam.
// That is what these two mutations are for: the dashboard puts them behind a
// button on a message.

// SpamMutation is teaching the classifier.
type SpamMutation interface {
	// Teach the built-in spam filter that a message is spam, or is not
	MarkMail(ctx context.Context, arguments MarkMailArguments) (*SpamTrainingResult, error)

	// Undo a marking, taking back exactly what it contributed
	ForgetMail(ctx context.Context, arguments ForgetMailArguments) (*SpamTrainingResult, error)
}

// SpamTrainingResult is what the dashboard shows after marking a message.
type SpamTrainingResult struct {
	// MailID is the message that was marked.
	MailID string `json:"mailId"`

	// Label is what it is now marked as, or empty once it is forgotten.
	Label string `json:"label"`

	// LearnedSpam and LearnedHam are the corpus totals afterwards, so the
	// page can say how far along the classifier is without asking again.
	LearnedSpam int64 `json:"learnedSpam"`
	LearnedHam  int64 `json:"learnedHam"`
}

// MarkMailArguments names the message and what it is.
type MarkMailArguments struct {
	MailID string `json:"mailId"`

	// Label is "spam" or "ham". "Ham" means "not spam", which is the word
	// every mail filter has used for it since the first Bayesian ones.
	Label string `json:"label"`
}

// MarkMail teaches the classifier that a message is spam, or is not.
//
// Marking the same message twice does nothing, and changing its label takes
// back what the previous one contributed, so the counts always describe
// exactly the set of marked messages.
func (self *graph) MarkMail(ctx context.Context, arguments MarkMailArguments) (*SpamTrainingResult, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	if arguments.Label != models.SpamTrainingLabelSpam && arguments.Label != models.SpamTrainingLabelHam {
		return nil, fmt.Errorf("label has to be %q or %q, not %q",
			models.SpamTrainingLabelSpam, models.SpamTrainingLabelHam, arguments.Label)
	}

	headers, body, err := self.trainableMail(ctx, arguments.MailID)
	if err != nil {
		return nil, err
	}
	if err := strainer.Learn(self.database, arguments.MailID, arguments.Label, headers, body); err != nil {
		return nil, err
	}
	return self.trainingResult(arguments.MailID, arguments.Label)
}

// ForgetMailArguments names the message to un-mark.
type ForgetMailArguments struct {
	MailID string `json:"mailId"`
}

// ForgetMail undoes a marking, exactly.
func (self *graph) ForgetMail(ctx context.Context, arguments ForgetMailArguments) (*SpamTrainingResult, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	headers, body, err := self.trainableMail(ctx, arguments.MailID)
	if err != nil {
		return nil, err
	}
	if err := strainer.Forget(self.database, arguments.MailID, headers, body); err != nil {
		return nil, err
	}
	return self.trainingResult(arguments.MailID, "")
}

// trainableMail fetches the stored message to learn from.
//
// The message has to still be in the spool: the classifier learns from words,
// and the words are in the body, which retention eventually removes. A
// message whose body is gone cannot be learned from, and saying so is better
// than recording a label that taught nothing.
func (self *graph) trainableMail(ctx context.Context, mailId string) ([]string, []byte, error) {
	mail, err := api.ContextTransaction(ctx).GetMail(mailId, nil)
	if err != nil {
		return nil, nil, err
	}
	if mail == nil {
		return nil, nil, api.ErrNotFound
	}

	headers, body, err := self.storage.Get(ctx, mail.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("the message is no longer in the spool, so there is nothing to learn from: %w", err)
	}
	return headers, body, nil
}

func (self *graph) trainingResult(mailId, label string) (*SpamTrainingResult, error) {
	spam, ham, err := self.database.CountSpamTraining()
	if err != nil {
		return nil, err
	}
	return &SpamTrainingResult{
		MailID:      mailId,
		Label:       label,
		LearnedSpam: spam,
		LearnedHam:  ham,
	}, nil
}
