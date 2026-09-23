package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/sources"
)

// A source's type may declare secrets: a token for a service's web API,
// say. The person whose source it is fills them in, one value for each
// source, sealed with the server secret; they are opened only to be sent
// to the computer that reads the source, and never come back out of here.

// AgentSourceSecretQuery reads which secrets a source asks for.
type AgentSourceSecretQuery interface {
	// The secrets one of the caller's sources asks for, and whether each
	// is filled in. The values never come back. Needs agent:use.
	ListAgentKnowledgeSourceSecrets(ctx context.Context, arguments AgentSourceSecretsArguments) ([]*AgentSourceSecretView, error)
}

// AgentSourceSecretMutation fills them in and forgets them.
type AgentSourceSecretMutation interface {
	// Keep one value for one of the caller's sources, sealed. Needs
	// agent:use.
	SetAgentKnowledgeSourceSecret(ctx context.Context, arguments SetAgentSourceSecretArguments) (*AgentSourceSecretView, error)

	// Forget one value. Needs agent:use.
	ClearAgentKnowledgeSourceSecret(ctx context.Context, arguments ClearAgentSourceSecretArguments) (bool, error)
}

// AgentSourceSecretView is one secret a source asks for.
type AgentSourceSecretView struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	IsOptional  bool   `json:"isOptional"`
	IsSet       bool   `json:"isSet"`
}

// AgentSourceSecretsArguments name a source.
type AgentSourceSecretsArguments struct {
	SourceID string `json:"sourceId"`
}

// SetAgentSourceSecretArguments say which source, which key and what value.
type SetAgentSourceSecretArguments struct {
	SourceID string `json:"sourceId"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}

// ClearAgentSourceSecretArguments say which source and which key.
type ClearAgentSourceSecretArguments struct {
	SourceID string `json:"sourceId"`
	Key      string `json:"key"`
}

// sourceSecretsAsked is the secrets one of the caller's sources asks for,
// with whether each is set, and the agent the source is of.
func (self *graph) sourceSecretsAsked(ctx context.Context, found *models.Agent, sourceId string) ([]*AgentSourceSecretView, error) {
	var views []*AgentSourceSecretView
	if err := func(tx db.Transaction) error {
		// Locked, so a change of type committed meanwhile cannot leave a
		// value checked against the old type behind for the new one.
		source, err := tx.LockAgentSource(found.ID, sourceId)
		if err != nil {
			return err
		}
		if source == nil {
			return api.ErrNotFound
		}
		views = []*AgentSourceSecretView{}
		if source.Specification.Type == "" {
			return nil
		}
		installed, err := tx.GetAgentSourceType(source.Specification.Type)
		if err != nil || installed == nil {
			return err
		}
		parsed, err := sources.Parse([]byte(installed.Content))
		if err != nil {
			return nil
		}
		stored, err := tx.ListAgentSourceSecrets(source.ID)
		if err != nil {
			return err
		}
		isSet := map[string]bool{}
		for _, secret := range stored {
			isSet[secret.Key] = secret.Value != ""
		}
		for _, secret := range parsed.Secrets {
			views = append(views, &AgentSourceSecretView{
				Key: secret.Key, Description: secret.Description, IsOptional: secret.Optional, IsSet: isSet[secret.Key],
			})
		}
		return nil
	}(self.writing(ctx)); err != nil {
		return nil, err
	}
	return views, nil
}

func (self *graph) ListAgentKnowledgeSourceSecrets(ctx context.Context, arguments AgentSourceSecretsArguments) ([]*AgentSourceSecretView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.sourceSecretsAsked(ctx, found, arguments.SourceID)
}

func (self *graph) SetAgentKnowledgeSourceSecret(ctx context.Context, arguments SetAgentSourceSecretArguments) (*AgentSourceSecretView, error) {
	key := strings.TrimSpace(arguments.Key)
	// A token pasted with a newline after it is the token.
	value := strings.TrimSpace(arguments.Value)
	if key == "" || value == "" {
		return nil, fmt.Errorf("%w: a key and a value are needed", api.ErrInvalidArguments)
	}
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	asked, err := self.sourceSecretsAsked(ctx, found, arguments.SourceID)
	if err != nil {
		return nil, err
	}
	// Only a key the source's type asks for, so the table cannot be used as
	// somewhere to keep arbitrary secrets.
	var wanted *AgentSourceSecretView
	for _, view := range asked {
		if view.Key == key {
			wanted = view
		}
	}
	if wanted == nil {
		return nil, fmt.Errorf("%w: this source's type does not ask for %s", api.ErrInvalidArguments, key)
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	sealed, err := worker.SealSecret(value)
	if err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	if err := tx.PutAgentSourceSecret(found.ID, &models.AgentSourceSecret{SourceID: arguments.SourceID, Key: key, Value: sealed}); err != nil {
		return nil, err
	}
	// A source that was waiting for this reads now, not at its hour.
	if source, err := tx.LockAgentSource(found.ID, arguments.SourceID); err == nil && source != nil && source.Enabled {
		now := time.Now()
		source.NextRunAt = &now
		if _, err := tx.PutAgentSource(source); err != nil {
			return nil, err
		}
	}
	wanted.IsSet = true
	return wanted, nil
}

func (self *graph) ClearAgentKnowledgeSourceSecret(ctx context.Context, arguments ClearAgentSourceSecretArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	asked, err := self.sourceSecretsAsked(ctx, found, arguments.SourceID)
	if err != nil {
		return false, err
	}
	// One declared key: an empty one would forget every value.
	key := strings.TrimSpace(arguments.Key)
	declared := false
	for _, view := range asked {
		declared = declared || view.Key == key
	}
	if !declared {
		return false, fmt.Errorf("%w: this source's type does not ask for %q", api.ErrInvalidArguments, key)
	}
	return true, self.writing(ctx).DeleteAgentSourceSecret(found.ID, arguments.SourceID, key)
}
