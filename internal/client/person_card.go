package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A person's card: who somebody is to the signed-in account, and a few items
// of everything of theirs the account can already see, in one answer. It is
// the operation behind `teanode circle person`.

// PersonCardSectionState says whether a section has something to show: shown,
// notShared, needsContact, unavailable or failed.
type PersonCardSectionState string

const (
	PersonCardSectionStateShown        PersonCardSectionState = "shown"
	PersonCardSectionStateNotShared    PersonCardSectionState = "notShared"
	PersonCardSectionStateNeedsContact PersonCardSectionState = "needsContact"
	PersonCardSectionStateUnavailable  PersonCardSectionState = "unavailable"
	PersonCardSectionStateFailed       PersonCardSectionState = "failed"
)

// PersonCard is one person's card.
type PersonCard struct {
	// Person is the account, null for a contact that is not linked to one.
	Person *CircleMember `json:"person"`

	// Contact is the signed-in account's own contact for them, null when
	// there is none.
	Contact *Contact `json:"contact"`

	SharedCircles []*PersonCardCircle `json:"sharedCircles"`
	CanMessage    bool                `json:"canMessage"`

	Location   *PersonCardLocation  `json:"location"`
	Health     *PersonCardHealth    `json:"health"`
	Photos     *PersonCardPhotos    `json:"photos"`
	Mail       *PersonCardMail      `json:"mail"`
	Recordings *PersonCardRecording `json:"recordings"`
	Memory     *PersonCardMemory    `json:"memory"`
}

// PersonCardCircle is a circle the two share.
type PersonCardCircle struct {
	CircleID   string `json:"circleId"`
	CircleName string `json:"circleName"`
	CircleKind string `json:"circleKind"`
}

// PersonCardLocation is where they are and their visits today.
type PersonCardLocation struct {
	SectionState       PersonCardSectionState `json:"sectionState"`
	Location           *Location              `json:"location"`
	TodayVisits        []*PlaceVisit          `json:"todayVisits"`
	HasMoreTodayVisits bool                   `json:"hasMoreTodayVisits"`
}

// PersonCardHealth is the health they share, one day of each kind.
type PersonCardHealth struct {
	SectionState PersonCardSectionState `json:"sectionState"`
	HealthTypes  []*HealthTypeView      `json:"healthTypes"`
	HasMore      bool                   `json:"hasMore"`
}

// PersonCardPhotos is photos of them in the library, and the albums they
// shared.
type PersonCardPhotos struct {
	PhotosState   PersonCardSectionState `json:"photosState"`
	Photos        []*models.Photo        `json:"photos"`
	HasMorePhotos bool                   `json:"hasMorePhotos"`
	AlbumsState   PersonCardSectionState `json:"albumsState"`
	Albums        []*models.PhotoAlbum   `json:"albums"`
	HasMoreAlbums bool                   `json:"hasMoreAlbums"`
}

// PersonCardMail is recent mail from or to the addresses on their contact.
type PersonCardMail struct {
	SectionState PersonCardSectionState `json:"sectionState"`
	Mails        []*PersonCardMailItem  `json:"mails"`
	HasMore      bool                   `json:"hasMore"`
}

// PersonCardMailItem is one mail with the person.
type PersonCardMailItem struct {
	MailboxItemID string    `json:"mailboxItemId"`
	MailboxID     string    `json:"mailboxId"`
	FolderPath    string    `json:"folderPath"`
	Subject       string    `json:"subject"`
	FromAddress   string    `json:"fromAddress"`
	FromName      string    `json:"fromName"`
	IsFromPerson  bool      `json:"isFromPerson"`
	IsUnread      bool      `json:"isUnread"`
	ReceivedAt    time.Time `json:"receivedAt"`
	Snippet       string    `json:"snippet"`
}

// PersonCardRecording is the recordings they speak in.
type PersonCardRecording struct {
	SectionState PersonCardSectionState     `json:"sectionState"`
	Recordings   []*PersonCardRecordingItem `json:"recordings"`
	HasMore      bool                       `json:"hasMore"`
}

// PersonCardRecordingItem is one recording they speak in.
type PersonCardRecordingItem struct {
	RecordingID        string     `json:"recordingId"`
	Title              string     `json:"title"`
	RecordedAt         *time.Time `json:"recordedAt"`
	DurationMS         int64      `json:"durationMS"`
	SpokenPassageCount int        `json:"spokenPassageCount"`
}

// PersonCardMemory is the memory pages about them.
type PersonCardMemory struct {
	SectionState PersonCardSectionState  `json:"sectionState"`
	Pages        []*PersonCardMemoryPage `json:"pages"`
	HasMore      bool                    `json:"hasMore"`
}

// PersonCardMemoryPage is one memory page about them.
type PersonCardMemoryPage struct {
	NodePath  string    `json:"nodePath"`
	NodeName  string    `json:"nodeName"`
	NodeKind  string    `json:"nodeKind"`
	Summary   string    `json:"summary"`
	UpdatedAt time.Time `json:"updatedAt"`

	// IsMatchedByName says the page is not tied to the contact: it is the
	// person page with the contact's name.
	IsMatchedByName bool `json:"isMatchedByName"`
}

const DocumentReadPersonCard = `query ($userId: String, $addressBookId: String, $contactId: String, $itemLimit: Int) {
  ReadPersonCard(userId: $userId, addressBookId: $addressBookId, contactId: $contactId, itemLimit: $itemLimit) {
    person ` + circleMemberFields + `
    contact {
      id uid name organization emails phones note hasPhoto
      addresses { label street locality region postalCode country written }
    }
    sharedCircles { circleId circleName circleKind }
    canMessage
    location {
      sectionState hasMoreTodayVisits
      location {
        userId username name shareLevel
        latestPoint { ` + locationPointFields + ` }
        openVisit { ` + placeVisitFields + ` }
      }
      todayVisits { ` + placeVisitFields + ` }
    }
    health {
      sectionState hasMore
      healthTypes {
        healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
        latestDaySummary { ` + healthDayFields + ` }
      }
    }
    photos {
      photosState hasMorePhotos albumsState hasMoreAlbums
      photos ` + photoSelection + `
      albums ` + photoAlbumSelection + `
    }
    mail {
      sectionState hasMore
      mails { mailboxItemId mailboxId folderPath subject fromAddress fromName isFromPerson isUnread receivedAt snippet }
    }
    recordings {
      sectionState hasMore
      recordings { recordingId title recordedAt durationMS spokenPassageCount }
    }
    memory {
      sectionState hasMore
      pages { nodePath nodeName nodeKind summary updatedAt isMatchedByName }
    }
  }
}`

// ReadPersonCard reads the card of an account the signed-in account shares a
// circle with, or of one of its own contacts when addressBookId and contactId
// are given in place of userId. itemLimit is how many items each section
// keeps; zero is the server's default.
func ReadPersonCard(ctx context.Context, connection Executor, userId, addressBookId, contactId string, itemLimit int) (*PersonCard, error) {
	variables := map[string]any{}
	if userId != "" {
		variables["userId"] = userId
	}
	if addressBookId != "" {
		variables["addressBookId"] = addressBookId
	}
	if contactId != "" {
		variables["contactId"] = contactId
	}
	if itemLimit > 0 {
		variables["itemLimit"] = itemLimit
	}
	var result struct {
		ReadPersonCard *PersonCard `json:"ReadPersonCard"`
	}
	if err := connection.Execute(ctx, DocumentReadPersonCard, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadPersonCard, nil
}

// FindCirclePersonID turns what the command line speaks in, a username or an
// account's identifier, into the identifier the schema takes, from everybody
// the signed-in account shares a circle with.
func FindCirclePersonID(ctx context.Context, connection Executor, usernameOrID string) (string, error) {
	usernameOrID = strings.TrimSpace(usernameOrID)
	people, err := ListCirclePeople(ctx, connection)
	if err != nil {
		return "", err
	}
	for _, person := range people {
		if strings.EqualFold(person.Username, usernameOrID) {
			return person.UserID, nil
		}
	}
	for _, person := range people {
		if person.UserID == usernameOrID {
			return person.UserID, nil
		}
	}
	return "", fmt.Errorf("client: nobody called %q shares a circle with you", usernameOrID)
}
