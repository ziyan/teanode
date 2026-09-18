// Package reading is how far the night has got through what was indexed:
// one measure that the API, the command line and the knowledge tool share,
// in a package of its own because the tool cannot import the agent.
package reading

import (
	"fmt"
	"strconv"
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

	// Unreadable is what nothing here can read yet: a picture or a file
	// that came with a record, whose bytes are kept and of which no text
	// has been made, and which the night has not yet decided about. It is
	// neither waiting nor read -- a night could do nothing with it today
	// -- so it is counted apart and said in its own words, rather than
	// sitting in the waiting count as work that never moves.
	Unreadable int64 `json:"unreadable"`

	// Declined is what the night looked at the outside of -- the name,
	// the size, the kind of file, the words it came with -- and decided
	// was not worth the cost of opening. Described is what it did open
	// and made text of.
	//
	// Three numbers for the files rather than one, because "1,020 files
	// nothing here can read yet" is a different thing to be told from
	// "1,020 the agent decided against" and from "1,020 it read", and a
	// person looking at a source's page wants to know which of the three
	// they are looking at. A described file is a document like any other
	// and is in Waiting or Read as well; these two never are.
	Declined  int64 `json:"declined"`
	Described int64 `json:"described"`

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
	counts, err := tx.CountAgentDocumentsReading(agent.ID, ChatNamesOf(owner))
	if err != nil {
		return nil, err
	}
	progress := &Progress{
		Waiting: counts.Waiting, Read: counts.Read, Unreadable: counts.Undecided,
		Declined: counts.Declined, Described: counts.Described,
		Bootstrapping: agent.DreamBootstrap,
	}
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
		progress.HoursLeft = float64(progress.Waiting) / progress.PerHour
	}
	return progress, nil
}

// Describe is the one line the command line, the tool and the dashboard
// all say about the reading.
func (self *Progress) Describe() string {
	total := self.Read + self.Waiting
	if total == 0 {
		if files := self.describeFiles(); files != "" {
			return files
		}
		return "nothing has been indexed yet"
	}
	line := fmt.Sprintf("%d of %d documents read, %d waiting", self.Read, total, self.Waiting)
	if self.Waiting == 0 {
		line = fmt.Sprintf("all %d documents read", self.Read)
	} else if self.PerHour > 0 {
		line += fmt.Sprintf("; about %s left at %.0f an hour", describeHours(self.HoursLeft), self.PerHour)
		if !self.Bootstrapping {
			line += ", if it dreamed without pause"
		}
	}
	// After the pace rather than inside it: these are not waiting for a
	// night, they are waiting for something that can read them, and a
	// person told "1,020 waiting" about pictures nothing opens would
	// watch that number never move.
	if files := self.describeFiles(); files != "" {
		line += "; " + files
	}
	return line
}

// describeFiles is what became of the pictures and files a record came
// with, in the words a person would use for them: the ones still waiting
// for the night to look at them, the ones it decided against opening, and
// the ones it opened and read.
//
// Each clause appears only when there is one, so that a source with no
// such files reads exactly as it did before there were any.
func (self *Progress) describeFiles() string {
	var parts []string
	if self.Unreadable > 0 {
		parts = append(parts, fmt.Sprintf("%s %s nothing here can read yet",
			describeCount(self.Unreadable), pluralFiles(self.Unreadable)))
	}
	if self.Declined > 0 {
		parts = append(parts, fmt.Sprintf("%s %s it decided against opening",
			describeCount(self.Declined), pluralFiles(self.Declined)))
	}
	if self.Described > 0 {
		parts = append(parts, fmt.Sprintf("%s %s it opened and read",
			describeCount(self.Described), pluralFiles(self.Described)))
	}
	return strings.Join(parts, "; ")
}

func pluralFiles(count int64) string {
	if count == 1 {
		return "file"
	}
	return "files"
}

// describeCount groups a number the way a person writing it down would,
// because these run to tens of thousands and "1020" is read twice.
func describeCount(count int64) string {
	digits := strconv.FormatInt(count, 10)
	if len(digits) <= 3 {
		return digits
	}
	var grouped strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return grouped.String()
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
