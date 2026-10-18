package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Relationships: what somebody the caller shares a circle with is to them.
// One fact between the two, from the side of whoever said it last, seen only
// by the two of them; models/relationship.go has the kinds and their words,
// and docs/subsystems/circles.md the rules.

// RelationshipTerm is one word a relationship can be said with.
type RelationshipTerm struct {
	// What is passed as relationshipTerm: wife, father, aunt_or_uncle
	Term string `json:"term"`

	// What it says: spouse, parent, aunt_or_uncle
	RelationshipKind string `json:"relationshipKind"`

	// masculine, feminine or neutral
	TermGender string `json:"termGender"`

	// The word in English: wife, father, aunt or uncle
	Word string `json:"word"`
}

func (self *graph) ListRelationshipTerms(ctx context.Context) ([]*RelationshipTerm, error) {
	if _, err := self.requireCirclePerson(ctx); err != nil {
		return nil, err
	}
	terms := models.RelationshipTerms()
	described := make([]*RelationshipTerm, 0, len(terms))
	for _, term := range terms {
		described = append(described, &RelationshipTerm{
			Term: term.Term, RelationshipKind: string(term.RelationshipKind), TermGender: string(term.TermGender),
			Word: term.RelationshipKind.Word(term.TermGender),
		})
	}
	return described, nil
}

type SetPersonRelationshipArguments struct {
	// The account the caller is describing
	UserID string `json:"userId"`

	// What they are to the caller, as one of the words ListRelationshipTerms
	// gives, such as wife, father or father_in_law. Spaces and hyphens may
	// stand for the underscores.
	RelationshipTerm string `json:"relationshipTerm"`
}

func (self *graph) SetPersonRelationship(ctx context.Context, arguments SetPersonRelationshipArguments) (*CircleMember, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	other, err := self.chatContact(tx, user, arguments.UserID)
	if err != nil {
		return nil, err
	}
	term, err := models.ParseRelationshipTerm(arguments.RelationshipTerm)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	before, err := tx.GetPersonRelationship(user.ID, other.ID)
	if err != nil {
		return nil, err
	}
	relationship, err := tx.SetPersonRelationship(user.ID, other.ID, term.RelationshipKind, term.TermGender)
	if err != nil {
		return nil, translateError(err)
	}
	// What the caller now is to the other person, worded as the other
	// person will see it. Saying the same again tells nobody twice, and
	// neither does the caller changing what they said within the hour.
	toldKind := relationship.KindFor(other.ID)
	toldGender := relationship.TermGenderFor(other.ID, user.Pronoun)
	isUnchanged := before != nil && before.KindFor(other.ID) == toldKind && before.TermGenderFor(other.ID, user.Pronoun) == toldGender
	isRecentlyTold := before != nil && before.UserID == user.ID && time.Since(before.ModifiedAt) < models.RelationshipTellingInterval
	if !isUnchanged && !isRecentlyTold {
		if err := self.tellAboutRelationship(ctx, other.ID, models.NoteRelationshipSet, models.RelationshipDetail(user.DisplayName(), toldKind, toldGender)); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s said how they are related to another account", user.Username)
	return describeCircleMember(&models.CircleMember{UserID: other.ID, User: other}, user.ID,
		map[string]*models.PersonRelationship{other.ID: relationship}), nil
}

type RemovePersonRelationshipArguments struct {
	// The account the caller is no longer saying anything about
	UserID string `json:"userId"`
}

func (self *graph) RemovePersonRelationship(ctx context.Context, arguments RemovePersonRelationshipArguments) (bool, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	other, err := self.chatContact(tx, user, arguments.UserID)
	if err != nil {
		return false, err
	}
	before, err := tx.GetPersonRelationship(user.ID, other.ID)
	if err != nil {
		return false, err
	}
	if before == nil {
		return false, nil
	}
	if _, err := tx.RemovePersonRelationship(user.ID, other.ID); err != nil {
		return false, err
	}
	// Taking back within the hour what the caller said themselves is not
	// told a second time either.
	isRecentlyTold := before.UserID == user.ID && time.Since(before.ModifiedAt) < models.RelationshipTellingInterval
	if !isRecentlyTold {
		if err := self.tellAboutRelationship(ctx, other.ID, models.NoteRelationshipRemoved,
			models.RelationshipDetail(user.DisplayName(), before.KindFor(other.ID), before.TermGenderFor(other.ID, user.Pronoun))); err != nil {
			return false, err
		}
	}
	log.Noticef("%s removed how they are related to another account", user.Username)
	return true, nil
}

// tellAboutRelationship writes the note to the other person in this
// transaction, so there is never a change without it, and sends their phones
// the notification once it commits.
func (self *graph) tellAboutRelationship(ctx context.Context, toldUserId string, noteKind models.AgentNoteKind, detail string) error {
	tx := self.transaction(ctx)
	agentId, err := agent.NoteRelationship(tx, toldUserId, noteKind, detail)
	if err != nil {
		return err
	}
	if notifier := self.phoneNotifier(); notifier != nil && agentId != "" {
		tx.AfterCommit(func() { notifier.NotifyRelationship(agentId, noteKind, detail) })
	}
	return nil
}

// pronounArgument reads the pronoun a person gave for themselves: he, she or
// they, or empty to say nothing.
func pronounArgument(pronoun string) (models.Pronoun, error) {
	parsed := models.Pronoun(strings.ToLower(strings.TrimSpace(pronoun)))
	if parsed != "" && !parsed.IsValid() {
		return "", fmt.Errorf("%w: the pronoun is he, she or they, or empty", api.ErrInvalidArguments)
	}
	return parsed, nil
}
