// Package reading is how far the night has got through what was indexed:
// one measure that the API, the command line and the knowledge tool share,
// in a package of its own because the tool cannot import the agent.
package reading

import (
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Progress is how far the night has got through what was indexed,
// and a guess at how long the rest takes at the pace of the last dreams.
type Progress struct {
	// Waiting is what has been indexed and not yet read; Read is what has.
	Waiting int64 `json:"waiting"`
	Read    int64 `json:"read"`

	// PerHour is how many documents the last dreams read in an hour of
	// dreaming, and HoursLeft is Waiting at that pace. Zero when no dream
	// has read anything yet, which is when there is nothing to guess from.
	PerHour   float64 `json:"perHour"`
	HoursLeft float64 `json:"hoursLeft"`

	// Bootstrapping says the night runs at every tick until the wait is
	// gone, which is what the guess assumes; off, the rest takes as many
	// nights as it takes.
	Bootstrapping bool `json:"bootstrapping"`
}

// readingPaceDreams is how many finished dreams the pace is taken from:
// enough to smooth a slow one, few enough to follow a change of model.
const readingPaceDreams = 12

// For measures the night's progress for one agent.
func For(tx db.Transaction, agent *models.Agent, owner *models.User) (*Progress, error) {
	waiting, read, err := tx.CountAgentDocumentsReading(agent.ID, ChatNamesOf(owner))
	if err != nil {
		return nil, err
	}
	progress := &Progress{Waiting: waiting, Read: read, Bootstrapping: agent.DreamBootstrap}
	dreams, err := tx.ListAgentDreams(agent.ID, 40)
	if err != nil {
		return nil, err
	}
	var documents int
	var spent time.Duration
	var counted int
	for _, dream := range dreams {
		if dream.FinishedAt == nil || dream.Digested == 0 {
			continue
		}
		took := dream.FinishedAt.Sub(dream.StartedAt)
		if took < time.Minute {
			continue
		}
		documents += dream.Digested
		spent += took
		counted++
		if counted == readingPaceDreams {
			break
		}
	}
	if documents > 0 && spent > 0 {
		progress.PerHour = float64(documents) / spent.Hours()
		progress.HoursLeft = float64(waiting) / progress.PerHour
	}
	return progress, nil
}

// Describe is the one line the command line, the tool and the dashboard
// all say about the reading.
func (self *Progress) Describe() string {
	total := self.Read + self.Waiting
	if total == 0 {
		return "nothing has been indexed yet"
	}
	if self.Waiting == 0 {
		return fmt.Sprintf("all %d documents read", self.Read)
	}
	line := fmt.Sprintf("%d of %d documents read, %d waiting", self.Read, total, self.Waiting)
	if self.PerHour > 0 {
		line += fmt.Sprintf("; about %s left at %.0f an hour", describeHours(self.HoursLeft), self.PerHour)
		if !self.Bootstrapping {
			line += ", if it dreamed without pause"
		}
	}
	return line
}

// describeHours says a span in the unit a person would use for it.
func describeHours(hours float64) string {
	switch {
	case hours < 1:
		return fmt.Sprintf("%.0f minutes", hours*60)
	case hours < 48:
		return fmt.Sprintf("%.0f hours", hours)
	default:
		return fmt.Sprintf("%.0f days", hours/24)
	}
}

// ChatNamesOf is the names a chat unit has to carry for the person to
// have been in it: their username and the words of their name, lowered,
// none shorter than three letters.
func ChatNamesOf(owner *models.User) []string {
	if owner == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if len(name) < 3 || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(owner.Username)
	for _, word := range strings.Fields(owner.Name) {
		add(word)
	}
	return names
}
