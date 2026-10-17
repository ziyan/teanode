package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// calendarShareWorld is a server with four people who can use circles and
// calendars: ada and ben in one circle, cora in none of ada's, and dave in a
// circle of his own.
type calendarShareWorld struct {
	*avatarWorld
	ada, ben, cora, dave *models.User
	home                 string
	adaCalendar          string
}

func newCalendarShareWorld(t *testing.T) *calendarShareWorld {
	t.Helper()
	world := &calendarShareWorld{avatarWorld: newAvatarWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		create := func(username string) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username, Name: strings.ToUpper(username[:1]) + username[1:]})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: []models.Permission{
				models.PermissionCircleUse, models.PermissionCalendarUse}})
			if err != nil {
				t.Fatalf("CreateRole: %s", err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
			return user
		}
		world.ada, world.ben, world.cora, world.dave = create("share-ada"), create("share-ben"), create("share-cora"), create("share-dave")
	})
	world.home = world.makeCircle(t, world.ada, "Home")
	world.join(t, world.ada, world.home, world.ben)
	world.makeCircle(t, world.dave, "Dave's")
	// ada's calendar, as the server makes it the first time she looks.
	data := world.call(t, world.ada, `query { ListCalendars { id calendarKind shareLevel } }`)
	for _, each := range data["ListCalendars"].([]any) {
		listed := each.(map[string]any)
		if listed["calendarKind"] == "events" {
			world.adaCalendar = listed["id"].(string)
		}
	}
	if world.adaCalendar == "" {
		t.Fatal("ada has no calendar")
	}
	return world
}

func (self *calendarShareWorld) call(t *testing.T, user *models.User, query string) map[string]any {
	t.Helper()
	data, failures := self.ask(t, user, query)
	if len(failures) > 0 {
		t.Fatalf("%s: %v\n%s", user.Username, failures, query)
	}
	return data
}

// refused returns what a refusal said, and fails when there was none.
func (self *calendarShareWorld) refused(t *testing.T, user *models.User, query string) string {
	t.Helper()
	data, failures := self.ask(t, user, query)
	if len(failures) == 0 {
		t.Fatalf("%s was not refused: %v\n%s", user.Username, data, query)
	}
	return strings.Join(failures, "; ")
}

func (self *calendarShareWorld) makeCircle(t *testing.T, owner *models.User, name string) string {
	t.Helper()
	data := self.call(t, owner, fmt.Sprintf(`mutation { CreateCircle(name: %q, circleKind: "family") { id } }`, name))
	return data["CreateCircle"].(map[string]any)["id"].(string)
}

func (self *calendarShareWorld) join(t *testing.T, admin *models.User, circleId string, invited *models.User) {
	t.Helper()
	self.call(t, admin, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, circleId, invited.Username))
	data := self.call(t, invited, `query { ListCircleInvitations { id circleId } }`)
	for _, each := range data["ListCircleInvitations"].([]any) {
		invitation := each.(map[string]any)
		if invitation["circleId"] == circleId {
			self.call(t, invited, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`, invitation["id"]))
			return
		}
	}
	t.Fatalf("%s has no invitation", invited.Username)
}

func (self *calendarShareWorld) share(t *testing.T, owner *models.User, calendarId string, grantee string, shareLevel string) map[string]any {
	t.Helper()
	data := self.call(t, owner, fmt.Sprintf(`mutation { ShareCalendar(calendarId: %q, %s, shareLevel: %q) { id shareLevel granteeUsername granteeCircleName } }`,
		calendarId, grantee, shareLevel))
	return data["ShareCalendar"].(map[string]any)
}

func userGrantee(user *models.User) string { return fmt.Sprintf(`granteeUserId: %q`, user.ID) }

// putEvent keeps a file in a calendar without going through the API, the way
// a phone's PUT would.
func (self *calendarShareWorld) putEvent(t *testing.T, calendarId, data string) string {
	t.Helper()
	parsed, err := calendar.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	occurrences, indexedUntil, err := calendar.Indexed(parsed)
	if err != nil {
		t.Fatalf("Indexed: %s", err)
	}
	rows := make([]models.Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		rows = append(rows, models.Occurrence{StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay})
	}
	var id string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		kept, err := tx.PutCalendarObject(&models.CalendarObject{
			ID: parsed.UID + ".ics", CalendarID: calendarId, UID: parsed.UID, ETag: calendar.ETag(parsed.Data), Data: string(parsed.Data),
			Summary: parsed.Summary, Location: parsed.Location, StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt,
			Recurring: parsed.Recurring, Status: parsed.Status, IndexedUntil: &indexedUntil,
		}, rows)
		if err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}
		id = kept.ID
	})
	return id
}

func event(uid, summary, starts, ends string, extra ...string) string {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN", "BEGIN:VEVENT",
		"UID:" + uid, "DTSTAMP:20300101T000000Z", "DTSTART:" + starts, "DTEND:" + ends, "SUMMARY:" + summary}
	lines = append(lines, extra...)
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	return strings.Join(lines, "\r\n") + "\r\n"
}

const window2030 = `from: "2030-01-01T00:00:00Z", until: "2030-01-08T00:00:00Z"`

func listed(t *testing.T, data map[string]any, operation string) []map[string]any {
	t.Helper()
	events := []map[string]any{}
	for _, each := range data[operation].([]any) {
		events = append(events, each.(map[string]any))
	}
	return events
}

// A calendar shared at titles shows the title and place and not the notes or
// the guests, a private event is busy, and the owner sees all of it.
func TestACalendarSharedAtTitlesShowsTitlesOnly(t *testing.T) {
	world := newCalendarShareWorld(t)
	world.putEvent(t, world.adaCalendar, event("dentist", "Dentist", "20300102T100000Z", "20300102T110000Z",
		"LOCATION:Elm Street", "DESCRIPTION:Bring the form", "ORGANIZER:mailto:ada@example.com", "ATTENDEE:mailto:guest@example.net",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT15M", "DESCRIPTION:Dentist", "END:VALARM"))
	world.putEvent(t, world.adaCalendar, event("secret", "Surprise party", "20300103T180000Z", "20300103T200000Z",
		"CLASS:PRIVATE", "LOCATION:Barn"))

	// Nothing is shared yet: ben does not even know the calendar is there.
	benList := world.call(t, world.ben, `query { ListCalendars { id shareLevel } }`)
	for _, each := range listed(t, benList, "ListCalendars") {
		if each["id"] == world.adaCalendar {
			t.Fatalf("an unshared calendar is listed for ben: %v", each)
		}
	}
	listQuery := fmt.Sprintf(`query { ListCalendarEvents(calendarId: %q, %s) { id summary location description isBusyOnly attendees { address } startsAt } }`, world.adaCalendar, window2030)
	world.refused(t, world.ben, listQuery)

	shared := world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "titles")
	if shared["shareLevel"] != "titles" || shared["granteeUsername"] != world.ben.Username {
		t.Fatalf("the share: %v", shared)
	}

	// ben's list has it, marked with whose it is and the level.
	var seen map[string]any
	for _, each := range listed(t, world.call(t, world.ben, `query { ListCalendars { id name shareLevel ownerUserId ownerName circleId isCircleCalendar colorIndex agentGranted events } }`), "ListCalendars") {
		if each["id"] == world.adaCalendar {
			seen = each
		}
	}
	if seen == nil || seen["shareLevel"] != "titles" || seen["ownerUserId"] != world.ada.ID || seen["ownerName"] != "Share-ada" ||
		seen["circleId"] != nil || seen["isCircleCalendar"] != false || seen["agentGranted"] != false || seen["events"] != float64(2) {
		t.Fatalf("ben sees the calendar as %v", seen)
	}

	events := listed(t, world.call(t, world.ben, listQuery), "ListCalendarEvents")
	if len(events) != 2 {
		t.Fatalf("ben sees %d events: %v", len(events), events)
	}
	for _, each := range events {
		switch each["summary"] {
		case "Dentist":
			if each["location"] != "Elm Street" || each["description"] != nil && each["description"] != "" || each["isBusyOnly"] != false ||
				len(each["attendees"].([]any)) != 0 {
				t.Errorf("titles are the title and the place: %v", each)
			}
		case "":
			// The private one: busy, with nothing else.
			if each["isBusyOnly"] != true || each["location"] != nil && each["location"] != "" {
				t.Errorf("a private event is busy and nothing else: %v", each)
			}
		default:
			t.Errorf("an event ben should not read: %v", each)
		}
	}
	// And one event fetched whole is redacted the same way.
	data := world.call(t, world.ben, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: "dentist.ics") { summary description file organizer attendees { address } } }`, world.adaCalendar))
	fetched := data["GetCalendarEvent"].(map[string]any)
	if fetched["summary"] != "Dentist" || fetched["description"] != nil && fetched["description"] != "" ||
		strings.Contains(fmt.Sprint(fetched["file"]), "Bring the form") || strings.Contains(fmt.Sprint(fetched["file"]), "guest@example.net") ||
		strings.Contains(fmt.Sprint(fetched["file"]), "VALARM") {
		t.Errorf("a fetched event at titles: %v", fetched)
	}

	// The owner sees all of it.
	data = world.call(t, world.ada, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: "dentist.ics") { description attendees { address } } }`, world.adaCalendar))
	if owner := data["GetCalendarEvent"].(map[string]any); owner["description"] != "Bring the form" || len(owner["attendees"].([]any)) != 1 {
		t.Errorf("the owner reads everything: %v", owner)
	}

	// Raised to details, ben reads the notes and the guests, still not the
	// private event.
	world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "details")
	data = world.call(t, world.ben, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: "dentist.ics") { summary isBusyOnly description attendees { address } } }`, world.adaCalendar))
	if details := data["GetCalendarEvent"].(map[string]any); details["description"] != "Bring the form" || len(details["attendees"].([]any)) != 1 ||
		details["summary"] != "Dentist" || details["isBusyOnly"] != false {
		t.Errorf("details are everything: %v", details)
	}
	data = world.call(t, world.ben, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: "secret.ics") { summary isBusyOnly } }`, world.adaCalendar))
	if private := data["GetCalendarEvent"].(map[string]any); private["isBusyOnly"] != true || private["summary"] != "" {
		t.Errorf("a private event is busy at every level: %v", private)
	}

	// Somebody outside the circle reaches nothing.
	for _, outsider := range []*models.User{world.cora, world.dave} {
		world.refused(t, outsider, listQuery)
		world.refused(t, outsider, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: "dentist.ics") { id } }`, world.adaCalendar))
		world.refused(t, outsider, fmt.Sprintf(`query { ListCalendarShares(calendarId: %q) { id } }`, world.adaCalendar))
	}
}

// A calendar shared at busy shows grey blocks and no events.
func TestACalendarSharedAtBusyShowsOnlyWhenTheOwnerIsBusy(t *testing.T) {
	world := newCalendarShareWorld(t)
	world.putEvent(t, world.adaCalendar, event("one", "Dentist", "20300102T100000Z", "20300102T110000Z", "LOCATION:Elm Street"))
	world.putEvent(t, world.adaCalendar, event("two", "Lunch", "20300102T110000Z", "20300102T120000Z"))
	world.putEvent(t, world.adaCalendar, event("free", "Holiday reminder", "20300102T130000Z", "20300102T140000Z", "TRANSP:TRANSPARENT"))
	world.putEvent(t, world.adaCalendar, event("secret", "Surprise", "20300103T100000Z", "20300103T110000Z", "CLASS:PRIVATE"))
	world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")

	data := world.call(t, world.ben, fmt.Sprintf(`query { ListCalendarEvents(calendarId: %q, %s) { id uid summary location startsAt endsAt isBusyOnly file } }`, world.adaCalendar, window2030))
	events := listed(t, data, "ListCalendarEvents")
	if len(events) != 2 {
		t.Fatalf("two stretches, the touching meetings merged and the transparent one left out: %v", events)
	}
	for _, each := range events {
		if each["isBusyOnly"] != true || each["id"] != "" || each["uid"] != "" || each["summary"] != "" || each["location"] != nil && each["location"] != "" {
			t.Errorf("a busy block says nothing else: %v", each)
		}
	}
	if events[0]["startsAt"] != "2030-01-02T10:00:00Z" || events[0]["endsAt"] != "2030-01-02T12:00:00Z" {
		t.Errorf("the first stretch is the two meetings together: %v", events[0])
	}
	// No event can be fetched at busy.
	world.refused(t, world.ben, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: "one.ics") { id } }`, world.adaCalendar))
	// And the list says there is no more to know.
	for _, each := range listed(t, world.call(t, world.ben, `query { ListCalendars { id shareLevel events timezone } }`), "ListCalendars") {
		if each["id"] == world.adaCalendar && (each["shareLevel"] != "busy" || each["events"] != float64(0)) {
			t.Errorf("a busy share says nothing of how much is in it: %v", each)
		}
	}
}

// Writing needs edit, never touches an event with guests, and says why.
func TestAWriteToASharedCalendarNeedsEditAndNoGuests(t *testing.T) {
	world := newCalendarShareWorld(t)
	world.putEvent(t, world.adaCalendar, event("plain", "Plain", "20300102T100000Z", "20300102T110000Z"))
	world.putEvent(t, world.adaCalendar, event("meeting", "Meeting", "20300103T100000Z", "20300103T110000Z",
		"ORGANIZER:mailto:ada@example.com", "ATTENDEE:mailto:guest@example.net"))
	world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "details")

	add := func(attendees string) string {
		return fmt.Sprintf(`mutation { SaveCalendarEvent(calendarId: %q, summary: "From ben", startsAt: "2030-01-04T10:00:00Z", endsAt: "2030-01-04T11:00:00Z"%s) { id summary } }`,
			world.adaCalendar, attendees)
	}
	edit := func(id string) string {
		return fmt.Sprintf(`mutation { SaveCalendarEvent(calendarId: %q, id: %q, summary: "Moved") { id } }`, world.adaCalendar, id)
	}
	remove := func(id string) string {
		return fmt.Sprintf(`mutation { DeleteCalendarEvent(calendarId: %q, id: %q) }`, world.adaCalendar, id)
	}

	// Below edit, in words.
	for name, query := range map[string]string{"add": add(""), "edit": edit("plain.ics"), "remove": remove("plain.ics")} {
		if said := world.refused(t, world.ben, query); !strings.Contains(said, "edit") {
			t.Errorf("%s below edit is refused in words: %q", name, said)
		}
	}
	// And with a request identifier, which is what the command line sends.
	if said := world.refused(t, world.ben, strings.Replace(add(""), "SaveCalendarEvent(", `SaveCalendarEvent(requestId: "r-1", `, 1)); !strings.Contains(said, "edit") {
		t.Errorf("a retained request is refused in words too: %q", said)
	}
	// Nobody else is even told it is read only.
	world.refused(t, world.cora, add(""))

	world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "edit")
	data := world.call(t, world.ben, add(""))
	made := data["SaveCalendarEvent"].(map[string]any)
	if made["summary"] != "From ben" {
		t.Fatalf("ben may add at edit: %v", made)
	}
	// It is in ada's calendar, with no organizer and no guests.
	data = world.call(t, world.ada, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: %q) { summary organizer attendees { address } } }`, world.adaCalendar, made["id"]))
	if kept := data["GetCalendarEvent"].(map[string]any); kept["summary"] != "From ben" || kept["organizer"] != nil && kept["organizer"] != "" || len(kept["attendees"].([]any)) != 0 {
		t.Errorf("what ben added: %v", kept)
	}
	world.call(t, world.ben, edit("plain.ics"))
	world.call(t, world.ben, remove(fmt.Sprint(made["id"])))

	// Guests: not on a new event, not on one that has them already.
	for name, query := range map[string]string{
		"add with guests":        add(`, attendees: ["guest@example.net"]`),
		"edit one with guests":   edit("meeting.ics"),
		"remove one with guests": remove("meeting.ics"),
	} {
		if said := world.refused(t, world.ben, query); !strings.Contains(said, "guests") {
			t.Errorf("%s is refused because of the guests: %q", name, said)
		}
	}
	// The owner still may, and the event with guests is untouched meanwhile.
	data = world.call(t, world.ada, fmt.Sprintf(`query { GetCalendarEvent(calendarId: %q, id: "meeting.ics") { summary } }`, world.adaCalendar))
	if data["GetCalendarEvent"].(map[string]any)["summary"] != "Meeting" {
		t.Errorf("the meeting was changed")
	}
}

// Sharing needs a circle in common, and only the owner shares or unshares.
func TestSharingNeedsACircleAndTheOwner(t *testing.T) {
	world := newCalendarShareWorld(t)
	shareWith := func(user *models.User, level string) string {
		return fmt.Sprintf(`mutation { ShareCalendar(calendarId: %q, granteeUserId: %q, shareLevel: %q) { id } }`, world.adaCalendar, user.ID, level)
	}
	// cora and dave share no circle with ada.
	for _, stranger := range []*models.User{world.cora, world.dave} {
		world.refused(t, world.ada, shareWith(stranger, "busy"))
	}
	// A circle she is not in.
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ShareCalendar(calendarId: %q, granteeCircleId: "nothing", shareLevel: "busy") { id } }`, world.adaCalendar))
	// Both, neither, and a level there is not.
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ShareCalendar(calendarId: %q, granteeUserId: %q, granteeCircleId: %q, shareLevel: "busy") { id } }`, world.adaCalendar, world.ben.ID, world.home))
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ShareCalendar(calendarId: %q, shareLevel: "busy") { id } }`, world.adaCalendar))
	world.refused(t, world.ada, shareWith(world.ben, "owner"))
	world.refused(t, world.ada, shareWith(world.ben, "everything"))
	// Not somebody else's calendar.
	world.refused(t, world.ben, shareWith(world.ada, "edit"))

	first := world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")
	circle := world.share(t, world.ada, world.adaCalendar, "granteeCircleId: "+fmt.Sprintf("%q", world.home), "titles")
	if circle["granteeCircleName"] != "Home" {
		t.Errorf("the share to a circle names it: %v", circle)
	}
	// Sharing again changes the level; there is still one share per grantee.
	again := world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "details")
	if again["id"] != first["id"] || again["shareLevel"] != "details" {
		t.Errorf("sharing again changes the level: %v then %v", first, again)
	}
	data := world.call(t, world.ada, fmt.Sprintf(`query { ListCalendarShares(calendarId: %q) { id shareLevel granteeUserId granteeCircleId } }`, world.adaCalendar))
	if shares := listed(t, data, "ListCalendarShares"); len(shares) != 2 {
		t.Errorf("two shares, one to a person and one to a circle: %v", shares)
	}
	world.refused(t, world.ben, fmt.Sprintf(`query { ListCalendarShares(calendarId: %q) { id } }`, world.adaCalendar))
	world.refused(t, world.ben, fmt.Sprintf(`mutation { UnshareCalendar(shareId: %q) }`, first["id"]))
	world.refused(t, world.cora, fmt.Sprintf(`mutation { UnshareCalendar(shareId: %q) }`, first["id"]))
	world.call(t, world.ada, fmt.Sprintf(`mutation { UnshareCalendar(shareId: %q) }`, first["id"]))
	world.refused(t, world.ada, fmt.Sprintf(`mutation { UnshareCalendar(shareId: %q) }`, first["id"]))

	// The circle's share is left, at titles.
	for _, each := range listed(t, world.call(t, world.ben, `query { ListCalendars { id shareLevel } }`), "ListCalendars") {
		if each["id"] == world.adaCalendar && each["shareLevel"] != "titles" {
			t.Errorf("ben is left with the circle's share: %v", each)
		}
	}
}

// When the owner leaves the circle, the people she shared with no longer see
// the calendar, on any surface.
func TestLeavingTheCircleEndsWhatWasSharedThroughIt(t *testing.T) {
	world := newCalendarShareWorld(t)
	world.putEvent(t, world.adaCalendar, event("dentist", "Dentist", "20300102T100000Z", "20300102T110000Z"))
	world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "titles")
	world.share(t, world.ada, world.adaCalendar, "granteeCircleId: "+fmt.Sprintf("%q", world.home), "titles")

	canSee := func() bool {
		for _, each := range listed(t, world.call(t, world.ben, `query { ListCalendars { id } }`), "ListCalendars") {
			if each["id"] == world.adaCalendar {
				return true
			}
		}
		return false
	}
	if !canSee() {
		t.Fatal("ben sees ada's calendar before she leaves")
	}
	world.call(t, world.ada, fmt.Sprintf(`mutation { SetCircleMemberRole(circleId: %q, userId: %q, memberRole: "admin") { id } }`, world.home, world.ben.ID))
	world.call(t, world.ada, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, world.home))
	if canSee() {
		t.Error("ben still sees ada's calendar after she left")
	}
	world.refused(t, world.ben, fmt.Sprintf(`query { ListCalendarEvents(calendarId: %q, %s) { id } }`, world.adaCalendar, window2030))
}

// A circle has one calendar, which its members read and write and nobody
// else reaches.
func TestACircleCalendarIsEveryMembersAndNobodyElses(t *testing.T) {
	world := newCalendarShareWorld(t)
	made := world.call(t, world.ada, fmt.Sprintf(`mutation { CreateCircleCalendar(circleId: %q, name: "The household") { id name shareLevel isCircleCalendar circleId circleName ownerUserId ownerName calendarKind } }`, world.home))
	family := made["CreateCircleCalendar"].(map[string]any)
	familyId := family["id"].(string)
	if family["name"] != "The household" || family["shareLevel"] != "edit" || family["isCircleCalendar"] != true ||
		family["circleId"] != world.home || family["circleName"] != "Home" || family["ownerUserId"] != nil || family["calendarKind"] != "events" {
		t.Fatalf("the circle calendar: %v", family)
	}
	// One only; only an admin makes it; nobody outside does.
	world.refused(t, world.ada, fmt.Sprintf(`mutation { CreateCircleCalendar(circleId: %q) { id } }`, world.home))
	world.refused(t, world.ben, fmt.Sprintf(`mutation { CreateCircleCalendar(circleId: %q) { id } }`, world.home))
	world.refused(t, world.cora, fmt.Sprintf(`mutation { CreateCircleCalendar(circleId: %q) { id } }`, world.home))

	// Both members list it, with the circle's name, and write to it.
	for _, member := range []*models.User{world.ada, world.ben} {
		found := false
		for _, each := range listed(t, world.call(t, member, `query { ListCalendars { id shareLevel isCircleCalendar circleName } }`), "ListCalendars") {
			if each["id"] == familyId {
				found = true
				if each["shareLevel"] != "edit" || each["isCircleCalendar"] != true || each["circleName"] != "Home" {
					t.Errorf("%s sees %v", member.Username, each)
				}
			}
		}
		if !found {
			t.Errorf("%s does not see the circle's calendar", member.Username)
		}
	}
	added := world.call(t, world.ben, fmt.Sprintf(`mutation { SaveCalendarEvent(calendarId: %q, summary: "School play", startsAt: "2030-01-05T18:00:00Z", endsAt: "2030-01-05T19:00:00Z") { id } }`, familyId))
	eventId := added["SaveCalendarEvent"].(map[string]any)["id"].(string)
	data := world.call(t, world.ada, fmt.Sprintf(`query { ListCalendarEvents(calendarId: %q, from: "2030-01-01T00:00:00Z", until: "2030-01-31T00:00:00Z") { id summary } }`, familyId))
	if events := listed(t, data, "ListCalendarEvents"); len(events) != 1 || events[0]["summary"] != "School play" {
		t.Errorf("ada sees what ben added: %v", events)
	}
	world.call(t, world.ada, fmt.Sprintf(`mutation { SaveCalendarEvent(calendarId: %q, id: %q, summary: "School concert") { id } }`, familyId, eventId))
	// No guests, because nobody would send the invitations.
	if said := world.refused(t, world.ada, fmt.Sprintf(`mutation { SaveCalendarEvent(calendarId: %q, summary: "Party", startsAt: "2030-01-06T18:00:00Z", endsAt: "2030-01-06T19:00:00Z", attendees: ["guest@example.net"]) { id } }`, familyId)); !strings.Contains(said, "guests") {
		t.Errorf("a circle's event has no guests: %q", said)
	}
	// Nobody outside it reaches it.
	for _, outsider := range []*models.User{world.cora, world.dave} {
		world.refused(t, outsider, fmt.Sprintf(`query { ListCalendarEvents(calendarId: %q, from: "2030-01-01T00:00:00Z", until: "2030-01-31T00:00:00Z") { id } }`, familyId))
		world.refused(t, outsider, fmt.Sprintf(`mutation { SaveCalendarEvent(calendarId: %q, summary: "Mine", startsAt: "2030-01-06T18:00:00Z", endsAt: "2030-01-06T19:00:00Z") { id } }`, familyId))
		world.refused(t, outsider, fmt.Sprintf(`mutation { DeleteCalendarEvent(calendarId: %q, id: %q) }`, familyId, eventId))
		for _, each := range listed(t, world.call(t, outsider, `query { ListCalendars { id } }`), "ListCalendars") {
			if each["id"] == familyId {
				t.Errorf("%s lists the circle's calendar", outsider.Username)
			}
		}
	}
	// A member who leaves stops reaching it.
	world.call(t, world.ben, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, world.home))
	world.refused(t, world.ben, fmt.Sprintf(`query { ListCalendarEvents(calendarId: %q, from: "2030-01-01T00:00:00Z", until: "2030-01-31T00:00:00Z") { id } }`, familyId))
	// It cannot be shared, being everybody's already, and nobody but its
	// members can rename or share it.
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ShareCalendar(calendarId: %q, granteeUserId: %q, shareLevel: "busy") { id } }`, familyId, world.ben.ID))
}

// Free-busy answers for the people who shared with the caller and their own,
// the same way the owner's is worked out, and says nothing for anyone else.
func TestFreeBusyIsAnsweredForThePeopleWhoSharedAndNobodyElse(t *testing.T) {
	world := newCalendarShareWorld(t)
	world.putEvent(t, world.adaCalendar, event("one", "Dentist", "20300102T100000Z", "20300102T110000Z"))
	world.putEvent(t, world.adaCalendar, event("two", "Lunch", "20300102T110000Z", "20300102T120000Z"))
	world.putEvent(t, world.adaCalendar, event("free", "Transparent", "20300102T130000Z", "20300102T140000Z", "TRANSP:TRANSPARENT"))
	world.putEvent(t, world.adaCalendar, event("secret", "Surprise", "20300102T150000Z", "20300102T160000Z", "CLASS:PRIVATE"))

	ask := func(user *models.User, ids ...string) []map[string]any {
		quoted := make([]string, 0, len(ids))
		for _, id := range ids {
			quoted = append(quoted, fmt.Sprintf("%q", id))
		}
		data := world.call(t, user, fmt.Sprintf(`query { ListCalendarFreeBusy(userIds: [%s], %s) { userId username name isShared shareLevel periods { startsAt endsAt } } }`,
			strings.Join(quoted, ", "), window2030))
		return listed(t, data, "ListCalendarFreeBusy")
	}

	// Before any share ada is not shared, which looks like nobody at all.
	answers := ask(world.ben, world.ada.ID, "nobody")
	for _, each := range answers {
		if each["isShared"] != false || each["username"] != "" || len(each["periods"].([]any)) != 0 {
			t.Errorf("nothing shared tells nothing: %v", each)
		}
	}
	if answers[0]["userId"] != world.ada.ID || answers[1]["userId"] != "nobody" || len(answers) != 2 {
		t.Errorf("each person asked about is answered: %v", answers)
	}

	world.share(t, world.ada, world.adaCalendar, userGrantee(world.ben), "busy")
	answers = ask(world.ben, world.ada.ID, world.cora.ID)
	shared := answers[0]
	if shared["isShared"] != true || shared["username"] != world.ada.Username || shared["shareLevel"] != "busy" {
		t.Fatalf("ada shared with ben: %v", shared)
	}
	periods := shared["periods"].([]any)
	if len(periods) != 2 {
		t.Fatalf("two stretches, the touching meetings merged, the transparent one left out and the private one in: %v", periods)
	}
	if first := periods[0].(map[string]any); first["startsAt"] != "2030-01-02T10:00:00Z" || first["endsAt"] != "2030-01-02T12:00:00Z" {
		t.Errorf("the first stretch: %v", first)
	}
	if answers[1]["isShared"] != false {
		t.Errorf("cora shared nothing: %v", answers[1])
	}
	// The caller's own is always answered, as owner, and the same way.
	own := ask(world.ada, world.ada.ID)[0]
	if own["isShared"] != true || own["shareLevel"] != "owner" || len(own["periods"].([]any)) != 2 {
		t.Errorf("the owner's own: %v", own)
	}
	// A window the server will not describe, and too many people.
	world.refused(t, world.ben, `query { ListCalendarFreeBusy(userIds: ["x"], from: "2030-01-01T00:00:00Z", until: "2040-01-01T00:00:00Z") { userId } }`)
	many := make([]string, 0, 30)
	for index := range 30 {
		many = append(many, fmt.Sprintf("%q", fmt.Sprint("person-", index)))
	}
	world.refused(t, world.ben, fmt.Sprintf(`query { ListCalendarFreeBusy(userIds: [%s], %s) { userId } }`, strings.Join(many, ","), window2030))
}
