package models

import "time"

// ShareLevel is how much of a calendar somebody may see, and whether they may
// change it. A person shares their calendar with a circle or with one person
// in a circle they share, at one of the first four. docs/decisions/
// 20261016-a-calendar-is-shared-at-a-level.md says why these four.
type ShareLevel string

// The levels, from least to most. ShareLevelOwner is not a level anybody can
// be given: it is what a person's own calendar is to them, so that "may this
// person do this to this calendar" has one answer for the owner too.
const (
	// ShareLevelBusy is when the person is busy and nothing else: the same
	// answer free-busy gives, from the same code.
	ShareLevelBusy ShareLevel = "busy"
	// ShareLevelTitles adds the time, title and location of each event.
	ShareLevelTitles ShareLevel = "titles"
	// ShareLevelDetails is everything in the event, including its
	// description and its guests.
	ShareLevelDetails ShareLevel = "details"
	// ShareLevelEdit is details, and the reader may add, change and remove
	// events. A circle's own calendar is edit for every member.
	ShareLevelEdit ShareLevel = "edit"
	// ShareLevelOwner is the calendar's owner.
	ShareLevelOwner ShareLevel = "owner"
)

// IsGrantable reports whether a share may be made at this level.
func (self ShareLevel) IsGrantable() bool {
	switch self {
	case ShareLevelBusy, ShareLevelTitles, ShareLevelDetails, ShareLevelEdit:
		return true
	}
	return false
}

// Rank orders the levels; zero is no access at all.
func (self ShareLevel) Rank() int {
	switch self {
	case ShareLevelBusy:
		return 1
	case ShareLevelTitles:
		return 2
	case ShareLevelDetails:
		return 3
	case ShareLevelEdit:
		return 4
	case ShareLevelOwner:
		return 5
	}
	return 0
}

// AtLeast reports whether this level reaches the other.
func (self ShareLevel) AtLeast(other ShareLevel) bool {
	return self.Rank() >= other.Rank() && self.Rank() > 0
}

// CanRead reports whether the level shows events at all, as events rather than
// as busy time.
func (self ShareLevel) CanRead() bool { return self.AtLeast(ShareLevelTitles) }

// CanWrite reports whether the level may add, change and remove events.
func (self ShareLevel) CanWrite() bool { return self.AtLeast(ShareLevelEdit) }

// HighestShareLevel is the greater of two levels, so that a person reached by
// both a direct share and a circle's takes the better of them.
func HighestShareLevel(first, second ShareLevel) ShareLevel {
	if second.Rank() > first.Rank() {
		return second
	}
	return first
}

// CalendarShare gives one person, or every member of one circle, a level of
// access to one calendar. Exactly one of GranteeUserID and GranteeCircleID is
// set.
type CalendarShare struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendarId"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	ShareLevel ShareLevel `json:"shareLevel"`

	CreatedAt time.Time `json:"createdAt"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	// GranteeUsername, GranteeName and GranteeCircleName are filled in by the
	// listing, for a screen that says who a calendar is shared with.
	GranteeUsername   string `json:"granteeUsername,omitempty"`
	GranteeName       string `json:"granteeName,omitempty"`
	GranteeCircleName string `json:"granteeCircleName,omitempty"`
}

// ReachableCalendar is a calendar as one person reaches it: their own, a
// circle's they are in, or another person's shared with them.
type ReachableCalendar struct {
	Calendar *Calendar

	// ShareLevel is what the person may do with it, ShareLevelOwner for their
	// own.
	ShareLevel ShareLevel

	// OwnerUserID and Owner say whose it is; empty and nil for a circle's
	// calendar.
	OwnerUserID string
	Owner       *User

	// CircleName is the circle's name for a circle calendar.
	CircleName string
}
