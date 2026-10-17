package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// calendarListOperations answers the calendar list with the calendars it is
// given and nothing else.
type calendarListOperations struct {
	calendars string
}

func (self *calendarListOperations) Execute(_ context.Context, document string, _ map[string]any, result any) error {
	answer := `{}`
	if strings.Contains(document, "ListCalendars") {
		answer = `{"ListCalendars":` + self.calendars + `}`
	}
	return json.Unmarshal([]byte(answer), result)
}

func (self *calendarListOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})
}

// The prompt says the calendars other people share are readable only while
// the person's own calendar is granted, because the calendar tool refuses
// them otherwise. With it closed they are listed among what is withheld.
func TestTheSharedCalendarsLineIsOnlyWithTheGrantOfTheOwnCalendar(t *testing.T) {
	configuration := config.Default()
	collectionsOf := func(isOwnGranted bool) ([]string, []string) {
		calendars := `[
			{"name":"Personal","calendarKind":"events","events":3,"agentGranted":` + map[bool]string{true: "true", false: "false"}[isOwnGranted] + `,"shareLevel":"owner"},
			{"name":"Pat's calendar","calendarKind":"events","events":0,"agentGranted":false,"shareLevel":"titles"}
		]`
		run := &AskRun{agent: &Agent{settings: &Settings{Configuration: func() *config.Configuration { return configuration }}},
			settings: &AskSettings{Agent: &models.Agent{ID: "agent01"}, Owner: &models.User{ID: "user01", Username: "robin"}, Operations: &calendarListOperations{calendars: calendars}}}
		return run.collections(context.Background())
	}

	granted, withheld := collectionsOf(true)
	if !strings.Contains(strings.Join(granted, "\n"), "1 calendars other people or their circles share with them; read with calendar agenda and whose") {
		t.Errorf("with the own calendar granted the shared ones are readable: %v", granted)
	}
	if strings.Contains(strings.Join(withheld, "\n"), "share with them") {
		t.Errorf("and are not withheld: %v", withheld)
	}

	granted, withheld = collectionsOf(false)
	if strings.Contains(strings.Join(granted, "\n"), "share with them") {
		t.Errorf("without it the prompt does not promise them: %v", granted)
	}
	if !strings.Contains(strings.Join(withheld, "\n"), "calendars others share with them (closed while their own calendar is closed to you)") {
		t.Errorf("without it they are listed as closed: %v", withheld)
	}
}
