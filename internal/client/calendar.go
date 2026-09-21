package client

import (
	"context"
	"time"
)

// A person's own calendar: the events they keep, which their devices
// synchronize over CalDAV.

// Calendar is one calendar and how much is in it.
type Calendar struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Colour      string `json:"colour,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	WeekStart   string `json:"weekStart,omitempty"`
	Events      int    `json:"events"`
}

// CalendarEvent is one entry. In a listing it is one time something happens,
// so an event that repeats weekly appears once for each week in the window;
// StartsAt and EndsAt are that occurrence rather than the event's own first
// time. File is the whole of it and comes back only when one is asked for.
type CalendarEvent struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendarId"`
	UID        string `json:"uid"`
	ETag       string `json:"etag,omitempty"`

	Summary     string `json:"summary,omitempty"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`

	StartsAt string `json:"startsAt"`
	EndsAt   string `json:"endsAt"`
	AllDay   bool   `json:"allDay"`

	Recurring  bool   `json:"recurring"`
	Recurrence string `json:"recurrence,omitempty"`
	Occurrence bool   `json:"occurrence"`

	Status   string `json:"status,omitempty"`
	Timezone string `json:"timezone,omitempty"`

	Organizer string              `json:"organizer,omitempty"`
	Attendees []*CalendarAttendee `json:"attendees,omitempty"`

	File string `json:"file,omitempty"`
}

// CalendarAttendee is one person asked to an event.
type CalendarAttendee struct {
	Address       string `json:"address"`
	Name          string `json:"name,omitempty"`
	Participation string `json:"participation,omitempty"`
	Role          string `json:"role,omitempty"`
}

const (
	DocumentListCalendars = `query { ListCalendars { id name description colour timezone weekStart events } }`

	DocumentListCalendarEvents = `query ($calendarId: String!, $from: String!, $until: String!) {
  ListCalendarEvents(calendarId: $calendarId, from: $from, until: $until) {
    id calendarId uid summary location startsAt endsAt allDay recurring occurrence status
  }
}`

	DocumentGetCalendarEvent = `query ($calendarId: String!, $id: String!) {
  GetCalendarEvent(calendarId: $calendarId, id: $id) {
    id calendarId uid etag summary location description startsAt endsAt allDay
    recurring recurrence status timezone organizer file
    attendees { address name participation role }
  }
}`

	DocumentSaveCalendarEvent = `mutation ($calendarId: String!, $id: String, $file: String,
    $summary: String, $location: String, $description: String,
    $startsAt: String, $endsAt: String, $allDay: Boolean,
    $timezone: String, $recurrence: String, $status: String, $attendees: [String!]) {
  SaveCalendarEvent(calendarId: $calendarId, id: $id, file: $file,
    summary: $summary, location: $location, description: $description,
    startsAt: $startsAt, endsAt: $endsAt, allDay: $allDay,
    timezone: $timezone, recurrence: $recurrence, status: $status,
    attendees: $attendees) { id uid summary startsAt endsAt allDay }
}`

	DocumentSaveCalendarEventWithRequest = `mutation ($requestId: String!, $calendarId: String!, $id: String, $file: String,
    $summary: String, $location: String, $description: String,
    $startsAt: String, $endsAt: String, $allDay: Boolean,
    $timezone: String, $recurrence: String, $status: String, $attendees: [String!]) {
  SaveCalendarEvent(requestId: $requestId, calendarId: $calendarId, id: $id, file: $file,
    summary: $summary, location: $location, description: $description,
    startsAt: $startsAt, endsAt: $endsAt, allDay: $allDay,
    timezone: $timezone, recurrence: $recurrence, status: $status,
    attendees: $attendees) { id uid summary startsAt endsAt allDay }
}`

	DocumentGetCalendarRequest = `query ($requestId: String!) {
 GetCalendarRequest(requestId: $requestId) { requestId calendarId objectId operation completedAt isMissing }
 }`

	DocumentDeleteCalendarEvent = `mutation ($calendarId: String!, $id: String!) {
  DeleteCalendarEvent(calendarId: $calendarId, id: $id)
}`

	DocumentSaveCalendar = `mutation ($id: String!, $name: String, $description: String,
    $colour: String, $timezone: String, $weekStart: String) {
  SaveCalendar(id: $id, name: $name, description: $description,
    colour: $colour, timezone: $timezone, weekStart: $weekStart) {
    id name description colour timezone weekStart events
  }
}`
)

// ListCalendars are the caller's. An account that has never had one is given
// one by the server when it first looks.
func ListCalendars(ctx context.Context, connection *Client) ([]*Calendar, error) {
	var result struct {
		ListCalendars []*Calendar `json:"ListCalendars"`
	}
	if err := connection.Execute(ctx, DocumentListCalendars, nil, &result); err != nil {
		return nil, err
	}
	return result.ListCalendars, nil
}

// ListCalendarEvents is what is on between two moments, written as RFC 3339.
func ListCalendarEvents(ctx context.Context, connection *Client, calendarId, from, until string) ([]*CalendarEvent, error) {
	var result struct {
		ListCalendarEvents []*CalendarEvent `json:"ListCalendarEvents"`
	}
	if err := connection.Execute(ctx, DocumentListCalendarEvents,
		map[string]any{"calendarId": calendarId, "from": from, "until": until}, &result); err != nil {
		return nil, err
	}
	return result.ListCalendarEvents, nil
}

// GetCalendarEvent is one event, with the file as it is stored.
func GetCalendarEvent(ctx context.Context, connection *Client, calendarId, id string) (*CalendarEvent, error) {
	var result struct {
		GetCalendarEvent *CalendarEvent `json:"GetCalendarEvent"`
	}
	if err := connection.Execute(ctx, DocumentGetCalendarEvent,
		map[string]any{"calendarId": calendarId, "id": id}, &result); err != nil {
		return nil, err
	}
	return result.GetCalendarEvent, nil
}

// SaveCalendarEventFields keeps an event from filled-in fields. A nil field is
// left as it was; a pointer to an empty value clears it.
type SaveCalendarEventFields struct {
	RequestID  string
	CalendarID string
	ID         string
	File       string

	Summary     *string
	Location    *string
	Description *string
	StartsAt    *string
	EndsAt      *string
	AllDay      *bool
	Timezone    string
	Recurrence  *string
	Status      *string
	Attendees   *[]string
}

// SaveCalendarEvent keeps one, or changes one that is kept.
func SaveCalendarEvent(ctx context.Context, connection *Client, fields *SaveCalendarEventFields) (*CalendarEvent, error) {
	arguments := map[string]any{
		"calendarId": fields.CalendarID,
		"id":         nil, "file": nil, "summary": nil, "location": nil, "description": nil,
		"startsAt": nil, "endsAt": nil, "allDay": nil, "timezone": nil,
		"recurrence": nil, "status": nil, "attendees": nil,
	}
	if fields.ID != "" {
		arguments["id"] = fields.ID
	}
	if fields.File != "" {
		arguments["file"] = fields.File
	}
	if fields.Timezone != "" {
		arguments["timezone"] = fields.Timezone
	}
	for name, value := range map[string]*string{
		"summary": fields.Summary, "location": fields.Location, "description": fields.Description,
		"startsAt": fields.StartsAt, "endsAt": fields.EndsAt,
		"recurrence": fields.Recurrence, "status": fields.Status,
	} {
		if value != nil {
			arguments[name] = *value
		}
	}
	if fields.AllDay != nil {
		arguments["allDay"] = *fields.AllDay
	}
	if fields.Attendees != nil {
		arguments["attendees"] = *fields.Attendees
	}
	var result struct {
		SaveCalendarEvent *CalendarEvent `json:"SaveCalendarEvent"`
	}
	document := DocumentSaveCalendarEvent
	if fields.RequestID != "" {
		document = DocumentSaveCalendarEventWithRequest
		arguments["requestId"] = fields.RequestID
	}
	if err := connection.Execute(ctx, document, arguments, &result); err != nil {
		return nil, err
	}
	return result.SaveCalendarEvent, nil
}

// DeleteCalendarEvent takes one away.
func DeleteCalendarEvent(ctx context.Context, connection *Client, calendarId, id string) error {
	var result struct {
		DeleteCalendarEvent bool `json:"DeleteCalendarEvent"`
	}
	return connection.Execute(ctx, DocumentDeleteCalendarEvent,
		map[string]any{"calendarId": calendarId, "id": id}, &result)
}

// SaveCalendar renames one, or changes how it is shown.
func SaveCalendar(ctx context.Context, connection *Client, id, name, description, colour, timezone, weekStart string) (*Calendar, error) {
	var result struct {
		SaveCalendar *Calendar `json:"SaveCalendar"`
	}
	if err := connection.Execute(ctx, DocumentSaveCalendar, map[string]any{
		"id": id, "name": name, "description": description, "colour": colour,
		"timezone": timezone, "weekStart": weekStart,
	}, &result); err != nil {
		return nil, err
	}
	return result.SaveCalendar, nil
}

// CalendarRequest identifies a committed change even when its event was deleted.
type CalendarRequest struct {
	RequestID   string    `json:"requestId"`
	CalendarID  string    `json:"calendarId"`
	ObjectID    string    `json:"objectId"`
	Operation   string    `json:"operation"`
	CompletedAt time.Time `json:"completedAt"`
	IsMissing   bool      `json:"isMissing"`
}

// GetCalendarRequest looks up completion without repeating the original mutation.
// A nil receipt does not prove that an in-flight request will never commit.
func GetCalendarRequest(ctx context.Context, connection *Client, requestId string) (*CalendarRequest, error) {
	var response struct {
		GetCalendarRequest *CalendarRequest `json:"GetCalendarRequest"`
	}
	if err := connection.Execute(ctx, DocumentGetCalendarRequest, map[string]any{"requestId": requestId}, &response); err != nil {
		return nil, err
	}
	return response.GetCalendarRequest, nil
}
