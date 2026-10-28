package apigraph

import (
	"fmt"
	"testing"
	"time"

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

// A calendar shared at busy is shown to the person it is shared with by the
// owner's name, the way the calendar itself is, because its title can say what
// is in it. From titles up the title is shown.
func TestListSharingHidesTheTitleOfACalendarSharedAtBusy(test *testing.T) {
	world := newSharingWorld(test)
	dbtest.Exec(test, world.database, fmt.Sprintf(`UPDATE "calendar" SET "name" = 'Private appointments' WHERE "id" = '%s'`, world.adaCalendar))
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")
	_, received := world.sharing(test, world.ben)
	if row := rowOfKind(test, received, "calendar"); row["subjectName"] != "Share-ada" || row["shareLevel"] != "busy" {
		test.Errorf("a calendar shared at busy is listed as %v", row)
	}
	// The owner sees their own title.
	made, _ := world.sharing(test, world.ada)
	if row := rowOfKind(test, made, "calendar"); row["subjectName"] != "Private appointments" {
		test.Errorf("ada's own row is %v", row)
	}
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "titles")
	_, received = world.sharing(test, world.ben)
	if row := rowOfKind(test, received, "calendar"); row["subjectName"] != "Private appointments" {
		test.Errorf("a calendar shared at titles is listed as %v", row)
	}
}

// A memory page and a skill reach only people who may use the agent, so a
// member of the circle who may not is not told of them, and nobody whose
// account is disabled is told of anything.
func TestAShareIsNotToldToAMemberWhoCannotUseItOrWhoIsDisabled(test *testing.T) {
	world := newSharingWorld(test)
	// Dave may use circles and calendars but not the agent, and his agent is on.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		person, err := tx.EnsureAgent(world.dave.ID)
		if err != nil {
			test.Fatalf("EnsureAgent: %s", err)
		}
		if _, err := tx.UpdateAgent(person.ID, func(agent *models.Agent) error {
			agent.Enabled = true
			return nil
		}); err != nil {
			test.Fatalf("UpdateAgent: %s", err)
		}
	})
	world.join(test, world.ada, world.home, world.dave)
	world.join(test, world.ada, world.home, world.cora)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/kim", granteeCircleId: %q) { id } }`, world.home))
	if got := world.notesOf(test, world.dave, models.NoteShareReceived); len(got) != 0 {
		test.Errorf("dave, who may not use the agent, was told %q", got)
	}
	for _, user := range []*models.User{world.ben, world.cora} {
		if got := world.notesOf(test, user, models.NoteShareReceived); fmt.Sprint(got) != "[Share-ada shared the memory page Kim with you]" {
			test.Errorf("%s was told %q", user.Username, got)
		}
	}

	// Ben's account is disabled: the next share to the circle tells cora only.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateUser(world.ben.ID, func(current *models.User) error {
			current.DisabledAt = new(time.Now())
			return nil
		}); err != nil {
			test.Fatalf("UpdateUser: %s", err)
		}
	})
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeCircleId: %q, shareLevel: "current") { id } }`, world.home))
	if got := world.notesOf(test, world.ben, models.NoteShareReceived); len(got) != 1 {
		test.Errorf("ben, whose account is disabled, was told %q", got)
	}
	if got := world.notesOf(test, world.cora, models.NoteShareReceived); len(got) != 2 {
		test.Errorf("cora was told %q", got)
	}
}

// Nobody is told that a member joined when the account was in the circle
// already: not when an operator puts in an account that is in, and not when an
// invitation is answered by an account that is in by now.
func TestOwnersAreNotToldAgainWhenAMemberWasInTheCircleAlready(test *testing.T) {
	world := newSharingWorld(test)
	world.share(test, world.ada, world.adaCalendar, fmt.Sprintf(`granteeCircleId: %q`, world.home), "busy")
	add := func(user *models.User) string {
		return fmt.Sprintf(`mutation { AddCircleMember(circleId: %q, username: %q, memberRole: "member") { id } }`, world.home, user.Username)
	}
	reached := func() int { return len(world.notesOf(test, world.ada, models.NoteShareReached)) }

	world.call(test, world.manager, add(world.cora))
	if reached() != 1 {
		test.Fatalf("ada was told %q when cora was put in", world.notesOf(test, world.ada, models.NoteShareReached))
	}
	world.call(test, world.manager, add(world.cora))
	if reached() != 1 {
		test.Errorf("ada was told %q when cora was put in again", world.notesOf(test, world.ada, models.NoteShareReached))
	}

	// An invitation that is still open when the account is in the circle,
	// which an invitation answered at the same moment as another way in leaves.
	world.call(test, world.ada, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, world.home, world.dave.Username))
	dbtest.Exec(test, world.database, fmt.Sprintf(
		`INSERT INTO "circle_member" ("circle_id", "user_id", "member_role", "joined_at") VALUES ('%s', '%s', 'member', now())`, world.home, world.dave.ID))
	invitations := world.call(test, world.dave, `query { ListCircleInvitations { id circleId } }`)["ListCircleInvitations"].([]any)
	if len(invitations) != 1 {
		test.Fatalf("dave has the invitations %v", invitations)
	}
	world.call(test, world.dave, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`,
		invitations[0].(map[string]any)["id"]))
	if reached() != 1 {
		test.Errorf("ada was told %q when dave answered an invitation to a circle he was in", world.notesOf(test, world.ada, models.NoteShareReached))
	}
}

// A person is told of a share once an interval, counted in the database so that
// the instance a request reaches does not matter: the turn is a row, a notice
// inside the interval is not told, one after it is, and another share or
// another person has a turn of its own.
func TestAShareNoticeIsToldOncePerIntervalAcrossInstances(test *testing.T) {
	world := newSharingWorld(test)
	benNotes := func() []string { return world.notesOf(test, world.ben, models.NoteShareReceived) }
	throttleCount := func() string {
		return dbtest.QueryString(test, world.database, `SELECT count(*) FROM throttle WHERE throttle_key LIKE 'share-notice:%'`)
	}

	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "titles")
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "details")
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "details")
	if got := benNotes(); len(got) != 2 {
		test.Fatalf("a widening back to a level told within the interval told ben %q", got)
	}
	if got := throttleCount(); got != "2" {
		test.Fatalf("%s turns were taken for two notices", got)
	}

	// Another instance has nothing in memory to tell the difference by; only
	// time moves the turn, here by the turns having been taken long ago.
	dbtest.Exec(test, world.database, `UPDATE throttle SET taken_at = taken_at - interval '11 minutes'`)
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")
	world.share(test, world.ada, world.adaCalendar, userGrantee(world.ben), "details")
	if got := benNotes(); len(got) != 3 || got[2] != "Share-ada shared their calendar with you (details)" {
		test.Fatalf("a widening after the interval told ben %q", got)
	}
}

// What a person shared is listed after they lose the permission to use that
// kind: the shares are dormant for their readers, and the owner still sees what
// is shared in their name.
func TestListSharingKeepsWhatWasSharedAfterThePermissionIsLost(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		role, err := tx.GetRoleByName("Sharing role of " + world.ada.Username)
		if err != nil || role == nil {
			test.Fatalf("GetRoleByName: %v %v", role, err)
		}
		if _, err := tx.UpdateRole(role.ID, func(role *models.Role) error {
			role.Permissions = []models.Permission{models.PermissionAgentUse, models.PermissionCircleUse}
			return nil
		}); err != nil {
			test.Fatalf("UpdateRole: %s", err)
		}
	})
	made, _ := world.sharing(test, world.ada)
	if fmt.Sprint(kindsOf(made)) != "[location]" {
		test.Errorf("ada shares %v", kindsOf(made))
	}
	// Ben reads nothing of it while it is dormant, and Ben's own list is
	// unaffected by what ada may use.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		shares, err := tx.ListLocationSharesReaching(world.ben.ID)
		if err != nil {
			test.Fatalf("ListLocationSharesReaching: %s", err)
		}
		if len(shares) != 0 {
			test.Errorf("a dormant share reaches ben: %v", shares)
		}
	})
}

// A share left by a page that was deleted before shares were ended with it is
// not shown to the person it was for, and its owner is told the page is gone
// and can stop it.
func TestListSharingMarksASharedPageThatIsGone(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/kim", granteeUserId: %q) { id } }`, world.ben.ID))
	dbtest.Exec(test, world.database, `DELETE FROM "agent_node" WHERE "path" = 'people/kim'`)

	if _, received := world.sharing(test, world.ben); len(received) != 0 {
		test.Errorf("ben is shown %v", received)
	}
	view := world.run(test, world.ada, `query { ListSharing { sharesMade { shareKind shareId subjectId subjectName isSubjectDeleted } } }`,
		nil)["ListSharing"].(map[string]any)
	madeRows := view["sharesMade"].([]any)
	if len(madeRows) != 1 {
		test.Fatalf("ada shares %v", madeRows)
	}
	row := madeRows[0].(map[string]any)
	if row["isSubjectDeleted"] != true || row["subjectId"] != "people/kim" {
		test.Fatalf("the row of the page that is gone is %v", row)
	}
	world.call(test, world.ada, fmt.Sprintf(`mutation { UnshareAgentPage(shareId: %q) }`, row["shareId"]))
	if made, _ := world.sharing(test, world.ada); len(made) != 0 {
		test.Errorf("ada still shares %v", kindsOf(made))
	}
}
