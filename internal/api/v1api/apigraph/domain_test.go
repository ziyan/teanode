package apigraph_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/config"
)

// TestMutationsReachTheStore is the property the dashboard rests on: a change
// made through the API has to reach the store, keeping the identifiers that
// stored mail points at, and a rejected change has to leave what was there
// alone.
//
// It exercises config.Store the way the resolvers do rather than standing up
// the whole API, because that is where this actually lives.
func TestMutationsReachTheStore(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "teanode1.key"), []byte("not a real key"), 0o600); err != nil {
		t.Fatalf("failed to write the key file: %s", err)
	}

	configuration := config.Example()
	configuration.Server.DataDirectory = directory
	store := config.NewMemoryStore(configuration)
	defer func() { _ = store.Close() }()

	// Add a domain, an alias on it, and a credential, exactly as the
	// CreateDomain, CreateAlias and CreateCredential resolvers do.
	domainId := config.NewID()
	aliasId := config.NewID()
	credentialId := config.NewID()

	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Domains = append(configuration.Domains, &config.Domain{
			ID:        domainId,
			Domain:    "second.example",
			Subdomain: "mail",
			Aliases: []*config.Alias{
				{ID: aliasId, Pattern: "^support$", Kind: config.AliasKindEmail, Email: "support@example.net"},
			},
			Credentials: []*config.Credential{
				{ID: credentialId, Key: "0123456789abcdef", Comment: "laptop"},
			},
		})
		return nil
	}); err != nil {
		t.Fatalf("failed to add the domain: %s", err)
	}

	// What the store now holds is the point: this is what the server acts on,
	// and what an instance loading it fresh would see. That it survives being
	// written and read back is covered in internal/configdb.
	reloaded := store.Current()

	domain := reloaded.FindDomain("second.example")
	if domain == nil {
		t.Fatal("the new domain was not stored")
	}
	if domain.ID != domainId {
		t.Errorf("the domain identifier changed: %q", domain.ID)
	}
	if reloaded.FindAliasByID(aliasId) == nil {
		t.Error("the new alias was not stored")
	}
	if _, credential := reloaded.FindCredential(credentialId); credential == nil {
		t.Error("the new credential was not stored")
	}

	// Editing an alias must not change its identifier, because deliveries
	// already stored point at it.
	if err := store.Update(func(configuration *config.Configuration) error {
		alias := configuration.FindAliasByID(aliasId)
		alias.Pattern = "^help$"
		return nil
	}); err != nil {
		t.Fatalf("failed to edit the alias: %s", err)
	}
	reloaded = store.Current()
	alias := reloaded.FindAliasByID(aliasId)
	if alias == nil {
		t.Fatal("editing the alias lost it")
	}
	if alias.Pattern != "^help$" {
		t.Errorf("the pattern is %q, want ^help$", alias.Pattern)
	}

	// Deleting a domain must leave the configuration valid and the other
	// domain alone.
	if err := store.Update(func(configuration *config.Configuration) error {
		for index, candidate := range configuration.Domains {
			if candidate.ID == domainId {
				configuration.Domains = append(configuration.Domains[:index], configuration.Domains[index+1:]...)
				return nil
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to delete the domain: %s", err)
	}
	reloaded = store.Current()
	if reloaded.FindDomain("second.example") != nil {
		t.Error("the deleted domain is still there")
	}
	if reloaded.FindDomain("example.com") == nil {
		t.Error("deleting one domain removed another")
	}

	// A rejected change must leave the configuration untouched, so a bad
	// request from the dashboard cannot corrupt a working one.
	before, err := yaml.Marshal(store.Current())
	if err != nil {
		t.Fatalf("failed to encode the configuration: %s", err)
	}
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Domains[0].Aliases[0].Pattern = "^["
		return nil
	}); err == nil {
		t.Error("an invalid pattern was accepted")
	}
	after, err := yaml.Marshal(store.Current())
	if err != nil {
		t.Fatalf("failed to encode the configuration: %s", err)
	}
	if string(before) != string(after) {
		t.Error("a rejected change modified the configuration")
	}
	if !strings.Contains(string(after), "example.com") {
		t.Error("the configuration lost its content")
	}
}
