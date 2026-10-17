package models

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// CircleKind is what a circle is for. It is how screens describe the circle
// ("your family", "your colleagues") where the generic word would read
// oddly, and it decides nothing else.
type CircleKind string

// The kinds of circle.
const (
	CircleKindFamily     CircleKind = "family"
	CircleKindFriends    CircleKind = "friends"
	CircleKindColleagues CircleKind = "colleagues"
)

// IsValid reports whether this is one of the kinds.
func (self CircleKind) IsValid() bool {
	switch self {
	case CircleKindFamily, CircleKindFriends, CircleKindColleagues:
		return true
	}
	return false
}

// MemberRole is what somebody may do in a circle: an admin manages who is in
// it and its name, a member takes part. A role never decides what anybody may
// see of anybody else's; that is always the owner's choice.
type MemberRole string

// The member roles.
const (
	MemberRoleAdmin  MemberRole = "admin"
	MemberRoleMember MemberRole = "member"
)

// IsValid reports whether this is one of the roles.
func (self MemberRole) IsValid() bool {
	return self == MemberRoleAdmin || self == MemberRoleMember
}

// InvitationAnswer is how an invitation ended; empty while it waits.
type InvitationAnswer string

// The answers. Withdrawn is the inviting circle's own, before the invited
// person said anything.
const (
	InvitationAnswerAccepted  InvitationAnswer = "accepted"
	InvitationAnswerDeclined  InvitationAnswer = "declined"
	InvitationAnswerWithdrawn InvitationAnswer = "withdrawn"
)

// CircleInvitationLifetime is how long an invitation can be accepted.
const CircleInvitationLifetime = 30 * 24 * time.Hour

// CircleNameLength is the most characters a circle's name may have.
const CircleNameLength = 100

// The most one person does with circles. Making circles and inviting people
// to them is what a stranger on a server with open accounts would do first,
// and each invitation puts a card in someone's conversation with their agent,
// so neither is without a bound.
const (
	// CirclesMadeMost is how many circles one person made and still has.
	CirclesMadeMost = 20

	// CircleInvitationsPerHourMost is how many calls that invite somebody one
	// person makes in an hour, whether the invitation is new, renewed, or
	// none at all because nobody has that name.
	CircleInvitationsPerHourMost = 30
)

// Circle is a named set of accounts on one server that know each other.
// Everything this server shares between people is shared with a circle or
// with a person in one.
type Circle struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"createdAt"`
	ModifiedAt time.Time  `json:"modifiedAt"`
	Name       string     `json:"name"`
	CircleKind CircleKind `json:"circleKind"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	// Members is filled in by the queries that return a whole circle, the
	// admins first and then in the order they joined.
	Members []*CircleMember `json:"members,omitempty"`
}

// PlainCircleName is a circle's name as it is kept: on one line, with every
// control character and run of blanks made a single space. Whoever made the
// circle typed it, and it is shown to every member on cards and in the
// model's view, where a line break would pass for a line of its own.
func PlainCircleName(name string) string {
	cleaned := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, name)
	return strings.Join(strings.Fields(cleaned), " ")
}

// Validate reports everything wrong with the circle.
func (self *Circle) Validate() error {
	var errors ValidationErrors
	name := PlainCircleName(self.Name)
	if name == "" {
		errors.add("name", "required")
	} else if utf8.RuneCountInString(name) > CircleNameLength {
		errors.add("name", "must be at most %d characters", CircleNameLength)
	}
	if !self.CircleKind.IsValid() {
		errors.add("circleKind", "%q is not a kind of circle; say family, friends or colleagues", self.CircleKind)
	}
	return errors.ErrOrNil()
}

// Member is the row of one person in the circle, or nil.
func (self *Circle) Member(userId string) *CircleMember {
	for _, member := range self.Members {
		if member.UserID == userId {
			return member
		}
	}
	return nil
}

// AdminCount is how many of its members are admins.
func (self *Circle) AdminCount() int {
	count := 0
	for _, member := range self.Members {
		if member.MemberRole == MemberRoleAdmin {
			count++
		}
	}
	return count
}

// CircleMember is one person in one circle.
type CircleMember struct {
	CircleID        string     `json:"circleId"`
	UserID          string     `json:"userId"`
	MemberRole      MemberRole `json:"memberRole"`
	JoinedAt        time.Time  `json:"joinedAt"`
	InvitedByUserID string     `json:"invitedByUserId,omitempty"`

	// User is the account, filled in by the queries that return a whole
	// circle so that a screen can name and draw the person.
	User *User `json:"-"`
}

// CircleInvitation asks one person to join one circle.
type CircleInvitation struct {
	ID              string     `json:"id"`
	CircleID        string     `json:"circleId"`
	InvitedUserID   string     `json:"invitedUserId"`
	InvitedByUserID string     `json:"invitedByUserId,omitempty"`
	MemberRole      MemberRole `json:"memberRole"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`

	// AnsweredAt and InvitationAnswer are empty while it waits.
	AnsweredAt       *time.Time       `json:"answeredAt,omitempty"`
	InvitationAnswer InvitationAnswer `json:"invitationAnswer,omitempty"`

	// The rest is read along with the row, for the screens that list
	// invitations: the circle it is to, who asked and who was asked.
	CircleName        string     `json:"circleName,omitempty"`
	CircleKind        CircleKind `json:"circleKind,omitempty"`
	InvitedByUsername string     `json:"invitedByUsername,omitempty"`
	InvitedByName     string     `json:"invitedByName,omitempty"`
	InvitedUsername   string     `json:"invitedUsername,omitempty"`
}

// IsOpen says whether it can still be answered at this moment.
func (self *CircleInvitation) IsOpen(now time.Time) bool {
	return self.AnsweredAt == nil && now.Before(self.ExpiresAt)
}
