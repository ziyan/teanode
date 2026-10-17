package db

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/models"
)

// LocationKnowledgeOperation is what the memory is told of where a person
// went: one source for each agent, and one document in it for each finished
// day. The text of a day is written by the caller from the person's visits;
// the dream's reading takes the documents like any others.
type LocationKnowledgeOperation interface {
	// EnsureLocationSource answers the agent's location source, making it,
	// switched on, the first time. It has no schedule: nothing reads it from
	// anywhere, the documents are put in directly.
	EnsureLocationSource(agentId string) (*models.AgentKnowledgeSource, error)

	// PutLocationDayDocument files the text of one finished day. The day is
	// its start in the person's own time zone. Text that is the same as what
	// is filed already changes nothing, so the day is not read again, and
	// isChanged says whether anything was written.
	PutLocationDayDocument(agentId, sourceId string, day time.Time, text string) (isChanged bool, err error)

	// GetLatestLocationDayStart answers when the newest day filed in the
	// source began, or nil when none is.
	GetLatestLocationDayStart(sourceId string) (*time.Time, error)
}

// locationDayExternalID is the name a day goes by in its source.
func locationDayExternalID(day time.Time) string {
	return "location:" + day.Format(time.DateOnly)
}

func (self *transaction) EnsureLocationSource(agentId string) (*models.AgentKnowledgeSource, error) {
	if agentId == "" {
		return nil, ErrInvalidArguments
	}
	// The agent's row is taken first, so that two callers making the source
	// do not both find none.
	if err := lockRow(self.tx, &agentModel{}, agentId); err != nil {
		return nil, err
	}
	sources, err := self.ListAgentSources(agentId)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if source.Kind == models.SourceLocation {
			return source, nil
		}
	}
	return self.PutAgentSource(&models.AgentKnowledgeSource{
		AgentID: agentId, Kind: models.SourceLocation, Name: "location", Enabled: true,
	})
}

func (self *transaction) PutLocationDayDocument(agentId, sourceId string, day time.Time, text string) (bool, error) {
	if agentId == "" || sourceId == "" || day.IsZero() || text == "" {
		return false, ErrInvalidArguments
	}
	externalId := locationDayExternalID(day)
	digest := sha256.Sum256([]byte(text))
	hash := hex.EncodeToString(digest[:])
	existing, err := self.GetAgentDocumentByExternal(sourceId, externalId)
	if err != nil {
		return false, err
	}
	if existing != nil && existing.Hash == hash {
		return false, nil
	}
	dayStart := day
	document, err := self.PutAgentDocument(&models.AgentDocument{
		AgentID: agentId, SourceID: sourceId, ExternalID: externalId, Kind: models.DocumentJournal,
		Title:      "Where they went on " + day.Format("Monday 2 January 2006"),
		URL:        "/location?day=" + day.Format(time.DateOnly),
		HappenedAt: &dayStart, Hash: hash, Bytes: int64(len(text)), Private: true,
	})
	if err != nil {
		return false, err
	}
	if err := self.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: text, Segmented: isSegmentable(text)}}); err != nil {
		return false, err
	}
	return true, nil
}

func (self *transaction) GetLatestLocationDayStart(sourceId string) (*time.Time, error) {
	var found []agentDocumentModel
	if err := self.tx.Where(`"source_id" = ? AND "happened_at" IS NOT NULL`, sourceId).
		Order(`"happened_at" DESC`).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].HappenedAt, nil
}

// isSegmentable says PostgreSQL can split the text into words for the word
// index. Text that is mostly Han, Hiragana or Katakana cannot, and is found
// by meaning alone; the reading of files makes the same choice
// (agent.segmentable).
func isSegmentable(text string) bool {
	unsegmentedCount, letterCount := 0, 0
	for _, character := range text {
		if !unicode.IsLetter(character) {
			continue
		}
		letterCount++
		if unicode.Is(unicode.Han, character) || unicode.Is(unicode.Hiragana, character) || unicode.Is(unicode.Katakana, character) {
			unsegmentedCount++
		}
	}
	return letterCount == 0 || float64(unsegmentedCount)/float64(letterCount) < 0.3
}
