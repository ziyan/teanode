package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// SuggestRecordingSpeakerNamesArguments narrows the names offered while a
// speaker is being named to those containing what was typed.
type SuggestRecordingSpeakerNamesArguments struct {
	SearchText string `json:"searchText" graphapi:"nullable"`
	PageSize   int    `json:"pageSize" graphapi:"nullable"`
}

// RecordingSpeakerNameSuggestion is one name to offer, and where it came from:
// speaker for a name already given to a voice, memory for a person page of the
// agent's memory, contact for a card in the person's address books.
type RecordingSpeakerNameSuggestion struct {
	ConfirmedName     string `json:"confirmedName"`
	SpeakerNameSource string `json:"speakerNameSource"`
}

const recordingSpeakerNameSuggestionLimit = 20

// SuggestRecordingSpeakerNames offers names for a speaker: the voices the
// person has named, then the people in their agent's memory, then their
// contacts, each name once. Everything read is the person's own.
func (self *graph) SuggestRecordingSpeakerNames(ctx context.Context, arguments SuggestRecordingSpeakerNamesArguments) ([]*RecordingSpeakerNameSuggestion, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	pageSize := arguments.PageSize
	if pageSize <= 0 || pageSize > recordingSpeakerNameSuggestionLimit {
		pageSize = recordingSpeakerNameSuggestionLimit
	}
	searchText := strings.TrimSpace(arguments.SearchText)
	if len(searchText) > 100 {
		searchText = searchText[:100]
	}
	matches := func(name string) bool {
		return searchText == "" || strings.Contains(strings.ToLower(name), strings.ToLower(searchText))
	}
	suggestions := []*RecordingSpeakerNameSuggestion{}
	isOffered := map[string]bool{}
	offer := func(name, speakerNameSource string) {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if name == "" || len(name) > 64 || isOffered[key] || len(suggestions) >= pageSize || !matches(name) {
			return
		}
		isOffered[key] = true
		suggestions = append(suggestions, &RecordingSpeakerNameSuggestion{ConfirmedName: name, SpeakerNameSource: speakerNameSource})
	}
	transaction := self.transaction(ctx)
	identities, err := transaction.ListRecordingSpeakerIdentities(userId)
	if err != nil {
		return nil, translateError(err)
	}
	for _, identity := range identities {
		offer(identity.ConfirmedName, "speaker")
	}
	assistant, err := transaction.GetAgentByUser(userId)
	if err != nil {
		return nil, translateError(err)
	}
	if assistant != nil {
		people, err := transaction.ListAgentNodesOfKind(assistant.ID, models.NodePerson, searchText, pageSize*2)
		if err != nil {
			return nil, translateError(err)
		}
		for _, person := range people {
			offer(person.Name, "memory")
		}
	}
	addressBooks, err := transaction.ListAddressBooks(userId)
	if err != nil {
		return nil, translateError(err)
	}
	for _, addressBook := range addressBooks {
		contacts, err := transaction.ListContacts(addressBook.ID, searchText, pageSize*2)
		if err != nil {
			return nil, translateError(err)
		}
		for _, contact := range contacts {
			offer(contact.Name, "contact")
		}
	}
	return suggestions, nil
}
