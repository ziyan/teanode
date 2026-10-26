package journal

import (
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// WeekStartOf is the day the person's weeks start on: what their first
// calendar says, and Monday when it says nothing. The page, the command line
// and the agent's writing of weeks all cut weeks the same way.
func WeekStartOf(transaction db.Transaction, userId string) (string, error) {
	calendars, err := transaction.ListCalendars(userId)
	if err != nil {
		return "", err
	}
	if len(calendars) > 0 {
		if known := models.KnownWeekStart(calendars[0].WeekStart); known != "" {
			return known, nil
		}
	}
	return models.WeekStartsMonday, nil
}
