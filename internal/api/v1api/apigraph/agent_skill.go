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

	// The values the installed skills ask this person for, and whether
	// they have filled each in. The values themselves never come back.
	// Needs agent:use.
	ListAgentSkillSecrets(ctx context.Context) ([]*AgentSkillSecretView, error)
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

	// Settle who fills a skill's secrets in: "operator" for one set of
	// values for the whole server, "person" for each person's own, or
	// empty to leave it to what the skill declares per secret. Needs
	// server:manage.
	SetAgentSkillScope(ctx context.Context, arguments SetAgentSkillScopeArguments) (*AgentSkillView, error)

	// Keep one of the caller's own values for a skill that asks them for
	// it, sealed; or forget it. Needs agent:use.
	SetAgentSkillSecret(ctx context.Context, arguments SetAgentSkillSecretArguments) (*AgentSkillSecretView, error)
	ClearAgentSkillSecret(ctx context.Context, arguments ClearAgentSkillSecretArguments) (bool, error)
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

	// Scope is who the operator settled on to fill this skill's secrets
	// in -- "operator", "person", or empty for what the skill declares
	// per secret.
	Scope string `json:"scope"`

	// Tools are what it declares, by name. Secrets are the values an
	// operator fills in once for the whole server; PersonalSecrets the
	// ones each person fills in for themselves, which the operator
	// cannot set on their behalf. Readable says whether it still parses.
	Tools           []*AgentSkillToolView `json:"tools"`
	Secrets         []string              `json:"secrets"`
	PersonalSecrets []string              `json:"personalSecrets"`
	Readable        bool                  `json:"readable"`
	Problem         string                `json:"problem,omitempty"`
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

type SetAgentSkillScopeArguments struct {
	Name string `json:"name"`

	// Scope is "operator", "person", or empty to leave it to the skill.
	Scope string `json:"scope"`
}

func (self *graph) skillView(row *models.AgentSkill) *AgentSkillView {
	view := &AgentSkillView{
		Name: row.Name, Description: row.Description, Version: row.Version,
		Publisher: row.Publisher, URL: row.URL, Enabled: row.Enabled,
		Scope:       row.Scope,
		InstalledAt: row.CreatedAt, Tools: []*AgentSkillToolView{},
		Secrets: []string{}, PersonalSecrets: []string{},
	}
	parsed, err := skills.Parse([]byte(row.Content))
	if err != nil {
		view.Problem = err.Error()
		return view
	}
	view.Readable = true
	// The file's own description, not the one the index carried: the
	// signature covers the file through its hash, and covers the index's
	// description not at all.
	if parsed.Description != "" {
		view.Description = parsed.Description
	}
	for _, declared := range parsed.Tools {
		view.Tools = append(view.Tools, &AgentSkillToolView{
			Name: declared.Name, Description: declared.Description,
			Kind: declared.Type, NeedsComputer: agent.SkillRunsCommands(declared),
		})
	}
	for _, secret := range parsed.Secrets {
		if secret.ForPerson(view.Scope) {
			view.PersonalSecrets = append(view.PersonalSecrets, secret.Key)
			continue
		}
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
	if err := self.database.Transaction(func(tx db.Transaction) error {
		// A skill an operator switched off stays off when it is updated;
		// only a new one arrives switched on.
		enabled := true
		existing, err := tx.GetAgentSkill(entry.Name)
		if err != nil {
			return err
		}
		created := time.Time{}
		if existing != nil {
			enabled = existing.Enabled
			created = existing.CreatedAt
		}
		// Values people filled in for keys this version no longer asks
		// them for are forgotten. A later version that used the same key
		// for something else would otherwise be handed the old value.
		settled := ""
		if existing != nil {
			settled = existing.Scope
		}
		mine := map[string]bool{}
		for _, secret := range parsed.PersonalSecrets(settled) {
			mine[secret.Key] = true
		}
		if err := tx.SweepAgentSkillSecretsExcept(entry.Name, mine); err != nil {
			return err
		}
		stored, err = tx.PutAgentSkill(&models.AgentSkill{
			Name: entry.Name, CreatedAt: created, Version: entry.Version, Publisher: registry.Publisher(),
			URL: entry.URL, SHA256: entry.SHA256, Description: entry.Description,
			Content: string(content), Enabled: enabled, Scope: settled,
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
	var found *models.AgentSkill
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		if found, err = tx.GetAgentSkill(arguments.Name); err != nil || found == nil {
			return err
		}
		if err := tx.SweepAgentSkillSecrets(arguments.Name); err != nil {
			return err
		}
		return tx.DeleteAgentSkill(arguments.Name)
	}); err != nil {
		return false, err
	}
	if found == nil {
		return false, api.ErrNotFound
	}
	self.forgetSkills()
	return true, nil
}

// SetAgentSkillScope settles who fills a skill's secrets in. A skill's
// author declares which of its secrets are the deployment's and which are
// each person's own, and is usually right; but the same skill serves a
// household with one camera system and an office where twenty people each
// have their own, and only the operator here knows which this is.
func (self *graph) SetAgentSkillScope(ctx context.Context, arguments SetAgentSkillScopeArguments) (*AgentSkillView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	settled, err := skills.SettledScope(arguments.Scope)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
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
		found.Scope = settled
		stored, err = tx.PutAgentSkill(found)
		return err
	}); err != nil {
		return nil, err
	}
	self.forgetSkills()
	return self.skillView(stored), nil
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

// A person's own values for the secrets a skill declared as theirs. The
// operator's live in the settings and are one per server; these are one
// per person, sealed, and never come back out.

// AgentSkillSecretView is one key a person is asked for.
type AgentSkillSecretView struct {
	Skill       string `json:"skill"`
	Key         string `json:"key"`
	Description string `json:"description"`

	// Set says whether this person has filled it in. The value itself is
	// never returned.
	Set bool `json:"set"`
}

// SetAgentSkillSecretArguments carry one value.
type SetAgentSkillSecretArguments struct {
	Skill string `json:"skill"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ClearAgentSkillSecretArguments name one value to forget.
type ClearAgentSkillSecretArguments struct {
	Skill string `json:"skill"`
	Key   string `json:"key"`
}

// ListAgentSkillSecrets is every value the installed skills ask this
// person for, and whether they have filled it in.
func (self *graph) ListAgentSkillSecrets(ctx context.Context) ([]*AgentSkillSecretView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	var installed []*models.AgentSkill
	var stored []*models.AgentSkillSecret
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		if installed, err = tx.ListAgentSkills(); err != nil {
			return err
		}
		stored, err = tx.ListAgentSkillSecrets(found.ID)
		return err
	}); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, secret := range stored {
		set[strings.ToLower(secret.Skill)+"\n"+secret.Key] = true
	}
	views := []*AgentSkillSecretView{}
	for _, row := range installed {
		if !row.Enabled {
			continue
		}
		parsed, err := skills.Parse([]byte(row.Content))
		if err != nil {
			continue
		}
		for _, secret := range parsed.PersonalSecrets(row.Scope) {
			views = append(views, &AgentSkillSecretView{
				Skill: row.Name, Key: secret.Key, Description: secret.Description,
				Set: set[strings.ToLower(row.Name)+"\n"+secret.Key],
			})
		}
	}
	return views, nil
}

// SetAgentSkillSecret keeps one of this person's values, sealed.
func (self *graph) SetAgentSkillSecret(ctx context.Context, arguments SetAgentSkillSecretArguments) (*AgentSkillSecretView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(arguments.Key)
	if key == "" || strings.TrimSpace(arguments.Value) == "" {
		return nil, fmt.Errorf("%w: a key and a value are needed", api.ErrInvalidArguments)
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	// Only a key an installed skill actually asks this person for, so that
	// the table cannot be used as somewhere to keep arbitrary secrets.
	asked, err := self.ListAgentSkillSecrets(ctx)
	if err != nil {
		return nil, err
	}
	var wanted *AgentSkillSecretView
	for _, view := range asked {
		if strings.EqualFold(view.Skill, arguments.Skill) && view.Key == key {
			wanted = view
		}
	}
	if wanted == nil {
		return nil, fmt.Errorf("%w: no installed skill asks you for %s", api.ErrInvalidArguments, key)
	}
	sealed, err := worker.SealSecret(arguments.Value)
	if err != nil {
		return nil, err
	}
	if err := self.database.Transaction(func(tx db.Transaction) error {
		return tx.PutAgentSkillSecret(&models.AgentSkillSecret{
			AgentID: found.ID, Skill: wanted.Skill, Key: key, Value: sealed,
		})
	}); err != nil {
		return nil, err
	}
	wanted.Set = true
	return wanted, nil
}

// ClearAgentSkillSecret forgets one of this person's values.
func (self *graph) ClearAgentSkillSecret(ctx context.Context, arguments ClearAgentSkillSecretArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	skill := strings.TrimSpace(arguments.Skill)
	if skill == "" {
		return false, fmt.Errorf("%w: which skill", api.ErrInvalidArguments)
	}
	key := strings.TrimSpace(arguments.Key)
	// Whether there was anything to forget, so that clearing a key that
	// was never set does not read as having cleared one that was.
	forgotten := false
	if err := self.database.Transaction(func(tx db.Transaction) error {
		stored, err := tx.ListAgentSkillSecrets(found.ID)
		if err != nil {
			return err
		}
		for _, secret := range stored {
			if strings.EqualFold(secret.Skill, skill) && (key == "" || secret.Key == key) {
				forgotten = true
			}
		}
		return tx.DeleteAgentSkillSecret(found.ID, skill, key)
	}); err != nil {
		return false, err
	}
	return forgotten, nil
}
