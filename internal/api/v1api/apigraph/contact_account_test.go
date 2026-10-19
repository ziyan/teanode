package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// grantUse gives a person the permissions of keeping contacts and photos, on
// top of the circles the circle world gave them.
func (self *circleWorld) grantContactsAndPhotos(t *testing.T, users ...*models.User) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "contacts and photos", Permissions: []models.Permission{models.PermissionContactsUse, models.PermissionPhotosUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		userIds := make([]string, 0, len(users))
		for _, user := range users {
			userIds = append(userIds, user.ID)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "contacts and photos group", UserIDs: userIds, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
	})
}

// keepContactAs keeps a contact with GraphQL and returns the book and contact
// identifiers.
func (self *circleWorld) keepContactAs(t *testing.T, user *models.User, name string) (string, string) {
	t.Helper()
	books := self.call(t, user, `query { ListAddressBooks { id } }`)["ListAddressBooks"].([]any)
	bookId := books[0].(map[string]any)["id"].(string)
	saved := self.call(t, user, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, name: %q) { id } }`, bookId, name))
	return bookId, saved["SaveContact"].(map[string]any)["id"].(string)
}

// A contact is linked to somebody in the owner's circles, and only the owner
// sees it. The refusals are in words, and leaving the circle ends it.
func TestAContactIsLinkedToSomebodyInTheCirclesAndOnlyTheOwnerSeesIt(t *testing.T) {
	world := newCircleWorld(t)
	world.grantContactsAndPhotos(t, world.alice, world.bob, world.erin)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	world.join(t, world.alice, circleId, world.bob, "member")

	bookId, contactId := world.keepContactAs(t, world.alice, "Example Bob")
	_, secondId := world.keepContactAs(t, world.alice, "Example Bob again")

	link := func(user *models.User, bookId, contactId string, accountUserId string) string {
		return fmt.Sprintf(`mutation { SetContactAccount(addressBookId: %q, contactId: %q, accountUserId: %q) { id accountUserId account { userId username name avatarColorIndex } } }`,
			bookId, contactId, accountUserId)
	}
	linked := world.call(t, world.alice, link(world.alice, bookId, contactId, world.bob.ID))["SetContactAccount"].(map[string]any)
	if linked["accountUserId"] != world.bob.ID || linked["account"].(map[string]any)["username"] != world.bob.Username {
		t.Fatalf("the link is answered: %v", linked)
	}

	// Read back by every way a contact is read.
	one := world.call(t, world.alice, fmt.Sprintf(`query { GetContact(id: %q) { accountUserId account { username } } }`, contactId))["GetContact"].(map[string]any)
	if one["accountUserId"] != world.bob.ID {
		t.Fatalf("GetContact: %v", one)
	}
	listed := world.call(t, world.alice, fmt.Sprintf(`query { ListContacts(addressBookId: %q) { id accountUserId } }`, bookId))["ListContacts"].([]any)
	isListed := false
	for _, each := range listed {
		if row := each.(map[string]any); row["id"] == contactId {
			isListed = row["accountUserId"] == world.bob.ID
		}
	}
	if !isListed {
		t.Fatalf("ListContacts: %v", listed)
	}
	// Saving the card again keeps it.
	saved := world.call(t, world.alice, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, id: %q, name: "Bob Renamed") { accountUserId } }`, bookId, contactId))["SaveContact"].(map[string]any)
	if saved["accountUserId"] != world.bob.ID {
		t.Fatalf("saving the card dropped the link: %v", saved)
	}

	// The circle people carry the caller's own contact, for the linked person only.
	for _, each := range world.call(t, world.alice, `query { ListCirclePeople { username linkedContactId } }`)["ListCirclePeople"].([]any) {
		person := each.(map[string]any)
		if person["username"] == world.bob.Username && person["linkedContactId"] != contactId {
			t.Fatalf("Bob's row lacks the contact: %v", person)
		}
	}
	inCircle := world.call(t, world.alice, fmt.Sprintf(`query { ReadCircle(circleId: %q) { members { username linkedContactId } } }`, circleId))["ReadCircle"].(map[string]any)["members"].([]any)
	for _, each := range inCircle {
		if member := each.(map[string]any); member["username"] == world.bob.Username && member["linkedContactId"] != contactId {
			t.Fatalf("the circle's member lacks the contact: %v", member)
		}
	}

	// The linked person sees nothing of it.
	for _, each := range world.call(t, world.bob, `query { ListCirclePeople { username linkedContactId } }`)["ListCirclePeople"].([]any) {
		if person := each.(map[string]any); person["linkedContactId"] != "" {
			t.Fatalf("Bob sees a link: %v", person)
		}
	}
	if books := world.call(t, world.bob, `query { ListAddressBooks { id contacts } }`)["ListAddressBooks"].([]any); books[0].(map[string]any)["contacts"] != float64(0) {
		t.Fatalf("Bob's address book holds Alice's contacts: %v", books)
	}
	world.refused(t, world.bob, link(world.bob, bookId, contactId, world.alice.ID))

	// The refusals: your own account, somebody outside your circles, the same
	// person twice in one book.
	if said := world.refused(t, world.alice, link(world.alice, bookId, secondId, world.alice.ID)); !strings.Contains(said, "own account") {
		t.Errorf("your own account: %s", said)
	}
	if said := world.refused(t, world.alice, link(world.alice, bookId, secondId, world.erin.ID)); !strings.Contains(said, "not in a circle") {
		t.Errorf("somebody outside your circles: %s", said)
	}
	if said := world.refused(t, world.alice, link(world.alice, bookId, secondId, world.bob.ID)); !strings.Contains(said, "already linked") {
		t.Errorf("the same person twice: %s", said)
	}

	// Photos of the account: nothing for one who is not linked, and no error.
	none := world.call(t, world.alice, fmt.Sprintf(`query { ListPhotos(accountUserId: %q) { id } CountPhotos(accountUserId: %q) }`, world.erin.ID, world.erin.ID))
	if len(none["ListPhotos"].([]any)) != 0 || none["CountPhotos"] != float64(0) {
		t.Errorf("an unlinked account lists photos: %v", none)
	}

	// Unlinking with an empty account.
	cleared := world.call(t, world.alice, fmt.Sprintf(`mutation { SetContactAccount(addressBookId: %q, contactId: %q, accountUserId: "") { accountUserId account { userId } } }`, bookId, contactId))["SetContactAccount"].(map[string]any)
	if cleared["accountUserId"] != nil || cleared["account"] != nil {
		t.Errorf("the link is not cleared: %v", cleared)
	}

	// And leaving the circle ends a link.
	world.call(t, world.alice, link(world.alice, bookId, contactId, world.bob.ID))
	world.call(t, world.bob, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId))
	after := world.call(t, world.alice, fmt.Sprintf(`query { GetContact(id: %q) { accountUserId account { userId } } }`, contactId))["GetContact"].(map[string]any)
	if after["accountUserId"] != nil || after["account"] != nil {
		t.Errorf("a link outlives the shared circle: %v", after)
	}
}

// Somebody who may not keep contacts cannot link one.
func TestLinkingAContactNeedsContacts(t *testing.T) {
	world := newCircleWorld(t)
	world.refused(t, world.dave, `mutation { SetContactAccount(addressBookId: "x", contactId: "y", accountUserId: "z") { id } }`)
	world.refused(t, world.dave, `query { ListContactAccountSuggestions { account { userId } } }`)
	world.grantContactsAndPhotos(t, world.alice)
	if suggestions := world.call(t, world.alice, `query { ListContactAccountSuggestions { account { userId } } }`)["ListContactAccountSuggestions"].([]any); len(suggestions) != 0 {
		t.Errorf("suggestions from nothing: %v", suggestions)
	}
}
