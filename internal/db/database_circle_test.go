package db_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func newCircleTestUser(t *testing.T, tx db.Transaction, username string) *models.User {
	t.Helper()
	user, err := tx.CreateUser(&models.User{Username: username})
	if err != nil {
		t.Fatalf("CreateUser(%s): %s", username, err)
	}
	return user
}

// The rules that depend on the other members hold on the members as they
// stand: an admin is always left, and the last member leaving takes the
// circle with them.
func TestACircleKeepsAnAdminAndGoesWithItsLastMember(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "circle-maker")
		friend := newCircleTestUser(t, tx, "circle-friend")
		circle, err := tx.CreateCircle(&models.Circle{Name: " Home ", CircleKind: models.CircleKindFamily}, maker.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if circle.Name != "Home" || len(circle.Members) != 1 || circle.Members[0].MemberRole != models.MemberRoleAdmin {
			t.Fatalf("a new circle is %+v", circle)
		}
		if _, err := tx.CreateCircle(&models.Circle{Name: "", CircleKind: models.CircleKindFamily}, maker.ID); err == nil {
			t.Error("a circle with no name was made")
		}
		if _, err := tx.CreateCircle(&models.Circle{Name: "x", CircleKind: "club"}, maker.ID); err == nil {
			t.Error("a circle of a kind that is not one was made")
		}

		circle, err = tx.AddCircleMember(circle.ID, friend.ID, models.MemberRoleMember, maker.ID)
		if err != nil || len(circle.Members) != 2 {
			t.Fatalf("AddCircleMember: %v %+v", err, circle)
		}
		// Adding somebody who is in already changes nothing, not even their role.
		circle, err = tx.AddCircleMember(circle.ID, friend.ID, models.MemberRoleAdmin, maker.ID)
		if err != nil || circle.Member(friend.ID).MemberRole != models.MemberRoleMember {
			t.Fatalf("adding a member twice: %v %+v", err, circle.Member(friend.ID))
		}

		if _, err := tx.RemoveCircleMember(circle.ID, maker.ID); !errors.Is(err, db.ErrCircleNeedsAdmin) {
			t.Errorf("the last admin left with others remaining: %v", err)
		}
		if _, err := tx.SetCircleMemberRole(circle.ID, maker.ID, models.MemberRoleMember); !errors.Is(err, db.ErrCircleNeedsAdmin) {
			t.Errorf("the last admin was made a member: %v", err)
		}

		circle, err = tx.SetCircleMemberRole(circle.ID, friend.ID, models.MemberRoleAdmin)
		if err != nil || circle.AdminCount() != 2 {
			t.Fatalf("SetCircleMemberRole: %v %+v", err, circle)
		}
		circle, err = tx.RemoveCircleMember(circle.ID, maker.ID)
		if err != nil || len(circle.Members) != 1 || circle.Members[0].UserID != friend.ID {
			t.Fatalf("an admin leaving once another is: %v %+v", err, circle)
		}
		if _, err := tx.RemoveCircleMember(circle.ID, maker.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("removing somebody who is not in it: %v", err)
		}

		circleId := circle.ID
		circle, err = tx.RemoveCircleMember(circleId, friend.ID)
		if err != nil || circle != nil {
			t.Fatalf("the last member leaving: %v %+v", err, circle)
		}
		if found, err := tx.GetCircle(circleId); err != nil || found != nil {
			t.Errorf("the circle is still there: %v %+v", err, found)
		}
	})
}

// An invitation can be answered once, while it is still open, and a waiting
// one is renewed rather than doubled.
func TestAnInvitationIsAnsweredOnceAndWhileItIsOpen(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "invite-maker")
		guest := newCircleTestUser(t, tx, "invite-guest")
		circle, err := tx.CreateCircle(&models.Circle{Name: "Team", CircleKind: models.CircleKindColleagues}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}

		now := time.Now()
		invitation := &models.CircleInvitation{
			CircleID: circle.ID, InvitedUserID: guest.ID, InvitedByUserID: maker.ID, MemberRole: models.MemberRoleMember,
			CreatedAt: now, ExpiresAt: now.Add(models.CircleInvitationLifetime),
		}
		created, isNew, err := tx.CreateCircleInvitation(invitation)
		if err != nil || !isNew || created.CircleName != "Team" || created.InvitedUsername != "invite-guest" || created.InvitedByUsername != "invite-maker" {
			t.Fatalf("CreateCircleInvitation: %v %v %+v", err, isNew, created)
		}
		invitation.MemberRole = models.MemberRoleAdmin
		renewed, isNew, err := tx.CreateCircleInvitation(invitation)
		if err != nil || isNew || renewed.ID != created.ID || renewed.MemberRole != models.MemberRoleAdmin {
			t.Fatalf("inviting again: %v %v %+v", err, isNew, renewed)
		}
		waiting, err := tx.ListCircleInvitationsTo(guest.ID, now)
		if err != nil || len(waiting) != 1 {
			t.Fatalf("ListCircleInvitationsTo: %v %+v", err, waiting)
		}
		if of, err := tx.ListCircleInvitationsOf(circle.ID, now); err != nil || len(of) != 1 {
			t.Fatalf("ListCircleInvitationsOf: %v %+v", err, of)
		}
		// Past its lifetime it is not listed and cannot be answered.
		later := now.Add(models.CircleInvitationLifetime + time.Minute)
		if expired, err := tx.ListCircleInvitationsTo(guest.ID, later); err != nil || len(expired) != 0 {
			t.Errorf("an expired invitation was listed: %v %+v", err, expired)
		}
		if _, err := tx.AnswerCircleInvitation(created.ID, models.InvitationAnswerAccepted, later); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("an expired invitation was accepted: %v", err)
		}

		joined, err := tx.AnswerCircleInvitation(created.ID, models.InvitationAnswerAccepted, now)
		if err != nil || joined == nil || joined.Member(guest.ID) == nil || joined.Member(guest.ID).MemberRole != models.MemberRoleAdmin {
			t.Fatalf("accepting: %v %+v", err, joined)
		}
		if _, err := tx.AnswerCircleInvitation(created.ID, models.InvitationAnswerDeclined, now); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("an answered invitation was answered again: %v", err)
		}
		if again, isNew, err := tx.CreateCircleInvitation(invitation); err != nil || isNew || again != nil {
			t.Errorf("inviting a member: %v %v %+v", err, isNew, again)
		}
		if waiting, err := tx.ListCircleInvitationsTo(guest.ID, now); err != nil || len(waiting) != 0 {
			t.Errorf("an answered invitation is still listed: %v %+v", err, waiting)
		}
	})
}

// Declining, withdrawing and being added by an operator each close the
// invitation, and only accepting or being added puts anybody in.
func TestOnlyAcceptingOrBeingAddedPutsAnybodyInACircle(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "answer-maker")
		decliner := newCircleTestUser(t, tx, "answer-decliner")
		added := newCircleTestUser(t, tx, "answer-added")
		circle, err := tx.CreateCircle(&models.Circle{Name: "Friends", CircleKind: models.CircleKindFriends}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		invite := func(user *models.User) *models.CircleInvitation {
			created, _, err := tx.CreateCircleInvitation(&models.CircleInvitation{
				CircleID: circle.ID, InvitedUserID: user.ID, InvitedByUserID: maker.ID, MemberRole: models.MemberRoleMember,
				CreatedAt: now, ExpiresAt: now.Add(models.CircleInvitationLifetime),
			})
			if err != nil {
				t.Fatal(err)
			}
			return created
		}

		declined := invite(decliner)
		if joined, err := tx.AnswerCircleInvitation(declined.ID, models.InvitationAnswerDeclined, now); err != nil || joined != nil {
			t.Fatalf("declining: %v %+v", err, joined)
		}
		withdrawn := invite(decliner)
		if joined, err := tx.AnswerCircleInvitation(withdrawn.ID, models.InvitationAnswerWithdrawn, now); err != nil || joined != nil {
			t.Fatalf("withdrawing: %v %+v", err, joined)
		}
		if current, _ := tx.GetCircle(circle.ID); len(current.Members) != 1 {
			t.Fatalf("somebody joined without accepting: %+v", current.Members)
		}

		pending := invite(added)
		if _, err := tx.AddCircleMember(circle.ID, added.ID, models.MemberRoleMember, ""); err != nil {
			t.Fatal(err)
		}
		closed, err := tx.GetCircleInvitation(pending.ID)
		if err != nil || closed.AnsweredAt == nil || closed.InvitationAnswer != models.InvitationAnswerAccepted {
			t.Errorf("an invitation to somebody added directly is %+v, %v", closed, err)
		}
		if _, err := tx.AddCircleMember(circle.ID, "no-such-user", models.MemberRoleMember, ""); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("adding an account that is not there: %v", err)
		}
	})
}

// Who shares a circle with whom is what later features authorize on.
func TestWhoSharesACircle(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		first := newCircleTestUser(t, tx, "share-first")
		second := newCircleTestUser(t, tx, "share-second")
		third := newCircleTestUser(t, tx, "share-third")
		stranger := newCircleTestUser(t, tx, "share-stranger")
		home, err := tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		work, err := tx.CreateCircle(&models.Circle{Name: "Work", CircleKind: models.CircleKindColleagues}, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, circle := range []*models.Circle{home, work} {
			if _, err := tx.AddCircleMember(circle.ID, second.ID, models.MemberRoleMember, first.ID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.AddCircleMember(work.ID, third.ID, models.MemberRoleMember, first.ID); err != nil {
			t.Fatal(err)
		}

		for _, expectation := range []struct {
			one, other *models.User
			want       bool
		}{{first, second, true}, {second, first, true}, {second, third, true}, {first, stranger, false}, {first, first, false}} {
			if got, err := tx.SharesCircle(expectation.one.ID, expectation.other.ID); err != nil || got != expectation.want {
				t.Errorf("SharesCircle(%s, %s) = %v, %v; want %v", expectation.one.Username, expectation.other.Username, got, err, expectation.want)
			}
		}
		userIds, err := tx.CircleUserIDs(first.ID)
		if err != nil || len(userIds) != 2 {
			t.Fatalf("CircleUserIDs: %v %v", err, userIds)
		}
		people, err := tx.ListCirclePeople(first.ID)
		if err != nil || len(people) != 2 {
			t.Fatalf("ListCirclePeople lists somebody twice or not at all: %v %+v", err, people)
		}
		for _, person := range people {
			if person.MemberRole != "" || person.User == nil {
				t.Errorf("a person in the list is %+v", person)
			}
		}
		if circles, err := tx.ListCircles(second.ID); err != nil || len(circles) != 2 || circles[0].Name != "Home" {
			t.Fatalf("ListCircles: %v %+v", err, circles)
		}
		if circles, err := tx.ListCircles(stranger.ID); err != nil || len(circles) != 0 {
			t.Fatalf("a stranger's circles: %v %+v", err, circles)
		}
	})
}

// Deleting an account leaves no circle without an admin or without members.
func TestDeletingAnAccountLeavesNoCircleWithoutAnAdmin(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "delete-maker")
		member := newCircleTestUser(t, tx, "delete-member")
		shared, err := tx.CreateCircle(&models.Circle{Name: "Shared", CircleKind: models.CircleKindFamily}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}
		alone, err := tx.CreateCircle(&models.Circle{Name: "Alone", CircleKind: models.CircleKindFriends}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddCircleMember(shared.ID, member.ID, models.MemberRoleMember, maker.ID); err != nil {
			t.Fatal(err)
		}

		if err := tx.DeleteUser(maker.ID); err != nil {
			t.Fatalf("DeleteUser: %s", err)
		}
		left, err := tx.GetCircle(shared.ID)
		if err != nil || left == nil || len(left.Members) != 1 || left.Members[0].MemberRole != models.MemberRoleAdmin {
			t.Fatalf("the circle after its only admin was deleted: %v %+v", err, left)
		}
		if gone, err := tx.GetCircle(alone.ID); err != nil || gone != nil {
			t.Errorf("a circle nobody is in was kept: %v %+v", err, gone)
		}
	})
}

// One person makes only so many circles, counting the ones they made that
// still exist, and sends only so many invitations an hour.
func TestCirclesAndInvitationsAreBoundedForOnePerson(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "bound-maker")
		other := newCircleTestUser(t, tx, "bound-other")
		var circles []*models.Circle
		for count := 0; count < models.CirclesMadeMost; count++ {
			circle, err := tx.CreateCircle(&models.Circle{Name: "Circle", CircleKind: models.CircleKindFriends}, maker.ID)
			if err != nil {
				t.Fatalf("circle %d: %s", count+1, err)
			}
			circles = append(circles, circle)
		}
		_, err := tx.CreateCircle(&models.Circle{Name: "One too many", CircleKind: models.CircleKindFriends}, maker.ID)
		if !errors.Is(err, db.ErrLimitReached) || !strings.Contains(err.Error(), "20 circles") {
			t.Fatalf("the twenty-first circle: %v", err)
		}
		// Somebody else makes theirs, and deleting one makes room.
		if _, err := tx.CreateCircle(&models.Circle{Name: "Theirs", CircleKind: models.CircleKindFriends}, other.ID); err != nil {
			t.Errorf("another person's first circle: %v", err)
		}
		if err := tx.DeleteCircle(circles[0].ID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateCircle(&models.Circle{Name: "Room again", CircleKind: models.CircleKindFriends}, maker.ID); err != nil {
			t.Errorf("after deleting one: %v", err)
		}

		// Invitations, to one circle, from one person.
		inviting := circles[1]
		now := time.Now()
		invite := func(userId string, sentAt time.Time) error {
			_, _, err := tx.CreateCircleInvitation(&models.CircleInvitation{
				CircleID: inviting.ID, InvitedUserID: userId, InvitedByUserID: maker.ID, MemberRole: models.MemberRoleMember,
				CreatedAt: sentAt, ExpiresAt: sentAt.Add(models.CircleInvitationLifetime),
			})
			return err
		}
		var invited []*models.User
		for count := 0; count < models.CircleInvitationsPerHourMost; count++ {
			person := newCircleTestUser(t, tx, fmt.Sprintf("bound-invited-%02d", count))
			invited = append(invited, person)
			if err := invite(person.ID, now); err != nil {
				t.Fatalf("invitation %d: %s", count+1, err)
			}
		}
		extra := newCircleTestUser(t, tx, "bound-invited-extra")
		if err := invite(extra.ID, now); !errors.Is(err, db.ErrLimitReached) || !strings.Contains(err.Error(), "30 invitations") {
			t.Errorf("the thirty-first invitation: %v", err)
		}
		// A renewal is one more card as well.
		if err := invite(invited[0].ID, now); !errors.Is(err, db.ErrLimitReached) {
			t.Errorf("renewing one: %v", err)
		}
		// An hour later they are not counted.
		if err := invite(extra.ID, now.Add(61*time.Minute)); err != nil {
			t.Errorf("an hour on: %v", err)
		}
		// Somebody else has their own allowance.
		fresh := newCircleTestUser(t, tx, "bound-invited-fresh")
		if _, _, err := tx.CreateCircleInvitation(&models.CircleInvitation{
			CircleID: inviting.ID, InvitedUserID: fresh.ID, InvitedByUserID: other.ID, MemberRole: models.MemberRoleMember,
			CreatedAt: now, ExpiresAt: now.Add(models.CircleInvitationLifetime),
		}); err != nil {
			t.Errorf("another person's first invitation: %v", err)
		}
	})
}

// Two people who share two circles each leave one of them at the same time.
// Each leaving ends what only the circle it leaves made possible, on the
// circles as that transaction sees them, and each used to see the other
// person still in the circle they were about to leave: so the share and the
// group chat between the two, who then shared no circle, were both kept.
// Leaving takes turns now, and the second leaving sees the first.
func TestTwoPeopleLeavingTwoCirclesAtOnceLeaveNoShareOrGroupChatBetweenStrangers(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var ada, ben, adaCalendarID, groupChatID, firstCircleID, secondCircleID string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		adaUser := newCircleTestUser(t, tx, "parallel-ada")
		benUser := newCircleTestUser(t, tx, "parallel-ben")
		cora := newCircleTestUser(t, tx, "parallel-cora")
		dave := newCircleTestUser(t, tx, "parallel-dave")
		ada, ben = adaUser.ID, benUser.ID
		// Cora and Dave keep the two circles alive.
		first, err := tx.CreateCircle(&models.Circle{Name: "First", CircleKind: models.CircleKindFriends}, cora.ID)
		if err != nil {
			t.Fatal(err)
		}
		second, err := tx.CreateCircle(&models.Circle{Name: "Second", CircleKind: models.CircleKindFriends}, dave.ID)
		if err != nil {
			t.Fatal(err)
		}
		firstCircleID, secondCircleID = first.ID, second.ID
		for _, circleID := range []string{first.ID, second.ID} {
			for _, userID := range []string{ada, ben} {
				if _, err := tx.AddCircleMember(circleID, userID, models.MemberRoleMember, ""); err != nil {
					t.Fatal(err)
				}
			}
		}
		calendar, err := tx.CreateCalendar(&models.Calendar{UserID: ada, Name: "Calendar"})
		if err != nil {
			t.Fatal(err)
		}
		adaCalendarID = calendar.ID
		if _, err := tx.ShareCalendar(ada, &models.CalendarShare{CalendarID: calendar.ID, GranteeUserID: ben, ShareLevel: models.ShareLevelDetails}); err != nil {
			t.Fatal(err)
		}
		group, err := tx.CreateGroupChat(ada, "Trip", []string{ben})
		if err != nil {
			t.Fatal(err)
		}
		groupChatID = group.ID
	})

	// The first transaction has left and has not committed yet when the
	// second starts to leave.
	firstHasLeft := make(chan struct{})
	mayCommit := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- database.Transaction(func(tx db.Transaction) error {
			if _, err := tx.RemoveCircleMember(firstCircleID, ada); err != nil {
				return err
			}
			close(firstHasLeft)
			<-mayCommit
			return nil
		})
	}()
	<-firstHasLeft
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.RemoveCircleMember(secondCircleID, ben)
			return err
		})
	}()
	// The second waits for the first, which is the point; if it does not, it
	// finishes at once, having looked at the circles without the first's leaving.
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
		t.Log("the second leaving did not wait for the first")
		close(mayCommit)
	case <-time.After(500 * time.Millisecond):
		close(mayCommit)
		if err := <-secondDone; err != nil {
			t.Fatal(err)
		}
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if shares, err := tx.SharesCircle(ada, ben); err != nil || shares {
			t.Fatalf("Ada and Ben still share a circle: %v %v", shares, err)
		}
		calendarShares, err := tx.ListCalendarShares(adaCalendarID)
		if err != nil || len(calendarShares) != 0 {
			t.Errorf("Ada's calendar is still shared with Ben: %v %v", calendarShares, err)
		}
		group, err := tx.GetChat(groupChatID)
		if err != nil || group == nil {
			t.Fatalf("GetChat: %v %v", group, err)
		}
		adaIn, benIn := group.Member(ada), group.Member(ben)
		if adaIn != nil && !adaIn.IsRemoved && benIn != nil && !benIn.IsRemoved {
			t.Errorf("Ada and Ben, who share no circle, are still together in the group chat")
		}
	})
}
