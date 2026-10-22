package apigraph

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// sharingWorld is a passwordsWorld in which ada, ben and cora also keep an
// agent that is on, and may use location, photos and the agent; ada and ben
// may use Health too. Ada and ben share the circle home; cora is in none of
// theirs, and may not use Health.
// Ada has a page of memory, an album and a skill to share.
type sharingWorld struct {
	*passwordsWorld
	albumID string
}

func newSharingWorld(test *testing.T) *sharingWorld {
	test.Helper()
	world := &sharingWorld{passwordsWorld: newPasswordsWorld(test)}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		for _, user := range []*models.User{world.ada, world.ben, world.cora} {
			permissions := []models.Permission{models.PermissionLocationUse, models.PermissionPhotosUse, models.PermissionAgentUse}
			if user != world.cora {
				permissions = append(permissions, models.PermissionHealthUse)
			}
			role, err := tx.CreateRole(&models.Role{Name: "Sharing role of " + user.Username, Permissions: permissions})
			if err != nil {
				test.Fatalf("CreateRole: %s", err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: "Sharing group of " + user.Username, UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				test.Fatalf("CreateGroup: %s", err)
			}
			// Everybody who can use circles has an agent row already, off until
			// they turn it on.
			person, err := tx.EnsureAgent(user.ID)
			if err != nil {
				test.Fatalf("EnsureAgent: %s", err)
			}
			if _, err := tx.UpdateAgent(person.ID, func(agent *models.Agent) error {
				agent.Enabled = true
				return nil
			}); err != nil {
				test.Fatalf("UpdateAgent: %s", err)
			}
		}
		adaAgent, err := tx.GetAgentByUser(world.ada.ID)
		if err != nil || adaAgent == nil {
			test.Fatalf("GetAgentByUser: %v %v", adaAgent, err)
		}
		if err := tx.EnsureAgentRoots(adaAgent.ID); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: adaAgent.ID, Path: "people/kim", Kind: models.NodeTopic, Name: "Kim", Summary: "Likes boats."}); err != nil {
			test.Fatalf("PutAgentNode: %s", err)
		}
		album, err := tx.CreatePhotoAlbum(world.ada.ID, "Boat trip", nil)
		if err != nil {
			test.Fatalf("CreatePhotoAlbum: %s", err)
		}
		world.albumID = album.ID
	})
	return world
}

// notesOf are the notes in the main conversation of a person's agent, as
// kind and detail.
func (self *sharingWorld) notesOf(test *testing.T, user *models.User, noteKind models.AgentNoteKind) []string {
	test.Helper()
	var details []string
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		person, err := tx.GetAgentByUser(user.ID)
		if err != nil || person == nil {
			test.Fatalf("GetAgentByUser: %v %v", person, err)
		}
		main, err := tx.MainAgentConversation(person.ID, "drawer")
		if err != nil {
			test.Fatal(err)
		}
		messages, err := tx.ListAgentMessages(main.ID, nil)
		if err != nil {
			test.Fatal(err)
		}
		for _, message := range messages {
			if message.Role == models.AgentMessageNote && message.Name == string(noteKind) {
				details = append(details, models.NoteText(noteKind, message.Content))
			}
		}
	})
	return details
}

const listSharingQuery = `query { ListSharing {
  sharesMade { shareKind shareId subjectId subjectName shareLevel shareScopes passwordItemCount granteeUserId granteeUsername granteeCircleId granteeCircleName }
  sharesReceived { shareKind shareId subjectId subjectName shareLevel shareScopes passwordItemCount ownerUserId ownerUsername ownerName granteeCircleId granteeCircleName isDirect canDecline }
} }`

// sharing is the lists ListSharing answers a person, as rows keyed by kind.
func (self *sharingWorld) sharing(test *testing.T, user *models.User) (made, received []map[string]any) {
	test.Helper()
	view := self.run(test, user, listSharingQuery, nil)["ListSharing"].(map[string]any)
	for _, each := range view["sharesMade"].([]any) {
		made = append(made, each.(map[string]any))
	}
	for _, each := range view["sharesReceived"].([]any) {
		received = append(received, each.(map[string]any))
	}
	return made, received
}

func kindsOf(rows []map[string]any) []string {
	kinds := []string{}
	for _, row := range rows {
		kinds = append(kinds, row["shareKind"].(string))
	}
	return kinds
}

func rowOfKind(test *testing.T, rows []map[string]any, shareKind string) map[string]any {
	test.Helper()
	for _, row := range rows {
		if row["shareKind"] == shareKind {
			return row
		}
	}
	test.Fatalf("no %s row among %v", shareKind, kindsOf(rows))
	return nil
}

// shareEverything has ada share one thing of each kind with ben by name, and
// her location with the circle home.
func (self *sharingWorld) shareEverything(test *testing.T) {
	test.Helper()
	self.share(test, self.ada, self.adaCalendar, userGrantee(self.ben), "titles")
	self.call(test, self.ada, fmt.Sprintf(`mutation { ShareLocation(granteeCircleId: %q, shareLevel: "current") { id } }`, self.home))
	self.call(test, self.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: ["activity", "sleep"]) { id } }`, self.ben.ID))
	self.call(test, self.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/kim", granteeUserId: %q, isQuoteShared: true) { id } }`, self.ben.ID))
	self.call(test, self.ada, fmt.Sprintf(`mutation { SharePhotoAlbum(photoAlbumId: %q, granteeUserId: %q, shareLevel: "view", isLocationIncluded: true) { id } }`,
		self.albumID, self.ben.ID))
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		if _, err := tx.ShareSkill(self.ada.ID, &models.SkillShare{SkillName: "groceries", ToolNames: []string{"order"}, GranteeUserID: self.ben.ID}); err != nil {
			test.Fatalf("ShareSkill: %s", err)
		}
	})
	ada, ben := newPasswordsDevice(test, self.ada), newPasswordsDevice(test, self.ben)
	self.setUp(test, ada)
	self.setUp(test, ben)
	itemId := self.save(test, ada, "login")["id"].(string)
	self.run(test, self.ada, setPasswordItemSharesQuery, sharesInput(itemId, []any{map[string]any{"granteeUserId": self.ben.ID}}, []any{map[string]any{
		"itemKey": sealedOf("item key of " + itemId), "recipientUserId": self.ben.ID,
		"itemKeyByRecipientKey": sealedForKeyPairOf("item key for " + self.ben.Username), "recipientPublicKey": ben.publicKey}}))
}

// Every kind appears on both sides: what ada shares in her list, what ben
// receives in his, with who it came from and whether it came through a circle
// or by name. A person who shares and receives nothing sees two empty lists.
func TestListSharingShowsEveryKindOnBothSides(test *testing.T) {
	world := newSharingWorld(test)
	world.shareEverything(test)

	made, _ := world.sharing(test, world.ada)
	wantKinds := []string{"calendar", "location", "health", "memory_page", "photo_album", "skill", "passwords"}
	if got := kindsOf(made); fmt.Sprint(got) != fmt.Sprint(wantKinds) {
		test.Fatalf("ada shares %v, want %v", got, wantKinds)
	}
	if row := rowOfKind(test, made, "calendar"); row["shareLevel"] != "titles" || row["granteeUsername"] != world.ben.Username || row["subjectId"] != world.adaCalendar {
		test.Errorf("the calendar row is %v", row)
	}
	if row := rowOfKind(test, made, "location"); row["shareLevel"] != "current" || row["granteeCircleName"] != "Home" || row["granteeUserId"] != nil {
		test.Errorf("the location row is %v", row)
	}
	if row := rowOfKind(test, made, "health"); fmt.Sprint(row["shareScopes"]) != "[activity sleep]" {
		test.Errorf("the Health row is %v", row)
	}
	if row := rowOfKind(test, made, "memory_page"); row["subjectId"] != "people/kim" || row["subjectName"] != "Kim" || fmt.Sprint(row["shareScopes"]) != "[quotes]" {
		test.Errorf("the page row is %v", row)
	}
	if row := rowOfKind(test, made, "photo_album"); row["subjectName"] != "Boat trip" || row["shareLevel"] != "view" || fmt.Sprint(row["shareScopes"]) != "[location]" {
		test.Errorf("the album row is %v", row)
	}
	if row := rowOfKind(test, made, "skill"); row["subjectName"] != "groceries" || fmt.Sprint(row["shareScopes"]) != "[order]" {
		test.Errorf("the skill row is %v", row)
	}
	if row := rowOfKind(test, made, "passwords"); row["passwordItemCount"] != float64(1) || row["shareId"] != "" || row["granteeUsername"] != world.ben.Username {
		test.Errorf("the passwords row is %v", row)
	}

	_, received := world.sharing(test, world.ben)
	if got := kindsOf(received); fmt.Sprint(got) != fmt.Sprint(wantKinds) {
		test.Fatalf("ben receives %v, want %v", got, wantKinds)
	}
	for _, row := range received {
		if row["ownerUsername"] != world.ada.Username || row["ownerName"] != "Share-ada" {
			test.Errorf("a %s row says it is from %v (%v)", row["shareKind"], row["ownerUsername"], row["ownerName"])
		}
		isCircle := row["shareKind"] == "location"
		if isCircle == row["isDirect"].(bool) || (isCircle && row["granteeCircleName"] != "Home") {
			test.Errorf("a %s row is direct %v through %v", row["shareKind"], row["isDirect"], row["granteeCircleName"])
		}
		// Only what was made to ben by name can be declined, and not passwords.
		if canDecline := !isCircle && row["shareKind"] != "passwords"; row["canDecline"] != canDecline {
			test.Errorf("a %s row says canDecline %v", row["shareKind"], row["canDecline"])
		}
	}
	if row := rowOfKind(test, received, "calendar"); row["shareLevel"] != "titles" || row["subjectId"] != world.adaCalendar {
		test.Errorf("ben's calendar row is %v", row)
	}
	if row := rowOfKind(test, received, "photo_album"); row["subjectName"] != "Boat trip" || row["subjectId"] != world.albumID {
		test.Errorf("ben's album row is %v", row)
	}
	if row := rowOfKind(test, received, "passwords"); row["passwordItemCount"] != float64(1) {
		test.Errorf("ben's passwords row is %v", row)
	}

	for _, stranger := range []*models.User{world.cora, world.dave} {
		made, received := world.sharing(test, stranger)
		if len(made) != 0 || len(received) != 0 {
			test.Errorf("%s shares %v and receives %v", stranger.Username, made, received)
		}
	}
}

// What a person may not use is left out of their lists, and what reaches them
// only while they share a circle with the owner stops being listed when they
// leave it.
func TestListSharingLeavesOutWhatThePersonMayNotUseOrNoLongerReaches(test *testing.T) {
	world := newSharingWorld(test)
	world.shareEverything(test)
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if _, err := tx.RemoveCircleMember(world.home, world.ben.ID); err != nil {
			test.Fatalf("RemoveCircleMember: %s", err)
		}
	})
	if _, received := world.sharing(test, world.ben); len(received) != 0 {
		test.Errorf("after leaving, ben still receives %v", kindsOf(received))
	}
	// The direct shares were ended with the circle that made them possible;
	// the share to the circle is still ada's, for whoever is in it.
	if made, _ := world.sharing(test, world.ada); fmt.Sprint(kindsOf(made)) != "[location]" {
		test.Errorf("ada shares %v", kindsOf(made))
	}
}

// A person is told when something is shared with them, or widened for them,
// and the sharer and people it does not reach are not.
func TestASharedThingIsToldToWhoItReachesAndNotToTheSharer(test *testing.T) {
	world := newSharingWorld(test)
	benNotes := func() []string { return world.notesOf(test, world.ben, models.NoteShareReceived) }

	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "titles")
	if got := benNotes(); fmt.Sprint(got) != "[Share-ada shared their calendar with you (titles)]" {
		test.Fatalf("ben was told %q", got)
	}
	// The same share again, and a narrower one, tell nobody.
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "titles")
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")
	if got := benNotes(); len(got) != 1 {
		test.Fatalf("a share that gave nothing more told ben %q", got)
	}
	// Widening does, and so does a change back to a level he was told of
	// already, but not within ten minutes.
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "details")
	if got := benNotes(); len(got) != 2 || got[1] != "Share-ada shared their calendar with you (details)" {
		test.Fatalf("widening told ben %q", got)
	}
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "details")
	if got := benNotes(); len(got) != 2 {
		test.Fatalf("flipping back and forth told ben again: %q", got)
	}

	// The sharer is told nothing of their own shares, and neither is someone
	// the share does not reach: cora has an agent but shares no circle with ada.
	for _, user := range []*models.User{world.ada, world.cora} {
		if got := world.notesOf(test, user, models.NoteShareReceived); len(got) != 0 {
			test.Errorf("%s was told %q", user.Username, got)
		}
	}

	// A share to the circle tells its other members, never the sharer.
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeCircleId: %q, shareLevel: "history") { id } }`, world.home))
	if got := benNotes(); len(got) != 3 || got[2] != "Share-ada shared their location with you (where they are and where they have been)" {
		test.Fatalf("a circle share told ben %q", got)
	}
	if got := world.notesOf(test, world.ada, models.NoteShareReceived); len(got) != 0 {
		test.Errorf("ada was told %q of her own shares", got)
	}

	// Each of the other kinds tells too: Health, a page, an album and a skill.
	world.call(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: ["activity"]) { id } }`, world.ben.ID))
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/kim", granteeUserId: %q) { id } }`, world.ben.ID))
	world.call(test, world.ada, fmt.Sprintf(`mutation { SharePhotoAlbum(photoAlbumId: %q, granteeUserId: %q, shareLevel: "view") { id } }`, world.albumID, world.ben.ID))
	got := benNotes()
	if len(got) != 6 {
		test.Fatalf("ben was told %q", got)
	}
	for index, want := range []string{
		"Share-ada shared their Health with you",
		"Share-ada shared the memory page Kim with you",
		"Share-ada shared the photo album Boat trip with you (you can view it)",
	} {
		if got[3+index] != want {
			test.Errorf("note %d is %q, want %q", 3+index, got[3+index], want)
		}
	}
	// Sharing the quotes of a page already shared, or adding a category to
	// Health already shared, is wider; sharing less is not.
	world.call(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: ["sleep"]) { id } }`, world.ben.ID))
	world.call(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: ["sleep"]) { id } }`, world.ben.ID))
	if got := benNotes(); len(got) != 7 || got[6] != "Share-ada shared their Health with you" {
		test.Errorf("a new category told ben %q", got)
	}
}

// Updating an album's share to a higher level tells the person, as sharing it
// at that level would.
func TestWideningAnAlbumShareIsToldToo(test *testing.T) {
	world := newSharingWorld(test)
	shared := world.call(test, world.ada, fmt.Sprintf(`mutation { SharePhotoAlbum(photoAlbumId: %q, granteeUserId: %q, shareLevel: "view") { id } }`,
		world.albumID, world.ben.ID))["SharePhotoAlbum"].(map[string]any)
	world.call(test, world.ada, fmt.Sprintf(`mutation { UpdatePhotoAlbumShare(photoAlbumShareId: %q, shareLevel: "contribute") { id } }`, shared["id"]))
	got := world.notesOf(test, world.ben, models.NoteShareReceived)
	if len(got) != 2 || got[1] != "Share-ada shared the photo album Boat trip with you (you can add photos)" {
		test.Fatalf("ben was told %q", got)
	}
}

// When a person joins a circle, each member who shared with it is told once,
// with the kinds that person can use.
func TestOwnersAreToldWhenSomebodyJoinsACircleTheirSharesReach(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeCircleId: %q, shareLevel: "current") { id } }`, world.home))
	world.share(test, world.ada, world.adaCalendar, fmt.Sprintf(`granteeCircleId: %q`, world.home), "busy")
	world.call(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeCircleId: %q, healthCategories: ["activity"]) { id } }`, world.home))
	// Ben has shared nothing with the circle, and one direct share does not
	// count.
	for _, each := range world.call(test, world.ben, `query { ListCalendars { id calendarKind shareLevel } }`)["ListCalendars"].([]any) {
		if calendar := each.(map[string]any); calendar["calendarKind"] == "events" && calendar["shareLevel"] == "owner" {
			world.share(test, world.ben, calendar["id"].(string), userGrantee(world.ada), "titles")
		}
	}

	world.join(test, world.ada, world.home, world.cora)

	// Cora may not use Health, so it is not said that she can see it.
	got := world.notesOf(test, world.ada, models.NoteShareReached)
	if fmt.Sprint(got) != "[Share-cora joined Home and can now see your calendar and location]" {
		test.Fatalf("ada was told %q", got)
	}
	if got := world.notesOf(test, world.ben, models.NoteShareReached); len(got) != 0 {
		test.Errorf("ben, who shared nothing with the circle, was told %q", got)
	}
	if got := world.notesOf(test, world.cora, models.NoteShareReached); len(got) != 0 {
		test.Errorf("the person who joined was told %q", got)
	}
}

// Only the person a share was made to can decline it, and only a share made
// by name; the owner is not told.
func TestOnlyTheGranteeCanDeclineAShareMadeByName(test *testing.T) {
	world := newSharingWorld(test)
	world.shareEverything(test)
	_, received := world.sharing(test, world.ben)

	declineQuery := func(row map[string]any) string {
		return fmt.Sprintf(`mutation { DeclineShare(shareKind: %q, shareId: %q) }`, row["shareKind"], row["shareId"])
	}
	// Somebody else, and the owner, cannot decline it.
	calendarRow := rowOfKind(test, received, "calendar")
	for _, user := range []*models.User{world.cora, world.ada} {
		world.refused(test, user, declineQuery(calendarRow))
	}
	// A share to a circle cannot be declined: it ends when ben leaves.
	world.refused(test, world.ben, declineQuery(rowOfKind(test, received, "location")))
	// Passwords are removed by their owner.
	if message := world.refused(test, world.ben, `mutation { DeclineShare(shareKind: "passwords", shareId: "") }`); message == "" {
		test.Error("declining passwords was refused with no words")
	}
	world.refused(test, world.ben, `mutation { DeclineShare(shareKind: "mail", shareId: "x") }`)

	for _, shareKind := range []string{"calendar", "health", "memory_page", "photo_album", "skill"} {
		world.call(test, world.ben, declineQuery(rowOfKind(test, received, shareKind)))
	}
	made, _ := world.sharing(test, world.ada)
	if fmt.Sprint(kindsOf(made)) != "[location passwords]" {
		test.Errorf("ada still shares %v", kindsOf(made))
	}
	if _, received := world.sharing(test, world.ben); fmt.Sprint(kindsOf(received)) != "[location passwords]" {
		test.Errorf("ben still receives %v", kindsOf(received))
	}
	// Declined once, it is gone for a second try.
	world.refused(test, world.ben, declineQuery(calendarRow))

	// A location shared with ben by name, beside the one to the circle, is
	// declined alone: the circle's share still reaches him.
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	var directLocation map[string]any
	_, received = world.sharing(test, world.ben)
	for _, row := range received {
		if row["shareKind"] == "location" && row["isDirect"] == true {
			directLocation = row
		}
	}
	if directLocation == nil {
		test.Fatalf("ben receives no location by name: %v", received)
	}
	world.call(test, world.ben, declineQuery(directLocation))
	_, received = world.sharing(test, world.ben)
	if fmt.Sprint(kindsOf(received)) != "[location passwords]" || received[0]["isDirect"] != false {
		test.Errorf("after declining the location by name ben receives %v", received)
	}
	if made, _ := world.sharing(test, world.ada); fmt.Sprint(kindsOf(made)) != "[location passwords]" {
		test.Errorf("ada shares %v", kindsOf(made))
	}
	// Ben can still see what the circle gives him, and cannot read the
	// calendar that was declined.
	if data, failures := world.ask(test, world.ben, fmt.Sprintf(`query { ListCalendarShares(calendarId: %q) { id } }`, world.adaCalendar)); len(failures) == 0 {
		test.Errorf("ben listed the shares of a calendar he declined: %v", data)
	}
}

// Declining a share of an album takes out the photos the person added to it.
func TestDecliningAnAlbumShareTakesOutThePhotosTheGranteeAdded(test *testing.T) {
	world := newSharingWorld(test)
	shared := world.call(test, world.ada, fmt.Sprintf(`mutation { SharePhotoAlbum(photoAlbumId: %q, granteeUserId: %q, shareLevel: "contribute") { id } }`,
		world.albumID, world.ben.ID))["SharePhotoAlbum"].(map[string]any)
	world.call(test, world.ben, fmt.Sprintf(`mutation { DeclineShare(shareKind: "photo_album", shareId: %q) }`, shared["id"]))
	if _, received := world.sharing(test, world.ben); len(received) != 0 {
		test.Errorf("ben still receives %v", kindsOf(received))
	}
	if data, failures := world.ask(test, world.ben, fmt.Sprintf(`query { ListPhotos(albumId: %q) { id } }`, world.albumID)); len(failures) == 0 {
		test.Errorf("ben still reads the album: %v", data)
	}
}
