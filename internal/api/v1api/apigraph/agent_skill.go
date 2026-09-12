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
	"github.com/ziyan/teanode/internal/skills"
)

// Skills are tools that arrive without a release: a file of declarations
// fetched from a registry that signs what it publishes, installed by an
// operator, offered to everybody under the ordinary tool policy.

// AgentSkillQuery reads them.
type AgentSkillQuery interface {
	// The skills installed on this server, with what each declares.
	// Needs agent:use.
	ListAgentSkills(ctx context.Context) ([]*AgentSkillView, error)

	// What the registry publishes, with whether each is installed and
	// whether what is installed is behind. Needs server:manage, because
	// it reaches out of this server.
	SearchAgentSkills(ctx context.Context, arguments SearchAgentSkillsArguments) ([]*AgentSkillOffer, error)
}

// AgentSkillMutation installs and removes them.
type AgentSkillMutation interface {
	// Install a skill from the registry, or replace the installed one with
	// a newer version. The signature and the hash are checked before
	// anything is stored. Needs server:manage.
	InstallAgentSkill(ctx context.Context, arguments InstallAgentSkillArguments) (*AgentSkillView, error)

	// Take a skill away, with the tools it brought. Needs server:manage.
	RemoveAgentSkill(ctx context.Context, arguments AgentSkillArguments) (bool, error)

	// Offer a skill's tools, or stop offering them without taking it
	// away. Needs server:manage.
	SetAgentSkillEnabled(ctx context.Context, arguments SetAgentSkillEnabledArguments) (*AgentSkillView, error)
}

// AgentSkillView is an installed skill and what it declares.
type AgentSkillView struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Publisher   string    `json:"publisher"`
	URL         string    `json:"url"`
	Enabled     bool      `json:"enabled"`
	InstalledAt time.Time `json:"installedAt"`

	// Tools are what it declares, by name; Secrets the values it needs an
	// operator to fill in. Readable says whether the file still parses.
	Tools    []*AgentSkillToolView `json:"tools"`
	Secrets  []string              `json:"secrets"`
	Readable bool                  `json:"readable"`
	Problem  string                `json:"problem,omitempty"`
}

// AgentSkillToolView is one tool a skill declares.
type AgentSkillToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`

	// NeedsComputer says whether carrying it out runs a command, which
	// only happens on a computer the person attached.
	NeedsComputer bool `json:"needsComputer"`
}

// AgentSkillOffer is one skill the registry publishes.
type AgentSkillOffer struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`

	// Installed is the version on this server, empty when there is none;
	// Newer says whether the registry has moved on.
	Installed string `json:"installed,omitempty"`
	Newer     bool   `json:"newer"`
}

// SearchAgentSkillsArguments narrow what the registry offers.
type SearchAgentSkillsArguments struct {
	Query string `json:"query" graphapi:"nullable"`
}

// InstallAgentSkillArguments name the skill to install.
type InstallAgentSkillArguments struct {
	Name string `json:"name"`
}

// AgentSkillArguments name a skill.
type AgentSkillArguments struct {
	Name string `json:"name"`
}

// SetAgentSkillEnabledArguments say which and whether.
type SetAgentSkillEnabledArguments struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (self *graph) skillView(row *models.AgentSkill) *AgentSkillView {
	view := &AgentSkillView{
		Name: row.Name, Description: row.Description, Version: row.Version,
		Publisher: row.Publisher, URL: row.URL, Enabled: row.Enabled,
		InstalledAt: row.CreatedAt, Tools: []*AgentSkillToolView{}, Secrets: []string{},
	}
	parsed, err := skills.Parse([]byte(row.Content))
	if err != nil {
		view.Problem = err.Error()
		return view
	}
	view.Readable = true
	if view.Description == "" {
		view.Description = parsed.Description
	}
	for _, declared := range parsed.Tools {
		view.Tools = append(view.Tools, &AgentSkillToolView{
			Name: declared.Name, Description: declared.Description,
			Kind: declared.Type, NeedsComputer: agent.SkillRunsCommands(declared),
		})
	}
	for _, secret := range parsed.Secrets {
		view.Secrets = append(view.Secrets, secret.Key)
	}
	return view
}

func (self *graph) ListAgentSkills(ctx context.Context) ([]*AgentSkillView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentUse); err != nil {
		return nil, err
	}
	var installed []*models.AgentSkill
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		installed, err = tx.ListAgentSkills()
		return err
	}); err != nil {
		return nil, err
	}
	views := make([]*AgentSkillView, 0, len(installed))
	for _, row := range installed {
		views = append(views, self.skillView(row))
	}
	return views, nil
}

func (self *graph) SearchAgentSkills(ctx context.Context, arguments SearchAgentSkillsArguments) ([]*AgentSkillOffer, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	registry := &skills.Registry{}
	index, err := registry.Index(ctx)
	if err != nil {
		return nil, err
	}
	var installed []*models.AgentSkill
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		installed, err = tx.ListAgentSkills()
		return err
	}); err != nil {
		return nil, err
	}
	have := map[string]string{}
	for _, row := range installed {
		have[row.Name] = row.Version
	}
	words := strings.ToLower(strings.TrimSpace(arguments.Query))
	offers := make([]*AgentSkillOffer, 0, len(index.Skills))
	for _, entry := range index.Skills {
		// An entry the registry did not sign is not offered at all.
		if err := registry.Verify(entry); err != nil {
			continue
		}
		if words != "" && !skillMatches(entry, words) {
			continue
		}
		offer := &AgentSkillOffer{
			Name: entry.Name, Description: entry.Description, Version: entry.Version,
			Tags: entry.Tags, Installed: have[entry.Name],
		}
		if offer.Tags == nil {
			offer.Tags = []string{}
		}
		offer.Newer = offer.Installed != "" && skills.Newer(entry.Version, offer.Installed)
		offers = append(offers, offer)
	}
	return offers, nil
}

func skillMatches(entry *skills.Entry, words string) bool {
	if strings.Contains(strings.ToLower(entry.Name), words) || strings.Contains(strings.ToLower(entry.Description), words) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), words) {
			return true
		}
	}
	return false
}

func (self *graph) InstallAgentSkill(ctx context.Context, arguments InstallAgentSkillArguments) (*AgentSkillView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(arguments.Name))
	if name == "" {
		return nil, fmt.Errorf("%w: which skill", api.ErrInvalidArguments)
	}
	registry := &skills.Registry{}
	entry, err := registry.Find(ctx, name)
	if err != nil {
		return nil, err
	}
	// Signed, and the bytes are what was signed for; then it has to be a
	// skill this server can actually carry out before it is kept.
	content, err := registry.Download(ctx, entry)
	if err != nil {
		return nil, err
	}
	parsed, err := skills.Parse(content)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(parsed.Name, entry.Name) {
		return nil, fmt.Errorf("skills: the registry calls this %q and the file calls itself %q", entry.Name, parsed.Name)
	}
	var stored *models.AgentSkill
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		stored, err = tx.PutAgentSkill(&models.AgentSkill{
			Name: entry.Name, Version: entry.Version, Publisher: registry.Publisher(),
			URL: entry.URL, SHA256: entry.SHA256, Description: entry.Description,
			Content: string(content), Enabled: true,
		})
		return err
	}); err != nil {
		return nil, err
	}
	self.forgetSkills()
	return self.skillView(stored), nil
}

func (self *graph) RemoveAgentSkill(ctx context.Context, arguments AgentSkillArguments) (bool, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return false, err
	}
	if err := self.database.Transaction(func(tx db.Transaction) error {
		return tx.DeleteAgentSkill(arguments.Name)
	}); err != nil {
		return false, err
	}
	self.forgetSkills()
	return true, nil
}

func (self *graph) SetAgentSkillEnabled(ctx context.Context, arguments SetAgentSkillEnabledArguments) (*AgentSkillView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	var stored *models.AgentSkill
	if err := self.database.Transaction(func(tx db.Transaction) error {
		found, err := tx.GetAgentSkill(arguments.Name)
		if err != nil {
			return err
		}
		if found == nil {
			return api.ErrNotFound
		}
		found.Enabled = arguments.Enabled
		stored, err = tx.PutAgentSkill(found)
		return err
	}); err != nil {
		return nil, err
	}
	self.forgetSkills()
	return self.skillView(stored), nil
}

// forgetSkills tells the worker to read the rows again, so a skill just
// installed is offered on the next turn rather than within the minute.
func (self *graph) forgetSkills() {
	if worker := self.agentWorker(); worker != nil {
		worker.ForgetSkills()
	}
}
