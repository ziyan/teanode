package agent

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The recording glossary: the names and terms from the person's own data that
// a transcript is likely to contain, so that a misheard "Tea node" can be
// spelled the way the person spells it. It is built only from what the person
// keeps (their notes, contacts that they gave their agent, calendar events
// around the recording and a title they typed), never from the transcript
// itself, which is other people's speech and therefore not trusted.

const (
	// recordingGlossaryTermCount bounds the whole glossary.
	recordingGlossaryTermCount = 400

	// recordingGlossaryNodeCandidateCount is how many graph pages are read
	// from the graph index, which lists pinned and important pages first.
	recordingGlossaryNodeCandidateCount = 1500

	// recordingGlossaryContactCount is how many contacts one address book
	// contributes.
	recordingGlossaryContactCount = 200

	// recordingGlossaryTermCharacters bounds one term; a longer string is a
	// sentence, not a name.
	recordingGlossaryTermCharacters = 80

	// recordingGlossaryCalendarMargin is how far either side of the moment
	// a recording was made an event still counts as the meeting it was.
	recordingGlossaryCalendarMargin = time.Hour

	// recordingVocabularyPromptTermCount and recordingVocabularyPromptCharacters
	// keep the hint sent to the transcription provider under about 200 tokens.
	recordingVocabularyPromptTermCount    = 40
	recordingVocabularyPromptCharacters   = 700
	recordingVocabularyPromptIntroduction = "Names and terms that may be spoken: "
)

// recordingGlossaryNodeKinds are the kinds of page whose names are worth
// spelling right. Folders and periods are filing, not things people say.
var recordingGlossaryNodeKinds = map[models.AgentNodeKind]bool{
	models.NodeSelf: true, models.NodePerson: true, models.NodeOrganization: true, models.NodeProject: true,
	models.NodeTopic: true, models.NodePlace: true, models.NodeThing: true,
}

// cleanGlossaryTerm keeps a name as one short line of printable text, or
// returns "" for something that cannot be a name.
func cleanGlossaryTerm(term string) string {
	term = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, term)
	term = strings.Join(strings.Fields(term), " ")
	if len([]rune(term)) < 2 || len([]rune(term)) > recordingGlossaryTermCharacters {
		return ""
	}
	return term
}

// glossaryTerms collects terms in the order they are added, once each
// (compared without regard to case), up to the bound.
type glossaryTerms struct {
	terms []string
	seen  map[string]bool
}

func (self *glossaryTerms) add(term string) {
	term = cleanGlossaryTerm(term)
	if term == "" || len(self.terms) >= recordingGlossaryTermCount {
		return
	}
	key := strings.ToLower(term)
	if self.seen == nil {
		self.seen = map[string]bool{}
	}
	if self.seen[key] {
		return
	}
	self.seen[key] = true
	self.terms = append(self.terms, term)
}

// buildRecordingGlossary reads the glossary for one recording, in priority
// order: the title the person typed, the people and subject of the calendar
// events around the recording, the pages of the agent's memory graph, and the
// contacts the person gave their agent. The pages are the first
// recordingGlossaryNodeCandidateCount of the graph index (pinned, then most
// important, then most recently changed), listed here most recently changed
// first.
func buildRecordingGlossary(transaction db.Transaction, agent *models.Agent, source *models.Recording) ([]string, error) {
	collected := &glossaryTerms{}
	if source.RecordingTitleSource == models.RecordingTitleSourceUser {
		collected.add(source.RecordingTitle)
	}
	if source.RecordedAt != nil {
		if err := addCalendarGlossaryTerms(transaction, agent.UserID, *source.RecordedAt, collected); err != nil {
			return nil, err
		}
	}
	nodes, err := transaction.ListAgentIndex(agent.ID, recordingGlossaryNodeCandidateCount)
	if err != nil {
		return nil, err
	}
	var kept []*models.AgentNode
	for _, node := range nodes {
		if recordingGlossaryNodeKinds[node.Kind] && !node.Dormant {
			kept = append(kept, node)
		}
	}
	sort.SliceStable(kept, func(first, second int) bool { return kept[first].ModifiedAt.After(kept[second].ModifiedAt) })
	for _, node := range kept {
		collected.add(node.Name)
		for _, alias := range node.Aliases {
			collected.add(alias)
		}
	}
	books, err := transaction.ListAddressBooks(agent.UserID)
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		if !book.AgentGranted {
			continue
		}
		contacts, err := transaction.ListContacts(book.ID, "", recordingGlossaryContactCount)
		if err != nil {
			return nil, err
		}
		for _, contact := range contacts {
			collected.add(contact.Name)
		}
	}
	return collected.terms, nil
}

// addCalendarGlossaryTerms adds the title and the named attendees of the
// events that were on within an hour of the moment the recording was made, from
// the calendars the person gave their agent.
func addCalendarGlossaryTerms(transaction db.Transaction, userId string, recordedAt time.Time, collected *glossaryTerms) error {
	calendars, err := transaction.ListCalendars(userId)
	if err != nil {
		return err
	}
	for _, calendarRow := range calendars {
		if !calendarRow.AgentGranted || calendarRow.CalendarKind != models.CalendarEvents {
			continue
		}
		occurrences, err := transaction.ListOccurrences(calendarRow.ID, recordedAt.Add(-recordingGlossaryCalendarMargin), recordedAt.Add(recordingGlossaryCalendarMargin))
		if err != nil {
			return err
		}
		for _, occurrence := range occurrences {
			object, err := transaction.GetCalendarObject(calendarRow.ID, occurrence.ObjectID)
			if err != nil {
				return err
			}
			if object == nil {
				continue
			}
			collected.add(object.Summary)
			parsed, err := calendar.Parse([]byte(object.Data))
			if err != nil {
				continue
			}
			for _, attendee := range parsed.Attendees {
				collected.add(attendee.Name)
			}
		}
	}
	return nil
}

// recordingVocabularyPrompt is the hint sent with the audio: the first terms
// of the glossary, as many as fit in the bound, or "" when there are none.
func recordingVocabularyPrompt(terms []string) string {
	var prompt strings.Builder
	count := 0
	for _, term := range terms {
		if count >= recordingVocabularyPromptTermCount {
			break
		}
		addition := term
		if count > 0 {
			addition = ", " + term
		}
		if prompt.Len() == 0 {
			prompt.WriteString(recordingVocabularyPromptIntroduction)
		}
		if prompt.Len()+len(addition)+1 > recordingVocabularyPromptCharacters {
			break
		}
		prompt.WriteString(addition)
		count++
	}
	if count == 0 {
		return ""
	}
	prompt.WriteString(".")
	return prompt.String()
}
