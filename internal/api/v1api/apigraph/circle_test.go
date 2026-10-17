package apigraph

import (
	"fmt"
	"image/color"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// circleWorld is a server with four people who can use circles, one who
// cannot, and an operator who manages accounts.
type circleWorld struct {
	*avatarWorld

	// alice makes circles; bob, carol and erin join them; dave holds no
	// circle:use; operator holds user:manage as well as circle:use.
	alice, bob, carol, dave, erin, operator *models.User

	// carolAgent is carol's agent, which is on.
	carolAgent *models.Agent
}

func newCircleWorld(t *testing.T) *circleWorld {
	t.Helper()
	world := &circleWorld{avatarWorld: newAvatarWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		create := func(username string, permissions ...models.Permission) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username, Name: strings.ToUpper(username[:1]) + username[1:]})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			if len(permissions) > 0 {
				role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: permissions})
				if err != nil {
					t.Fatalf("CreateRole: %s", err)
				}
				if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
					t.Fatalf("CreateGroup: %s", err)
				}
			}
			return user
		}
		world.alice = create("circle-alice", models.PermissionCircleUse)
		world.bob = create("circle-bob", models.PermissionCircleUse)
		world.carol = create("circle-carol", models.PermissionCircleUse)
		world.dave = create("circle-dave")
		world.erin = create("circle-erin", models.PermissionCircleUse)
		world.operator = create("circle-operator", models.PermissionCircleUse, models.PermissionUserManage)
		var err error
		if world.carolAgent, err = tx.CreateAgent(&models.Agent{UserID: world.carol.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	return world
}

// call sends GraphQL as an account and fails the test when it is refused.
func (self *circleWorld) call(t *testing.T, user *models.User, query string) map[string]any {
	t.Helper()
	data, errors := self.ask(t, user, query)
	if len(errors) > 0 {
		t.Fatalf("%s: %v\n%s", user.Username, errors, query)
	}
	return data
}

// refused sends GraphQL as an account and returns what the refusal said,
// failing the test when it was not refused.
func (self *circleWorld) refused(t *testing.T, user *models.User, query string) string {
	t.Helper()
	data, errors := self.ask(t, user, query)
	if len(errors) == 0 {
		t.Fatalf("%s was not refused: %v\n%s", user.Username, data, query)
	}
	return strings.Join(errors, "; ")
}

func (self *circleWorld) makeCircle(t *testing.T, owner *models.User, name, circleKind string) string {
	t.Helper()
	data := self.call(t, owner, fmt.Sprintf(`mutation { CreateCircle(name: %q, circleKind: %q) { id } }`, name, circleKind))
	return data["CreateCircle"].(map[string]any)["id"].(string)
}

func (self *circleWorld) invite(t *testing.T, admin *models.User, circleId string, invited *models.User, memberRole string) {
	t.Helper()
	data := self.call(t, admin, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: %q) }`, circleId, invited.Username, memberRole))
	if data["InviteToCircle"] != true {
		t.Fatalf("InviteToCircle answered %v", data["InviteToCircle"])
	}
}

// join invites a person and has them accept.
func (self *circleWorld) join(t *testing.T, admin *models.User, circleId string, invited *models.User, memberRole string) {
	t.Helper()
	self.invite(t, admin, circleId, invited, memberRole)
	data := self.call(t, invited, `query { ListCircleInvitations { id circleId } }`)
	for _, each := range data["ListCircleInvitations"].([]any) {
		invitation := each.(map[string]any)
		if invitation["circleId"] == circleId {
			self.call(t, invited, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`, invitation["id"]))
			return
		}
	}
	t.Fatalf("%s has no invitation to %s", invited.Username, circleId)
}

func (self *circleWorld) members(t *testing.T, user *models.User, circleId string) map[string]string {
	t.Helper()
	data := self.call(t, user, fmt.Sprintf(`query { ReadCircle(circleId: %q) { members { username memberRole } } }`, circleId))
	roles := map[string]string{}
	for _, each := range data["ReadCircle"].(map[string]any)["members"].([]any) {
		member := each.(map[string]any)
		roles[member["username"].(string)] = member["memberRole"].(string)
	}
	return roles
}

func TestACircleIsMadeInvitedToAndJoined(t *testing.T) {
	world := newCircleWorld(t)
	data := world.call(t, world.alice, `mutation { CreateCircle(name: " Home ", circleKind: "family") {
		id name circleKind memberRole createdAt members { userId username name avatarColorIndex memberRole joinedAt } pendingInvitations { id } } }`)
	circle := data["CreateCircle"].(map[string]any)
	if circle["name"] != "Home" || circle["circleKind"] != "family" || circle["memberRole"] != "admin" {
		t.Fatalf("a new circle is %v", circle)
	}
	circleId := circle["id"].(string)
	if members := circle["members"].([]any); len(members) != 1 || members[0].(map[string]any)["username"] != world.alice.Username {
		t.Fatalf("a new circle has the members %v", circle["members"])
	}

	world.invite(t, world.alice, circleId, world.bob, "member")
	world.invite(t, world.alice, circleId, world.carol, "member")

	// The admin sees who is still being waited for; the invited see the invitation.
	data = world.call(t, world.alice, fmt.Sprintf(`query { ReadCircle(circleId: %q) { pendingInvitations { id invitedUsername invitedByName memberRole circleName circleKind expiresAt } } }`, circleId))
	pending := data["ReadCircle"].(map[string]any)["pendingInvitations"].([]any)
	if len(pending) != 2 {
		t.Fatalf("the admin sees %d pending invitations: %v", len(pending), pending)
	}
	data = world.call(t, world.bob, `query { ListCircleInvitations { id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt } }`)
	invitations := data["ListCircleInvitations"].([]any)
	if len(invitations) != 1 {
		t.Fatalf("bob has %d invitations", len(invitations))
	}
	invitation := invitations[0].(map[string]any)
	if invitation["circleName"] != "Home" || invitation["circleKind"] != "family" || invitation["invitedByName"] != "Circle-alice" || invitation["invitedUsername"] != world.bob.Username {
		t.Fatalf("bob's invitation is %v", invitation)
	}
	expires, err := time.Parse(time.RFC3339, invitation["expiresAt"].(string))
	if err != nil || time.Until(expires) < 29*24*time.Hour || time.Until(expires) > 31*24*time.Hour {
		t.Errorf("the invitation expires at %v, %v; want in thirty days", invitation["expiresAt"], err)
	}
	// Not the circle's pending list to a member who has not joined.
	world.refused(t, world.bob, fmt.Sprintf(`query { ReadCircle(circleId: %q) { id } }`, circleId))

	data = world.call(t, world.bob, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id memberRole members { username } pendingInvitations { id } } }`, invitation["id"]))
	joined := data["AnswerCircleInvitation"].(map[string]any)
	if joined["id"] != circleId || joined["memberRole"] != "member" || len(joined["members"].([]any)) != 2 || len(joined["pendingInvitations"].([]any)) != 0 {
		t.Fatalf("after accepting, bob sees %v", joined)
	}
	if left := world.call(t, world.bob, `query { ListCircleInvitations { id } }`)["ListCircleInvitations"].([]any); len(left) != 0 {
		t.Errorf("an accepted invitation is still listed: %v", left)
	}
	roles := world.members(t, world.alice, circleId)
	if roles[world.alice.Username] != "admin" || roles[world.bob.Username] != "member" || len(roles) != 2 {
		t.Errorf("the members are %v", roles)
	}

	// Declining joins nobody and answers null.
	data = world.call(t, world.carol, `query { ListCircleInvitations { id } }`)
	carolInvitation := data["ListCircleInvitations"].([]any)[0].(map[string]any)["id"].(string)
	declined := world.call(t, world.carol, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: false) { id } }`, carolInvitation))
	if declined["AnswerCircleInvitation"] != nil {
		t.Errorf("declining answered %v", declined["AnswerCircleInvitation"])
	}
	if len(world.members(t, world.alice, circleId)) != 2 {
		t.Error("declining put carol in the circle")
	}
	// An invitation already answered says so, to the person it was for.
	if said := world.refused(t, world.carol, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`, carolInvitation)); !strings.Contains(said, "That invitation was already answered") {
		t.Errorf("answering an answered invitation said %q", said)
	}

	// Nobody answers somebody else's invitation.
	world.invite(t, world.alice, circleId, world.erin, "member")
	erinInvitation := world.call(t, world.erin, `query { ListCircleInvitations { id } }`)["ListCircleInvitations"].([]any)[0].(map[string]any)["id"].(string)
	world.refused(t, world.bob, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`, erinInvitation))
	world.refused(t, world.carol, fmt.Sprintf(`mutation { WithdrawCircleInvitation(invitationId: %q) }`, erinInvitation))
}

func TestACircleIsNotFoundByAnyoneNotInIt(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")

	for _, query := range []string{
		fmt.Sprintf(`query { ReadCircle(circleId: %q) { id } }`, circleId),
		fmt.Sprintf(`mutation { RenameCircle(circleId: %q, name: "Mine") { id } }`, circleId),
		fmt.Sprintf(`mutation { DeleteCircle(circleId: %q) }`, circleId),
		fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: "circle-carol", memberRole: "member") }`, circleId),
		fmt.Sprintf(`mutation { SetCircleMemberRole(circleId: %q, userId: %q, memberRole: "member") { id } }`, circleId, world.alice.ID),
		fmt.Sprintf(`mutation { RemoveCircleMember(circleId: %q, userId: %q) { id } }`, circleId, world.alice.ID),
		fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId),
	} {
		for _, stranger := range []*models.User{world.bob, world.dave} {
			if message := world.refused(t, stranger, query); !strings.Contains(message, "not found") {
				t.Errorf("%s was told %q rather than not found for %s", stranger.Username, message, query)
			}
		}
		// And the same words for a circle that does not exist.
		missing := strings.ReplaceAll(query, circleId, "no-such-circle")
		if message := world.refused(t, world.alice, missing); !strings.Contains(message, "not found") {
			t.Errorf("a circle that is not there answered %q for %s", message, missing)
		}
	}
	if circles := world.call(t, world.bob, `query { ListCircles { id } }`)["ListCircles"].([]any); len(circles) != 0 {
		t.Errorf("bob lists %v", circles)
	}
	if world.members(t, world.alice, circleId)[world.alice.Username] != "admin" {
		t.Error("a stranger's refused calls changed the circle")
	}
}

func TestAMemberWhoIsNotAnAdminCannotManageTheCircle(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Team", "colleagues")
	world.join(t, world.alice, circleId, world.bob, "member")
	world.join(t, world.alice, circleId, world.carol, "member")

	for _, query := range []string{
		fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, circleId, world.erin.Username),
		fmt.Sprintf(`mutation { RemoveCircleMember(circleId: %q, userId: %q) { id } }`, circleId, world.carol.ID),
		fmt.Sprintf(`mutation { SetCircleMemberRole(circleId: %q, userId: %q, memberRole: "admin") { id } }`, circleId, world.bob.ID),
		fmt.Sprintf(`mutation { SetCircleMemberRole(circleId: %q, userId: %q, memberRole: "member") { id } }`, circleId, world.alice.ID),
		fmt.Sprintf(`mutation { RenameCircle(circleId: %q, name: "Mine") { id } }`, circleId),
		fmt.Sprintf(`mutation { DeleteCircle(circleId: %q) }`, circleId),
	} {
		if message := world.refused(t, world.bob, query); !strings.Contains(message, "only an admin") {
			t.Errorf("a member was told %q for %s", message, query)
		}
	}
	// And did not see who is waiting.
	world.invite(t, world.alice, circleId, world.erin, "member")
	data := world.call(t, world.bob, fmt.Sprintf(`query { ReadCircle(circleId: %q) { name memberRole pendingInvitations { id } } }`, circleId))
	circle := data["ReadCircle"].(map[string]any)
	if circle["memberRole"] != "member" || len(circle["pendingInvitations"].([]any)) != 0 {
		t.Errorf("a member sees %v", circle)
	}
	invitationId := world.call(t, world.erin, `query { ListCircleInvitations { id } }`)["ListCircleInvitations"].([]any)[0].(map[string]any)["id"].(string)
	if message := world.refused(t, world.bob, fmt.Sprintf(`mutation { WithdrawCircleInvitation(invitationId: %q) }`, invitationId)); !strings.Contains(message, "only an admin") {
		t.Errorf("a member withdrawing an invitation was told %q", message)
	}
	if roles := world.members(t, world.alice, circleId); len(roles) != 3 || roles[world.bob.Username] != "member" {
		t.Errorf("refused calls changed the members: %v", roles)
	}
}

func TestTheLastAdminCannotLeaveOrBeDemotedWhileOthersRemain(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	world.join(t, world.alice, circleId, world.bob, "member")

	for _, query := range []string{
		fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId),
		fmt.Sprintf(`mutation { SetCircleMemberRole(circleId: %q, userId: %q, memberRole: "member") { id } }`, circleId, world.alice.ID),
		fmt.Sprintf(`mutation { RemoveCircleMember(circleId: %q, userId: %q) { id } }`, circleId, world.alice.ID),
	} {
		if message := world.refused(t, world.alice, query); !strings.Contains(message, "needs an admin") {
			t.Errorf("the last admin was told %q for %s", message, query)
		}
	}
	if world.members(t, world.bob, circleId)[world.alice.Username] != "admin" {
		t.Fatal("the last admin is no longer an admin")
	}

	// With a second admin she may.
	data := world.call(t, world.alice, fmt.Sprintf(`mutation { SetCircleMemberRole(circleId: %q, userId: %q, memberRole: "admin") { memberRole members { username memberRole } } }`, circleId, world.bob.ID))
	if data["SetCircleMemberRole"].(map[string]any)["memberRole"] != "admin" {
		t.Errorf("SetCircleMemberRole answered %v", data["SetCircleMemberRole"])
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId))
	if roles := world.members(t, world.bob, circleId); len(roles) != 1 || roles[world.bob.Username] != "admin" {
		t.Errorf("after alice left: %v", roles)
	}
	world.refused(t, world.alice, fmt.Sprintf(`query { ReadCircle(circleId: %q) { id } }`, circleId))
}

func TestTheLastMemberLeavingDeletesTheCircle(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Short lived", "friends")
	world.call(t, world.alice, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId))

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if found, err := tx.GetCircle(circleId); err != nil || found != nil {
			t.Errorf("the circle is still there: %v %v", err, found)
		}
	})
	if circles := world.call(t, world.alice, `query { ListCircles { id } }`)["ListCircles"].([]any); len(circles) != 0 {
		t.Errorf("alice still lists %v", circles)
	}

	// An admin may also delete one outright, with everybody still in it.
	circleId = world.makeCircle(t, world.alice, "Home", "family")
	world.join(t, world.alice, circleId, world.bob, "member")
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteCircle(circleId: %q) }`, circleId))
	world.refused(t, world.bob, fmt.Sprintf(`query { ReadCircle(circleId: %q) { id } }`, circleId))
}

// The bound on invitations an hour says no after the same number of calls
// whatever the names are, so that it cannot be used to find which usernames
// exist, and deleting the circle the invitations were to does not give the
// allowance back.
func TestTheInvitationBoundCountsEveryCallAndSurvivesDeletingTheCircle(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	invite := func(circleId, username string) (map[string]any, string) {
		t.Helper()
		data, failures := world.ask(t, world.alice, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, circleId, username))
		return data, strings.Join(failures, "; ")
	}
	// Thirty calls for names nobody has, none of which stores anything.
	for count := 0; count < models.CircleInvitationsPerHourMost; count++ {
		if data, said := invite(circleId, fmt.Sprintf("circle-nobody-%02d", count)); said != "" || data["InviteToCircle"] != true {
			t.Fatalf("call %d: %v %q", count+1, data, said)
		}
	}
	// The next is refused the same way for a name that exists and for one that
	// does not.
	_, forAccount := invite(circleId, world.carol.Username)
	_, forNobody := invite(circleId, "circle-nobody-extra")
	if forAccount == "" || forAccount != forNobody || !strings.Contains(forAccount, "30 invitations") {
		t.Fatalf("the refusals: %q for an account and %q for nobody", forAccount, forNobody)
	}
	// A new circle is no new allowance.
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteCircle(circleId: %q) }`, circleId))
	again := world.makeCircle(t, world.alice, "Home again", "family")
	if _, said := invite(again, world.carol.Username); !strings.Contains(said, "30 invitations") {
		t.Errorf("after deleting the circle: %q", said)
	}
}

func TestInvitingAUsernameThatIsNotAnAccountAnswersTheSameAndStoresNothing(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	world.join(t, world.alice, circleId, world.bob, "member")

	invite := func(username string) any {
		return world.call(t, world.alice, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, circleId, username))["InviteToCircle"]
	}
	// An account, a member already, nobody, and an account that cannot use circles.
	answers := []any{invite(world.carol.Username), invite(world.bob.Username), invite("circle-nobody"), invite(world.dave.Username), invite("circle-alice")}
	for _, answer := range answers {
		if answer != true {
			t.Fatalf("the answers were %v; each should be true", answers)
		}
	}

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		waiting, err := tx.ListCircleInvitationsOf(circleId, time.Now())
		if err != nil || len(waiting) != 1 || waiting[0].InvitedUserID != world.carol.ID {
			t.Errorf("the circle is waiting for %+v, %v; want only carol", waiting, err)
		}
		if invitations, err := tx.ListCircleInvitationsTo(world.dave.ID, time.Now()); err != nil || len(invitations) != 0 {
			t.Errorf("dave, who cannot use circles, was invited: %+v %v", invitations, err)
		}
	})

	// Inviting again renews the one invitation rather than doubling it, and a
	// bad role is refused.
	invite(world.carol.Username)
	if pending := world.call(t, world.alice, fmt.Sprintf(`query { ReadCircle(circleId: %q) { pendingInvitations { id } } }`, circleId))["ReadCircle"].(map[string]any)["pendingInvitations"].([]any); len(pending) != 1 {
		t.Errorf("inviting twice left %d invitations", len(pending))
	}
	world.refused(t, world.alice, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: "circle-erin", memberRole: "owner") }`, circleId))
}

func TestAnAdminWithdrawsAnInvitationAndMakesAnotherAdmin(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Team", "colleagues")
	world.join(t, world.alice, circleId, world.bob, "member")

	// As B, inviting is refused; once A makes B an admin it works.
	invite := fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, circleId, world.carol.Username)
	world.refused(t, world.bob, invite)
	world.call(t, world.alice, fmt.Sprintf(`mutation { SetCircleMemberRole(circleId: %q, userId: %q, memberRole: "admin") { id } }`, circleId, world.bob.ID))
	world.call(t, world.bob, invite)

	invitationId := world.call(t, world.carol, `query { ListCircleInvitations { id } }`)["ListCircleInvitations"].([]any)[0].(map[string]any)["id"].(string)
	if data := world.call(t, world.alice, fmt.Sprintf(`mutation { WithdrawCircleInvitation(invitationId: %q) }`, invitationId)); data["WithdrawCircleInvitation"] != true {
		t.Errorf("WithdrawCircleInvitation answered %v", data)
	}
	if invitations := world.call(t, world.carol, `query { ListCircleInvitations { id } }`)["ListCircleInvitations"].([]any); len(invitations) != 0 {
		t.Errorf("a withdrawn invitation is still listed: %v", invitations)
	}
	world.refused(t, world.carol, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`, invitationId))

	// Removing a member, and renaming.
	data := world.call(t, world.alice, fmt.Sprintf(`mutation { RemoveCircleMember(circleId: %q, userId: %q) { members { username } } }`, circleId, world.bob.ID))
	if members := data["RemoveCircleMember"].(map[string]any)["members"].([]any); len(members) != 1 {
		t.Errorf("after removing bob: %v", members)
	}
	world.refused(t, world.bob, fmt.Sprintf(`query { ReadCircle(circleId: %q) { id } }`, circleId))
	data = world.call(t, world.alice, fmt.Sprintf(`mutation { RenameCircle(circleId: %q, name: "Platform team") { name } }`, circleId))
	if data["RenameCircle"].(map[string]any)["name"] != "Platform team" {
		t.Errorf("RenameCircle answered %v", data)
	}
	world.refused(t, world.alice, fmt.Sprintf(`mutation { RenameCircle(circleId: %q, name: "   ") { name } }`, circleId))
	world.refused(t, world.alice, `mutation { CreateCircle(name: "Club", circleKind: "club") { id } }`)
}

func TestAddCircleMemberNeedsUserManage(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	add := fmt.Sprintf(`mutation { AddCircleMember(circleId: %q, username: %q, memberRole: "member") { id memberRole members { username } } }`, circleId, world.bob.Username)

	// Not an admin of the circle, not a member, and not an account manager.
	world.refused(t, world.carol, add)
	// Not even the circle's own admin, whose way in is an invitation, who is
	// told what is missing rather than that the circle is not found.
	if said := world.refused(t, world.alice, add); !strings.Contains(said, "needs the user:manage permission") || strings.Contains(said, "not found") {
		t.Errorf("the circle's admin was told %q", said)
	}
	if len(world.members(t, world.alice, circleId)) != 1 {
		t.Fatal("a refused AddCircleMember put somebody in")
	}

	data := world.call(t, world.operator, add)
	circle := data["AddCircleMember"].(map[string]any)
	if circle["id"] != circleId || len(circle["members"].([]any)) != 2 {
		t.Errorf("AddCircleMember answered %v", circle)
	}
	if circle["memberRole"] != "" {
		t.Errorf("the operator, who is not in it, was shown the role %q", circle["memberRole"])
	}
	if roles := world.members(t, world.bob, circleId); roles[world.bob.Username] != "member" {
		t.Errorf("the members are %v", roles)
	}
	world.refused(t, world.operator, fmt.Sprintf(`mutation { AddCircleMember(circleId: %q, username: "circle-nobody", memberRole: "member") { id } }`, circleId))
}

func TestCirclePeopleAreListedOnceEach(t *testing.T) {
	world := newCircleWorld(t)
	home := world.makeCircle(t, world.alice, "Home", "family")
	work := world.makeCircle(t, world.alice, "Work", "colleagues")
	for _, circleId := range []string{home, work} {
		world.join(t, world.alice, circleId, world.bob, "member")
	}
	world.join(t, world.alice, work, world.carol, "member")

	data := world.call(t, world.alice, `query { ListCirclePeople { userId username name avatarUrl avatarColorIndex memberRole joinedAt } }`)
	people := map[string]map[string]any{}
	for _, each := range data["ListCirclePeople"].([]any) {
		person := each.(map[string]any)
		people[person["username"].(string)] = person
	}
	if len(people) != 2 || people[world.bob.Username] == nil || people[world.carol.Username] == nil {
		t.Fatalf("alice shares a circle with %v", people)
	}
	if people[world.bob.Username]["memberRole"] != "" {
		t.Errorf("a person in the list has the role %q", people[world.bob.Username]["memberRole"])
	}
	// Carol and bob share work, so each sees the other; carol does not see erin.
	data = world.call(t, world.carol, `query { ListCirclePeople { username } }`)
	if seen := data["ListCirclePeople"].([]any); len(seen) != 2 {
		t.Errorf("carol sees %v", seen)
	}
	if seen := world.call(t, world.erin, `query { ListCirclePeople { username } }`)["ListCirclePeople"].([]any); len(seen) != 0 {
		t.Errorf("erin, who is in no circle, sees %v", seen)
	}
	world.refused(t, world.dave, `query { ListCirclePeople { username } }`)
	world.refused(t, world.dave, `query { ListCircles { id } }`)
	world.refused(t, world.dave, `mutation { CreateCircle(name: "x", circleKind: "family") { id } }`)
}

func TestACircleMateSeesAPictureAndAStrangerDoesNot(t *testing.T) {
	world := newCircleWorld(t)
	if _, errors := world.ask(t, world.alice, setAvatar("", picture(t, color.RGBA{R: 40, G: 90, A: 255}))); len(errors) > 0 {
		t.Fatalf("SetUserAvatar: %v", errors)
	}
	address := "/api/v1/avatar/user/" + world.alice.ID

	if got := world.fetch(world.bob, address).Code; got != http.StatusNotFound {
		t.Fatalf("a stranger fetching the picture got %d", got)
	}
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	world.join(t, world.alice, circleId, world.bob, "member")
	if got := world.fetch(world.bob, address).Code; got != http.StatusOK {
		t.Errorf("a circle-mate fetching the picture got %d", got)
	}
	if got := world.fetch(world.carol, address).Code; got != http.StatusNotFound {
		t.Errorf("somebody in no circle with her fetching the picture got %d", got)
	}
	// The circle's member list carries the address, so the screens can draw it.
	data := world.call(t, world.bob, fmt.Sprintf(`query { ReadCircle(circleId: %q) { members { username avatarUrl } } }`, circleId))
	for _, each := range data["ReadCircle"].(map[string]any)["members"].([]any) {
		member := each.(map[string]any)
		if member["username"] == world.alice.Username && !strings.HasPrefix(fmt.Sprint(member["avatarUrl"]), address) {
			t.Errorf("alice's address in the member list is %v", member["avatarUrl"])
		}
	}

	// Leaving ends it.
	world.call(t, world.bob, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId))
	if got := world.fetch(world.bob, address).Code; got != http.StatusNotFound {
		t.Errorf("after leaving, the picture answered %d", got)
	}
}

func TestAnInvitationIsNotedForAPersonWhoseAgentIsOn(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	world.invite(t, world.alice, circleId, world.carol, "member")
	world.invite(t, world.alice, circleId, world.bob, "member")
	// Renewing it says nothing a second time.
	world.invite(t, world.alice, circleId, world.carol, "admin")

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		main, err := tx.MainAgentConversation(world.carolAgent.ID, "drawer")
		if err != nil {
			t.Fatal(err)
		}
		messages, err := tx.ListAgentMessages(main.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0].Role != models.AgentMessageNote || messages[0].Name != string(models.NoteCircleInvited) {
			t.Fatalf("carol's main conversation holds %+v", messages)
		}
		if got := models.NoteText(models.NoteCircleInvited, messages[0].Content); got != "Circle-alice invited you to Home" {
			t.Errorf("the note says %q", got)
		}
		note := models.ParseCircleInvitedNote(messages[0].Content)
		if note == nil || note.InvitedByName != "Circle-alice" || note.CircleName != "Home" {
			t.Errorf("the note's detail is %q", messages[0].Content)
		}
		// Bob has no agent, so no conversation was made for him.
		if agent, err := tx.GetAgentByUser(world.bob.ID); err != nil || agent != nil {
			t.Errorf("bob has an agent: %v %v", agent, err)
		}
	})
}

// A person who has made as many circles as one person may is told so, in
// words they can act on.
func TestMakingTooManyCirclesIsRefusedInPlainWords(t *testing.T) {
	world := newCircleWorld(t)
	for count := 0; count < models.CirclesMadeMost; count++ {
		world.makeCircle(t, world.alice, fmt.Sprintf("Circle %d", count), "friends")
	}
	message := world.refused(t, world.alice, `mutation { CreateCircle(name: "One too many", circleKind: "friends") { id } }`)
	if !strings.Contains(message, "20 circles") || !strings.Contains(message, "delete one") {
		t.Errorf("the refusal said %q", message)
	}
}
