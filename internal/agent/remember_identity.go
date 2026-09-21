package agent

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// bindContact keeps a person the agent learned about in the address book,
// and points the page at them. The address book is the only list of
// people this server keeps, so a
// page about somebody and a card for them are two views of one thing.
func (self *Agent) bindContact(tx db.Transaction, owner *models.User, node *models.AgentNode) error {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		return nil
	}
	books, err := tx.ListAddressBooks(owner.ID)
	if err != nil || len(books) == 0 {
		return err
	}
	book := books[0]
	// Somebody they already keep, matched by name, is not written again.
	existing, err := tx.ListContacts(book.ID, name, 5)
	if err != nil {
		return err
	}
	for _, contact := range existing {
		if strings.EqualFold(strings.TrimSpace(contact.Name), name) {
			_, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: node.AgentID, Path: node.Path, Kind: node.Kind, Name: node.Name,
				Aliases: node.Aliases, Summary: node.Summary, ContactID: contact.ID,
				Pinned: node.Pinned, Importance: node.Importance,
			})
			return err
		}
	}
	// A card with a name and nothing else is a valid card. The page is
	// where what the agent learned lives; this is only the entry that
	// says the person exists.
	built, err := contacts.Build(nil, &contacts.Fields{Name: &name})
	if err != nil {
		return err
	}
	contact, err := tx.PutContact(&models.Contact{
		AddressBookID: book.ID, UID: built.UID, Name: built.Name, Card: string(built.Card),
	})
	if err != nil {
		return err
	}
	_, err = tx.PutAgentNode(&models.AgentNode{
		AgentID: node.AgentID, Path: node.Path, Kind: node.Kind, Name: node.Name,
		Aliases: node.Aliases, Summary: node.Summary, ContactID: contact.ID,
		Pinned: node.Pinned, Importance: node.Importance,
	})
	return err
}

// whenHappened reads the date the run gave, in the person's zone.
func whenHappened(run *Run, said string) *time.Time {
	said = strings.TrimSpace(said)
	if said == "" || strings.EqualFold(said, "null") {
		return nil
	}
	location := time.Local
	if run.Owner != nil && run.Owner.Timezone != "" {
		if loaded, err := time.LoadLocation(run.Owner.Timezone); err == nil {
			location = loaded
		}
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006", time.RFC3339} {
		if when, err := time.ParseInLocation(layout, said, location); err == nil {
			return &when
		}
	}
	return nil
}

// kindOfPath guesses what a page is about from where it was filed.
func kindOfPath(path string) models.AgentNodeKind {
	switch strings.Split(path, "/")[0] {
	case models.PathPeople:
		return models.NodePerson
	case "organizations":
		return models.NodeOrganization
	case models.PathProjects:
		return models.NodeProject
	case models.PathPlaces:
		return models.NodePlace
	case models.PathThings:
		return models.NodeThing
	case models.PathTime:
		return models.NodePeriod
	case models.PathSelf:
		return models.NodeSelf
	}
	return models.NodeTopic
}

// nameFromSlug is a path segment as a name.
func nameFromSlug(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "-", " "), " ")
	for index, word := range words {
		if word == "" {
			continue
		}
		runes := []rune(word)
		words[index] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(words, " ")
}

// firstWordsOf is the opening of a sentence, for naming a page nobody
// named.
func firstWordsOf(text string, count int) string {
	words := strings.Fields(text)
	if len(words) > count {
		words = words[:count]
	}
	return strings.Join(words, " ")
}
