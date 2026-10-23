package models

import (
	"encoding/json"
	"strings"
	"time"
)

// ShareKind names a kind of thing a person shares with other people, for the
// one place that lists them all (the Sharing page, "teanode sharing") and for
// the notes that tell a person something was shared with them.
type ShareKind string

// The kinds. Each is shared by its own table, with its own rules; a skill is
// shared with one person only, and passwords a whole item at a time. A finance
// share is of one account, and is the person's own tap to make.
const (
	ShareKindCalendar   ShareKind = "calendar"
	ShareKindLocation   ShareKind = "location"
	ShareKindHealth     ShareKind = "health"
	ShareKindMemoryPage ShareKind = "memory_page"
	ShareKindSkill      ShareKind = "skill"
	ShareKindPhotoAlbum ShareKind = "photo_album"
	ShareKindRecording  ShareKind = "recording"
	ShareKindFinance    ShareKind = "finance"
	ShareKindPasswords  ShareKind = "passwords"
)

// ShareKinds is every kind, in the order the Sharing page lists them.
var ShareKinds = []ShareKind{
	ShareKindCalendar, ShareKindLocation, ShareKindHealth, ShareKindMemoryPage,
	ShareKindPhotoAlbum, ShareKindRecording, ShareKindFinance, ShareKindSkill, ShareKindPasswords,
}

// IsValid reports whether this is one of the kinds.
func (self ShareKind) IsValid() bool {
	for _, kind := range ShareKinds {
		if kind == self {
			return true
		}
	}
	return false
}

// ShareRow is one share of one thing, read for the list of everything shared:
// what it is, who owns it, who it is for and how much it gives. The owner is
// the person whose thing it is; for the shares a person made, that is them.
type ShareRow struct {
	ShareKind ShareKind
	ShareID   string

	// SubjectID and SubjectName are the calendar, the album, the page, the
	// skill, the recording or the finance account shared: its identifier and
	// what it is called (a recording's title, an account's name). Both are
	// empty for a person's location and Health, which are shared whole.
	// SubjectMask is the last digits of a finance account's number, when the
	// provider gave them, and empty for every other kind.
	SubjectID   string
	SubjectName string
	SubjectMask string

	// IsSubjectDeleted is true for a share of a memory page that no longer
	// exists, which its owner can still stop. SubjectName is then the path.
	IsSubjectDeleted bool

	OwnerUserID   string
	OwnerUsername string
	OwnerName     string
	OwnerUser     *User

	// GranteeUserID and GranteeCircleID are who it is shared with: one of
	// them, never both.
	GranteeUserID     string
	GranteeUsername   string
	GranteeName       string
	GranteeCircleID   string
	GranteeCircleName string

	// ShareLevel is the level of a calendar, location, album, recording or
	// finance share, and empty for the other kinds. ShareScopes are the rest of what it gives: the
	// Health categories, the skill's tools, "quotes" for a page shared with
	// the words each fact was read in, "location" for an album shared with
	// where its photos were taken.
	ShareLevel  string
	ShareScopes []string

	// ItemCount is how many password items it reaches, for the passwords kind
	// only: an item's title is sealed, so a share of passwords is counted and
	// never named.
	ItemCount int

	CreatedAt time.Time
}

// CircleShareOwner is a member of a circle and the kinds of thing they have
// shared with that circle, so that a person who joins it can be told to the
// owner.
type CircleShareOwner struct {
	OwnerUserID string
	ShareKinds  []ShareKind
}

// AlbumShareScopes is what an album share gives beyond its level: where its
// photos were taken, when the share includes that.
func AlbumShareScopes(isLocationIncluded, isFaceNamesIncluded bool) []string {
	scopes := []string{}
	if isLocationIncluded {
		scopes = append(scopes, "location")
	}
	if isFaceNamesIncluded {
		scopes = append(scopes, "faceNames")
	}
	return scopes
}

// PageShareScopes is what a page share gives beyond the page: the words each
// fact was read in, when the share includes them.
func PageShareScopes(isQuoteShared bool) []string {
	if isQuoteShared {
		return []string{"quotes"}
	}
	return []string{}
}

// ShareReceivedNote is the detail of a share_received note: who shared what
// with the person, as JSON so that each client words it in its own language.
// ShareLevel is the level of a calendar, location, album, recording or finance
// share (busy, titles, details, edit; current, history; view, contribute;
// summary, transcript; balance, spending, transactions) and empty for the rest.
// ShareSubject is the page's name, the album's title, the skill's name or the
// recording's title. A recording's title is the sharer's own text and may
// summarize what other people said, and a finance share names no account: the
// note goes into the conversation of the person's agent, which has no part in
// either, so it says that a recording or an account was shared and leaves out
// which.
type ShareReceivedNote struct {
	SharerName   string    `json:"sharerName"`
	ShareKind    ShareKind `json:"shareKind"`
	ShareLevel   string    `json:"shareLevel,omitempty"`
	ShareSubject string    `json:"shareSubject,omitempty"`

	// ShareSubjectID is which thing was shared (the calendar, page path, skill
	// name, album, recording or finance account), only to tell two shares of
	// one kind at one level apart when deciding whether a person was just told.
	// It is not sent to a client: a name or an identifier the person has no
	// part in does not belong in a conversation.
	ShareSubjectID string `json:"-"`

	// ShareScopes are what else it gives: the Health categories, quotes for a
	// page shared with the words each fact was read in, location for an album
	// shared with where its photos were taken.
	ShareScopes []string `json:"shareScopes,omitempty"`
}

// ShareReceivedDetail is the detail of a share_received note.
func ShareReceivedDetail(note ShareReceivedNote) string {
	encoded, err := json.Marshal(note)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// ParseShareReceivedNote reads the detail of a share_received note, or
// returns nil when it is not one.
func ParseShareReceivedNote(detail string) *ShareReceivedNote {
	var note ShareReceivedNote
	if err := json.Unmarshal([]byte(detail), &note); err != nil || note.SharerName == "" || !note.ShareKind.IsValid() {
		return nil
	}
	return &note
}

// ShareReachedNote is the detail of a share_reached note: a person joined a
// circle that the reader shares things with, so they can now see those. It is
// one note for each owner, listing the kinds.
type ShareReachedNote struct {
	MemberName string      `json:"memberName"`
	CircleName string      `json:"circleName"`
	ShareKinds []ShareKind `json:"shareKinds"`
}

// ShareReachedDetail is the detail of a share_reached note.
func ShareReachedDetail(note ShareReachedNote) string {
	encoded, err := json.Marshal(note)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// ParseShareReachedNote reads the detail of a share_reached note, or returns
// nil when it is not one.
func ParseShareReachedNote(detail string) *ShareReachedNote {
	var note ShareReachedNote
	if err := json.Unmarshal([]byte(detail), &note); err != nil || note.MemberName == "" || note.CircleName == "" || len(note.ShareKinds) == 0 {
		return nil
	}
	return &note
}

// shareLevelWords is each level of a share in English, for the note and the
// notification.
var shareLevelWords = map[string]string{
	"busy":         "busy times only",
	"titles":       "titles",
	"details":      "details",
	"edit":         "you can edit it",
	"current":      "where they are now",
	"history":      "where they are and where they have been",
	"view":         "you can view it",
	"contribute":   "you can add photos",
	"summary":      "title and summary",
	"transcript":   "summary and transcript",
	"balance":      "balance only",
	"spending":     "balance and spending",
	"transactions": "balance, spending and transactions",
}

// shareKindNouns is what each kind is called in a list: "calendar and
// location".
var shareKindNouns = map[ShareKind]string{
	ShareKindCalendar:   "calendar",
	ShareKindLocation:   "location",
	ShareKindHealth:     "Health",
	ShareKindMemoryPage: "memory pages",
	ShareKindPhotoAlbum: "photo albums",
	ShareKindRecording:  "recordings",
	ShareKindFinance:    "finance",
	ShareKindSkill:      "skills",
	ShareKindPasswords:  "passwords",
}

// ShareReceivedText is a share_received note in English: "Alex shared their
// calendar with you (titles)".
func ShareReceivedText(note *ShareReceivedNote) string {
	level := shareLevelWords[note.ShareLevel]
	withLevel := func(sentence string) string {
		if level == "" {
			return sentence
		}
		return sentence + " (" + level + ")"
	}
	switch note.ShareKind {
	case ShareKindCalendar:
		return withLevel(note.SharerName + " shared their calendar with you")
	case ShareKindLocation:
		return withLevel(note.SharerName + " shared their location with you")
	case ShareKindHealth:
		return note.SharerName + " shared their Health with you"
	case ShareKindMemoryPage:
		return note.SharerName + " shared the memory page " + note.ShareSubject + " with you"
	case ShareKindSkill:
		return note.SharerName + " shared their " + note.ShareSubject + " skill with you"
	case ShareKindPhotoAlbum:
		return withLevel(note.SharerName + " shared the photo album " + note.ShareSubject + " with you")
	case ShareKindRecording:
		if note.ShareSubject == "" {
			return withLevel(note.SharerName + " shared a recording with you")
		}
		return withLevel(note.SharerName + " shared the recording " + note.ShareSubject + " with you")
	case ShareKindFinance:
		return withLevel(note.SharerName + " shared a finance account with you")
	}
	return note.SharerName + " shared something with you"
}

// ShareReachedText is a share_reached note in English: "Sam joined Family and
// can now see your calendar and location".
func ShareReachedText(note *ShareReachedNote) string {
	nouns := make([]string, 0, len(note.ShareKinds))
	for _, kind := range note.ShareKinds {
		nouns = append(nouns, shareKindNouns[kind])
	}
	listed := strings.Join(nouns, ", ")
	if last := len(nouns) - 1; last > 0 {
		listed = strings.Join(nouns[:last], ", ") + " and " + nouns[last]
	}
	return note.MemberName + " joined " + note.CircleName + " and can now see your " + listed
}
