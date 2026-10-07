package skills

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A watch is something the agent looks at on its own, through the skill's
// own tools, so that it can tell the person about what arrives without
// being asked: new mail, a new transaction, a new mention. The skill says
// what to look at, which of its tools lists what arrived since a moment,
// which reads one item, and how an item is judged; the agent runs it on a
// cadence and hands what deserves it to the person's alerts.
//
// A list tool answers with a JSON array of items, or an object whose
// "items" is one, each item an object with these keys (WatchedItem):
// id, which is required; version, which changes when the item does (a
// thread's message count); at, when it happened; from, who it is from;
// title; text; and url. The skill shapes what its program prints into
// that, with jq where it has to; the agent reads nothing else.
//
// The moment to look from reaches the list tool as whichever of its own
// parameters it declares: since (RFC 3339), since_epoch (seconds) and
// since_date (YYYY-MM-DD, in UTC). A read tool gets the item's id.

// The kinds of watch: mail is sorted the way the person's own mail is,
// and item is judged with the watch's own guidance.
const (
	WatchKindMail = "mail"
	WatchKindItem = "item"
)

// The parameters a watch fills in for the tools it runs.
const (
	WatchSince      = "since"
	WatchSinceEpoch = "since_epoch"
	WatchSinceDate  = "since_date"
	WatchItemID     = "id"
)

const (
	// watchEveryDefault and watchEveryLeast bound how often a watch looks.
	watchEveryDefault = 10 * time.Minute
	watchEveryLeast   = time.Minute

	// watchOverlapDefault is how far each look overlaps the last.
	watchOverlapDefault = 10 * time.Minute
)

// Watch is one thing a skill says is worth watching.
type Watch struct {
	// Name is what the watch is known by within the skill, and
	// Description what it watches, in a line the person reads.
	Name        string `yaml:"name"`
	Description string `yaml:"description"`

	// Kind is mail or item; Guidance, for an item, says what is worth
	// telling the person about and what is not.
	Kind     string `yaml:"kind"`
	Guidance string `yaml:"guidance,omitempty"`

	// Every is how often it looks, ten minutes when unset. Overlap is how
	// far before the last look each look starts, ten
	// minutes when unset: longer for a source whose items appear late
	// with an earlier date, such as a card transaction that posts days
	// after it was made. Both are durations such as 10m or 72h. The first
	// look only takes note of what is there; it tells the person nothing.
	Every   string `yaml:"every,omitempty"`
	Overlap string `yaml:"overlap,omitempty"`

	// List lists what arrived since a moment; Read, when set, reads one
	// item in full.
	List *WatchCall `yaml:"list"`
	Read *WatchCall `yaml:"read,omitempty"`
}

// WatchCall is a call of one of the skill's tools, with the arguments it is
// always given.
type WatchCall struct {
	Tool      string         `yaml:"tool"`
	Arguments map[string]any `yaml:"arguments,omitempty"`
}

// WatchedItem is one item a list tool answers with.
type WatchedItem struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	At      string `json:"at"`
	From    string `json:"from"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	URL     string `json:"url"`
}

// Watch is the skill's watch of that name, or nil.
func (self *Skill) Watch(name string) *Watch {
	for _, watch := range self.Watches {
		if watch.Name == name {
			return watch
		}
	}
	return nil
}

// EveryDuration is how often the watch looks.
func (self *Watch) EveryDuration() time.Duration {
	every, err := time.ParseDuration(strings.TrimSpace(self.Every))
	if err != nil || every <= 0 {
		return watchEveryDefault
	}
	return max(every, watchEveryLeast)
}

// OverlapDuration is how far each look overlaps the last.
func (self *Watch) OverlapDuration() time.Duration {
	overlap, err := time.ParseDuration(strings.TrimSpace(self.Overlap))
	if err != nil || overlap <= 0 {
		return watchOverlapDefault
	}
	return overlap
}

// ListArguments are what the list tool is called with to list what
// arrived since the moment given: the watch's own arguments, and the
// moment in whichever forms the tool declares.
func (self *Skill) ListArguments(watch *Watch, since time.Time) map[string]any {
	arguments := map[string]any{}
	for name, value := range watch.List.Arguments {
		arguments[name] = value
	}
	declared := declaredParameters(self.Tool(watch.List.Tool))
	since = since.UTC()
	for name, value := range map[string]string{
		WatchSince:      since.Format(time.RFC3339),
		WatchSinceEpoch: strconv.FormatInt(since.Unix(), 10),
		WatchSinceDate:  since.Format(time.DateOnly),
	} {
		if declared[name] {
			arguments[name] = value
		}
	}
	return arguments
}

// ReadArguments are what the read tool is called with to read one item.
func (self *Skill) ReadArguments(watch *Watch, item *WatchedItem) map[string]any {
	arguments := map[string]any{}
	for name, value := range watch.Read.Arguments {
		arguments[name] = value
	}
	arguments[WatchItemID] = item.ID
	return arguments
}

// ParseWatchedItems reads a list tool's answer: what its command printed,
// as a JSON array of items or an object carrying one under "items". An
// item with no id is left out, since it could never be told apart from
// the next look's.
func ParseWatchedItems(answer map[string]any) ([]*WatchedItem, error) {
	text, _ := answer["text"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	var items []*WatchedItem
	if strings.HasPrefix(text, "{") {
		var wrapped struct {
			Items []*WatchedItem `json:"items"`
		}
		if err := json.Unmarshal([]byte(text), &wrapped); err != nil {
			return nil, fmt.Errorf("the list did not answer with items: %w", err)
		}
		items = wrapped.Items
	} else if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil, fmt.Errorf("the list did not answer with items: %s", cutTo(text, 300))
	}
	kept := items[:0]
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.ID) != "" {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

// declaredParameters are the names of the parameters a tool declares.
func declaredParameters(tool *Tool) map[string]bool {
	declared := map[string]bool{}
	if tool == nil {
		return declared
	}
	properties, _ := tool.Parameters["properties"].(map[string]any)
	for name := range properties {
		declared[name] = true
	}
	return declared
}

// validateWatches checks each watch names its own tools, and that a read
// tool takes the id it will be given.
func (self *Skill) validateWatches() error {
	seen := map[string]bool{}
	for _, watch := range self.Watches {
		if watch == nil {
			return fmt.Errorf("skills: %s declares an empty watch", self.Name)
		}
		where := self.Name + " watch " + watch.Name
		if !nameShape.MatchString(watch.Name) {
			return fmt.Errorf("skills: %q is not a watch name in %s: lower-case words joined by underscores", watch.Name, self.Name)
		}
		if seen[watch.Name] {
			return fmt.Errorf("skills: %s declares the watch %s twice", self.Name, watch.Name)
		}
		seen[watch.Name] = true
		if strings.TrimSpace(watch.Description) == "" {
			return fmt.Errorf("skills: %s has no description, which is what the person reads", where)
		}
		switch watch.Kind {
		case WatchKindMail:
		case WatchKindItem:
			if strings.TrimSpace(watch.Guidance) == "" {
				return fmt.Errorf("skills: %s is an item watch with no guidance on what is worth telling", where)
			}
		default:
			return fmt.Errorf("skills: %s is of kind %q, which is not mail or item", where, watch.Kind)
		}
		for field, value := range map[string]string{"every": watch.Every, "overlap": watch.Overlap} {
			if strings.TrimSpace(value) == "" {
				continue
			}
			if duration, err := time.ParseDuration(strings.TrimSpace(value)); err != nil || duration <= 0 {
				return fmt.Errorf("skills: the %s of %s is %q, which is not a duration such as 10m", field, where, value)
			}
		}
		if watch.List == nil || self.Tool(watch.List.Tool) == nil {
			return fmt.Errorf("skills: %s lists with a tool the skill does not have", where)
		}
		if watch.Read != nil {
			read := self.Tool(watch.Read.Tool)
			if read == nil {
				return fmt.Errorf("skills: %s reads with a tool the skill does not have", where)
			}
			if !declaredParameters(read)[WatchItemID] {
				return fmt.Errorf("skills: %s reads with %s, which takes no id", where, read.Name)
			}
		}
	}
	return nil
}
