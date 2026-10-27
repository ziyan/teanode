package journal

import (
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Reading a journal day: everything the person's sources hold for it, found
// by the day's time range when the day is read, beside the few things the
// journal keeps itself (the row, the links). Nothing here calls a model, and
// nothing is copied: a section holds the few fields a page shows of a record
// and the identifier that finds the whole of it again.

// Section names, as UnavailableSections lists them.
const (
	SectionCodingSessions = "codingSessions"
	SectionDocuments      = "documents"
	SectionConversations  = "conversations"
	SectionChats          = "chats"
	SectionMail           = "mail"
	SectionRecordings     = "recordings"
	SectionPhotos         = "photos"
	SectionVisits         = "visits"
	SectionEvents         = "events"
	SectionReminders      = "reminders"
	SectionTransactions   = "transactions"
	SectionHealth         = "health"
	SectionNotes          = "notes"
	SectionMemoryPages    = "memoryPages"
)

// The most each capped section keeps. The total of what there was is kept
// beside it.
const (
	PhotosMost    = 200
	DocumentsMost = 200
	MailMost      = 100

	// photoCandidatesMost is how many photos a day offers to stand for it.
	photoCandidatesMost = 12

	// photosScanMost is how many photos of one day are looked at to count them
	// and to spread the candidates; a day with more is a camera left running.
	photosScanMost = 2000

	// documentsScanMost is how many documents of one day are looked at.
	documentsScanMost = 4000
)

// ReadOptions says which sections the caller may read. The caller works it
// out from the person's permissions, because a section of a day is read only
// by somebody who could read its source anywhere else.
type ReadOptions struct {
	// CanReadMail is mail:read, for the mail of the day and the person's notes.
	CanReadMail bool

	// CanReadPhotos is photos:use, CanReadLocation location:use,
	// CanReadCalendar calendar:use (events and reminders), CanReadHealth
	// health:use, CanReadChats circle:use, and CanReadAgent agent:use, which
	// the agent's sources need: coding sessions, documents, conversations,
	// recordings, transactions and the memory pages.
	CanReadPhotos   bool
	CanReadLocation bool
	CanReadCalendar bool
	CanReadHealth   bool
	CanReadChats    bool
	CanReadAgent    bool

	// ShouldReadHealth is false when Health is to be left out although the
	// person may read it: the agent's dream sets it from the person's switch
	// for the agent to read Health (CanAgentReadHealth). The person's own page
	// sets it true.
	ShouldReadHealth bool
}

// AllReadOptions lets the caller read everything, as a test or a person who
// holds every permission does.
func AllReadOptions() ReadOptions {
	return ReadOptions{
		CanReadMail: true, CanReadPhotos: true, CanReadLocation: true, CanReadCalendar: true,
		CanReadHealth: true, CanReadChats: true, CanReadAgent: true, ShouldReadHealth: true,
	}
}

// Day is a journal day as it can be read now.
type Day struct {
	// Day is the date, YYYY-MM-DD, and TimeZone the zone its edges are cut in:
	// the stored row's, else the person's own. From is the day's first moment
	// and Until the next day's.
	Day      string
	TimeZone string
	From     time.Time
	Until    time.Time

	// Now is the moment it was read, which an unfinished visit is counted up
	// to.
	Now time.Time

	// location is the zone the day's edges are cut in.
	location *time.Location

	// Row is what the journal stores for the day, nil when nothing was ever
	// kept for it.
	Row *models.JournalDay

	// Links are the records the agent chose and the person pinned.
	Links []*models.JournalLink

	CodingSessions []CodingSession

	// WorkProjects are the coding sessions and the pull and merge requests of
	// the day, by project: what the day's work was, before its details.
	WorkProjects []WorkProject

	Documents     []Document
	DocumentTotal int

	// JournalNotes are the person's own dated notes from a notes folder, the
	// journal source.
	JournalNotes []JournalNote

	Conversations []Conversation
	Chats         []Chat

	// Mail is the messages that arrived and were sent, at most MailMost of
	// them (the important ones first), earliest first; the totals are of all.
	Mail              []Mail
	MailReceivedTotal int
	MailSentTotal     int

	Recordings []Recording

	// Photos are at most PhotosMost, earliest first. PhotoCandidateIDs are up
	// to twelve spread across the whole day.
	Photos            []Photo
	PhotoTotal        int
	PhotoCandidateIDs []string

	Visits       []Visit
	Events       []Event
	Reminders    []Reminder
	Transactions []Transaction

	// Health is nil when the person has none for the day or it was left out.
	Health *Health

	Notes       []Note
	MemoryPages []MemoryPage

	// UnavailableSections names the sections left out because the caller may
	// not read them.
	UnavailableSections []string
}

// CodingSession is a stretch of work in Claude Code or Codex: an ingested
// chat document whose metadata names an assistant.
type CodingSession struct {
	DocumentID string
	Title      string
	Directory  string

	// Assistant is claude-code or codex.
	Assistant string
	StartedAt time.Time

	// EndedAt is when the last post of the day's last stretch was made, the
	// same as StartedAt for a stretch of one post.
	EndedAt time.Time

	// StretchCount is how many stretches of work (each ended by a pause of
	// half an hour) the day had in this project and title; DocumentID is the
	// first one's.
	StretchCount int

	// StretchDocumentIDs are the stretches, each an ingested document: what
	// was said in the session that day, for whoever reads it.
	StretchDocumentIDs []string

	// PersonSpans are when the person was at it, which the time worked is
	// added up from: each of their posts, run on to their next one when that
	// came within half an hour. An assistant working on its own for hours
	// between their posts is its time, not theirs, and neither are the pauses
	// between stretches.
	PersonSpans [][2]time.Time
}

// Document is an ingested document of the day that involves the person: work
// chat, a wiki page, a shared document.
type Document struct {
	DocumentID string
	Title      string
	SourceName string
	HappenedAt time.Time
	ModifiedAt time.Time
}

// JournalNote is one of the person's own dated notes, from their notes
// folder.
type JournalNote struct {
	DocumentID string
	Title      string
	Text       string
}

// Conversation is a conversation with the agent that had something said in
// it that day.
type Conversation struct {
	ConversationID string

	// Title is the conversation's title; the main conversation is called the
	// main chat (IsMain), whatever title a turn once gave it.
	Title          string
	IsMain         bool
	FirstMessageID string
	FirstAt        time.Time
	LastAt         time.Time
	MessageCount   int
}

// Chat is a circle or group chat that had something said in it that day.
type Chat struct {
	ChatID   string
	ChatName string

	// ConversationID is the person's own conversation for the chat, where its
	// messages open; empty when they have none yet.
	ConversationID string
	FirstMessageID string
	FirstAt        time.Time
	LastAt         time.Time
	MessageCount   int
}

// Mail is a message that arrived or was sent that day.
type Mail struct {
	ItemID string
	MailID string

	// Counterpart is who it is from, for a message that arrived, and who it
	// went to (the first recipient), for one that was sent.
	Counterpart string
	Subject     string
	ReceivedAt  time.Time
	IsSent      bool

	// IsImportant: high priority, needs a reply, or the agent alerted about it
	// or follows it up.
	IsImportant bool

	// RepeatCount is how many messages of the day this one stands for: the
	// same sender and subject arriving again (to another address of the
	// person's, or a code sent over and over) is one line with a count.
	RepeatCount int
}

// Recording is a recording made that day.
type Recording struct {
	RecordingID string
	Title       string
	RecordedAt  time.Time
	UpdatedAt   time.Time
	DurationMS  int64
	Summary     string
}

// Photo is a photo or video taken that day.
type Photo struct {
	PhotoID   string
	TakenAt   time.Time
	PlaceName string
	IsVideo   bool
}

// Visit is a stay at a place that overlaps the day.
type Visit struct {
	VisitID    string
	PlaceID    string
	PlaceName  string
	ArrivedAt  *time.Time
	DepartedAt *time.Time
	Latitude   float64
	Longitude  float64
}

// Event is an occurrence of a calendar event that day.
type Event struct {
	EventID      string
	Title        string
	CalendarName string
	StartsAt     time.Time
	EndsAt       time.Time
	IsAllDay     bool
	Location     string
	ModifiedAt   time.Time
}

// Reminder is a reminder due or done that day.
type Reminder struct {
	ReminderID string
	Title      string
	DueAt      *time.Time
	DoneAt     *time.Time
	ModifiedAt time.Time
}

// Transaction is money in or out that day.
type Transaction struct {
	TransactionID string
	PostedOn      string
	TransactedAt  *time.Time
	Description   string
	AccountName   string

	// AmountMinorUnits is negative for money spent.
	AmountMinorUnits int64
	Currency         string
}

// Note is a note of the person's mailbox written or changed that day.
type Note struct {
	NoteID string

	// FolderID and ItemID are where the newest version of the note is filed,
	// which is where it opens.
	FolderID   string
	ItemID     string
	Title      string
	ModifiedAt time.Time
}

// MemoryPage is a memory page with facts that happened that day.
type MemoryPage struct {
	NodeID    string
	Path      string
	Title     string
	FactCount int
}

// Health is a day's Health, from the summaries the phone sent and the
// readings kept.
type Health struct {
	StepCount int

	// StepsByHour is 24 counts from midnight in the day's zone, nil when the
	// readings are not kept.
	StepsByHour []int

	WalkedMeters float64

	// SleepMinutes and SleepStages are the night that ended that morning: the
	// phone files a night under the day it ends.
	SleepMinutes int
	SleepStages  []SleepStage

	RestingHeartRate float64
	Workouts         []Workout

	// MoodLevel is the day's State of Mind as 1 to 5, 0 when there is none;
	// see HealthMoodLevel.
	MoodLevel int
}

// SleepStage is one stage of the night and how long it lasted.
type SleepStage struct {
	SleepStage string
	Minutes    int
}

// Workout is a workout that started that day.
type Workout struct {
	SampleID     string
	ActivityType string
	StartedAt    time.Time
	Minutes      int
	Meters       float64
}

// ZoneName is the name of the zone a person's new days are cut in.
func ZoneName(user *models.User) string {
	return Zone(user).String()
}

// ReadDay reads a journal day: the stored row and links, and every section
// the options allow, each read by the day's time range. agent may be nil for
// a person with no agent, whose agent sections are then unavailable. A day
// with nothing kept and nothing in it is an empty Day, not an error.
func ReadDay(transaction db.Transaction, user *models.User, agent *models.Agent, day string, now time.Time, options ReadOptions) (*Day, error) {
	if _, err := ParseDay(day); err != nil {
		return nil, err
	}
	result := &Day{Day: day, Now: now}

	row, err := transaction.GetJournalDay(user.ID, day)
	switch {
	case err == nil:
		result.Row = row
	case errors.Is(err, db.ErrNotFound):
	default:
		return nil, err
	}
	result.TimeZone = ZoneName(user)
	if result.Row != nil && result.Row.TimeZone != "" {
		result.TimeZone = result.Row.TimeZone
	}
	location := ZoneOfDay(user, result.TimeZone)
	result.From, result.Until, err = DayRange(day, location)
	if err != nil {
		return nil, err
	}
	if result.Row != nil {
		links, err := transaction.ListJournalLinks(user.ID, result.Row.ID, "")
		switch {
		case err == nil:
			result.Links = links
		case errors.Is(err, db.ErrNotFound):
		default:
			return nil, err
		}
	}

	result.location = location
	reader := &dayReader{
		transaction: transaction, user: user, agent: agent, day: result, location: location,
		options: options, isAgentReadable: options.CanReadAgent && agent != nil,
	}
	if err := reader.readAll(); err != nil {
		return nil, err
	}
	return result, nil
}

// dayReader reads the sections of one day.
type dayReader struct {
	transaction db.Transaction
	user        *models.User
	agent       *models.Agent
	day         *Day
	location    *time.Location
	options     ReadOptions

	// isAgentReadable: the caller may read the agent's sources and there is an
	// agent to read them from.
	isAgentReadable bool

	// sources, by id, for the documents' kinds and names; read once.
	sources map[string]*models.AgentKnowledgeSource
}

// unavailable names a section the caller may not read.
func (self *dayReader) unavailable(section string) {
	self.day.UnavailableSections = append(self.day.UnavailableSections, section)
}

func (self *dayReader) readAll() error {
	type step struct {
		section   string
		isAllowed bool
		read      func() error
	}
	isHealthAllowed := self.options.CanReadHealth && self.options.ShouldReadHealth
	steps := []step{
		{SectionCodingSessions, self.isAgentReadable, self.readDocuments},
		{SectionDocuments, self.isAgentReadable, nil},
		{SectionConversations, self.isAgentReadable, self.readConversations},
		{SectionChats, self.options.CanReadChats, self.readChats},
		{SectionMail, self.options.CanReadMail, self.readMail},
		{SectionRecordings, self.isAgentReadable, self.readRecordings},
		{SectionPhotos, self.options.CanReadPhotos, self.readPhotos},
		{SectionVisits, self.options.CanReadLocation, self.readVisits},
		{SectionEvents, self.options.CanReadCalendar, self.readCalendar},
		{SectionReminders, self.options.CanReadCalendar, nil},
		{SectionTransactions, self.isAgentReadable, self.readTransactions},
		{SectionHealth, isHealthAllowed, self.readHealth},
		{SectionNotes, self.options.CanReadMail, self.readNotes},
		{SectionMemoryPages, self.isAgentReadable, self.readMemoryPages},
	}
	for _, step := range steps {
		if !step.isAllowed {
			// A person whose Health the dream was told to leave out has not
			// lost anything: the section is only not asked for. It is listed
			// only when the person could not read it.
			if step.section == SectionHealth && self.options.CanReadHealth {
				continue
			}
			self.unavailable(step.section)
			continue
		}
		if step.read == nil {
			continue
		}
		if err := step.read(); err != nil {
			return fmt.Errorf("journal: reading the %s of %s: %w", step.section, self.day.Day, err)
		}
	}
	return nil
}

// zone is the zone the day's clock times are told in.
func (self *Day) zone() *time.Location {
	if self.location != nil {
		return self.location
	}
	if location, err := time.LoadLocation(self.TimeZone); err == nil && self.TimeZone != "" {
		return location
	}
	return time.UTC
}

// OptionsFor is the sections a person's permissions let them read.
// shouldReadHealth is false for a read the agent makes of a person who has
// not let it read their Health.
func OptionsFor(permissions *models.EffectivePermissions, shouldReadHealth bool) ReadOptions {
	return ReadOptions{
		CanReadMail:      permissions.Has(models.PermissionMailRead),
		CanReadPhotos:    permissions.Has(models.PermissionPhotosUse),
		CanReadLocation:  permissions.Has(models.PermissionLocationUse),
		CanReadCalendar:  permissions.Has(models.PermissionCalendarUse),
		CanReadHealth:    permissions.Has(models.PermissionHealthUse),
		CanReadChats:     permissions.Has(models.PermissionCircleUse),
		CanReadAgent:     permissions.Has(models.PermissionAgentUse),
		ShouldReadHealth: shouldReadHealth,
	}
}

// RecordCount is how many records the day was read from, the lists that are
// capped counted by their totals.
func (self *Day) RecordCount() int {
	count := len(self.CodingSessions) + self.DocumentTotal + len(self.JournalNotes) + len(self.Conversations) + len(self.Chats) +
		self.MailReceivedTotal + self.MailSentTotal + len(self.Recordings) + self.PhotoTotal + len(self.Visits) + len(self.Events) +
		len(self.Reminders) + len(self.Transactions) + len(self.Notes) + len(self.MemoryPages)
	if self.Health != nil {
		count += len(self.Health.Workouts)
	}
	return count
}
