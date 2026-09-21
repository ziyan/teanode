// Package reading is how far the night has got through what was indexed:
// one measure that the API, the command line and the knowledge tool share,
// in a package of its own because the tool cannot import the agent.
package reading

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
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

	// Spent is what the dreams the pace was taken from actually cost, and
	// CostLeft is what the rest would cost at that rate. Currency is what
	// both are said in.
	//
	// The hours were the only thing the row guessed at, and hours are not
	// what a person is deciding about. Somebody watching a hundred and
	// fifty thousand documents go by wants to know what finishing them
	// comes to before it has come to it -- and the same rows that say how
	// fast the night reads say what the reading cost, so the guess costs
	// one more query and no more model calls.
	//
	// Zero where nothing has been read yet, or where the models in use
	// have no prices configured, which is the ordinary case for a model
	// somebody runs themselves. A zero is shown as nothing rather than as
	// free.
	Spent    float64 `json:"spent"`
	CostLeft float64 `json:"costLeft"`
	Currency string  `json:"currency,omitempty"`
}

// readingPaceDreams is how many finished dreams the pace is taken from:
// enough to smooth a slow one, few enough to follow a change of model.
const readingPaceDreams = 12

// For measures the night's progress for one agent.
//
// The configuration is for the prices: usage is kept per model and each
// model is priced by its own provider, so what the reading cost cannot be
// worked out from the rows alone. Nil asks for the counts and the pace
// without the money, which is what a caller with no configuration to hand
// wants.
func For(tx db.Transaction, configuration *config.Configuration, agent *models.Agent, owner *models.User) (*Progress, error) {
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
	// The oldest moment the pace was taken from, which is the window the
	// money is read over so that both describe the same nights.
	var since time.Time
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
		since = dream.StartedAt
		counted++
		if counted == readingPaceDreams {
			break
		}
	}
	if documents > 0 && spent > 0 {
		progress.PerHour = float64(documents) / spent.Hours()
		progress.HoursLeft = float64(progress.Waiting) / progress.PerHour
		if configuration != nil && !since.IsZero() {
			// The counts and the pace are the answer; the money is a line
			// under them. A usage table that would not answer loses that
			// line and nothing else, so the error goes no further.
			_ = progress.priceTheReading(tx, configuration, agent.ID, since, documents)
		}
	}
	return progress, nil
}

// priceTheReading works out what the dreams the pace came from cost, and
// what the rest would cost at the same price a document.
//
// Read from the same window the pace was read from, so the two numbers
// describe the same nights. Only the dream's own calls: an agent whose
// person also talks to it all day would otherwise have those turns priced
// into the reading and the guess would be nonsense.
func (self *Progress) priceTheReading(tx db.Transaction, configuration *config.Configuration, agentId string, since time.Time, documents int) error {
	rows, err := tx.QueryAgentUsageByModel(agentId, since, time.Time{}, "kind")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Key != string(models.AgentJobDream) {
			continue
		}
		self.Spent += configuration.Agent.CostOf(row.Model,
			int(row.Totals.PromptTokens), int(row.Totals.CompletionTokens),
			int(row.Totals.CacheReadTokens), int(row.Totals.CacheWriteTokens))
	}
	if self.Spent > 0 && documents > 0 {
		self.CostLeft = self.Spent / float64(documents) * float64(self.Waiting)
		self.Currency = configuration.Agent.Currency
	}
	return nil
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
		// And what that comes to, where the models have prices. The same
		// sentence serves the command line, the agent's own tool and the
		// dashboard, so all three say it or none do.
		if self.CostLeft > 0 {
			line += fmt.Sprintf(", about %s more", money(self.CostLeft, self.Currency))
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

// money is an amount in the operator's currency, to the nearest penny
// above a pound and to the nearest hundredth of one below it.
//
// Its own rather than the dashboard's, because this package is below the
// one that formats money for a browser and a reading that is a tenth of a
// penny a document should not read as "0".
func money(amount float64, currency string) string {
	symbol := "$"
	switch strings.ToLower(currency) {
	case "eur":
		symbol = "\u20ac"
	case "gbp":
		symbol = "\u00a3"
	case "jpy":
		symbol = "\u00a5"
	}
	if amount < 1 {
		return fmt.Sprintf("%s%.2f", symbol, amount)
	}
	return fmt.Sprintf("%s%.0f", symbol, amount)
}
