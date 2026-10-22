package apigraph

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// circleBookOf makes the circle's address book as a member and returns its id.
func (self *circleWorld) circleBookOf(t *testing.T, circleId string, creatorName string) string {
	t.Helper()
	user := self.alice
	if creatorName == "bob" {
		user = self.bob
	}
	made := self.call(t, user, fmt.Sprintf(`mutation { CreateCircleAddressBook(circleId: %q) { id name circleId circleName isCircleBook canManage agentGranted } }`, circleId))["CreateCircleAddressBook"].(map[string]any)
	return made["id"].(string)
}

// Any member makes the circle's address book, once; every member lists, reads
// and edits it, and a person outside the circle finds nothing.
func TestACircleAddressBookIsMadeByAMemberAndUsedByEveryMember(t *testing.T) {
	world := newCircleWorld(t)
	world.grantContactsAndPhotos(t, world.alice, world.bob, world.carol, world.erin)
	circleId := world.makeCircle(t, world.alice, "Family", "family")
	world.join(t, world.alice, circleId, world.bob, "member")

	made := world.call(t, world.bob, fmt.Sprintf(`mutation { CreateCircleAddressBook(circleId: %q) { id name circleId circleName isCircleBook canManage agentGranted contacts } }`, circleId))["CreateCircleAddressBook"].(map[string]any)
	if made["name"] != "Family" || made["circleId"] != circleId || made["circleName"] != "Family" || made["isCircleBook"] != true ||
		made["canManage"] != false || made["agentGranted"] != false {
		t.Fatalf("a member makes the circle's book: %v", made)
	}
	bookId := made["id"].(string)
	if message := world.refused(t, world.alice, fmt.Sprintf(`mutation { CreateCircleAddressBook(circleId: %q) { id } }`, circleId)); !strings.Contains(message, "already") {
		t.Errorf("a circle has one: %s", message)
	}
	world.refused(t, world.erin, fmt.Sprintf(`mutation { CreateCircleAddressBook(circleId: %q) { id } }`, circleId))

	// Each member lists the circle's book beside their own.
	for _, user := range []string{"alice", "bob"} {
		member := world.alice
		if user == "bob" {
			member = world.bob
		}
		books := world.call(t, member, `query { ListReachableAddressBooks { id isCircleBook circleName canManage } }`)["ListReachableAddressBooks"].([]any)
		if len(books) != 2 || books[0].(map[string]any)["isCircleBook"] != false || books[1].(map[string]any)["id"] != bookId ||
			books[1].(map[string]any)["circleName"] != "Family" {
			t.Errorf("%s lists their own book, then the circle's: %v", user, books)
		}
		if books[1].(map[string]any)["canManage"] != (user == "alice") {
			t.Errorf("only the admin manages: %v", books[1])
		}
		// What means "my address book" is only their own.
		if own := world.call(t, member, `query { ListAddressBooks { id isCircleBook } }`)["ListAddressBooks"].([]any); len(own) != 1 || own[0].(map[string]any)["isCircleBook"] != false {
			t.Errorf("%s's own books leave the circle's out: %v", user, own)
		}
	}
	// Somebody outside lists only their own, and cannot read the circle's.
	books := world.call(t, world.erin, `query { ListReachableAddressBooks { id } }`)["ListReachableAddressBooks"].([]any)
	if len(books) != 1 || books[0].(map[string]any)["id"] == bookId {
		t.Errorf("an outsider lists only their own book: %v", books)
	}
	world.refused(t, world.erin, fmt.Sprintf(`query { ListContacts(addressBookId: %q) { id } }`, bookId))
	world.refused(t, world.erin, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, name: "Sam") { id } }`, bookId))

	// A card added by one member is read and changed by the other.
	saved := world.call(t, world.bob, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, name: "Kim", emails: ["kim@example.test"]) { id addressBookId } }`, bookId))["SaveContact"].(map[string]any)
	contactId := saved["id"].(string)
	listed := world.call(t, world.alice, fmt.Sprintf(`query { ListContacts(addressBookId: %q) { id name accountUserId } }`, bookId))["ListContacts"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["id"] != contactId || listed[0].(map[string]any)["accountUserId"] != nil {
		t.Fatalf("alice reads bob's card: %v", listed)
	}
	if read := world.call(t, world.alice, fmt.Sprintf(`query { GetContact(id: %q) { name emails } }`, contactId))["GetContact"].(map[string]any); read["name"] != "Kim" {
		t.Fatalf("GetContact: %v", read)
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, id: %q, name: "Kim Example") { name } }`, bookId, contactId))
	world.refused(t, world.erin, fmt.Sprintf(`query { GetContact(id: %q) { name } }`, contactId))
	world.refused(t, world.erin, fmt.Sprintf(`mutation { DeleteContact(id: %q) }`, contactId))

	// A circle card is never linked to an account.
	if message := world.refused(t, world.alice, fmt.Sprintf(`mutation { SetContactAccount(addressBookId: %q, contactId: %q, accountUserId: %q) { id } }`, bookId, contactId, world.bob.ID)); message == "" {
		t.Errorf("the link was refused without words")
	}

	// Leaving ends it at once, and what bob added stays for the others.
	world.call(t, world.bob, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId))
	world.refused(t, world.bob, fmt.Sprintf(`query { ListContacts(addressBookId: %q) { id } }`, bookId))
	world.refused(t, world.bob, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, id: %q, name: "Changed") { id } }`, bookId, contactId))
	world.refused(t, world.bob, fmt.Sprintf(`query { GetContact(id: %q) { name } }`, contactId))
	if books := world.call(t, world.bob, `query { ListReachableAddressBooks { id } }`)["ListReachableAddressBooks"].([]any); len(books) != 1 {
		t.Errorf("bob lists only his own book now: %v", books)
	}
	if listed := world.call(t, world.alice, fmt.Sprintf(`query { ListContacts(addressBookId: %q) { id } }`, bookId))["ListContacts"].([]any); len(listed) != 1 {
		t.Errorf("the card stays: %v", listed)
	}
}

// An admin renames and deletes the circle's book; a member is told it is the
// admin's to do; a person's own book cannot be deleted.
func TestOnlyAnAdminRenamesOrDeletesACircleAddressBook(t *testing.T) {
	world := newCircleWorld(t)
	world.grantContactsAndPhotos(t, world.alice, world.bob)
	circleId := world.makeCircle(t, world.alice, "Family", "family")
	world.join(t, world.alice, circleId, world.bob, "member")
	bookId := world.circleBookOf(t, circleId, "bob")

	if message := world.refused(t, world.bob, fmt.Sprintf(`mutation { SaveAddressBook(id: %q, name: "Ours") { id } }`, bookId)); !strings.Contains(message, "admin") {
		t.Errorf("a member is told it is for an admin: %s", message)
	}
	if message := world.refused(t, world.bob, fmt.Sprintf(`mutation { DeleteCircleAddressBook(addressBookId: %q) }`, bookId)); !strings.Contains(message, "admin") {
		t.Errorf("a member is told it is for an admin: %s", message)
	}
	renamed := world.call(t, world.alice, fmt.Sprintf(`mutation { SaveAddressBook(id: %q, name: "Ours") { name circleName isCircleBook canManage } }`, bookId))["SaveAddressBook"].(map[string]any)
	if renamed["name"] != "Ours" || renamed["circleName"] != "Family" || renamed["isCircleBook"] != true || renamed["canManage"] != true {
		t.Errorf("an admin renames it: %v", renamed)
	}

	own := world.call(t, world.alice, `query { ListAddressBooks { id } }`)["ListAddressBooks"].([]any)[0].(map[string]any)["id"].(string)
	world.refused(t, world.alice, fmt.Sprintf(`mutation { DeleteCircleAddressBook(addressBookId: %q) }`, own))

	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteCircleAddressBook(addressBookId: %q) }`, bookId))
	if books := world.call(t, world.alice, `query { ListReachableAddressBooks { id } }`)["ListReachableAddressBooks"].([]any); len(books) != 1 {
		t.Errorf("the circle's book is gone: %v", books)
	}
	// And can be made again.
	world.circleBookOf(t, circleId, "alice")
}

// The picture route serves a card in a circle's book to its members only, and
// a card in somebody's own book to nobody else.
func TestTheContactPictureOfACircleCardIsServedToMembersOnly(t *testing.T) {
	world := newCircleWorld(t)
	world.grantContactsAndPhotos(t, world.alice, world.bob, world.carol)
	circleId := world.makeCircle(t, world.alice, "Family", "family")
	world.join(t, world.alice, circleId, world.bob, "member")
	bookId := world.circleBookOf(t, circleId, "alice")
	card := "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:kim\r\nFN:Kim\r\nPHOTO:data:image/png;base64,aGVsbG8=\r\nEND:VCARD\r\n"
	saved := world.call(t, world.alice, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, card: %q) { id } }`, bookId, card))["SaveContact"].(map[string]any)
	contactId := saved["id"].(string)
	ownBooks := world.call(t, world.carol, `query { ListAddressBooks { id } }`)["ListAddressBooks"].([]any)
	ownSaved := world.call(t, world.carol, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, card: %q) { id } }`, ownBooks[0].(map[string]any)["id"], strings.ReplaceAll(card, "kim", "own")))["SaveContact"].(map[string]any)

	resolver := &graph{database: world.database}
	fetch := func(username, contactId, query string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/"+query, nil)
		request.Header.Set(api.AuthenticatedUsernameHeader, username)
		response := httptest.NewRecorder()
		resolver.contactPhotoView(response, mux.SetURLVars(request, map[string]string{"contactId": contactId}))
		return response
	}
	for _, username := range []string{world.alice.Username, world.bob.Username} {
		if response := fetch(username, contactId, ""); response.Code != http.StatusOK || response.Body.String() != "hello" || response.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s is served the picture: %d %q", username, response.Code, response.Body.String())
		}
		if response := fetch(username, contactId, "?addressBookId="+bookId); response.Code != http.StatusOK {
			t.Errorf("%s is served the picture of a named book: %d", username, response.Code)
		}
	}
	if response := fetch(world.carol.Username, contactId, ""); response.Code != http.StatusNotFound {
		t.Errorf("a stranger is told there is none: %d", response.Code)
	}
	if response := fetch(world.carol.Username, contactId, "?addressBookId="+bookId); response.Code != http.StatusNotFound {
		t.Errorf("a stranger naming the book is told there is none: %d", response.Code)
	}
	if response := fetch(world.bob.Username, ownSaved["id"].(string), ""); response.Code != http.StatusNotFound {
		t.Errorf("somebody's own book is not served to a circle mate: %d", response.Code)
	}
	if response := fetch(world.carol.Username, ownSaved["id"].(string), ""); response.Code != http.StatusOK {
		t.Errorf("the owner is served their own: %d", response.Code)
	}
}

// A contact is named only within its book, so a contact in the circle's book
// and one in the person's own book can have the same id. Naming the circle's
// book reaches the circle's, and deleting it, or taking its picture off, leaves
// the person's own alone.
func TestAContactInACircleBookIsReachedByNamingTheBook(t *testing.T) {
	world := newCircleWorld(t)
	world.grantContactsAndPhotos(t, world.alice, world.bob)
	circleId := world.makeCircle(t, world.alice, "Family", "family")
	world.join(t, world.alice, circleId, world.bob, "member")
	circleBookId := world.circleBookOf(t, circleId, "alice")
	ownBookId := world.call(t, world.alice, `query { ListAddressBooks { id } }`)["ListAddressBooks"].([]any)[0].(map[string]any)["id"].(string)

	cardOf := func(name string) string {
		return "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:kim\r\nFN:" + name + "\r\nPHOTO:data:image/png;base64,aGVsbG8=\r\nEND:VCARD\r\n"
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for bookId, name := range map[string]string{circleBookId: "Circle Kim", ownBookId: "Own Kim"} {
			if _, err := tx.PutContact(&models.Contact{ID: "kim.vcf", AddressBookID: bookId, UID: "urn:uuid:kim", ETag: "e", Card: cardOf(name), Name: name}); err != nil {
				t.Fatalf("PutContact: %s", err)
			}
		}
	})

	nameOf := func(user *models.User, bookId string) any {
		query := `query { GetContact(id: "kim.vcf") { name addressBookId } }`
		if bookId != "" {
			query = fmt.Sprintf(`query { GetContact(id: "kim.vcf", addressBookId: %q) { name addressBookId } }`, bookId)
		}
		return world.call(t, user, query)["GetContact"].(map[string]any)["name"]
	}
	if got := nameOf(world.alice, ""); got != "Own Kim" {
		t.Errorf("without a book the person's own contact is found first: %v", got)
	}
	if got := nameOf(world.alice, circleBookId); got != "Circle Kim" {
		t.Errorf("naming the circle's book reaches the circle's contact: %v", got)
	}
	if got := nameOf(world.bob, ""); got != "Circle Kim" {
		t.Errorf("a member with no book of their own holding it finds the circle's: %v", got)
	}
	world.refused(t, world.bob, fmt.Sprintf(`query { GetContact(id: "kim.vcf", addressBookId: %q) { name } }`, ownBookId))

	// Taking the picture off the circle's contact leaves the person's own.
	world.call(t, world.alice, fmt.Sprintf(`mutation { RemoveContactPicture(id: "kim.vcf", addressBookId: %q) { name hasPhoto } }`, circleBookId))
	hasPhoto := func(bookId string) any {
		return world.call(t, world.alice, fmt.Sprintf(`query { GetContact(id: "kim.vcf", addressBookId: %q) { hasPhoto } }`, bookId))["GetContact"].(map[string]any)["hasPhoto"]
	}
	if hasPhoto(circleBookId) != false || hasPhoto(ownBookId) != true {
		t.Errorf("only the circle's picture came off: circle %v own %v", hasPhoto(circleBookId), hasPhoto(ownBookId))
	}

	// Deleting the circle's contact leaves the person's own.
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteContact(id: "kim.vcf", addressBookId: %q) }`, circleBookId))
	if got := nameOf(world.alice, ownBookId); got != "Own Kim" {
		t.Errorf("the person's own contact is kept: %v", got)
	}
	world.refused(t, world.alice, fmt.Sprintf(`query { GetContact(id: "kim.vcf", addressBookId: %q) { name } }`, circleBookId))
	world.refused(t, world.alice, fmt.Sprintf(`mutation { DeleteContact(id: "kim.vcf", addressBookId: %q) }`, circleBookId))
	if got := nameOf(world.alice, ""); got != "Own Kim" {
		t.Errorf("and is still the one found without a book: %v", got)
	}
}

// A member whose roles lack circle:use does not list the circle's book, and
// reads nothing in it.
func TestACircleAddressBookNeedsCircleUse(t *testing.T) {
	world := newCircleWorld(t)
	world.grantContactsAndPhotos(t, world.alice, world.dave)
	circleId := world.makeCircle(t, world.alice, "Family", "family")
	bookId := world.circleBookOf(t, circleId, "alice")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.AddCircleMember(circleId, world.dave.ID, models.MemberRoleMember, world.alice.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
	})
	if books := world.call(t, world.dave, `query { ListReachableAddressBooks { id } }`)["ListReachableAddressBooks"].([]any); len(books) != 1 || books[0].(map[string]any)["id"] == bookId {
		t.Errorf("dave lists only his own book without circle:use: %v", books)
	}
	world.refused(t, world.dave, fmt.Sprintf(`query { ListContacts(addressBookId: %q) { id } }`, bookId))
	world.refused(t, world.dave, fmt.Sprintf(`mutation { SaveContact(addressBookId: %q, name: "Sam") { id } }`, bookId))
}
