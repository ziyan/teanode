package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// importCredential imports a credential as the owner, in a step of its own.
func (self *financeFixture) importCredential(test *testing.T, arguments ImportFinanceCredentialArguments) (*FinanceSourceView, error) {
	test.Helper()
	var imported *FinanceSourceView
	var importErr error
	self.as(test, self.owner, func(ctx context.Context, tx db.Transaction) {
		imported, importErr = self.resolver.ImportFinanceCredential(ctx, arguments)
	})
	return imported, importErr
}

// ownerFinanceSources is every finance source the owner has, as stored.
func (self *financeFixture) ownerFinanceSources(test *testing.T) []*models.AgentKnowledgeSource {
	test.Helper()
	var sources []*models.AgentKnowledgeSource
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if sources, err = financeSourcesOf(tx, self.ownerAgent.ID); err != nil {
			test.Fatal(err)
		}
	})
	return sources
}

// assertRefusalHidesCredential fails unless the error is a refusal the
// caller can act on and does not repeat the credential.
func assertRefusalHidesCredential(test *testing.T, name string, err error, credential string) {
	test.Helper()
	if !errors.Is(err, api.ErrInvalidArguments) {
		test.Errorf("%s answered %v, not a refusal", name, err)
		return
	}
	if strings.Contains(err.Error(), credential) {
		test.Errorf("%s repeated the credential: %s", name, err)
	}
}

// A Plaid credential made elsewhere becomes a finance source holding
// Plaid's id for the link, named for its institution, due now, with the
// credential sealed and never answered back; the same link is not taken
// twice, and nothing is ever ended at Plaid.
func TestImportFinanceCredentialThroughPlaid(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	imported, err := fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: " Plaid ", Credential: " " + importedPlaidCredential + "\n"})
	if err != nil {
		test.Fatalf("ImportFinanceCredential: %s", err)
	}
	if imported.ProviderKind != config.AgentFinanceProviderPlaid || imported.InstitutionName != "Invented Savings Bank" || !imported.IsEnabled || imported.NextRunAt == nil {
		test.Errorf("imported %+v", imported)
	}
	assertNoSecret(test, "ImportFinanceCredential", imported)

	sources := fixture.ownerFinanceSources(test)
	if len(sources) != 1 {
		test.Fatalf("%d finance sources, want one", len(sources))
	}
	settings, _ := sources[0].FinanceSourceSettings()
	if settings.ProviderReference != "item-invented-imported" || settings.InstitutionID != "institution-invented" || sources[0].Cron != models.FinanceSourceCron {
		test.Errorf("the finance source is %+v with %+v", sources[0], settings)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		secrets, err := tx.ListAgentSourceSecrets(sources[0].ID)
		if err != nil || len(secrets) != 1 || secrets[0].Key != models.FinanceCredentialSecretKey || strings.Contains(secrets[0].Value, importedPlaidCredential) {
			test.Errorf("the credential is not kept sealed: %v %v", secrets, err)
		}
	})

	_, err = fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "plaid", Credential: importedPlaidCredential})
	if !errors.Is(err, errFinanceSourceExists) {
		test.Errorf("importing the same link again answered %v", err)
	}
	assertRefusalHidesCredential(test, "a second import", err, importedPlaidCredential)
	if sources := fixture.ownerFinanceSources(test); len(sources) != 1 {
		test.Errorf("%d finance sources after importing the same link twice", len(sources))
	}
	if removed := fixture.linker.removedCredentials(); len(removed) != 0 {
		test.Errorf("an imported link was ended at Plaid: %v", removed)
	}
}

// A name the person gives is the finance source's, and Plaid is not asked.
func TestImportFinanceCredentialKeepsTheGivenName(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	imported, err := fixture.importCredential(test, ImportFinanceCredentialArguments{
		ProviderKind: "plaid", Credential: importedPlaidCredential, InstitutionName: "  My Invented Bank ",
	})
	if err != nil || imported.InstitutionName != "My Invented Bank" || imported.Name != "My Invented Bank" {
		test.Errorf("imported %+v %v", imported, err)
	}
}

// A credential Plaid does not accept, or cannot be asked about, is refused
// without repeating it, and makes nothing.
func TestImportFinanceCredentialRefusesWhatPlaidRefuses(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, err := fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "plaid", Credential: unknownPlaidCredential})
	assertRefusalHidesCredential(test, "an unknown credential", err, unknownPlaidCredential)
	if err != nil && !strings.Contains(err.Error(), "client id") {
		test.Errorf("the refusal does not say what may be wrong: %s", err)
	}
	_, err = fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "plaid", Credential: unreachablePlaidCredential})
	assertRefusalHidesCredential(test, "an unreachable Plaid", err, unreachablePlaidCredential)
	if err != nil && !strings.Contains(err.Error(), "cannot reach Plaid") {
		test.Errorf("the refusal lost Plaid's reason: %s", err)
	}
	_, err = fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "plaid", Credential: "   "})
	if !errors.Is(err, api.ErrInvalidArguments) {
		test.Errorf("an empty credential answered %v", err)
	}
	if sources := fixture.ownerFinanceSources(test); len(sources) != 0 {
		test.Errorf("a refused credential made %d finance sources", len(sources))
	}
}

// An import whose finance source cannot be made leaves nothing here and,
// unlike a link made here, ends nothing at Plaid: the link was the
// person's before, and stays so.
func TestImportFinanceCredentialNeverEndsTheLinkAtPlaid(test *testing.T) {
	// No server secret: sealing the credential fails after Plaid accepted it.
	fixture := newFinanceFixture(test, false)
	_, err := fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "plaid", Credential: importedPlaidCredential})
	if err == nil {
		test.Fatal("a credential that could not be sealed was kept")
	}
	if strings.Contains(err.Error(), importedPlaidCredential) {
		test.Errorf("the failure repeated the credential: %s", err)
	}
	if removed := fixture.linker.removedCredentials(); len(removed) != 0 {
		test.Errorf("an imported link was ended at Plaid: %v", removed)
	}
	if sources := fixture.ownerFinanceSources(test); len(sources) != 0 {
		test.Errorf("a source was left behind: %v", sources)
	}
}

// A SimpleFIN credential claimed elsewhere is checked for its shape before
// anything is sent, proved by the bridge, and named for its institution.
func TestImportFinanceCredentialThroughSimpleFIN(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	for _, refused := range []string{
		"http://person:" + inventedSimpleFinPassword + "@bridge.example.net/simplefin",
		"https://bridge.example.net/simplefin",
		"not-an-address-" + inventedSimpleFinPassword,
		"https://person:" + inventedSimpleFinPassword + "@bridge.example.net/revoked",
	} {
		_, err := fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "simplefin", Credential: refused})
		assertRefusalHidesCredential(test, refused, err, inventedSimpleFinPassword)
	}
	if sources := fixture.ownerFinanceSources(test); len(sources) != 0 {
		test.Fatalf("a refused credential made %d finance sources", len(sources))
	}

	imported, err := fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "simplefin", Credential: inventedSimpleFinAddress})
	if err != nil {
		test.Fatalf("ImportFinanceCredential: %s", err)
	}
	if imported.ProviderKind != config.AgentFinanceProviderSimpleFIN || imported.InstitutionName != "Invented Credit Union" || imported.NextRunAt == nil {
		test.Errorf("imported %+v", imported)
	}
	assertNoSecret(test, "ImportFinanceCredential", imported)
	sources := fixture.ownerFinanceSources(test)
	if len(sources) != 1 {
		test.Fatalf("%d finance sources, want one", len(sources))
	}
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		secrets, err := tx.ListAgentSourceSecrets(sources[0].ID)
		if err != nil || len(secrets) != 1 || strings.Contains(secrets[0].Value, inventedSimpleFinPassword) {
			test.Errorf("the credential is not kept sealed: %v %v", secrets, err)
		}
	})
}

// A provider the operator does not offer, or no such provider, is refused
// before the credential goes anywhere.
func TestImportFinanceCredentialNeedsTheProviderOffered(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	fixture.configuration.Agent.Finance.OfferedProviders = []string{config.AgentFinanceProviderPlaid}
	_, err := fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "simplefin", Credential: inventedSimpleFinAddress})
	if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "does not offer SimpleFIN") {
		test.Errorf("SimpleFIN not offered answered %v", err)
	}
	_, err = fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "bank-of-nowhere", Credential: importedPlaidCredential})
	assertRefusalHidesCredential(test, "an unknown provider", err, importedPlaidCredential)

	fixture.configuration.Agent.Finance.OfferedProviders = []string{config.AgentFinanceProviderSimpleFIN}
	_, err = fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "plaid", Credential: importedPlaidCredential})
	if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "does not offer Plaid") {
		test.Errorf("Plaid not offered answered %v", err)
	}

	fixture.configuration.Agent.Finance.OfferedProviders = nil
	_, err = fixture.importCredential(test, ImportFinanceCredentialArguments{ProviderKind: "plaid", Credential: importedPlaidCredential})
	if !errors.Is(err, errFinanceNotOffered) {
		test.Errorf("finance not offered answered %v", err)
	}
	if sources := fixture.ownerFinanceSources(test); len(sources) != 0 {
		test.Errorf("a refused import made %d finance sources", len(sources))
	}
}
