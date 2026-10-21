package apigraph

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// personCardWorld is the health world (ada and ben in one circle, cora in none
// of ada's, dave in a circle of his own), where ada, ben and dave may also use
// everything a card reads: Location, Contacts, Mail, Photos and the agent.
type personCardWorld struct {
	*healthWorld

	// addressBooks are the address books made so far, by owner.
	addressBooks map[string]string
}

func newPersonCardWorld(test *testing.T) *personCardWorld {
	test.Helper()
	world := &personCardWorld{healthWorld: newHealthWorld(test), addressBooks: map[string]string{}}
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		role, err := transaction.CreateRole(&models.Role{Name: "card role", Permissions: []models.Permission{
			models.PermissionLocationUse, models.PermissionContactsUse, models.PermissionMailRead,
			models.PermissionPhotosUse, models.PermissionAgentUse,
		}})
		if err != nil {
			test.Fatal(err)
		}
		for _, user := range []*models.User{world.ada, world.ben, world.dave} {
			if _, err := transaction.CreateGroup(&models.Group{Name: user.Username + " card group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				test.Fatal(err)
			}
		}
	})
	return world
}

const personCardFields = `
  person { userId username name relationshipWord linkedContactId }
  contact { id name emails accountUserId }
  sharedCircles { circleName }
  canMessage
  location { sectionState hasMoreTodayVisits location { shareLevel latestPoint { latitudeDegrees } } todayVisits { id } }
  health { sectionState hasMore healthTypes { healthType latestDaySummary { summaryDay sumValue } } }
  photos { photosState hasMorePhotos albumsState hasMoreAlbums photos { id } albums { id albumTitle ownerUsername } }
  mail { sectionState hasMore mails { mailboxItemId mailboxId folderPath subject fromAddress isFromPerson isUnread snippet } }
  recordings { sectionState hasMore recordings { recordingId } }
  memory { sectionState hasMore pages { nodePath nodeKind summary isMatchedByName } }`

// readCard is a person's card as another reads it.
func (self *personCardWorld) readCard(test *testing.T, viewer *models.User, arguments string) map[string]any {
	test.Helper()
	data := self.run(test, viewer, `query { ReadPersonCard(`+arguments+`) {`+personCardFields+`} }`, nil)
	return data["ReadPersonCard"].(map[string]any)
}

// refusedCard is what a card that was refused said.
func (self *personCardWorld) refusedCard(test *testing.T, viewer *models.User, arguments string) string {
	test.Helper()
	return self.refusal(test, viewer, `query { ReadPersonCard(`+arguments+`) {`+personCardFields+`} }`, nil)
}

// cardOf is the card of an account.
func (self *personCardWorld) cardOf(test *testing.T, viewer, person *models.User) map[string]any {
	test.Helper()
	return self.readCard(test, viewer, fmt.Sprintf(`userId: %q`, person.ID))
}

// stateOf is the state of a section of a card, or of one part of the photos
// section (photosState, albumsState).
func stateOf(card map[string]any, name string) string {
	if name == "photosState" || name == "albumsState" {
		state, _ := sectionOf(card, "photos")[name].(string)
		return state
	}
	state, _ := sectionOf(card, name)["sectionState"].(string)
	return state
}

func sectionOf(card map[string]any, name string) map[string]any {
	section, _ := card[name].(map[string]any)
	return section
}

// switchOnAgent gives a person an agent that is on: the row a person who chats
// already has is switched on, and one is made for a person who has none.
func (self *personCardWorld) switchOnAgent(test *testing.T, transaction db.Transaction, user *models.User) *models.Agent {
	test.Helper()
	found, err := transaction.EnsureAgent(user.ID)
	if err != nil {
		test.Fatal(err)
	}
	if found, err = transaction.UpdateAgent(found.ID, func(each *models.Agent) error {
		each.Enabled = true
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	return found
}

// keepContact keeps a contact in the owner's address book, linked to an
// account when one is given, and says the address book and the contact.
func (self *personCardWorld) keepContact(test *testing.T, owner *models.User, name string, emails []string, accountUserId string) (string, string) {
	test.Helper()
	var bookId, contactId string
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		bookId = self.addressBooks[owner.ID]
		if bookId == "" {
			book, err := transaction.CreateAddressBook(&models.AddressBook{UserID: owner.ID, Name: "Contacts"})
			if err != nil {
				test.Fatal(err)
			}
			bookId = book.ID
			self.addressBooks[owner.ID] = bookId
		}
		card := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:" + name + "\r\n"
		for _, email := range emails {
			card += "EMAIL:" + email + "\r\n"
		}
		card += "END:VCARD\r\n"
		contact, err := transaction.PutContact(&models.Contact{
			AddressBookID: bookId, UID: "uid-" + name, ETag: "etag-" + name, Name: name, Emails: emails, Card: card,
		})
		if err != nil {
			test.Fatal(err)
		}
		contactId = contact.ID
		if accountUserId != "" {
			if err := transaction.SetContactAccount(bookId, contactId, accountUserId); err != nil {
				test.Fatal(err)
			}
		}
	})
	return bookId, contactId
}

// A person outside every circle the caller is in, and a contact that is not the
// caller's, are not found, and so is nobody at all; a card of oneself, and a
// question that does not say whose card it is, are refused in words.
func TestReadPersonCardNotFoundOutsideACircleAndForAnotherPersonsContact(test *testing.T) {
	world := newPersonCardWorld(test)
	benBookId, benContactId := world.keepContact(test, world.ben, "Ben's Friend", []string{"friend@example.org"}, "")
	adaBookId, adaContactId := world.keepContact(test, world.ada, "Ada's Friend", []string{"pal@example.org"}, "")

	if said := world.refusedCard(test, world.cora, fmt.Sprintf(`userId: %q`, world.ada.ID)); !strings.Contains(said, "not found") {
		test.Errorf("cora shares no circle with ada: %q", said)
	}
	if said := world.refusedCard(test, world.dave, fmt.Sprintf(`userId: %q`, world.ben.ID)); !strings.Contains(said, "not found") {
		test.Errorf("dave shares no circle with ben: %q", said)
	}
	if said := world.refusedCard(test, world.ada, `userId: "nobody-has-this-id"`); !strings.Contains(said, "not found") {
		test.Errorf("nobody: %q", said)
	}
	if said := world.refusedCard(test, world.ada, fmt.Sprintf(`userId: %q`, world.ada.ID)); !strings.Contains(said, "somebody else") {
		test.Errorf("a card of oneself: %q", said)
	}
	for _, arguments := range []string{
		`itemLimit: 3`,
		fmt.Sprintf(`userId: %q, addressBookId: %q, contactId: %q`, world.ben.ID, adaBookId, adaContactId),
		fmt.Sprintf(`addressBookId: %q`, adaBookId),
		fmt.Sprintf(`contactId: %q`, adaContactId),
	} {
		if said := world.refusedCard(test, world.ada, arguments); !strings.Contains(said, "give either userId") && !strings.Contains(said, "together") {
			test.Errorf("%q was not refused for what it said: %q", arguments, said)
		}
	}

	// Another person's contact, by its own book or ada's, and one that is
	// not there.
	for name, arguments := range map[string]string{
		"ben's book and contact":      fmt.Sprintf(`addressBookId: %q, contactId: %q`, benBookId, benContactId),
		"ben's contact in ada's book": fmt.Sprintf(`addressBookId: %q, contactId: %q`, adaBookId, benContactId),
		"no such contact":             fmt.Sprintf(`addressBookId: %q, contactId: "nothing"`, adaBookId),
		"no such book":                fmt.Sprintf(`addressBookId: "nothing", contactId: %q`, adaContactId),
	} {
		if said := world.refusedCard(test, world.ada, arguments); !strings.Contains(said, "not found") {
			test.Errorf("%s: %q", name, said)
		}
	}
	// Her own contact is her own to read.
	card := world.readCard(test, world.ada, fmt.Sprintf(`addressBookId: %q, contactId: %q`, adaBookId, adaContactId))
	if card["contact"].(map[string]any)["name"] != "Ada's Friend" || card["person"] != nil {
		test.Errorf("her own contact: %v", card)
	}
}

// With no share, location and health read as not shared; the section for a
// share turns shown the moment it is made, at the level it was made at; and
// the card names the circles the two share and lets the caller message them.
func TestReadPersonCardAShareThatIsOffIsNotShared(test *testing.T) {
	world := newPersonCardWorld(test)

	card := world.cardOf(test, world.ben, world.ada)
	person := card["person"].(map[string]any)
	if person["userId"] != world.ada.ID || person["username"] != world.ada.Username {
		test.Errorf("the person: %v", person)
	}
	if circles := card["sharedCircles"].([]any); len(circles) != 1 || circles[0].(map[string]any)["circleName"] != "Home" {
		test.Errorf("the circles they share: %v", card["sharedCircles"])
	}
	if card["canMessage"] != true {
		test.Errorf("ben can message ada: %v", card["canMessage"])
	}
	for _, name := range []string{"location", "health"} {
		if state := stateOf(card, name); state != "notShared" {
			test.Errorf("%s with nothing shared is %q", name, state)
		}
	}
	if location := sectionOf(card, "location"); location["location"] != nil || len(location["todayVisits"].([]any)) != 0 {
		test.Errorf("a location that is not shared shows nothing: %v", location)
	}

	// Ada reports where she is, a visit today and steps, and shares them.
	// The card counts today in UTC for a caller with no time zone, and just
	// after midnight half an hour ago was yesterday, so the visit is kept
	// inside today.
	now := time.Now().UTC()
	arrivedAt := laterOf(now.Add(-30*time.Minute), now.Truncate(24*time.Hour))
	departedAt := laterOf(now.Add(-10*time.Minute), arrivedAt.Add(time.Second))
	world.run(test, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: [%s]) { newPointCount } }`,
		pointInput(40.0005, -75.0003, ago(5*time.Minute)),
		visitInput("card-visit", 40.0005, -75.0003, arrivedAt.Format(time.RFC3339), departedAt.Format(time.RFC3339))), nil)
	world.run(test, world.ada, reportHealthQuery, map[string]any{"daySummaries": []any{
		daySummaryInput("stepCount", time.Now().UTC().Format("2006-01-02"), map[string]any{"quantityUnit": "count", "sumValue": 1234}),
	}})
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if _, err := transaction.ShareLocation(world.ada.ID, &models.LocationShare{GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelCurrent}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID, HealthCategories: []string{"activity"}}); err != nil {
			test.Fatal(err)
		}
	})

	card = world.cardOf(test, world.ben, world.ada)
	location := sectionOf(card, "location")
	if location["sectionState"] != "shown" || location["location"].(map[string]any)["shareLevel"] != "current" {
		test.Errorf("location shared as current: %v", location)
	}
	if location["location"].(map[string]any)["latestPoint"] == nil || len(location["todayVisits"].([]any)) != 0 {
		test.Errorf("the current level shows where, and no visits: %v", location)
	}
	health := sectionOf(card, "health")
	types := health["healthTypes"].([]any)
	if health["sectionState"] != "shown" || len(types) != 1 || types[0].(map[string]any)["healthType"] != "stepCount" ||
		types[0].(map[string]any)["latestDaySummary"].(map[string]any)["sumValue"] != float64(1234) {
		test.Errorf("health shared in activity: %v", health)
	}

	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if _, err := transaction.ShareLocation(world.ada.ID, &models.LocationShare{GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelHistory}); err != nil {
			test.Fatal(err)
		}
	})
	location = sectionOf(world.cardOf(test, world.ben, world.ada), "location")
	if visits := location["todayVisits"].([]any); location["location"].(map[string]any)["shareLevel"] != "history" || len(visits) != 1 {
		test.Errorf("the history level adds today's visits: %v", location)
	}
	// Taking the share back is seen at once.
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if _, err := transaction.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID, HealthCategories: []string{}}); err != nil {
			test.Fatal(err)
		}
	})
	if state := stateOf(world.cardOf(test, world.ben, world.ada), "health"); state != "notShared" {
		test.Errorf("a health share taken back is %q", state)
	}
}

// Everything found through the caller's contact for the person needs one:
// without it the card says so, and with it the sections are read.
func TestReadPersonCardNeedsAContactForWhatIsFoundThroughOne(test *testing.T) {
	world := newPersonCardWorld(test)
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		world.switchOnAgent(test, transaction, world.ben)
	})

	card := world.cardOf(test, world.ben, world.ada)
	if card["contact"] != nil || card["person"].(map[string]any)["linkedContactId"] != "" {
		test.Errorf("ben has no contact for ada: %v", card)
	}
	for name, want := range map[string]string{"mail": "needsContact", "recordings": "needsContact", "memory": "needsContact", "photosState": "needsContact", "albumsState": "shown"} {
		if state := stateOf(card, name); state != want {
			test.Errorf("%s without a contact is %q, not %q", name, state, want)
		}
	}

	bookId, contactId := world.keepContact(test, world.ben, "Ada", []string{"ada@example.org"}, world.ada.ID)
	card = world.cardOf(test, world.ben, world.ada)
	if card["contact"].(map[string]any)["id"] != contactId || card["person"].(map[string]any)["linkedContactId"] != contactId {
		test.Errorf("the contact for ada: %v", card["contact"])
	}
	for name, want := range map[string]string{"mail": "shown", "recordings": "shown", "memory": "shown", "photosState": "shown", "albumsState": "shown"} {
		if state := stateOf(card, name); state != want {
			test.Errorf("%s with a contact is %q, not %q", name, state, want)
		}
	}

	// The same card by the contact: the account's sections are there too.
	byContact := world.readCard(test, world.ben, fmt.Sprintf(`addressBookId: %q, contactId: %q`, bookId, contactId))
	if byContact["person"] == nil || stateOf(byContact, "location") != "notShared" || byContact["canMessage"] != true {
		test.Errorf("a contact linked to an account is that account: %v", byContact)
	}
}

// A contact that is not an account has the card without what hangs on the
// account: no person, no circles, no chat, and location, health and the albums
// are left out as unavailable.
func TestReadPersonCardOfAContactThatIsNotAnAccount(test *testing.T) {
	world := newPersonCardWorld(test)
	bookId, contactId := world.keepContact(test, world.ben, "Sam Example", []string{"sam@example.org"}, "")

	card := world.readCard(test, world.ben, fmt.Sprintf(`addressBookId: %q, contactId: %q`, bookId, contactId))
	if card["person"] != nil || card["canMessage"] != false || len(card["sharedCircles"].([]any)) != 0 {
		test.Errorf("nobody in the circles: %v", card)
	}
	if card["contact"].(map[string]any)["name"] != "Sam Example" {
		test.Errorf("the contact: %v", card["contact"])
	}
	for name, want := range map[string]string{"location": "unavailable", "health": "unavailable", "albumsState": "unavailable", "photosState": "shown", "mail": "shown"} {
		if state := stateOf(card, name); state != want {
			test.Errorf("%s is %q, not %q", name, state, want)
		}
	}

	// A contact linked to somebody ben no longer shares a circle with is
	// not that person's card.
	otherBookId, otherContactId := world.keepContact(test, world.ben, "Dave", nil, "")
	dbtest.Exec(test, world.database, fmt.Sprintf(`UPDATE contact SET account_user_id = '%s' WHERE id = '%s'`, world.dave.ID, otherContactId))
	card = world.readCard(test, world.ben, fmt.Sprintf(`addressBookId: %q, contactId: %q`, otherBookId, otherContactId))
	if card["person"] != nil || stateOf(card, "location") != "unavailable" {
		test.Errorf("a link to somebody outside the circles shows nothing of them: %v", card)
	}
}

// A person who may not use a part of the app has it left out as unavailable,
// not as not shared.
func TestReadPersonCardLeavesOutWhatTheCallerMayNotUse(test *testing.T) {
	world := newPersonCardWorld(test)
	// Cora may use circles and calendars only; she joins ada's circle.
	world.join(test, world.ada, world.home, world.cora)

	card := world.cardOf(test, world.cora, world.ada)
	for name := range map[string]bool{"location": true, "health": true, "mail": true, "recordings": true, "memory": true} {
		if state := stateOf(card, name); state != "unavailable" {
			test.Errorf("%s for somebody who may not use it is %q", name, state)
		}
	}
	for _, name := range []string{"photosState", "albumsState"} {
		if state := stateOf(card, name); state != "unavailable" {
			test.Errorf("%s for somebody who may not use Photos is %q", name, state)
		}
	}
	if card["contact"] != nil {
		test.Errorf("somebody who may not keep contacts has no contact: %v", card["contact"])
	}
}

// Albums the person owns and shared with the caller are listed newest first,
// and an album they did not share, or that the caller owns, is not.
func TestReadPersonCardListsTheAlbumsTheyShared(test *testing.T) {
	world := newPersonCardWorld(test)
	var sharedId, keptId string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		first, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip one", nil)
		if err != nil {
			test.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		second, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip two", nil)
		if err != nil {
			test.Fatal(err)
		}
		kept, err := transaction.CreatePhotoAlbum(world.ada.ID, "Private", nil)
		if err != nil {
			test.Fatal(err)
		}
		mine, err := transaction.CreatePhotoAlbum(world.ben.ID, "Ben's own", nil)
		if err != nil {
			test.Fatal(err)
		}
		_ = mine
		for _, album := range []*models.PhotoAlbum{first, second} {
			if _, err := transaction.SharePhotoAlbum(world.ada.ID, &models.PhotoAlbumShare{PhotoAlbumID: album.ID, GranteeUserID: world.ben.ID, ShareLevel: models.PhotoAlbumShareLevelView}); err != nil {
				test.Fatal(err)
			}
		}
		sharedId, keptId = second.ID, kept.ID
	})

	photos := sectionOf(world.cardOf(test, world.ben, world.ada), "photos")
	albums := photos["albums"].([]any)
	if photos["albumsState"] != "shown" || len(albums) != 2 || albums[0].(map[string]any)["id"] != sharedId ||
		albums[1].(map[string]any)["albumTitle"] != "Trip one" || albums[0].(map[string]any)["ownerUsername"] != world.ada.Username {
		test.Errorf("the albums ada shared, newest first: %v", photos)
	}
	for _, each := range albums {
		if each.(map[string]any)["id"] == keptId {
			test.Errorf("an album ada did not share: %v", photos)
		}
	}

	// Two are more than the one the card keeps.
	limited := world.readCard(test, world.ben, fmt.Sprintf(`userId: %q, itemLimit: 1`, world.ada.ID))
	if photos := sectionOf(limited, "photos"); len(photos["albums"].([]any)) != 1 || photos["hasMoreAlbums"] != true {
		test.Errorf("one album and more: %v", photos)
	}
}

// Mail with a person is found by the exact addresses on the contact, in any
// case and with a name beside them, across all of the caller's mailboxes; an
// address inside a longer one does not match, and neither does mail in the
// trash or the junk folder, a draft, or somebody else's mailbox.
func TestReadPersonCardMailMatchesWholeAddressesAcrossMailboxes(test *testing.T) {
	world := newPersonCardWorld(test)
	world.keepContact(test, world.ben, "Ada", []string{"ada.one@example.org", "Ada.Two@example.org"}, world.ada.ID)
	now := time.Now()
	var firstMailboxId, secondMailboxId string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		makeMailbox := func(userId, name string) (*models.Mailbox, func(kind models.MailboxFolderKind) *models.MailboxFolder) {
			mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: name})
			if err != nil {
				test.Fatal(err)
			}
			return mailbox, func(kind models.MailboxFolderKind) *models.MailboxFolder {
				folder, err := transaction.GetFolderByKind(mailbox.ID, kind)
				if err != nil || folder == nil {
					test.Fatalf("no %s folder: %v %v", kind, folder, err)
				}
				return folder
			}
		}
		deliver := func(folder *models.MailboxFolder, from, fromName string, recipients []string, subject string, receivedAt time.Time, kind models.MailKind) {
			mail, err := transaction.CreateMail(&models.Mail{
				From: from, FromName: fromName, Sender: from, Recipients: recipients, Subject: subject, Kind: kind,
				MessageID: "<" + subject + "@example.net>", ReceivedAt: receivedAt,
			}, nil)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				test.Fatal(err)
			}
		}
		first, firstFolder := makeMailbox(world.ben.ID, "Personal")
		second, secondFolder := makeMailbox(world.ben.ID, "Work")
		adasOwn, adasFolder := makeMailbox(world.ada.ID, "Ada's mailbox")
		firstMailboxId, secondMailboxId = first.ID, second.ID
		_ = adasOwn

		deliver(firstFolder(models.MailboxFolderKindInbox), "ada.one@example.org", "Ada", []string{"ben@example.net"}, "plain", now.Add(-5*time.Hour), models.MailKindIncoming)
		deliver(secondFolder(models.MailboxFolderKindInbox), "ADA.TWO@example.org", "", []string{"ben@example.net"}, "upper case", now.Add(-4*time.Hour), models.MailKindIncoming)
		deliver(secondFolder(models.MailboxFolderKindSent), "ben@example.net", "Ben", []string{"someone@example.org", "ada.one@example.org"}, "sent to her", now.Add(-3*time.Hour), models.MailKindOutgoing)
		deliver(firstFolder(models.MailboxFolderKindInbox), "Ada <ada.one@example.org>", "Ada", []string{"ben@example.net"}, "with a name", now.Add(-2*time.Hour), models.MailKindIncoming)
		// None of these is mail with her.
		deliver(firstFolder(models.MailboxFolderKindInbox), "xada.one@example.org", "", []string{"ben@example.net"}, "longer before", now.Add(-time.Hour), models.MailKindIncoming)
		deliver(firstFolder(models.MailboxFolderKindInbox), "ada.one@example.org.evil.example", "", []string{"ben@example.net"}, "longer after", now.Add(-time.Hour), models.MailKindIncoming)
		deliver(firstFolder(models.MailboxFolderKindInbox), "bob@example.org", "", []string{"xada.one@example.org"}, "recipient inside a longer one", now.Add(-time.Hour), models.MailKindIncoming)
		deliver(firstFolder(models.MailboxFolderKindTrash), "ada.one@example.org", "", []string{"ben@example.net"}, "in the trash", now, models.MailKindIncoming)
		deliver(firstFolder(models.MailboxFolderKindJunk), "ada.one@example.org", "", []string{"ben@example.net"}, "in junk", now, models.MailKindIncoming)
		deliver(firstFolder(models.MailboxFolderKindDrafts), "ben@example.net", "", []string{"ada.one@example.org"}, "a draft", now, models.MailKindDraft)
		// Ada's own mail, from the same address, is not ben's to see.
		deliver(adasFolder(models.MailboxFolderKindInbox), "ada.one@example.org", "", []string{"ada@example.net"}, "ada's mailbox", now, models.MailKindIncoming)
	})

	card := world.readCard(test, world.ben, fmt.Sprintf(`userId: %q, itemLimit: 10`, world.ada.ID))
	mail := sectionOf(card, "mail")
	var subjects []string
	mailboxIds := map[string]bool{}
	for _, each := range mail["mails"].([]any) {
		mailView := each.(map[string]any)
		subjects = append(subjects, mailView["subject"].(string))
		mailboxIds[mailView["mailboxId"].(string)] = true
		if mailView["subject"] == "sent to her" && mailView["isFromPerson"] != false {
			test.Errorf("mail ben sent is not from her: %v", mailView)
		}
		if mailView["subject"] == "plain" && (mailView["isFromPerson"] != true || mailView["isUnread"] != true || mailView["folderPath"] != "Inbox") {
			test.Errorf("mail she sent: %v", mailView)
		}
	}
	// Newest first.
	if want := []string{"with a name", "sent to her", "upper case", "plain"}; !slices.Equal(subjects, want) || mail["hasMore"] != false {
		test.Errorf("the mail with her was %v, want %v (%v)", subjects, want, mail)
	}
	if !mailboxIds[firstMailboxId] || !mailboxIds[secondMailboxId] {
		test.Errorf("both mailboxes are read: %v", mailboxIds)
	}

	// Fewer than there are says there is more.
	limited := sectionOf(world.readCard(test, world.ben, fmt.Sprintf(`userId: %q, itemLimit: 2`, world.ada.ID)), "mail")
	if len(limited["mails"].([]any)) != 2 || limited["hasMore"] != true {
		test.Errorf("two and more: %v", limited)
	}
}

// The agent's one line about a mail is the snippet, without the marks of
// Markdown; a mail it has not read has none.
func TestReadPersonCardMailSnippetIsWhatTheAgentWrote(test *testing.T) {
	world := newPersonCardWorld(test)
	world.keepContact(test, world.ben, "Ada", []string{"ada@example.org"}, world.ada.ID)
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		agent := world.switchOnAgent(test, transaction, world.ben)
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: world.ben.ID, Name: "Personal"})
		if err != nil {
			test.Fatal(err)
		}
		inbox, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			test.Fatal(err)
		}
		for index, subject := range []string{"read", "unread"} {
			mail, err := transaction.CreateMail(&models.Mail{From: "ada@example.org", Subject: subject, Kind: models.MailKindIncoming, ReceivedAt: time.Now().Add(-time.Duration(index+1) * time.Hour)}, nil)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				test.Fatal(err)
			}
			if subject == "read" {
				if err := transaction.PutMailInsight(&models.MailInsight{MailboxID: mailbox.ID, MailID: mail.ID, AgentID: agent.ID, Category: "personal", Priority: "normal", Summary: "# Plans\n- bring chairs"}); err != nil {
					test.Fatal(err)
				}
			}
		}
	})
	mails := sectionOf(world.cardOf(test, world.ben, world.ada), "mail")["mails"].([]any)
	if len(mails) != 2 || mails[0].(map[string]any)["snippet"] != "Plans bring chairs" || mails[1].(map[string]any)["snippet"] != "" {
		test.Errorf("the snippets: %v", mails)
	}
}

// Memory pages tied to the contact are listed with the page about the person
// first, whatever was changed last.
func TestReadPersonCardMemoryPutsThePersonPageFirst(test *testing.T) {
	world := newPersonCardWorld(test)
	_, contactId := world.keepContact(test, world.ben, "Ada", []string{"ada@example.org"}, world.ada.ID)
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		agent := world.switchOnAgent(test, transaction, world.ben)
		link := func(path string, kind models.AgentNodeKind, summary string) {
			node, err := transaction.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: path, Kind: kind, Name: path, Summary: summary})
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.PutAgentRecordLink(&models.AgentRecordLink{
				AgentID: agent.ID, NodeID: node.ID, RecordKind: models.RecordContact, RecordID: contactId,
				RecordRelation: models.RecordRelationAbout, LinkStatus: models.EdgeStated,
				Evidence: []models.Evidence{{Kind: models.EvidencePerson}},
			}); err != nil {
				test.Fatal(err)
			}
		}
		link("people/ada", models.NodePerson, "# Ada\n- plays the cello")
		time.Sleep(10 * time.Millisecond)
		link("projects/choir", models.NodeProject, "The choir")
		// A page about someone else is not hers.
		if _, err := transaction.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "people/other", Kind: models.NodePerson, Name: "Other", Summary: "Not Ada"}); err != nil {
			test.Fatal(err)
		}
	})
	memory := sectionOf(world.cardOf(test, world.ben, world.ada), "memory")
	pages := memory["pages"].([]any)
	if memory["sectionState"] != "shown" || len(pages) != 2 || pages[0].(map[string]any)["nodePath"] != "people/ada" ||
		pages[0].(map[string]any)["nodeKind"] != "person" || pages[0].(map[string]any)["summary"] != "Ada plays the cello" ||
		pages[1].(map[string]any)["nodePath"] != "projects/choir" {
		test.Errorf("the pages about her: %v", memory)
	}
}

// A page that says it is about the contact (its contact_id) is hers without a
// record link.
func TestReadPersonCardMemoryReadsThePageTiedToTheContact(test *testing.T) {
	world := newPersonCardWorld(test)
	_, contactId := world.keepContact(test, world.ben, "Ada", []string{"ada@example.org"}, world.ada.ID)
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		agent := world.switchOnAgent(test, transaction, world.ben)
		if _, err := transaction.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "people/ada-lovelace", Kind: models.NodePerson, Name: "Ada Lovelace", ContactID: contactId}); err != nil {
			test.Fatal(err)
		}
	})
	pages := sectionOf(world.cardOf(test, world.ben, world.ada), "memory")["pages"].([]any)
	if len(pages) != 1 || pages[0].(map[string]any)["nodePath"] != "people/ada-lovelace" || pages[0].(map[string]any)["isMatchedByName"] != false {
		test.Errorf("the page tied to her contact: %v", pages)
	}
}

// With nothing tied to the contact, the person page with the contact's own
// name is shown, marked as found by its name, unless it is another contact's.
func TestReadPersonCardMemoryFallsBackToThePageWithHerName(test *testing.T) {
	world := newPersonCardWorld(test)
	_, contactId := world.keepContact(test, world.ben, "Ada Lovelace", []string{"ada@example.org"}, world.ada.ID)
	var agentId string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		agentId = world.switchOnAgent(test, transaction, world.ben).ID
		if _, err := transaction.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "people/ada-lovelace", Kind: models.NodePerson, Name: "Ada Lovelace"}); err != nil {
			test.Fatal(err)
		}
		// Deeper down, a page with the same name is not the person page.
		if _, err := transaction.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "projects/history/ada-lovelace", Kind: models.NodePerson, Name: "Ada Lovelace"}); err != nil {
			test.Fatal(err)
		}
	})
	pages := sectionOf(world.cardOf(test, world.ben, world.ada), "memory")["pages"].([]any)
	if len(pages) != 1 || pages[0].(map[string]any)["nodePath"] != "people/ada-lovelace" || pages[0].(map[string]any)["isMatchedByName"] != true {
		test.Errorf("the page with her name: %v", pages)
	}

	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		node, err := transaction.FindAgentNodeByName(agentId, "people", models.NodePerson, "Ada Lovelace")
		if err != nil || node == nil {
			test.Fatal(node, err)
		}
		if err := transaction.SetAgentNodeContact(agentId, node.ID, contactId+"-gone"); err != nil {
			test.Fatal(err)
		}
	})
	if pages := sectionOf(world.cardOf(test, world.ben, world.ada), "memory")["pages"].([]any); len(pages) != 1 {
		test.Errorf("a page tied to a contact that is gone is still hers by name: %v", pages)
	}

	_, otherContactId := world.keepContact(test, world.ben, "Another Ada", []string{"another@example.org"}, "")
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		node, err := transaction.FindAgentNodeByName(agentId, "people", models.NodePerson, "Ada Lovelace")
		if err != nil || node == nil {
			test.Fatal(node, err)
		}
		if err := transaction.SetAgentNodeContact(agentId, node.ID, otherContactId); err != nil {
			test.Fatal(err)
		}
	})
	if pages := sectionOf(world.cardOf(test, world.ben, world.ada), "memory")["pages"].([]any); len(pages) != 0 {
		test.Errorf("a page tied to another contact is not hers: %v", pages)
	}
}

// The agent reads a card as the person gave it things: a contact only in an
// address book they granted it, mail only in a granted mailbox, and none of
// the health somebody else shares with the person.
func TestReadPersonCardAsTheAgentKeepsToWhatThePersonGaveIt(test *testing.T) {
	world := newPersonCardWorld(test)
	bookId, _ := world.keepContact(test, world.ben, "Ada", []string{"ada@example.org"}, world.ada.ID)
	world.run(test, world.ada, reportHealthQuery, map[string]any{"daySummaries": []any{
		daySummaryInput("stepCount", time.Now().UTC().Format("2006-01-02"), map[string]any{"quantityUnit": "count", "sumValue": 1234}),
	}})
	var mailboxId string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		world.switchOnAgent(test, transaction, world.ben)
		if _, err := transaction.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID, HealthCategories: []string{"activity"}}); err != nil {
			test.Fatal(err)
		}
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: world.ben.ID, Name: "Personal"})
		if err != nil {
			test.Fatal(err)
		}
		mailboxId = mailbox.ID
		folder, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || folder == nil {
			test.Fatal(folder, err)
		}
		mail, err := transaction.CreateMail(&models.Mail{From: "ada@example.org", Sender: "ada@example.org", Recipients: []string{"ben@example.net"},
			Subject: "hello", Kind: models.MailKindIncoming, MessageID: "<hello@example.net>", ReceivedAt: time.Now()}, nil)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			test.Fatal(err)
		}
	})
	operations := &agentOperations{graph: world.graph, user: world.ben, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{
		models.PermissionLocationUse, models.PermissionContactsUse, models.PermissionMailRead, models.PermissionPhotosUse,
		models.PermissionAgentUse, models.PermissionCircleUse, models.PermissionHealthUse,
	}}}
	ask := func() map[string]any {
		var found struct {
			ReadPersonCard map[string]any `json:"ReadPersonCard"`
		}
		if err := operations.Execute(test.Context(), fmt.Sprintf(`query { ReadPersonCard(userId: %q) {`+personCardFields+`} }`, world.ada.ID), nil, &found); err != nil {
			test.Fatal(err)
		}
		return found.ReadPersonCard
	}

	if state := stateOf(world.cardOf(test, world.ben, world.ada), "health"); state != "shown" {
		test.Fatalf("the person reads the health shared with them: %s", state)
	}
	card := ask()
	if card["contact"] != nil || stateOf(card, "mail") != "needsContact" || stateOf(card, "health") != "unavailable" {
		test.Errorf("a book not given to the agent, and health shared with the person: %v", card)
	}

	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		book, err := transaction.GetAddressBook(bookId)
		if err != nil {
			test.Fatal(err)
		}
		book.AgentGranted = true
		if _, err := transaction.UpdateAddressBook(book); err != nil {
			test.Fatal(err)
		}
	})
	card = ask()
	if card["contact"] == nil || stateOf(card, "mail") != "shown" || len(sectionOf(card, "mail")["mails"].([]any)) != 0 {
		test.Errorf("the book given, the mailbox not: %v", card)
	}

	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if _, err := transaction.UpdateMailbox(mailboxId, func(mailbox *models.Mailbox) error {
			mailbox.Agent = &models.AgentMailbox{Granted: true}
			return nil
		}); err != nil {
			test.Fatal(err)
		}
	})
	if mails := sectionOf(ask(), "mail")["mails"].([]any); len(mails) != 1 {
		test.Errorf("the mailbox given: %v", mails)
	}
}

// The item limit is 3 by default, at most 10, and the card says when a section
// has more.
func TestPersonCardItemLimit(test *testing.T) {
	for asked, want := range map[int]int{-1: 3, 0: 3, 1: 1, 7: 7, 10: 10, 11: 10, 500: 10} {
		if got := personCardItemLimitOf(asked); got != want {
			test.Errorf("itemLimit %d is %d, not %d", asked, got, want)
		}
	}
}

// A section that fails, panics or does not answer in time is reported alone:
// the read stops waiting at its time, and what it reads late is not used.
func TestAPersonCardSectionThatIsSlowOrBrokenFailsAlone(test *testing.T) {
	section, err := readPersonCardSection(context.Background(), time.Second, func(context.Context) (*string, error) {
		answer := "read"
		return &answer, nil
	})
	if err != nil || section == nil || *section != "read" {
		test.Fatalf("a section that answers: %v %v", section, err)
	}

	released := make(chan struct{})
	defer close(released)
	startedAt := time.Now()
	section, err = readPersonCardSection(context.Background(), 100*time.Millisecond, func(ctx context.Context) (*string, error) {
		// One that ignores its context, as a query that does not stop might.
		<-released
		late := "late"
		return &late, nil
	})
	if section != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(startedAt) > 2*time.Second {
		test.Errorf("a section out of time: %v %v after %s", section, err, time.Since(startedAt))
	}

	if section, err = readPersonCardSection(context.Background(), time.Second, func(context.Context) (*string, error) {
		return nil, errors.New("the query failed")
	}); section != nil || err == nil {
		test.Errorf("a section that fails: %v %v", section, err)
	}
	if section, err = readPersonCardSection(context.Background(), time.Second, func(context.Context) (*string, error) {
		panic("something broke")
	}); section != nil || err == nil || !strings.Contains(err.Error(), "something broke") {
		test.Errorf("a section that panics: %v %v", section, err)
	}
}

// Today is the caller's day, from midnight to midnight in their time zone,
// whatever the server's is.
func TestPersonCardDayIsTheCallersDay(test *testing.T) {
	now := time.Date(2026, time.March, 10, 23, 30, 0, 0, time.UTC)
	for name, expected := range map[string][2]string{
		"UTC":                {"2026-03-10T00:00:00Z", "2026-03-11T00:00:00Z"},
		"Pacific/Kiritimati": {"2026-03-11T00:00:00+14:00", "2026-03-12T00:00:00+14:00"},
		"Pacific/Pago_Pago":  {"2026-03-10T00:00:00-11:00", "2026-03-11T00:00:00-11:00"},
	} {
		location, err := time.LoadLocation(name)
		if err != nil {
			test.Fatal(err)
		}
		start, end := personCardDay(now, location)
		if start.Format(time.RFC3339) != expected[0] || end.Format(time.RFC3339) != expected[1] {
			test.Errorf("%s: %s to %s, want %v", name, start.Format(time.RFC3339), end.Format(time.RFC3339), expected)
		}
	}
}

// laterOf is the later of two times.
func laterOf(first, second time.Time) time.Time {
	if first.After(second) {
		return first
	}
	return second
}
