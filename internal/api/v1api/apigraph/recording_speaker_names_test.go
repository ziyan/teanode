package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Names come from the person's memory before their contacts, each once, and
// only those containing what was typed.
func TestSpeakerNamesComeFromMemoryThenContactsOnce(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "speaker-names-owner")
	strangerId := dbtest.CreateUser(test, database, "speaker-names-stranger")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		assistant, err := transaction.CreateAgent(&models.Agent{UserID: ownerId, Name: "Fixture assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		for _, name := range []string{"Alma Fixture", "Bram Example"} {
			if _, err := transaction.PutAgentNode(&models.AgentNode{AgentID: assistant.ID, Path: "people/" + name, Kind: models.NodePerson, Name: name}); err != nil {
				test.Fatal(err)
			}
		}
		if _, err := transaction.PutAgentNode(&models.AgentNode{AgentID: assistant.ID, Path: "projects/alma-project", Kind: models.NodeProject, Name: "Alma Project"}); err != nil {
			test.Fatal(err)
		}
		for userId, names := range map[string][]string{ownerId: {"alma fixture", "Almond Contact"}, strangerId: {"Almost Stranger"}} {
			book, err := transaction.CreateAddressBook(&models.AddressBook{UserID: userId, Name: "Contacts"})
			if err != nil {
				test.Fatal(err)
			}
			for index, name := range names {
				uid := userId + "-card-" + string(rune('a'+index))
				if _, err := transaction.PutContact(&models.Contact{AddressBookID: book.ID, UID: uid, Card: "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + name + "\r\nEND:VCARD\r\n", Name: name}); err != nil {
					test.Fatal(err)
				}
			}
		}
	})
	resolver := &graph{database: database}
	var suggestions []*RecordingSpeakerNameSuggestion
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		owner, err := transaction.GetUser(ownerId)
		if err != nil {
			test.Fatal(err)
		}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}), transaction)
		if suggestions, err = resolver.SuggestRecordingSpeakerNames(ctx, SuggestRecordingSpeakerNamesArguments{SearchText: "alm"}); err != nil {
			test.Fatal(err)
		}
	})
	offered := []string{}
	for _, suggestion := range suggestions {
		offered = append(offered, suggestion.ConfirmedName+"/"+suggestion.SpeakerNameSource)
	}
	if len(offered) != 2 || offered[0] != "Alma Fixture/memory" || offered[1] != "Almond Contact/contact" {
		test.Fatalf("names were not offered from memory, then contacts, once each and only the person's own: %v", offered)
	}
}
