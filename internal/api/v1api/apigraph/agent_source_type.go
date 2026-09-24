package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
	"github.com/ziyan/teanode/internal/sources"
)

// Source types are how a knowledge source is read: a file of declarations
// saying which tool to call and how its answers become records, installed
// from a registry that signs what it publishes, or added by an operator
// from a file of their own. A person adds a source of an installed type.

// AgentSourceTypeQuery reads them.
type AgentSourceTypeQuery interface {
	// The source types installed on this server, with the settings each
	// asks for. Needs agent:use.
	ListAgentSourceTypes(ctx context.Context) ([]*AgentSourceTypeView, error)

	// What the registry publishes, with whether each is installed and
	// whether what is installed is behind. Needs server:manage, because
	// it reaches out of this server.
	SearchAgentSourceTypes(ctx context.Context, arguments SearchAgentSourceTypesArguments) ([]*AgentSourceTypeOffer, error)
}

// AgentSourceTypeMutation installs and removes them.
type AgentSourceTypeMutation interface {
	// Install a type from the registry, or replace the installed one with
	// a newer version. The signature and the hash are checked before
	// anything is stored. Needs server:manage.
	InstallAgentSourceType(ctx context.Context, arguments AgentSourceTypeArguments) (*AgentSourceTypeView, error)

	// Add a type from the operator's own file, unsigned, marked local; or
	// replace the local type of that name. Needs server:manage.
	AddLocalAgentSourceType(ctx context.Context, arguments AddLocalAgentSourceTypeArguments) (*AgentSourceTypeView, error)

	// Take a type away. Refused while any source is of it. Needs
	// server:manage.
	RemoveAgentSourceType(ctx context.Context, arguments AgentSourceTypeArguments) (bool, error)
}

// AgentSourceTypeView is an installed type and what it asks for.
type AgentSourceTypeView struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Publisher   string    `json:"publisher"`
	URL         string    `json:"url"`
	IsLocal     bool      `json:"isLocal"`
	InstalledAt time.Time `json:"installedAt"`

	// Reader is the reader built into TeaNode the type names, or empty
	// for a type of commands and requests. Runs is where it can run:
	// computer, server. Requires is the tools the computer needs.
	Reader   string   `json:"reader"`
	Runs     []string `json:"runs"`
	Requires []string `json:"requires"`

	// Settings are what a person fills in for a source of the type, and
	// Secrets the values it keeps sealed, such as a token.
	Settings []*AgentSourceTypeSettingView `json:"settings"`
	Secrets  []*AgentSourceTypeSecretView  `json:"secrets"`

	// Guide is what the type says about itself, for people, in Markdown.
	Guide string `json:"guide"`

	// Readable says whether the file still parses, and Problem why not.
	Readable bool   `json:"readable"`
	Problem  string `json:"problem,omitempty"`
}

// AgentSourceTypeSettingView is one setting a type asks for.
type AgentSourceTypeSettingView struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	// SettingType is string, path, array, boolean or integer; for an
	// array, each value is text matching ItemPattern.
	SettingType string `json:"settingType"`
	Pattern     string `json:"pattern"`
	ItemPattern string `json:"itemPattern"`

	// Default is the value left unset means, as JSON; IsRequired says
	// there is none and a source cannot be added without it.
	Default    json.RawMessage `json:"default" graphapi:"nullable"`
	IsRequired bool            `json:"isRequired"`
	Minimum    *int            `json:"minimum" graphapi:"nullable"`
	Maximum    *int            `json:"maximum" graphapi:"nullable"`
}

// AgentSourceTypeSecretView is one secret a type asks each source for.
type AgentSourceTypeSecretView struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	IsOptional  bool   `json:"isOptional"`
}

// AgentSourceTypeOffer is one type the registry publishes.
type AgentSourceTypeOffer struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`

	// Installed is the version on this server, empty when there is none;
	// Newer says whether the registry has moved on.
	Installed string `json:"installed,omitempty"`
	Newer     bool   `json:"newer"`
}

// SearchAgentSourceTypesArguments narrow what the registry offers.
type SearchAgentSourceTypesArguments struct {
	Query string `json:"query" graphapi:"nullable"`
}

// AgentSourceTypeArguments name a type.
type AgentSourceTypeArguments struct {
	Name string `json:"name"`
}

// AddLocalAgentSourceTypeArguments carry the operator's file.
type AddLocalAgentSourceTypeArguments struct {
	Content string `json:"content"`
}

// localPublisher is who stands behind a local type: nobody but the
// operator who added it.
const localPublisher = "local"

func sourceTypeView(row *models.AgentSourceType) *AgentSourceTypeView {
	view := &AgentSourceTypeView{
		Name: row.Name, Description: row.Description, Version: row.Version,
		Publisher: row.Publisher, URL: row.URL, IsLocal: row.IsLocal, InstalledAt: row.CreatedAt,
		Runs: []string{}, Requires: []string{}, Settings: []*AgentSourceTypeSettingView{},
		Secrets: []*AgentSourceTypeSecretView{},
	}
	parsed, err := sources.Parse([]byte(row.Content))
	if err != nil {
		view.Problem = err.Error()
		return view
	}
	view.Readable = true
	view.Reader, view.Guide = parsed.Reader, parsed.Prose
	if parsed.Description != "" {
		view.Description = parsed.Description
	}
	view.Runs = append(view.Runs, parsed.Runs...)
	if len(view.Runs) == 0 {
		view.Runs = []string{sources.RunsComputer}
	}
	view.Requires = append(view.Requires, parsed.Requires...)
	for _, secret := range parsed.Secrets {
		view.Secrets = append(view.Secrets, &AgentSourceTypeSecretView{Key: secret.Key, Description: secret.Description, IsOptional: secret.Optional})
	}
	for _, setting := range parsed.Settings {
		one := &AgentSourceTypeSettingView{
			Name: setting.Name, Description: setting.Description, SettingType: setting.Type,
			Pattern: setting.Pattern, IsRequired: setting.Default == nil,
			Minimum: setting.Minimum, Maximum: setting.Maximum,
		}
		if setting.Items != nil {
			one.ItemPattern = setting.Items.Pattern
		}
		if setting.Default != nil {
			one.Default, _ = json.Marshal(setting.Default)
		}
		view.Settings = append(view.Settings, one)
	}
	return view
}

func (self *graph) ListAgentSourceTypes(ctx context.Context) ([]*AgentSourceTypeView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentUse); err != nil {
		return nil, err
	}
	var installed []*models.AgentSourceType
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		installed, err = tx.ListAgentSourceTypes()
		return err
	}); err != nil {
		return nil, err
	}
	views := make([]*AgentSourceTypeView, 0, len(installed))
	for _, row := range installed {
		views = append(views, sourceTypeView(row))
	}
	return views, nil
}

func (self *graph) SearchAgentSourceTypes(ctx context.Context, arguments SearchAgentSourceTypesArguments) ([]*AgentSourceTypeOffer, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	registry, err := sources.Registry("", nil)
	if err != nil {
		return nil, err
	}
	// Only the entries whose signatures hold; one the registry did not
	// sign is not offered at all.
	entries, err := sources.Entries(ctx, registry)
	if err != nil {
		return nil, err
	}
	var installed []*models.AgentSourceType
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		installed, err = tx.ListAgentSourceTypes()
		return err
	}); err != nil {
		return nil, err
	}
	have := map[string]string{}
	for _, row := range installed {
		if !row.IsLocal {
			have[row.Name] = row.Version
		}
	}
	words := strings.ToLower(strings.TrimSpace(arguments.Query))
	offers := make([]*AgentSourceTypeOffer, 0, len(entries))
	for _, entry := range entries {
		if words != "" && !skillMatches(entry, words) {
			continue
		}
		offer := &AgentSourceTypeOffer{
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

func (self *graph) InstallAgentSourceType(ctx context.Context, arguments AgentSourceTypeArguments) (*AgentSourceTypeView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(arguments.Name))
	if name == "" {
		return nil, fmt.Errorf("%w: which source type", api.ErrInvalidArguments)
	}
	registry, err := sources.Registry("", nil)
	if err != nil {
		return nil, err
	}
	entry, err := sources.Find(ctx, registry, name)
	if err != nil {
		return nil, err
	}
	// Signed, and the bytes are what was signed for; then it has to be a
	// type this server can read before it is kept.
	content, err := registry.Download(ctx, entry)
	if err != nil {
		return nil, err
	}
	parsed, err := sources.Parse(content)
	if err != nil {
		return nil, err
	}
	var existing *models.AgentSourceType
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		existing, err = tx.GetAgentSourceType(entry.Name)
		return err
	}); err != nil {
		return nil, err
	}
	// The registry's type does not replace a local one of the same name:
	// the sources of it were set up against the operator's file.
	if existing != nil && existing.IsLocal {
		return nil, fmt.Errorf("%w: %s is a local type here; remove it first to install the registry's", api.ErrInvalidArguments, entry.Name)
	}
	if !strings.EqualFold(parsed.Name, entry.Name) {
		return nil, fmt.Errorf("sources: the registry calls this %q and the file calls itself %q", entry.Name, parsed.Name)
	}
	return self.putSourceType(ctx, &models.AgentSourceType{
		Name: entry.Name, Version: entry.Version, Publisher: sources.OfficialPublisher,
		URL: entry.URL, SHA256: entry.SHA256, Description: parsed.Description, Content: string(content),
	})
}

func (self *graph) AddLocalAgentSourceType(ctx context.Context, arguments AddLocalAgentSourceTypeArguments) (*AgentSourceTypeView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	if len(arguments.Content) > 1<<20 {
		return nil, fmt.Errorf("%w: a source type is at most a megabyte", api.ErrInvalidArguments)
	}
	parsed, err := sources.Parse([]byte(arguments.Content))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	var existing *models.AgentSourceType
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		existing, err = tx.GetAgentSourceType(parsed.Name)
		return err
	}); err != nil {
		return nil, err
	}
	// A local file does not replace what the registry signed: the name is
	// the registry's, and a source of it expects the registry's type.
	if existing != nil && !existing.IsLocal {
		return nil, fmt.Errorf("%w: %s is installed from the registry; remove it first, or name the local type something else", api.ErrInvalidArguments, parsed.Name)
	}
	return self.putSourceType(ctx, &models.AgentSourceType{
		Name: parsed.Name, Publisher: localPublisher, Description: parsed.Description,
		Content: arguments.Content, IsLocal: true,
	})
}

func (self *graph) putSourceType(ctx context.Context, sourceType *models.AgentSourceType) (*AgentSourceTypeView, error) {
	var stored *models.AgentSourceType
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		existing, err := tx.GetAgentSourceType(sourceType.Name)
		if err != nil {
			return err
		}
		if existing != nil {
			sourceType.CreatedAt = existing.CreatedAt
		}
		stored, err = tx.PutAgentSourceType(sourceType)
		return err
	}); err != nil {
		return nil, err
	}
	return sourceTypeView(stored), nil
}

func (self *graph) RemoveAgentSourceType(ctx context.Context, arguments AgentSourceTypeArguments) (bool, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return false, err
	}
	var found *models.AgentSourceType
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if found, err = tx.GetAgentSourceType(arguments.Name); err != nil || found == nil {
			return err
		}
		using, err := tx.CountAgentSourcesOfType(found.Name)
		if err != nil {
			return err
		}
		if using > 0 {
			return fmt.Errorf("%w: %d sources are of %s; remove them first", api.ErrInvalidArguments, using, found.Name)
		}
		return tx.DeleteAgentSourceType(found.Name)
	}); err != nil {
		return false, err
	}
	if found == nil {
		return false, api.ErrNotFound
	}
	return true, nil
}
