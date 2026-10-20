package apigraph

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A person's card: who somebody is to the caller, and everything of theirs
// the caller can already see elsewhere, a few items of each, in one answer.
// Tapping a person anywhere opens it: a chat's author, a row on the Location
// map, a member of a circle, a contact.
//
// The card is of an account the caller shares a circle with, or of one of the
// caller's own contacts, which may be linked to such an account. Sections
// that hang on the account (where they are, their health, the albums they
// shared, a chat) need the account and its shares; sections that hang on the
// caller's contact for them (contact details, mail, photos of them, memory,
// recordings) need the contact. Each section is read through the same access
// check as the part of the app it stands for, so the card never shows more
// than that part would.

// PersonCardQuery reads a person's card.
type PersonCardQuery interface {
	// Reads one person's card: their name and picture, what they are to the
	// caller, the circles the two share, the caller's contact details for
	// them, and a few items from each part of the app that concerns them:
	// where they are and their visits today, the health they share, photos
	// of them and albums they shared, mail with them, recordings they speak
	// in, and memory pages about them. Give either userId, an account that
	// shares a circle with the caller, or addressBookId and contactId, one of
	// the caller's contacts. Anybody else is not found. Each section is read
	// as its own part of the app reads it, and one that fails or takes longer
	// than three seconds says so without failing the card. itemLimit is how
	// many items each section keeps, 3 by default and 10 at most. Needs a
	// signed-in caller.
	ReadPersonCard(ctx context.Context, arguments ReadPersonCardArguments) (*PersonCardView, error)
}

// ReadPersonCardArguments names the person: an account, or a contact.
type ReadPersonCardArguments struct {
	// UserID is an account that shares a circle with the caller.
	UserID string `json:"userId" graphapi:"nullable"`

	// AddressBookID and ContactID are one of the caller's contacts.
	AddressBookID string `json:"addressBookId" graphapi:"nullable"`
	ContactID     string `json:"contactId" graphapi:"nullable"`

	// ItemLimit is how many items each section keeps, 3 by default and 10
	// at most.
	ItemLimit int `json:"itemLimit" graphapi:"nullable"`
}

// PersonCardSectionState says whether a section has something to show.
//
//   - shown: read; its items may still be empty.
//   - notShared: the account has not shared this part with the caller.
//   - needsContact: the section is found through the caller's contact for
//     the account, and there is none linked.
//   - unavailable: the caller may not use this part of the app, or there is
//     no account to read it from (a contact that is not an account here).
//     A client leaves the section out.
//   - failed: it failed or did not answer in time.
type PersonCardSectionState string

const (
	PersonCardSectionStateShown        PersonCardSectionState = "shown"
	PersonCardSectionStateNotShared    PersonCardSectionState = "notShared"
	PersonCardSectionStateNeedsContact PersonCardSectionState = "needsContact"
	PersonCardSectionStateUnavailable  PersonCardSectionState = "unavailable"
	PersonCardSectionStateFailed       PersonCardSectionState = "failed"
)

// PersonCardView is one person's card.
type PersonCardView struct {
	// Person is the account, as a member of the caller's circles; null for a
	// contact that is not linked to an account the caller shares a circle
	// with. Its relationship fields are only ever between the two of them.
	Person *CircleMember `json:"person"`

	// Contact is the caller's own contact for them: the one asked for, or
	// the one the caller linked to the account. Null when there is none.
	Contact *ContactView `json:"contact"`

	// SharedCircles are the circles both are in, by name; empty for a
	// contact without an account.
	SharedCircles []*PersonCardCircleView `json:"sharedCircles"`

	// CanMessage says a chat with them can be opened (OpenDirectChat would
	// not refuse it).
	CanMessage bool `json:"canMessage"`

	Location   *PersonCardLocationSection  `json:"location"`
	Health     *PersonCardHealthSection    `json:"health"`
	Photos     *PersonCardPhotosSection    `json:"photos"`
	Mail       *PersonCardMailSection      `json:"mail"`
	Recordings *PersonCardRecordingSection `json:"recordings"`
	Memory     *PersonCardMemorySection    `json:"memory"`
}

// PersonCardCircleView is a circle the two share.
type PersonCardCircleView struct {
	CircleID   string            `json:"circleId"`
	CircleName string            `json:"circleName"`
	CircleKind models.CircleKind `json:"circleKind"`
}

// PersonCardLocationSection is where they are, as Location shows it.
type PersonCardLocationSection struct {
	SectionState PersonCardSectionState `json:"sectionState"`

	// Location is their shared location, with its share level, latest point
	// and the visit they are in; null unless shown.
	Location *LocationView `json:"location"`

	// TodayVisits are their visits today, newest first, when they share
	// where they have been; empty at the current-only level.
	TodayVisits []*PlaceVisitView `json:"todayVisits"`
}

// PersonCardHealthSection is the health they share with the caller.
type PersonCardHealthSection struct {
	SectionState PersonCardSectionState `json:"sectionState"`

	// HealthTypes are the kinds they share that have a day summary, each
	// with its latest day, steps first, then by the Health app's categories.
	// A client says "today" only when LatestDaySummary.SummaryDay is today.
	HealthTypes []*HealthTypeView `json:"healthTypes"`
	HasMore     bool              `json:"hasMore"`
}

// PersonCardPhotosSection is photos of them in the caller's library, and the
// albums they shared with the caller.
type PersonCardPhotosSection struct {
	// PhotosState is about Photos: needsContact when no contact is linked
	// to the account (photos of a person are found by the face group named
	// by that contact).
	PhotosState   PersonCardSectionState `json:"photosState"`
	Photos        []*PhotoView           `json:"photos"`
	HasMorePhotos bool                   `json:"hasMorePhotos"`

	// AlbumsState is about the albums they own and shared with the caller,
	// newest first; unavailable for a contact without an account.
	AlbumsState   PersonCardSectionState `json:"albumsState"`
	Albums        []*models.PhotoAlbum   `json:"albums"`
	HasMoreAlbums bool                   `json:"hasMoreAlbums"`
}

// PersonCardMailSection is the caller's recent mail from or to any of the
// addresses on their contact for the person, across all the caller's
// mailboxes. The account's own address is never used: it is private.
type PersonCardMailSection struct {
	SectionState PersonCardSectionState `json:"sectionState"`
	Mails        []*PersonCardMailView  `json:"mails"`
	HasMore      bool                   `json:"hasMore"`
}

// PersonCardMailView is one mail with the person, newest first.
type PersonCardMailView struct {
	MailboxItemID string `json:"mailboxItemId"`
	MailboxID     string `json:"mailboxId"`

	// FolderPath is the folder it is in, as the mailbox pages address it.
	FolderPath string `json:"folderPath"`

	Subject     string `json:"subject"`
	FromAddress string `json:"fromAddress"`
	FromName    string `json:"fromName"`

	// IsFromPerson says the person sent it; otherwise the caller sent it
	// to them.
	IsFromPerson bool `json:"isFromPerson"`

	IsUnread   bool      `json:"isUnread"`
	ReceivedAt time.Time `json:"receivedAt"`

	// Snippet is the start of the text, without Markdown marks.
	Snippet string `json:"snippet"`
}

// PersonCardRecordingSection is the caller's recordings the person speaks
// in: those with a passage spoken by a speaker the caller named and linked
// to their contact for the person.
type PersonCardRecordingSection struct {
	SectionState PersonCardSectionState    `json:"sectionState"`
	Recordings   []*PersonCardRecordingView `json:"recordings"`
	HasMore      bool                       `json:"hasMore"`
}

// PersonCardRecordingView is one recording they speak in, newest first.
type PersonCardRecordingView struct {
	RecordingID string `json:"recordingId"`
	Title       string `json:"title"`

	// RecordedAt is when it was recorded; null when that is not known.
	RecordedAt *time.Time `json:"recordedAt"`
	DurationMS int64      `json:"durationMS"`

	// SpokenPassageCount is how many passages the person speaks.
	SpokenPassageCount int `json:"spokenPassageCount"`
}

// PersonCardMemorySection is the caller's memory pages about the person: the
// pages tied to the caller's contact for them, their own person page first.
type PersonCardMemorySection struct {
	SectionState PersonCardSectionState  `json:"sectionState"`
	Pages        []*PersonCardMemoryPage `json:"pages"`
	HasMore      bool                    `json:"hasMore"`
}

// PersonCardMemoryPage is one memory page about them.
type PersonCardMemoryPage struct {
	NodePath string               `json:"nodePath"`
	NodeName string               `json:"nodeName"`
	NodeKind models.AgentNodeKind `json:"nodeKind"`

	// Summary is the page's first lines, without Markdown marks.
	Summary string `json:"summary"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// ReadPersonCard is implemented in milestone 1 of
// docs/planning/person-card-execplan.md.
func (self *graph) ReadPersonCard(ctx context.Context, arguments ReadPersonCardArguments) (*PersonCardView, error) {
	return nil, errors.New("not yet")
}
