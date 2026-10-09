package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// IdeaOperation keeps the agent's ideas: offers of work it can do for the
// person, and what became of each.
type IdeaOperation interface {
	// UpsertAgentIdea records an idea, or refreshes the words, the tools,
	// the evidence, the rank and the expiry of the one with the same key,
	// leaving what became of it alone: reading the catalog again never
	// brings back an idea the person dismissed.
	UpsertAgentIdea(idea *models.AgentIdea) (*models.AgentIdea, error)

	// GetAgentIdea is one of the agent's ideas, or nil.
	GetAgentIdea(agentId, ideaId string) (*models.AgentIdea, error)

	// UpdateAgentIdea changes one of the agent's ideas.
	UpdateAgentIdea(agentId, ideaId string, modify func(*models.AgentIdea) error) (*models.AgentIdea, error)

	// ListAgentIdeas is the agent's ideas in any of the statuses and kinds
	// given, or in every one when none are, the highest ranked first and
	// then the newest.
	ListAgentIdeas(agentId string, statuses []models.AgentIdeaStatus, kinds []models.AgentIdeaKind) ([]*models.AgentIdea, error)

	// MarkAgentIdeasShown notes that ideas were put in front of the person,
	// for those not shown before.
	MarkAgentIdeasShown(agentId string, ideaIds []string, at time.Time) error
}

type agentIdeaModel struct {
	ID                    string         `gorm:"column:id;primaryKey"`
	AgentID               string         `gorm:"column:agent_id"`
	IdeaKey               string         `gorm:"column:idea_key"`
	IdeaKind              string         `gorm:"column:idea_kind"`
	IdeaCategory          string         `gorm:"column:idea_category"`
	Emoji                 string         `gorm:"column:emoji"`
	Headline              string         `gorm:"column:headline"`
	Body                  string         `gorm:"column:body"`
	OpeningRequest        string         `gorm:"column:opening_request"`
	NeededToolNames       pq.StringArray `gorm:"column:needed_tool_names;type:text[]"`
	Evidence              []byte         `gorm:"column:evidence;type:jsonb"`
	SuggestionReason      string         `gorm:"column:suggestion_reason"`
	IdeaStatus            string         `gorm:"column:idea_status"`
	ExpiredReason         string         `gorm:"column:expired_reason"`
	IsRestoredByPerson    bool           `gorm:"column:is_restored_by_person"`
	RankScore             float64        `gorm:"column:rank_score"`
	StartedConversationID string         `gorm:"column:started_conversation_id"`
	CreatedAt             time.Time      `gorm:"column:created_at"`
	ModifiedAt            time.Time      `gorm:"column:modified_at"`
	ShownAt               *time.Time     `gorm:"column:shown_at"`
	StartedAt             *time.Time     `gorm:"column:started_at"`
	ClosedAt              *time.Time     `gorm:"column:closed_at"`
	ExpiresAt             *time.Time     `gorm:"column:expires_at"`
}

func (agentIdeaModel) TableName() string { return "agent_idea" }

func (self *agentIdeaModel) toModel() *models.AgentIdea {
	evidence := []models.AgentIdeaEvidence{}
	if len(self.Evidence) > 0 {
		if err := json.Unmarshal(self.Evidence, &evidence); err != nil {
			log.Warningf("the evidence of idea %q could not be read: %s", self.ID, err)
		}
	}
	return &models.AgentIdea{
		ID: self.ID, AgentID: self.AgentID, IdeaKey: self.IdeaKey,
		IdeaKind: models.AgentIdeaKind(self.IdeaKind), IdeaCategory: models.AgentIdeaCategory(self.IdeaCategory),
		Emoji: self.Emoji, Headline: self.Headline, Body: self.Body, OpeningRequest: self.OpeningRequest,
		NeededToolNames: append([]string{}, self.NeededToolNames...), Evidence: evidence,
		SuggestionReason: self.SuggestionReason, IdeaStatus: models.AgentIdeaStatus(self.IdeaStatus),
		ExpiredReason: models.AgentIdeaExpiredReason(self.ExpiredReason), IsRestoredByPerson: self.IsRestoredByPerson,
		RankScore: self.RankScore, StartedConversationID: self.StartedConversationID,
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
		ShownAt: inLocal(self.ShownAt), StartedAt: inLocal(self.StartedAt),
		ClosedAt: inLocal(self.ClosedAt), ExpiresAt: inLocal(self.ExpiresAt),
	}
}

func agentIdeaToModel(idea *models.AgentIdea) (*agentIdeaModel, error) {
	evidence := idea.Evidence
	if evidence == nil {
		evidence = []models.AgentIdeaEvidence{}
	}
	written, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	// Why it expired belongs to an expired idea only, so whatever takes an
	// idea out of expired, reopening it or the catalog offering it again,
	// cannot leave the old reason behind.
	expiredReason := idea.ExpiredReason
	if idea.IdeaStatus != models.IdeaExpired {
		expiredReason = ""
	}
	return &agentIdeaModel{
		ID: idea.ID, AgentID: idea.AgentID, IdeaKey: idea.IdeaKey,
		IdeaKind: string(idea.IdeaKind), IdeaCategory: string(idea.IdeaCategory),
		Emoji: idea.Emoji, Headline: idea.Headline, Body: idea.Body, OpeningRequest: idea.OpeningRequest,
		NeededToolNames: pq.StringArray(append([]string{}, idea.NeededToolNames...)), Evidence: written,
		SuggestionReason: idea.SuggestionReason, IdeaStatus: string(idea.IdeaStatus),
		ExpiredReason: string(expiredReason), IsRestoredByPerson: idea.IsRestoredByPerson,
		RankScore: idea.RankScore, StartedConversationID: idea.StartedConversationID,
		CreatedAt: idea.CreatedAt, ModifiedAt: idea.ModifiedAt,
		ShownAt: idea.ShownAt, StartedAt: idea.StartedAt, ClosedAt: idea.ClosedAt, ExpiresAt: idea.ExpiresAt,
	}, nil
}

// inLocal is a moment in the server's zone, or nil.
func inLocal(moment *time.Time) *time.Time {
	if moment == nil {
		return nil
	}
	local := moment.In(time.Local)
	return &local
}

// validIdea refuses an idea no list could show.
func validIdea(idea *models.AgentIdea) error {
	if strings.TrimSpace(idea.IdeaKey) == "" || strings.TrimSpace(idea.Headline) == "" {
		return fmt.Errorf("%w: an idea needs a key and a headline", ErrInvalidArguments)
	}
	if idea.IdeaKind != models.IdeaCatalog && idea.IdeaKind != models.IdeaPersonal {
		return fmt.Errorf("%w: %q is not a kind of idea", ErrInvalidArguments, idea.IdeaKind)
	}
	if _, ok := models.IdeaCategoryOf(idea.IdeaCategory); !ok {
		return fmt.Errorf("%w: %q is not an area an idea can be in", ErrInvalidArguments, idea.IdeaCategory)
	}
	if !idea.IdeaStatus.IsValid() {
		return fmt.Errorf("%w: %q is not what can become of an idea", ErrInvalidArguments, idea.IdeaStatus)
	}
	if !idea.ExpiredReason.IsValid() {
		return fmt.Errorf("%w: %q is not why an idea expires", ErrInvalidArguments, idea.ExpiredReason)
	}
	return nil
}

func (self *transaction) UpsertAgentIdea(idea *models.AgentIdea) (*models.AgentIdea, error) {
	if idea.IdeaStatus == "" {
		idea.IdeaStatus = models.IdeaOpen
	}
	if err := validIdea(idea); err != nil {
		return nil, err
	}
	now := time.Now()
	row, err := agentIdeaToModel(idea)
	if err != nil {
		return nil, err
	}
	row.ID, row.CreatedAt, row.ModifiedAt = newID(), now, now
	if err := self.tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "agent_id"}, {Name: "idea_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"idea_category", "emoji", "headline", "body", "opening_request", "needed_tool_names",
			"evidence", "suggestion_reason", "rank_score", "expires_at", "modified_at",
		}),
	}).Create(row).Error; err != nil {
		return nil, err
	}
	var found []agentIdeaModel
	if err := self.tx.Where(`"agent_id" = ? AND "idea_key" = ?`, idea.AgentID, idea.IdeaKey).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	return found[0].toModel(), nil
}

func (self *transaction) GetAgentIdea(agentId, ideaId string) (*models.AgentIdea, error) {
	var found []agentIdeaModel
	if err := self.tx.Where(`"id" = ? AND "agent_id" = ?`, ideaId, agentId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) UpdateAgentIdea(agentId, ideaId string, modify func(*models.AgentIdea) error) (*models.AgentIdea, error) {
	var locked agentIdeaModel
	if err := lockRow(self.tx, &locked, ideaId); err != nil {
		return nil, err
	}
	if locked.AgentID != agentId {
		return nil, ErrNotFound
	}
	idea := locked.toModel()
	if err := modify(idea); err != nil {
		return nil, err
	}
	if err := validIdea(idea); err != nil {
		return nil, err
	}
	// Whose it is, what it is called and when it was made are not the
	// modifier's.
	idea.ID, idea.AgentID, idea.IdeaKey, idea.CreatedAt = locked.ID, locked.AgentID, locked.IdeaKey, locked.CreatedAt
	idea.ModifiedAt = time.Now()
	row, err := agentIdeaToModel(idea)
	if err != nil {
		return nil, err
	}
	if err := self.tx.Save(row).Error; err != nil {
		return nil, err
	}
	return self.GetAgentIdea(agentId, ideaId)
}

func (self *transaction) ListAgentIdeas(agentId string, statuses []models.AgentIdeaStatus, kinds []models.AgentIdeaKind) ([]*models.AgentIdea, error) {
	query := self.tx.Where(`"agent_id" = ?`, agentId)
	if len(statuses) > 0 {
		query = query.Where(`"idea_status" IN ?`, statuses)
	}
	if len(kinds) > 0 {
		query = query.Where(`"idea_kind" IN ?`, kinds)
	}
	var found []agentIdeaModel
	if err := query.Order(`"rank_score" DESC, "created_at" DESC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	ideas := make([]*models.AgentIdea, 0, len(found))
	for index := range found {
		ideas = append(ideas, found[index].toModel())
	}
	return ideas, nil
}

func (self *transaction) MarkAgentIdeasShown(agentId string, ideaIds []string, at time.Time) error {
	if len(ideaIds) == 0 {
		return nil
	}
	// Rows another request holds are skipped rather than waited for: being
	// shown is noted again the next time, and a refresh locking the same
	// rows in another order would otherwise deadlock with this.
	return self.tx.Exec(`UPDATE "agent_idea" SET "shown_at" = ? WHERE "id" IN (
		SELECT "id" FROM "agent_idea" WHERE "agent_id" = ? AND "id" IN ? AND "shown_at" IS NULL ORDER BY "id" FOR UPDATE SKIP LOCKED)`,
		at, agentId, ideaIds).Error
}
