package db

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ChannelOperation is a person's chat-app bots.
type ChannelOperation interface {
	// PutAgentChannel writes or replaces the person's bot for an app.
	PutAgentChannel(channel *models.AgentChannel) (*models.AgentChannel, error)
	GetAgentChannel(agentId string, kind models.AgentChannelKind) (*models.AgentChannel, error)
	ListAgentChannels(agentId string) ([]*models.AgentChannel, error)
	// ListEnabledAgentChannels is every bot that should be running, for
	// the manager that runs them.
	ListEnabledAgentChannels() ([]*models.AgentChannel, error)
	DeleteAgentChannel(agentId string, kind models.AgentChannelKind) error

	// ClaimAgentChannel takes or renews the claim to run a bot for this
	// instance until the moment given, and says whether it holds: it does
	// when nobody holds it, when the holder's claim has lapsed, or when
	// this instance holds it already.
	ClaimAgentChannel(channelId, instance string, until time.Time) (bool, error)
	// ReleaseAgentChannel gives a claim up, if this instance holds it.
	ReleaseAgentChannel(channelId, instance string) error
	// NoteAgentChannel records what the running bot saw: its name, the
	// last time it was heard from, and what went wrong.
	NoteAgentChannel(channelId, botName, lastError string, seenAt *time.Time) error
}

type agentChannelModel struct {
	ID           string     `gorm:"column:id;primaryKey"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	ModifiedAt   time.Time  `gorm:"column:modified_at"`
	AgentID      string     `gorm:"column:agent_id"`
	Kind         string     `gorm:"column:kind"`
	Token        string     `gorm:"column:token"`
	BotName      string     `gorm:"column:bot_name"`
	LinkedID     string     `gorm:"column:linked_id"`
	LinkedName   string     `gorm:"column:linked_name"`
	LinkCode     string     `gorm:"column:link_code"`
	Enabled      bool       `gorm:"column:enabled"`
	LastError    string     `gorm:"column:last_error"`
	LastSeenAt   *time.Time `gorm:"column:last_seen_at"`
	ClaimedBy    string     `gorm:"column:claimed_by"`
	ClaimedUntil *time.Time `gorm:"column:claimed_until"`
}

func (agentChannelModel) TableName() string { return "agent_channel" }

func (self *agentChannelModel) toModel() *models.AgentChannel {
	return &models.AgentChannel{ID: self.ID, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt, AgentID: self.AgentID, Kind: models.AgentChannelKind(self.Kind), Token: self.Token, BotName: self.BotName, LinkedID: self.LinkedID, LinkedName: self.LinkedName, LinkCode: self.LinkCode, Enabled: self.Enabled, LastError: self.LastError, LastSeenAt: self.LastSeenAt, ClaimedBy: self.ClaimedBy, ClaimedUntil: self.ClaimedUntil}
}

func (self *transaction) PutAgentChannel(channel *models.AgentChannel) (*models.AgentChannel, error) {
	if channel.AgentID == "" || channel.Kind == "" {
		return nil, fmt.Errorf("db: a channel needs an agent and a kind")
	}
	existing, err := self.GetAgentChannel(channel.AgentID, channel.Kind)
	if err != nil {
		return nil, err
	}
	stored := *channel
	stored.ModifiedAt = time.Now()
	if existing != nil {
		stored.ID = existing.ID
		stored.CreatedAt = existing.CreatedAt
	} else {
		stored.ID = newID()
		stored.CreatedAt = stored.ModifiedAt
	}
	model := &agentChannelModel{ID: stored.ID, CreatedAt: stored.CreatedAt, ModifiedAt: stored.ModifiedAt, AgentID: stored.AgentID, Kind: string(stored.Kind), Token: stored.Token, BotName: stored.BotName, LinkedID: stored.LinkedID, LinkedName: stored.LinkedName, LinkCode: stored.LinkCode, Enabled: stored.Enabled, LastError: stored.LastError, LastSeenAt: stored.LastSeenAt}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}, {Name: "kind"}},
		DoUpdates: clause.AssignmentColumns([]string{"modified_at", "token", "bot_name", "linked_id", "linked_name", "link_code", "enabled", "last_error", "last_seen_at"}),
	}).Create(model).Error; err != nil {
		return nil, err
	}
	return &stored, nil
}

func (self *transaction) GetAgentChannel(agentId string, kind models.AgentChannelKind) (*models.AgentChannel, error) {
	var found []agentChannelModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"kind\" = ?", agentId, string(kind)).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListAgentChannels(agentId string) ([]*models.AgentChannel, error) {
	var found []agentChannelModel
	if err := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"kind\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	channels := make([]*models.AgentChannel, 0, len(found))
	for index := range found {
		channels = append(channels, found[index].toModel())
	}
	return channels, nil
}

func (self *transaction) ListEnabledAgentChannels() ([]*models.AgentChannel, error) {
	var found []agentChannelModel
	if err := self.tx.Where("\"enabled\" = TRUE AND \"token\" <> ''").Order("\"agent_id\" ASC, \"kind\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	channels := make([]*models.AgentChannel, 0, len(found))
	for index := range found {
		channels = append(channels, found[index].toModel())
	}
	return channels, nil
}

func (self *transaction) DeleteAgentChannel(agentId string, kind models.AgentChannelKind) error {
	return self.tx.Where("\"agent_id\" = ? AND \"kind\" = ?", agentId, string(kind)).Delete(&agentChannelModel{}).Error
}

func (self *transaction) ClaimAgentChannel(channelId, instance string, until time.Time) (bool, error) {
	result := self.tx.Model(&agentChannelModel{}).
		Where("\"id\" = ? AND (\"claimed_until\" IS NULL OR \"claimed_until\" < ? OR \"claimed_by\" = ?)", channelId, time.Now(), instance).
		Updates(map[string]any{"claimed_by": instance, "claimed_until": until})
	return result.RowsAffected > 0, result.Error
}

func (self *transaction) ReleaseAgentChannel(channelId, instance string) error {
	return self.tx.Model(&agentChannelModel{}).
		Where("\"id\" = ? AND \"claimed_by\" = ?", channelId, instance).
		Updates(map[string]any{"claimed_by": "", "claimed_until": nil}).Error
}

func (self *transaction) NoteAgentChannel(channelId, botName, lastError string, seenAt *time.Time) error {
	changes := map[string]any{"last_error": lastError}
	if botName != "" {
		changes["bot_name"] = botName
	}
	if seenAt != nil {
		changes["last_seen_at"] = seenAt
	}
	return self.tx.Model(&agentChannelModel{}).Where("\"id\" = ?", channelId).Updates(changes).Error
}
