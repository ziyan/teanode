package apigraph

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// A person's chat apps: the bot they made in Telegram or Discord, whose
// token they hand their agent here, and the one chat they link to it with
// a code the page shows. The bots themselves are run by the server
// (internal/channel); this is the person's part.

// AgentChannelQuery lists a person's chat apps.
type AgentChannelQuery interface {
	// The chat apps the caller set up for their agent, with the code a
	// chat sends to link itself; never the token. Needs agent:use.
	ListAgentChannels(ctx context.Context) ([]*AgentChannelView, error)
}

// AgentChannelMutation changes them.
type AgentChannelMutation interface {
	// Set a chat app's bot: a token (kept sealed; a new one drops the
	// linked chat and draws a new code) and whether it runs. Needs
	// agent:use.
	SetAgentChannel(ctx context.Context, arguments SetAgentChannelArguments) (*AgentChannelView, error)

	// Drop the linked chat and draw a new code, so another chat can link.
	// Needs agent:use.
	UnlinkAgentChannel(ctx context.Context, arguments AgentChannelArguments) (*AgentChannelView, error)

	// Forget a chat app's bot altogether. Needs agent:use.
	RemoveAgentChannel(ctx context.Context, arguments AgentChannelArguments) (bool, error)
}

// AgentChannelView is one chat app of the person's.
type AgentChannelView struct {
	Kind       string     `json:"kind"`
	HasToken   bool       `json:"hasToken"`
	BotName    string     `json:"botName,omitempty"`
	Linked     bool       `json:"linked"`
	LinkedName string     `json:"linkedName,omitempty"`
	LinkCode   string     `json:"linkCode,omitempty"`
	Enabled    bool       `json:"enabled"`
	Running    bool       `json:"running"`
	LastError  string     `json:"lastError,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

// SetAgentChannelArguments name the app and what to set.
type SetAgentChannelArguments struct {
	// telegram or discord
	Kind string `json:"kind"`
	// The bot's token; empty keeps the one there is
	Token string `json:"token" graphapi:"nullable"`
	// Whether the bot runs; empty keeps it as it is
	Enabled *bool `json:"enabled" graphapi:"nullable"`
}

// AgentChannelArguments name the app.
type AgentChannelArguments struct {
	// telegram or discord
	Kind string `json:"kind"`
}

func channelKindOf(kind string) (models.AgentChannelKind, error) {
	for _, known := range models.AgentChannelKinds {
		if string(known) == strings.ToLower(strings.TrimSpace(kind)) {
			return known, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not a chat app; telegram and discord are", api.ErrInvalidArguments, kind)
}

func channelView(channel *models.AgentChannel) *AgentChannelView {
	running := channel.ClaimedUntil != nil && channel.ClaimedUntil.After(time.Now()) && channel.LastError == ""
	return &AgentChannelView{
		Kind: string(channel.Kind), HasToken: channel.Token != "", BotName: channel.BotName,
		Linked: channel.Linked(), LinkedName: channel.LinkedName, LinkCode: channel.LinkCode,
		Enabled: channel.Enabled, Running: running, LastError: channel.LastError, LastSeenAt: channel.LastSeenAt,
	}
}

// newLinkCode is what a chat sends to link itself: six characters from an
// alphabet without look-alikes, drawn at random.
func newLinkCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	code := make([]byte, len(bytes))
	for index, value := range bytes {
		code[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(code)
}

func (self *graph) ListAgentChannels(ctx context.Context) ([]*AgentChannelView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := self.transaction(ctx).ListAgentChannels(found.ID)
	if err != nil {
		return nil, translateError(err)
	}
	views := make([]*AgentChannelView, 0, len(channels))
	for _, channel := range channels {
		views = append(views, channelView(channel))
	}
	return views, nil
}

func (self *graph) SetAgentChannel(ctx context.Context, arguments SetAgentChannelArguments) (*AgentChannelView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if !agent.FeatureAllowed(self.config.Current(), "chatApps") {
		return nil, fmt.Errorf("%w: chat apps are off on this server", api.ErrInvalidArguments)
	}
	kind, err := channelKindOf(arguments.Kind)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, fmt.Errorf("%w: the agent is off on this server", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	channel, err := tx.GetAgentChannel(found.ID, kind)
	if err != nil {
		return nil, translateError(err)
	}
	if channel == nil {
		channel = &models.AgentChannel{AgentID: found.ID, Kind: kind, Enabled: true, LinkCode: newLinkCode()}
	}
	if token := strings.TrimSpace(arguments.Token); token != "" {
		sealed, err := worker.SealSecret(token)
		if err != nil {
			return nil, err
		}
		// A new bot: whoever was linked to the old one is not linked to
		// this one, and the running bot is told by the row's change.
		channel.Token = sealed
		channel.BotName = ""
		channel.LinkedID, channel.LinkedName = "", ""
		channel.LinkCode = newLinkCode()
		channel.LastError = ""
	}
	if channel.Token == "" {
		return nil, fmt.Errorf("%w: a bot token is needed", api.ErrInvalidArguments)
	}
	if arguments.Enabled != nil {
		channel.Enabled = *arguments.Enabled
	}
	stored, err := tx.PutAgentChannel(channel)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s set their %s bot for their agent", operatorName(ctx), kind)
	return channelView(stored), nil
}

func (self *graph) UnlinkAgentChannel(ctx context.Context, arguments AgentChannelArguments) (*AgentChannelView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	kind, err := channelKindOf(arguments.Kind)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	channel, err := tx.GetAgentChannel(found.ID, kind)
	if err != nil {
		return nil, translateError(err)
	}
	if channel == nil {
		return nil, fmt.Errorf("%w: no %s bot is set", api.ErrInvalidArguments, kind)
	}
	channel.LinkedID, channel.LinkedName = "", ""
	channel.LinkCode = newLinkCode()
	stored, err := tx.PutAgentChannel(channel)
	if err != nil {
		return nil, translateError(err)
	}
	return channelView(stored), nil
}

func (self *graph) RemoveAgentChannel(ctx context.Context, arguments AgentChannelArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	kind, err := channelKindOf(arguments.Kind)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).DeleteAgentChannel(found.ID, kind); err != nil {
		return false, translateError(err)
	}
	log.Noticef("%s removed their %s bot", operatorName(ctx), kind)
	return true, nil
}
