package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// A person's calendar, and the events in it, kept as iCalendar text.
//
// The occurrence table beside them is derived: it is rewritten whenever an
// object is written, in the same transaction, so it can never be left
// describing a version of an event that is no longer there.

type calendarModel struct {
	ID          string    `gorm:"column:id;primaryKey"`
	UserID      string    `gorm:"column:user_id"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	Name        string    `gorm:"column:name"`
	Description string    `gorm:"column:description"`
	Colour      string    `gorm:"column:colour"`
	Timezone    string    `gorm:"column:time_zone"`
	WeekStart   string    `gorm:"column:week_start"`
}

func (calendarModel) TableName() string { return "calendar" }

func (self *calendarModel) toModel() *models.Calendar {
	return &models.Calendar{
		ID: self.ID, UserID: self.UserID, CreatedAt: self.CreatedAt,
		ModifiedAt: self.ModifiedAt, Name: self.Name, Description: self.Description,
		Colour: self.Colour, Timezone: self.Timezone, WeekStart: self.WeekStart,
	}
}

type calendarObjectModel struct {
	// The identifier is the file name the client chose, so it is unique
	// only within one calendar, and the key is both columns together.
	ID         string    `gorm:"column:id;primaryKey"`
	CalendarID string    `gorm:"column:calendar_id;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
	UID        string    `gorm:"column:uid"`
	ETag       string    `gorm:"column:etag"`
	Data       string    `gorm:"column:data"`
	Summary    string    `gorm:"column:summary"`
	Location   string    `gorm:"column:location"`

	// Nullable, because a file may describe something with no time at all --
	// a to-do, or a journal entry -- and a zero time would be read as the
	// first day of year one and sort in front of everything.
	StartsAt     *time.Time `gorm:"column:starts_at"`
	EndsAt       *time.Time `gorm:"column:ends_at"`
	AllDay       bool       `gorm:"column:all_day"`
	Recurring    bool       `gorm:"column:recurring"`
	Status       string     `gorm:"column:status"`
	IndexedUntil *time.Time `gorm:"column:indexed_until"`
	IndexedAt    *time.Time `gorm:"column:indexed_at"`
}

func (calendarObjectModel) TableName() string { return "calendar_object" }

func (self *calendarObjectModel) toModel() *models.CalendarObject {
	object := &models.CalendarObject{
		ID: self.ID, CalendarID: self.CalendarID, CreatedAt: self.CreatedAt,
		ModifiedAt: self.ModifiedAt, UID: self.UID, ETag: self.ETag, Data: self.Data,
		Summary: self.Summary, Location: self.Location,
		AllDay: self.AllDay, Recurring: self.Recurring, Status: self.Status,
		IndexedUntil: self.IndexedUntil, IndexedAt: self.IndexedAt,
	}
	if self.StartsAt != nil {
		object.StartsAt = *self.StartsAt
	}
	if self.EndsAt != nil {
		object.EndsAt = *self.EndsAt
	}
	return object
}

type calendarOccurrenceModel struct {
	CalendarID string    `gorm:"column:calendar_id;primaryKey"`
	ObjectID   string    `gorm:"column:object_id;primaryKey"`
	StartsAt   time.Time `gorm:"column:starts_at;primaryKey"`
	EndsAt     time.Time `gorm:"column:ends_at"`
	AllDay     bool      `gorm:"column:all_day"`
}

func (calendarOccurrenceModel) TableName() string { return "calendar_occurrence" }

func (self *calendarOccurrenceModel) toModel() *models.Occurrence {
	return &models.Occurrence{
		CalendarID: self.CalendarID, ObjectID: self.ObjectID,
		StartsAt: self.StartsAt, EndsAt: self.EndsAt, AllDay: self.AllDay,
	}
}

// ListCalendars are one account's, oldest first, which is the order they were
// made in and so the order somebody expects to see them.
func (self *transaction) ListCalendars(userId string) ([]*models.Calendar, error) {
	var found []calendarModel
	if err := self.tx.Where("\"user_id\" = ?", userId).Order("\"created_at\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	calendars := make([]*models.Calendar, 0, len(found))
	for index := range found {
		calendars = append(calendars, found[index].toModel())
	}
	return calendars, nil
}

func (self *transaction) GetCalendar(calendarId string) (*models.Calendar, error) {
	var found []calendarModel
	if err := self.tx.Where("\"id\" = ?", calendarId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// CreateCalendar makes one. Audited: a calendar appearing or disappearing
// changes the shape of an account, which is what the administrative log is
// for. What goes inside it is not audited -- see PutCalendarObject.
func (self *transaction) CreateCalendar(calendar *models.Calendar) (*models.Calendar, error) {
	if calendar == nil || strings.TrimSpace(calendar.UserID) == "" {
		return nil, fmt.Errorf("db: a calendar needs an account")
	}
	now := time.Now()
	row := &calendarModel{
		ID: newID(), UserID: calendar.UserID, CreatedAt: now, ModifiedAt: now,
		Name: truncateRunes(strings.TrimSpace(calendar.Name), 200), Description: calendar.Description,
		Colour: truncateRunes(strings.TrimSpace(calendar.Colour), 16),
		// Not validated against the zone database here: a name this machine
		// does not know is still what the person asked for, and refusing it
		// would make a calendar undeliverable because of a missing package.
		Timezone: truncateRunes(strings.TrimSpace(calendar.Timezone), 64),
		// Sunday unless this person says otherwise, and never a word this
		// server does not know: the dashboard draws its columns from this.
		WeekStart: models.KnownWeekStart(calendar.WeekStart),
	}
	if row.Name == "" {
		row.Name = "Calendar"
	}
	if row.WeekStart == "" {
		row.WeekStart = models.WeekStartsSunday
	}
	if err := self.applyMutation(models.AuditResourceCalendar, row.ID, models.AuditActionCreate,
		nil, row.toModel(), func(tx *gorm.DB) error {
			return tx.Create(row).Error
		}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

// UpdateCalendar renames one, or changes how it is shown.
func (self *transaction) UpdateCalendar(calendar *models.Calendar) (*models.Calendar, error) {
	if calendar == nil || strings.TrimSpace(calendar.ID) == "" {
		return nil, fmt.Errorf("db: which calendar")
	}
	before, err := self.GetCalendar(calendar.ID)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, nil
	}
	row := &calendarModel{
		ID: calendar.ID, UserID: before.UserID, CreatedAt: before.CreatedAt, ModifiedAt: time.Now(),
		Name: truncateRunes(strings.TrimSpace(calendar.Name), 200), Description: calendar.Description,
		Colour:    truncateRunes(strings.TrimSpace(calendar.Colour), 16),
		Timezone:  truncateRunes(strings.TrimSpace(calendar.Timezone), 64),
		WeekStart: models.KnownWeekStart(calendar.WeekStart),
	}
	if row.Name == "" {
		row.Name = before.Name
	}
	if row.WeekStart == "" {
		row.WeekStart = models.KnownWeekStart(before.WeekStart)
	}
	if row.WeekStart == "" {
		row.WeekStart = models.WeekStartsSunday
	}
	if err := self.applyMutation(models.AuditResourceCalendar, row.ID, models.AuditActionUpdate,
		before, row.toModel(), func(tx *gorm.DB) error {
			return tx.Model(&calendarModel{}).Where("\"id\" = ?", row.ID).
				Updates(map[string]any{
					"modified_at": row.ModifiedAt, "name": row.Name, "description": row.Description,
					"colour": row.Colour, "time_zone": row.Timezone,
					"week_start": row.WeekStart,
				}).Error
		}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

// DeleteCalendar takes one away, with everything in it: the events and their
// occurrences are removed by the foreign keys, not by further statements here.
//
// Whose it is has to be said, and is checked. An identifier alone is not
// permission to remove something -- every other door into a calendar resolves
// it through the account, and one that did not would be the way somebody
// deletes a calendar that is not theirs.
func (self *transaction) DeleteCalendar(userId, calendarId string) error {
	before, err := self.GetCalendar(calendarId)
	if err != nil || before == nil {
		return err
	}
	if before.UserID != userId {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceCalendar, calendarId, models.AuditActionDelete,
		before, nil, func(tx *gorm.DB) error {
			return tx.Where("\"id\" = ?", calendarId).Delete(&calendarModel{}).Error
		})
}

// ObjectsPerCalendar is how many events one calendar may hold.
//
// There has to be a number, because the listing a client reads is the whole
// calendar in one response and nothing else bounds it. It is enforced where
// an object is written, so a client is told plainly, rather than by cutting
// the listing short, which would read to a phone as "those events were
// deleted". Exported because both doors into a calendar -- the dashboard's
// API and CalDAV -- have to enforce the same number, and a limit enforced on
// one of two doors is not a limit.
const ObjectsPerCalendar = 10000

// ListCalendarObjects are one calendar's, earliest first.
func (self *transaction) ListCalendarObjects(calendarId string) ([]*models.CalendarObject, error) {
	var found []calendarObjectModel
	// Everything, deliberately: a CalDAV client reads the listing of a
	// calendar as the whole truth and treats anything missing from it as
	// deleted, so a quiet cap here would tell a phone to forget the events
	// it could not see. How many a calendar may hold is decided where an
	// object is written.
	if err := self.tx.Where("\"calendar_id\" = ?", calendarId).
		Order("\"starts_at\" ASC NULLS LAST, \"id\" ASC").
		Limit(ObjectsPerCalendar + 1).Find(&found).Error; err != nil {
		return nil, err
	}
	objects := make([]*models.CalendarObject, 0, len(found))
	for index := range found {
		objects = append(objects, found[index].toModel())
	}
	return objects, nil
}

func (self *transaction) GetCalendarObject(calendarId, objectId string) (*models.CalendarObject, error) {
	return self.calendarObject(calendarId, objectId, false)
}

// LockCalendarObject is GetCalendarObject for a caller about to write it: the
// row is held for the rest of the transaction.
//
// Two devices holding the same version both ask "is it still this version",
// both are told yes, and both write -- and the first one's change is gone,
// which is exactly what the version is there to prevent. Reading the row
// under a lock makes the second wait and see the first one's answer. A row
// nobody has written yet cannot be locked, so a file being created is settled
// by the unique index on the identifier instead.
func (self *transaction) LockCalendarObject(calendarId, objectId string) (*models.CalendarObject, error) {
	return self.calendarObject(calendarId, objectId, true)
}

func (self *transaction) calendarObject(calendarId, objectId string, holding bool) (*models.CalendarObject, error) {
	query := self.tx
	if holding {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var found []calendarObjectModel
	if err := query.Where("\"calendar_id\" = ? AND \"id\" = ?", calendarId, objectId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// GetCalendarObjectByUID finds the same event a device already knows about,
// which is also how a reply that arrives by mail is matched to what it
// answers. Two devices adding an event at the same time each choose a file
// name of their own but agree on the UID.
func (self *transaction) GetCalendarObjectByUID(calendarId, uid string) (*models.CalendarObject, error) {
	return self.calendarObjectByUID(calendarId, uid, false)
}

// LockCalendarObjectByUID is the same for a caller about to write what it
// finds: the row is held for the rest of the transaction, so that two
// messages about one event -- a change and an answer arriving together --
// cannot both be applied to the same copy and one of them lost.
func (self *transaction) LockCalendarObjectByUID(calendarId, uid string) (*models.CalendarObject, error) {
	return self.calendarObjectByUID(calendarId, uid, true)
}

func (self *transaction) calendarObjectByUID(calendarId, uid string, holding bool) (*models.CalendarObject, error) {
	query := self.tx
	if holding {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var found []calendarObjectModel
	if err := query.Where("\"calendar_id\" = ? AND \"uid\" = ?", calendarId, strings.TrimSpace(uid)).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// PutCalendarObject writes one event, replacing what was there under that id,
// and rewrites its occurrences in the same transaction.
//
// The two go together on purpose. The occurrence rows are derived from the
// text, so leaving them behind after the text changes would mean a calendar
// that shows a meeting at the time it used to be at; doing them in a second
// transaction would leave a window in which exactly that is true.
//
// Not audited, deliberately. A person's phone rewrites events all day, and a
// row per edit would bury the administrative log that exists to show what
// operators did to the server. The calendar appearing and disappearing is
// audited; what somebody keeps in it is their own business.
func (self *transaction) PutCalendarObject(object *models.CalendarObject, occurrences []models.Occurrence) (*models.CalendarObject, error) {
	if object == nil || strings.TrimSpace(object.CalendarID) == "" {
		return nil, fmt.Errorf("db: an event needs a calendar")
	}
	if strings.TrimSpace(object.UID) == "" || strings.TrimSpace(object.Data) == "" {
		return nil, fmt.Errorf("db: an event needs a file and an identifier")
	}
	now := time.Now()
	row := &calendarObjectModel{
		ID: strings.TrimSpace(object.ID), CalendarID: object.CalendarID,
		CreatedAt: object.CreatedAt, ModifiedAt: now,
		UID: strings.TrimSpace(object.UID), ETag: object.ETag, Data: object.Data,
		Summary:  truncateRunes(strings.TrimSpace(object.Summary), 512),
		Location: truncateRunes(strings.TrimSpace(object.Location), 512),
		AllDay:   object.AllDay, Recurring: object.Recurring,
		Status:       truncateRunes(strings.ToUpper(strings.TrimSpace(object.Status)), 32),
		IndexedUntil: object.IndexedUntil, IndexedAt: object.IndexedAt,
	}
	if row.ID == "" {
		row.ID = newID()
	}
	if len(row.UID) > 255 {
		// Not cut short: every later lookup uses the identifier the file
		// actually carries, so a shortened one is an event that can never
		// be found again and a collision waiting to happen.
		return nil, fmt.Errorf("db: an event's identifier in the file is longer than 255 characters")
	}
	if len(row.ID) > 255 {
		// The identifier is the file name a client chose. Cutting it short
		// would make two events one, so this is refused instead.
		return nil, fmt.Errorf("db: an event's identifier is longer than 255 characters")
	}
	if !object.StartsAt.IsZero() {
		starts := object.StartsAt.UTC()
		row.StartsAt = &starts
	}
	if !object.EndsAt.IsZero() {
		ends := object.EndsAt.UTC()
		row.EndsAt = &ends
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if err := self.tx.Save(row).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Where("\"calendar_id\" = ? AND \"object_id\" = ?", row.CalendarID, row.ID).
		Delete(&calendarOccurrenceModel{}).Error; err != nil {
		return nil, err
	}
	if len(occurrences) > 0 {
		rows := make([]calendarOccurrenceModel, 0, len(occurrences))
		seen := make(map[time.Time]struct{}, len(occurrences))
		for _, occurrence := range occurrences {
			starts := occurrence.StartsAt.UTC()
			// A file may name the same moment twice -- a rule and a date
			// added by hand that agree -- and the key is the moment, so the
			// second would be refused as a duplicate. It is the same
			// occurrence either way.
			if _, already := seen[starts]; already {
				continue
			}
			seen[starts] = struct{}{}
			ends := occurrence.EndsAt.UTC()
			if ends.Before(starts) {
				ends = starts
			}
			rows = append(rows, calendarOccurrenceModel{
				CalendarID: row.CalendarID, ObjectID: row.ID,
				StartsAt: starts, EndsAt: ends, AllDay: occurrence.AllDay,
			})
		}
		if len(rows) > 0 {
			if err := self.tx.CreateInBatches(rows, 500).Error; err != nil {
				return nil, err
			}
		}
	}
	return row.toModel(), nil
}

// DeleteCalendarObject takes one event away, with its occurrences, which go
// by the foreign key.
func (self *transaction) DeleteCalendarObject(calendarId, objectId string) error {
	return self.tx.Where("\"calendar_id\" = ? AND \"id\" = ?", calendarId, objectId).
		Delete(&calendarObjectModel{}).Error
}

// CountCalendarObjects is how many a calendar holds, for a list that says so
// without reading every file.
func (self *transaction) CountCalendarObjects(calendarId string) (int64, error) {
	var count int64
	err := self.tx.Model(&calendarObjectModel{}).Where("\"calendar_id\" = ?", calendarId).Count(&count).Error
	return count, err
}

// OccurrencesPerWindow is how many times something happening this server will
// describe in one answer.
//
// There has to be a number, because the window is chosen by whoever is asking
// and every occurrence in it is written out. It is not the number of events a
// calendar may hold: one repeating file contributes thousands of these, so
// bounding occurrences by that number cut a three-year window off after a few
// months -- and because the rows come back in order, what was cut was always
// the far end. Free-busy then reported somebody free for the rest of the
// window, which is the one mistake that code is written not to make.
const OccurrencesPerWindow = 100000

// ListOccurrences is what is on between two moments.
//
// Overlapping the window, not contained by it: a conference that began on
// Monday is on on Wednesday, and somebody looking at Wednesday wants to see
// it. The window is half-open -- an event that finishes exactly when the
// window opens is not on, and one that begins exactly as it closes belongs to
// the next window -- so that two adjacent windows show each event once.
func (self *transaction) ListOccurrences(calendarId string, from, until time.Time) ([]*models.Occurrence, error) {
	var found []calendarOccurrenceModel
	if err := self.tx.Where(
		"\"calendar_id\" = ? AND \"starts_at\" < ? AND \"ends_at\" > ?",
		calendarId, until.UTC(), from.UTC()).
		Order("\"starts_at\" ASC, \"object_id\" ASC").
		Limit(OccurrencesPerWindow + 1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) > OccurrencesPerWindow {
		// Said rather than done quietly. Handing back the first hundred
		// thousand would answer "what is on" and "when is this person
		// busy" with a window that stops somewhere in the middle, and
		// nothing downstream could tell that it had.
		return nil, fmt.Errorf("%w: that stretch of time holds more than %d times something happens; ask about a shorter one",
			ErrTooMuchAsked, OccurrencesPerWindow)
	}
	occurrences := make([]*models.Occurrence, 0, len(found))
	for index := range found {
		occurrences = append(occurrences, found[index].toModel())
	}
	return occurrences, nil
}

// ListCalendarObjectsRunningOut are the recurring events worked out no
// further than a given moment.
//
// Asked of the horizon each event was last worked out to, rather than of its
// furthest occurrence. Those are not the same question: a series that has
// already finished has no occurrence in the window at all, so by the second
// question it is always running out -- rewritten every tick for ever, and
// permanently in front of the events that genuinely need extending.
//
// And not the ones just done, however short they fell. A repeat too fine to
// reach the horizon never stops running out however often it is worked out,
// so without this it came straight back and held up every other account's --
// the same jam by a different road.
func (self *transaction) ListCalendarObjectsRunningOut(before, since time.Time, limit int) ([]*models.CalendarObject, error) {
	if limit <= 0 {
		limit = 50
	}
	var found []calendarObjectModel
	if err := self.tx.Where(
		"\"recurring\" AND (\"indexed_until\" IS NULL OR \"indexed_until\" < ?)"+
			" AND (\"indexed_at\" IS NULL OR \"indexed_at\" < ?)", before, since).
		Order("\"indexed_until\" ASC NULLS FIRST").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	objects := make([]*models.CalendarObject, 0, len(found))
	for index := range found {
		objects = append(objects, found[index].toModel())
	}
	return objects, nil
}

// TouchCalendarObjectHorizon records how far ahead an event has been worked
// out, without touching anything else about it.
//
// For the event that cannot be worked out at all: its occurrences are left as
// they are, and only its place in the queue moves. Writing it through
// PutCalendarObject would rewrite the file and its occurrences, which is a
// great deal of work to say "not now".
func (self *transaction) TouchCalendarObjectHorizon(calendarId, objectId string, until time.Time) (bool, error) {
	result := self.tx.Model(&calendarObjectModel{}).
		Where("\"calendar_id\" = ? AND \"id\" = ?", calendarId, objectId).
		Updates(map[string]any{"indexed_until": until, "indexed_at": time.Now().UTC()})
	return result.RowsAffected > 0, result.Error
}
