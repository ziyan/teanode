package apigraph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/search"
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

	// TodayVisits are their visits today, in the caller's time zone, newest
	// first, when they share where they have been; empty at the current-only
	// level.
	TodayVisits []*PlaceVisitView `json:"todayVisits"`

	// HasMoreTodayVisits says there were more visits today than these.
	HasMoreTodayVisits bool `json:"hasMoreTodayVisits"`
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
	Photos        []*models.Photo        `json:"photos"`
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

	// Snippet is what the caller's agent wrote about the mail, on one line
	// without Markdown marks; empty when the agent has not read it.
	Snippet string `json:"snippet"`
}

// PersonCardRecordingSection is the caller's recordings the person speaks
// in: those with a passage spoken by a speaker the caller named and linked
// to their contact for the person.
type PersonCardRecordingSection struct {
	SectionState PersonCardSectionState     `json:"sectionState"`
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

	// IsMatchedByName says the page is not tied to the contact: it is the
	// person page with the contact's name, shown while nothing is tied.
	IsMatchedByName bool `json:"isMatchedByName"`
}

const (
	// personCardItemLimitDefault is how many items a section keeps when the
	// caller names no number, and personCardItemLimitMost the most it can be
	// asked for.
	personCardItemLimitDefault = 3
	personCardItemLimitMost    = 10

	// personCardHealthItemLimitLeast is the fewest health kinds the card
	// keeps: a day's summary of one kind is a few words, so a short card
	// still shows the steps and the kinds beside them.
	personCardHealthItemLimitLeast = 4

	// personCardSectionTimeout is how long a section has. A card is only as
	// slow as its slowest section, so one that has not answered by then is
	// reported as failed instead of holding the rest back.
	personCardSectionTimeout = 3 * time.Second
)

// personCardSubject is who a card is of, found once before the sections are
// read side by side. Nothing in it changes after that.
type personCardSubject struct {
	caller *models.User

	// callerLocation is the caller's time zone, which "today" is counted in.
	callerLocation *time.Location

	// accountUserID is the account the caller shares a circle with, empty
	// for a contact that is not one.
	accountUserID string

	// contact is the caller's contact for the person, nil when there is
	// none. isContactUsable says the caller may keep contacts at all, so
	// that no contact means "link one" only to somebody who can.
	contact         *models.Contact
	isContactUsable bool

	// isAgent says the person's agent is reading the card, which keeps to
	// what the person gave it: the address books and mailboxes they granted
	// it, and no health but the person's own.
	isAgent bool
}

// personCardItemLimitOf is the number of items to keep in each section for the
// number a caller asked for.
func personCardItemLimitOf(asked int) int {
	switch {
	case asked <= 0:
		return personCardItemLimitDefault
	case asked > personCardItemLimitMost:
		return personCardItemLimitMost
	}
	return asked
}

// ReadPersonCard is the operation. It is one of isModelBackedQuery, though it
// asks no model: it holds no transaction of its own across its work, because
// the person is found in one transaction and every section reads in another of
// its own, side by side, and a transaction shared between them could only be
// used by one at a time.
func (self *graph) ReadPersonCard(ctx context.Context, arguments ReadPersonCardArguments) (*PersonCardView, error) {
	userId := strings.TrimSpace(arguments.UserID)
	addressBookId, contactId := strings.TrimSpace(arguments.AddressBookID), strings.TrimSpace(arguments.ContactID)
	isAccount, isContact := userId != "", addressBookId != "" || contactId != ""
	if isAccount == isContact {
		return nil, fmt.Errorf("%w: give either userId, or addressBookId and contactId", api.ErrInvalidArguments)
	}
	if isContact && (addressBookId == "" || contactId == "") {
		return nil, fmt.Errorf("%w: a contact is named by addressBookId and contactId together", api.ErrInvalidArguments)
	}
	itemLimit := personCardItemLimitOf(arguments.ItemLimit)
	var card *PersonCardView
	var subject *personCardSubject
	if err := self.readInPhase(ctx, func(phaseContext context.Context) (err error) {
		// The caller is known only inside a phase. Who the person is decides
		// more, in findPersonCardSubject; this turns away a caller without a
		// session before any of it is read.
		if _, err := self.requireSignedIn(phaseContext); err != nil {
			return err
		}
		card, subject, err = self.findPersonCardSubject(phaseContext, userId, addressBookId, contactId)
		return err
	}); err != nil {
		return nil, err
	}

	var group sync.WaitGroup
	for _, read := range []func(){
		func() {
			card.Location = personCardSection(self, ctx, "location", func(phaseContext context.Context) (*PersonCardLocationSection, error) {
				return self.readPersonCardLocation(phaseContext, subject, itemLimit)
			}, newPersonCardLocationSection(PersonCardSectionStateFailed))
		},
		func() {
			card.Health = personCardSection(self, ctx, "health", func(phaseContext context.Context) (*PersonCardHealthSection, error) {
				return self.readPersonCardHealth(phaseContext, subject, itemLimit)
			}, newPersonCardHealthSection(PersonCardSectionStateFailed))
		},
		func() {
			card.Photos = personCardSection(self, ctx, "photos", func(phaseContext context.Context) (*PersonCardPhotosSection, error) {
				return self.readPersonCardPhotos(phaseContext, subject, itemLimit)
			}, newPersonCardPhotosSection(PersonCardSectionStateFailed))
		},
		func() {
			card.Mail = personCardSection(self, ctx, "mail", func(phaseContext context.Context) (*PersonCardMailSection, error) {
				return self.readPersonCardMail(phaseContext, subject, itemLimit)
			}, newPersonCardMailSection(PersonCardSectionStateFailed))
		},
		func() {
			card.Recordings = personCardSection(self, ctx, "recordings", func(phaseContext context.Context) (*PersonCardRecordingSection, error) {
				return self.readPersonCardRecordings(phaseContext, subject, itemLimit)
			}, newPersonCardRecordingSection(PersonCardSectionStateFailed))
		},
		func() {
			card.Memory = personCardSection(self, ctx, "memory", func(phaseContext context.Context) (*PersonCardMemorySection, error) {
				return self.readPersonCardMemory(phaseContext, subject, itemLimit)
			}, newPersonCardMemorySection(PersonCardSectionStateFailed))
		},
	} {
		group.Add(1)
		go func() {
			defer group.Done()
			read()
		}()
	}
	group.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return card, nil
}

// personCardSection reads one section in a transaction of its own, with the
// caller's permissions read afresh in it, under the time a section has. A
// section that fails or runs out of time is logged and answered as failed, so
// that the rest of the card still shows.
func personCardSection[Section any](self *graph, ctx context.Context, sectionName string, read func(phaseContext context.Context) (*Section, error), failed *Section) *Section {
	section, err := readPersonCardSection(ctx, personCardSectionTimeout, func(sectionContext context.Context) (*Section, error) {
		var section *Section
		err := self.readInPhase(sectionContext, func(phaseContext context.Context) (err error) {
			section, err = read(phaseContext)
			return err
		})
		return section, err
	})
	if err != nil {
		log.Warningf("the %s part of a person's card could not be read: %s", sectionName, err)
		return failed
	}
	return section
}

// readPersonCardSection runs a read under its time limit. It stops waiting at
// the limit even when the read does not stop, so a read that ignores its
// context cannot hold the card up; its goroutine ends on its own and writes
// into a channel nobody is left reading, and what it read is not used.
func readPersonCardSection[Section any](ctx context.Context, timeout time.Duration, read func(sectionContext context.Context) (*Section, error)) (*Section, error) {
	sectionContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type outcome struct {
		section *Section
		err     error
	}
	answered := make(chan outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				answered <- outcome{err: fmt.Errorf("it panicked: %v", recovered)}
			}
		}()
		section, err := read(sectionContext)
		answered <- outcome{section: section, err: err}
	}()
	select {
	case found := <-answered:
		return found.section, found.err
	case <-sectionContext.Done():
		// It may have answered in the same instant the time ran out.
		select {
		case found := <-answered:
			return found.section, found.err
		default:
		}
		return nil, fmt.Errorf("it did not answer in time: %w", sectionContext.Err())
	}
}

func newPersonCardLocationSection(sectionState PersonCardSectionState) *PersonCardLocationSection {
	return &PersonCardLocationSection{SectionState: sectionState, TodayVisits: []*PlaceVisitView{}}
}

func newPersonCardHealthSection(sectionState PersonCardSectionState) *PersonCardHealthSection {
	return &PersonCardHealthSection{SectionState: sectionState, HealthTypes: []*HealthTypeView{}}
}

func newPersonCardPhotosSection(sectionState PersonCardSectionState) *PersonCardPhotosSection {
	return &PersonCardPhotosSection{PhotosState: sectionState, Photos: []*models.Photo{}, AlbumsState: sectionState, Albums: []*models.PhotoAlbum{}}
}

func newPersonCardMailSection(sectionState PersonCardSectionState) *PersonCardMailSection {
	return &PersonCardMailSection{SectionState: sectionState, Mails: []*PersonCardMailView{}}
}

func newPersonCardRecordingSection(sectionState PersonCardSectionState) *PersonCardRecordingSection {
	return &PersonCardRecordingSection{SectionState: sectionState, Recordings: []*PersonCardRecordingView{}}
}

func newPersonCardMemorySection(sectionState PersonCardSectionState) *PersonCardMemorySection {
	return &PersonCardMemorySection{SectionState: sectionState, Pages: []*PersonCardMemoryPage{}}
}

// isRefusedAsUnavailable says the caller may not use a part of the app: what
// the helpers that decide access answer to them (not found, not signed in, the
// service off, no agent that is on), which a card leaves out of the part as
// unavailable rather than failed.
func isRefusedAsUnavailable(err error) bool {
	return errors.Is(refusedAsUnavailable(err), search.ErrNotAvailable)
}

// findPersonCardSubject is who the card is of, and the part of the card that
// needs no section: the person, the contact, the circles the two share and
// whether a chat can be opened. Anybody the caller has no way to know is not
// found.
func (self *graph) findPersonCardSubject(ctx context.Context, userId, addressBookId, contactId string) (*PersonCardView, *personCardSubject, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if principal.User == nil {
		return nil, nil, api.ErrNotFound
	}
	caller := principal.User
	tx := self.transaction(ctx)
	card := &PersonCardView{SharedCircles: []*PersonCardCircleView{}}
	subject := &personCardSubject{caller: caller, callerLocation: time.UTC, isAgent: db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent}
	grantedAddressBookIds, err := self.agentGrantedAddressBookIds(ctx, caller.ID)
	if err != nil {
		return nil, nil, err
	}
	if caller.Timezone != "" {
		if location, err := time.LoadLocation(caller.Timezone); err == nil {
			subject.callerLocation = location
		}
	}

	if userId != "" {
		// As OpenDirectChat says of a chat with oneself.
		if userId == caller.ID {
			return nil, nil, fmt.Errorf("%w: a card is of somebody else", api.ErrInvalidArguments)
		}
		if _, err := self.requireCirclePerson(ctx); err != nil {
			return nil, nil, err
		}
		isFound, err := self.fillPersonCardAccount(ctx, card, subject, userId)
		if err != nil {
			return nil, nil, err
		}
		if !isFound {
			return nil, nil, api.ErrNotFound
		}
		if _, err := self.requireAddressBookPerson(ctx); err == nil {
			subject.isContactUsable = true
			if subject.contact, err = tx.FindContactByAccount(caller.ID, userId); err != nil {
				return nil, nil, err
			}
			// The agent reads a contact only in a book the person gave it.
			if subject.contact != nil && grantedAddressBookIds != nil && !grantedAddressBookIds[subject.contact.AddressBookID] {
				subject.contact = nil
			}
		} else if !isRefusedAsUnavailable(err) {
			return nil, nil, err
		}
	} else {
		book, err := self.requireOwnAddressBook(ctx, addressBookId)
		if err != nil {
			return nil, nil, err
		}
		if grantedAddressBookIds != nil && !grantedAddressBookIds[book.ID] {
			return nil, nil, api.ErrNotFound
		}
		contact, err := tx.GetContact(book.ID, contactId)
		if err != nil {
			return nil, nil, err
		}
		if contact == nil {
			return nil, nil, api.ErrNotFound
		}
		subject.isContactUsable, subject.contact = true, contact
		// A contact linked to an account the caller still shares a circle
		// with is that account's card too, for a caller who may use circles.
		if contact.AccountUserID != "" {
			if _, err := self.requireCirclePerson(ctx); err == nil {
				if _, err := self.fillPersonCardAccount(ctx, card, subject, contact.AccountUserID); err != nil {
					return nil, nil, err
				}
			} else if !isRefusedAsUnavailable(err) {
				return nil, nil, err
			}
		}
	}

	if subject.contact != nil {
		if card.Contact, err = self.contactViewWithAccount(ctx, caller.ID, subject.contact, false); err != nil {
			return nil, nil, err
		}
	}
	return card, subject, nil
}

// fillPersonCardAccount fills in what hangs on the account: the person as the
// caller's circles show them, the circles the two share, and whether a chat
// with them can be opened. It says false, and leaves the card as it was, when
// the two share no circle.
func (self *graph) fillPersonCardAccount(ctx context.Context, card *PersonCardView, subject *personCardSubject, accountUserId string) (bool, error) {
	tx := self.transaction(ctx)
	caller := subject.caller
	people, err := tx.ListCirclePeople(caller.ID)
	if err != nil {
		return false, err
	}
	var member *models.CircleMember
	for _, each := range people {
		if each.UserID == accountUserId {
			member = each
			break
		}
	}
	if member == nil {
		return false, nil
	}
	relationships, err := self.relationshipsOf(ctx, caller.ID)
	if err != nil {
		return false, err
	}
	linkedContactIds, err := self.linkedContactIdsOf(ctx, caller.ID)
	if err != nil {
		return false, err
	}
	circles, err := tx.ListCircles(caller.ID)
	if err != nil {
		return false, err
	}
	sharedCircles := make([]*PersonCardCircleView, 0, len(circles))
	for _, circle := range circles {
		if circle.Member(accountUserId) != nil {
			sharedCircles = append(sharedCircles, &PersonCardCircleView{CircleID: circle.ID, CircleName: circle.Name, CircleKind: circle.CircleKind})
		}
	}
	// What OpenDirectChat would allow: a chat that exists can be opened by
	// either of its people, and a new one only with somebody who can use
	// circles.
	canMessage := false
	if _, err := self.requireCirclePerson(ctx); err == nil {
		existing, err := tx.GetDirectChat(caller.ID, accountUserId)
		if err != nil {
			return false, err
		}
		if canMessage = existing != nil; !canMessage {
			if _, err := self.chatContact(tx, caller, accountUserId); err == nil {
				canMessage = true
			} else if !errors.Is(err, api.ErrNotFound) {
				return false, err
			}
		}
	}
	subject.accountUserID = accountUserId
	card.Person = describeCircleMember(member, caller.ID, relationships, linkedContactIds)
	card.SharedCircles = sharedCircles
	card.CanMessage = canMessage
	return true, nil
}

// personCardDay is the day that holds the moment, in the time zone: from its
// midnight to the next.
func personCardDay(moment time.Time, location *time.Location) (time.Time, time.Time) {
	local := moment.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return start, start.AddDate(0, 0, 1)
}

// readPersonCardLocation is where they are, and their visits today when they
// share where they have been, each as Location shows it.
func (self *graph) readPersonCardLocation(ctx context.Context, subject *personCardSubject, itemLimit int) (*PersonCardLocationSection, error) {
	section := newPersonCardLocationSection(PersonCardSectionStateUnavailable)
	if subject.accountUserID == "" {
		return section, nil
	}
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return refusedPersonCardSection(section, err)
	}
	reach, err := self.reachLocation(ctx, principal, subject.accountUserID, models.LocationShareLevelCurrent)
	if errors.Is(err, api.ErrNotFound) {
		section.SectionState = PersonCardSectionStateNotShared
		return section, nil
	}
	if err != nil {
		return nil, err
	}
	if section.Location, err = describeLocation(self.transaction(ctx), reach); err != nil {
		return nil, err
	}
	section.SectionState = PersonCardSectionStateShown
	if !reach.ShareLevel.AtLeast(models.LocationShareLevelHistory) {
		return section, nil
	}
	dayStart, dayEnd := personCardDay(time.Now(), subject.callerLocation)
	asked := itemLimit
	visits, err := self.ListLocationVisits(ctx, ListLocationVisitsArguments{UserID: subject.accountUserID, From: &dayStart, Until: &dayEnd, LimitCount: &asked})
	if err != nil {
		return nil, err
	}
	section.TodayVisits, section.HasMoreTodayVisits = visits.PlaceVisits, visits.NextOffset != nil
	return section, nil
}

// readPersonCardHealth is the kinds of health they share that have a day
// summary, as the Health page lists them, with the steps first.
func (self *graph) readPersonCardHealth(ctx context.Context, subject *personCardSubject, itemLimit int) (*PersonCardHealthSection, error) {
	section := newPersonCardHealthSection(PersonCardSectionStateUnavailable)
	// The agent reads the person's own Health only, and only when they let
	// it (canAgentReadHealth); what others share is for the person to see.
	if subject.accountUserID == "" || subject.isAgent {
		return section, nil
	}
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return refusedPersonCardSection(section, err)
	}
	if _, err := self.reachHealth(ctx, principal, subject.accountUserID); errors.Is(err, api.ErrNotFound) {
		section.SectionState = PersonCardSectionStateNotShared
		return section, nil
	} else if err != nil {
		return nil, err
	}
	listed, err := self.ListHealthTypes(ctx, ListHealthTypesArguments{OwnerUserID: subject.accountUserID})
	if err != nil {
		return nil, err
	}
	kept := make([]*HealthTypeView, 0, len(listed))
	for _, each := range listed {
		if each.LatestDaySummary != nil {
			kept = append(kept, each)
		}
	}
	// The list is in the Health page's order already; the steps lead it.
	sort.SliceStable(kept, func(left, right int) bool {
		return kept[left].HealthType == "stepCount" && kept[right].HealthType != "stepCount"
	})
	limit := max(itemLimit, personCardHealthItemLimitLeast)
	section.SectionState, section.HasMore = PersonCardSectionStateShown, len(kept) > limit
	section.HealthTypes = kept[:min(len(kept), limit)]
	return section, nil
}

// readPersonCardPhotos is recent photos of them in the caller's library, found
// by the face group their contact names, and the albums they own that are
// shared with the caller.
func (self *graph) readPersonCardPhotos(ctx context.Context, subject *personCardSubject, itemLimit int) (*PersonCardPhotosSection, error) {
	section := newPersonCardPhotosSection(PersonCardSectionStateUnavailable)
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return refusedPersonCardSection(section, err)
	}
	tx := self.transaction(ctx)
	switch {
	case !subject.isContactUsable:
	case subject.contact == nil:
		section.PhotosState = PersonCardSectionStateNeedsContact
	default:
		photos, err := tx.ListPhotos(principal.User.ID, &db.PhotoFilter{
			PageSize: itemLimit + 1, ContactID: subject.contact.ID, ContactAddressBookID: subject.contact.AddressBookID,
		})
		if err != nil {
			return nil, translateError(err)
		}
		section.HasMorePhotos = len(photos) > itemLimit
		photos = photos[:min(len(photos), itemLimit)]
		if err := self.withFilesAndPreviews(ctx, principal.User.ID, photos, false); err != nil {
			return nil, translateError(err)
		}
		section.PhotosState, section.Photos = PersonCardSectionStateShown, photos
	}

	if subject.accountUserID == "" {
		return section, nil
	}
	albums, err := tx.ListPhotoAlbums(principal.User.ID)
	if err != nil {
		return nil, translateError(err)
	}
	withCoverAddresses(albums...)
	shared := make([]*models.PhotoAlbum, 0, len(albums))
	for _, album := range albums {
		if album.OwnerUserID != nil && *album.OwnerUserID == subject.accountUserID && !album.IsOwnedByCaller {
			shared = append(shared, album)
		}
	}
	sort.SliceStable(shared, func(left, right int) bool { return shared[left].CreatedAt.After(shared[right].CreatedAt) })
	section.AlbumsState, section.HasMoreAlbums = PersonCardSectionStateShown, len(shared) > itemLimit
	section.Albums = shared[:min(len(shared), itemLimit)]
	return section, nil
}

// readPersonCardMail is the caller's recent mail from or to any address on
// their contact for the person, across all their mailboxes.
func (self *graph) readPersonCardMail(ctx context.Context, subject *personCardSubject, itemLimit int) (*PersonCardMailSection, error) {
	section := newPersonCardMailSection(PersonCardSectionStateUnavailable)
	if err := self.requireService(models.ServiceMailboxes); err != nil {
		return refusedPersonCardSection(section, err)
	}
	principal, err := self.requirePermission(ctx, models.PermissionMailRead)
	if err != nil {
		return refusedPersonCardSection(section, err)
	}
	if principal.User == nil || !subject.isContactUsable {
		return section, nil
	}
	if subject.contact == nil {
		section.SectionState = PersonCardSectionStateNeedsContact
		return section, nil
	}
	tx := self.transaction(ctx)
	// The agent reads only the mailboxes the person granted it.
	var mailboxIds []string
	if subject.isAgent {
		mailboxes, err := tx.ListMailboxes(principal.User.ID)
		if err != nil {
			return nil, err
		}
		mailboxIds = []string{}
		for _, mailbox := range mailboxes {
			if mailbox.Agent != nil && mailbox.Agent.Granted {
				mailboxIds = append(mailboxIds, mailbox.ID)
			}
		}
	}
	found, hasMore, err := tx.ListMailWithAddresses(principal.User.ID, mailboxIds, subject.contact.Emails, itemLimit)
	if err != nil {
		return nil, err
	}

	// The agent's one-line summary stands for the start of the text, which is
	// kept with the message in the blob store and is not read for a card.
	mailIdsOfMailbox := map[string][]string{}
	for _, each := range found {
		mailIdsOfMailbox[each.MailboxID] = append(mailIdsOfMailbox[each.MailboxID], each.Item.MailID)
	}
	insights := map[string]*models.MailInsight{}
	for mailboxId, mailIds := range mailIdsOfMailbox {
		insightsOfMailbox, err := tx.GetMailInsights(mailboxId, mailIds)
		if err != nil {
			return nil, err
		}
		for mailId, insight := range insightsOfMailbox {
			insights[mailboxId+"\x00"+mailId] = insight
		}
	}
	folders := map[string]*models.MailboxFolder{}
	folderPathOf := func(folderId string) (string, error) {
		path := ""
		for folderId != "" {
			folder, isKept := folders[folderId]
			if !isKept {
				var err error
				if folder, err = tx.GetFolder(folderId); err != nil {
					return "", err
				}
				folders[folderId] = folder
			}
			if folder == nil {
				break
			}
			if path == "" {
				path = folder.Name
			} else {
				path = folder.Name + "/" + path
			}
			folderId = folder.ParentID
		}
		return path, nil
	}
	for _, each := range found {
		mail := each.Item.Mail
		folderPath, err := folderPathOf(each.Item.FolderID)
		if err != nil {
			return nil, err
		}
		view := &PersonCardMailView{
			MailboxItemID: each.Item.ID, MailboxID: each.MailboxID, FolderPath: folderPath,
			Subject: strings.TrimSpace(mail.Subject), FromAddress: mail.From, FromName: mail.FromName,
			IsFromPerson: each.IsFromAddress, IsUnread: !each.Item.Seen, ReceivedAt: mail.ReceivedAt,
		}
		if insight := insights[each.MailboxID+"\x00"+each.Item.MailID]; insight != nil {
			view.Snippet = snippetOf(insight.Summary)
		}
		section.Mails = append(section.Mails, view)
	}
	section.SectionState, section.HasMore = PersonCardSectionStateShown, hasMore
	return section, nil
}

// readPersonCardRecordings is the caller's recordings with a passage spoken by
// a speaker they tied to their contact for the person.
func (self *graph) readPersonCardRecordings(ctx context.Context, subject *personCardSubject, itemLimit int) (*PersonCardRecordingSection, error) {
	section := newPersonCardRecordingSection(PersonCardSectionStateUnavailable)
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return refusedPersonCardSection(section, err)
	}
	if principal.User == nil || !subject.isContactUsable {
		return section, nil
	}
	if subject.contact == nil {
		section.SectionState = PersonCardSectionStateNeedsContact
		return section, nil
	}
	spoken, hasMore, err := self.transaction(ctx).ListRecordingsSpokenByContact(principal.User.ID, subject.contact.AddressBookID, subject.contact.ID, itemLimit)
	if err != nil {
		return nil, err
	}
	for _, each := range spoken {
		section.Recordings = append(section.Recordings, &PersonCardRecordingView{
			RecordingID: each.Recording.ID, Title: strings.TrimSpace(each.Recording.RecordingTitle),
			RecordedAt: each.Recording.RecordedAt, DurationMS: each.Recording.TimelineDurationMS,
			SpokenPassageCount: each.SpokenPassageCount,
		})
	}
	section.SectionState, section.HasMore = PersonCardSectionStateShown, hasMore
	return section, nil
}

// readPersonCardMemory is the pages of the caller's memory that are linked to
// their contact for the person, the page that is about the person first and
// then the page changed last.
func (self *graph) readPersonCardMemory(ctx context.Context, subject *personCardSubject, itemLimit int) (*PersonCardMemorySection, error) {
	section := newPersonCardMemorySection(PersonCardSectionStateUnavailable)
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return refusedPersonCardSection(section, err)
	}
	if !subject.isContactUsable {
		return section, nil
	}
	if subject.contact == nil {
		section.SectionState = PersonCardSectionStateNeedsContact
		return section, nil
	}
	tx := self.transaction(ctx)
	links, err := tx.ListAgentRecordBacklinks(found.ID, models.RecordContact, subject.contact.ID, 0, 0, recordLinkPageMost)
	if err != nil {
		return nil, err
	}
	nodeIds := make([]string, 0, len(links))
	seenNodeIds := map[string]bool{}
	for _, link := range links {
		if !seenNodeIds[link.NodeID] {
			seenNodeIds[link.NodeID] = true
			nodeIds = append(nodeIds, link.NodeID)
		}
	}
	linkedNodes, err := tx.GetAgentNodes(found.ID, nodeIds)
	if err != nil {
		return nil, err
	}
	// The pages that say they are about the contact, and the pages that
	// link to it, each once.
	nodes, err := tx.ListAgentNodesOfContact(found.ID, subject.contact.ID, recordLinkPageMost)
	if err != nil {
		return nil, err
	}
	seenNodeIds = map[string]bool{}
	for _, node := range nodes {
		seenNodeIds[node.ID] = true
	}
	for _, node := range linkedNodes {
		if !seenNodeIds[node.ID] {
			seenNodeIds[node.ID] = true
			nodes = append(nodes, node)
		}
	}
	// Nothing tied to the contact yet (the dreams tie a person page to its
	// contact when they come to it): the person page with the contact's own
	// name, if it is not another contact's, marked as found by its name.
	isMatchedByName := false
	if len(nodes) == 0 {
		named, err := tx.FindAgentNodeByName(found.ID, "people", models.NodePerson, subject.contact.Name)
		if err != nil {
			return nil, err
		}
		if named != nil {
			isOtherContacts, err := self.isTiedToAnotherContact(ctx, subject, named)
			if err != nil {
				return nil, err
			}
			if !isOtherContacts {
				nodes, isMatchedByName = []*models.AgentNode{named}, true
			}
		}
	}
	sort.SliceStable(nodes, func(left, right int) bool {
		isLeftPerson, isRightPerson := nodes[left].Kind == models.NodePerson, nodes[right].Kind == models.NodePerson
		if isLeftPerson != isRightPerson {
			return isLeftPerson
		}
		return nodes[left].ModifiedAt.After(nodes[right].ModifiedAt)
	})
	section.SectionState, section.HasMore = PersonCardSectionStateShown, len(nodes) > itemLimit
	for _, node := range nodes[:min(len(nodes), itemLimit)] {
		section.Pages = append(section.Pages, &PersonCardMemoryPage{
			NodePath: node.Path, NodeName: firstNonEmpty(node.Name, node.Path), NodeKind: node.Kind,
			Summary: snippetOf(node.Summary), UpdatedAt: node.ModifiedAt, IsMatchedByName: isMatchedByName,
		})
	}
	return section, nil
}

// isTiedToAnotherContact says a page is about another of the caller's
// contacts. A tie to a contact that is gone (deleted, or an address book read
// in again under new identifiers) ties it to nobody.
func (self *graph) isTiedToAnotherContact(ctx context.Context, subject *personCardSubject, node *models.AgentNode) (bool, error) {
	if node.ContactID == "" || node.ContactID == subject.contact.ID {
		return false, nil
	}
	tx := self.transaction(ctx)
	books, err := tx.ListAddressBooks(subject.caller.ID)
	if err != nil {
		return false, err
	}
	for _, book := range books {
		other, err := tx.GetContact(book.ID, node.ContactID)
		if err != nil {
			return false, err
		}
		if other != nil {
			return true, nil
		}
	}
	return false, nil
}

// refusedPersonCardSection is the section a card shows for a part of the app
// the caller may not use: left out, as unavailable. Any other error is a
// failure of the section.
func refusedPersonCardSection[Section any](section *Section, err error) (*Section, error) {
	if isRefusedAsUnavailable(err) {
		return section, nil
	}
	return nil, err
}
