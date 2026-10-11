package apigraph

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// setupProfileTestSecret stands in for the server secret. Invented; it seals
// nothing outside these tests.
var setupProfileTestSecret = []byte("an invented server secret for setup profile tests")

// downloadSetupProfile fetches a setup profile by the path the resolver gave.
func downloadSetupProfile(world *graph, path string) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	router.Path(api.PathMailboxSetupProfile).Methods(http.MethodGet).HandlerFunc(world.setupProfileView)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func setupProfilePath(token string) string {
	return strings.Replace(api.PathMailboxSetupProfile, "{token}", token, 1)
}

// A setup profile holds a password, so its address works once, and the
// download refuses whatever else is parked in the same store.
func TestASetupProfileDownloadsOnceAndOnlyAsAProfile(test *testing.T) {
	ctx := context.Background()
	world := &graph{ceremonies: ceremony.NewMemoryStore(), settings: &api.Settings{Secret: setupProfileTestSecret}}
	box, err := world.setupProfileBox()
	if err != nil {
		test.Fatal(err)
	}
	sealed, err := box.Seal([]byte("<plist/>"))
	if err != nil {
		test.Fatal(err)
	}
	token, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: sealed})
	if err != nil {
		test.Fatal(err)
	}

	first := downloadSetupProfile(world, setupProfilePath(token))
	if first.Code != http.StatusOK || first.Body.String() != "<plist/>" {
		test.Fatalf("the first download must work: %d %q", first.Code, first.Body.String())
	}
	if contentType := first.Header().Get("Content-Type"); contentType != "application/x-apple-aspen-config" {
		test.Fatalf("a device installs only a profile served as one, not %q", contentType)
	}
	if first.Header().Get("Cache-Control") != "no-store" {
		test.Fatal("a profile with a password must not be cached")
	}
	if again := downloadSetupProfile(world, setupProfilePath(token)); again.Code != http.StatusNotFound {
		test.Fatalf("a second download must fail, got %d", again.Code)
	}

	// A passkey challenge parked in the same store is not a profile.
	challenge, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{SessionData: "{}"})
	if err != nil {
		test.Fatal(err)
	}
	if response := downloadSetupProfile(world, setupProfilePath(challenge)); response.Code != http.StatusNotFound {
		test.Fatalf("a passkey challenge must not download as a profile, got %d", response.Code)
	}
	// Nor is a profile a passkey challenge.
	profile, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: sealed})
	if err != nil {
		test.Fatal(err)
	}
	if _, err := world.take(ctx, profile); !errors.Is(err, api.ErrInvalidArguments) {
		test.Fatalf("a setup profile must not finish a passkey ceremony, got %v", err)
	}

	// One sealed under another server secret opens to nothing.
	other, err := secretbox.New([]byte("another invented server secret"), setupProfileLabel)
	if err != nil {
		test.Fatal(err)
	}
	foreign, err := other.Seal([]byte("<plist/>"))
	if err != nil {
		test.Fatal(err)
	}
	stranger, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: foreign})
	if err != nil {
		test.Fatal(err)
	}
	if response := downloadSetupProfile(world, setupProfilePath(stranger)); response.Code != http.StatusNotFound {
		test.Fatalf("a profile sealed under another secret must not download, got %d", response.Code)
	}
	if response := downloadSetupProfile(world, setupProfilePath("never-made")); response.Code != http.StatusNotFound {
		test.Fatalf("an unknown token must not download, got %d", response.Code)
	}
}

// setupProfileFixture is a database with a person who owns a mailbox with an
// address, and a graph over it.
type setupProfileFixture struct {
	database      db.Database
	configuration *config.Configuration
	world         *graph
	owner         *api.Principal
	mailboxId     string
}

func newSetupProfileFixture(test *testing.T) *setupProfileFixture {
	test.Helper()
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)
	ownerId := dbtest.CreateUser(test, database, "profile-owner")
	fixture := &setupProfileFixture{
		database: database,
		owner:    &api.Principal{User: &models.User{ID: ownerId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}})},
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: ownerId, Name: "Profile fixture"})
		if err != nil {
			test.Fatal(err)
		}
		fixture.mailboxId = mailbox.ID
		domain, err := transaction.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "^owner$", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.configuration = config.Example()
	fixture.configuration.Listen.IMAPS = ":993"
	fixture.world = &graph{
		database:   database,
		config:     config.NewMemoryStore(fixture.configuration),
		settings:   &api.Settings{Secret: setupProfileTestSecret},
		ceremonies: ceremony.NewMemoryStore(),
	}
	return fixture
}

// make asks for a profile as principal over request, in a transaction of its
// own, and says how many app passwords the mailbox has afterwards.
func (self *setupProfileFixture) make(test *testing.T, principal *api.Principal, request *http.Request, isDownloadOnly bool) (*CreatedSetupProfile, int, error) {
	test.Helper()
	var created *CreatedSetupProfile
	var makeError error
	appPasswordCount := 0
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		ctx := api.ContextWithRequest(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction), request)
		created, makeError = self.world.CreateMailboxSetupProfile(ctx, CreateMailboxSetupProfileArguments{MailboxID: self.mailboxId, DeviceName: "Phone", IsDownloadOnly: &isDownloadOnly})
		existing, err := transaction.ListAppPasswords(self.mailboxId)
		if err != nil {
			test.Fatal(err)
		}
		appPasswordCount = len(existing)
	})
	return created, appPasswordCount, makeError
}

// consoleRequest is the command line on the server: from loopback, to
// loopback, over plain http.
func consoleRequest() *http.Request {
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	request.Host = "127.0.0.1:10081"
	request.RemoteAddr = "127.0.0.1:50000"
	return request
}

// A profile holds a password and its address is how it is fetched, so it is
// made only over https or on the server itself. The Host header alone is the
// client's word: a proxy forwarding plain http can set it to 127.0.0.1.
func TestASetupProfileIsRefusedOverPlainHTTP(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	visitor := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	visitor.Host = "mail.example.com"
	visitor.RemoteAddr = "192.0.2.10:4000"
	rewritten := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	rewritten.Host = "127.0.0.1:10081"
	rewritten.RemoteAddr = "192.0.2.10:4000"
	for name, request := range map[string]*http.Request{"a visitor": visitor, "a proxy that rewrote the host": rewritten} {
		_, appPasswordCount, err := fixture.make(test, fixture.owner, request, false)
		if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "https") {
			test.Fatalf("%s over plain http: %v", name, err)
		}
		if appPasswordCount != 0 {
			test.Fatalf("%s over plain http spent an app password", name)
		}
	}
}

// Only the mailbox's owner, holding mailbox manage, makes one: anybody else
// is told the mailbox is not there.
func TestOnlyTheMailboxOwnerMakesASetupProfile(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	strangerId := dbtest.CreateUser(test, fixture.database, "profile-stranger")
	stranger := &api.Principal{User: &models.User{ID: strangerId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}})}
	if _, appPasswordCount, err := fixture.make(test, stranger, consoleRequest(), false); !errors.Is(err, api.ErrNotFound) || appPasswordCount != 0 {
		test.Fatalf("somebody else's mailbox: %v, %d app passwords", err, appPasswordCount)
	}
	reader := &api.Principal{User: fixture.owner.User, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	if _, appPasswordCount, err := fixture.make(test, reader, consoleRequest(), false); !errors.Is(err, api.ErrNotFound) || appPasswordCount != 0 {
		test.Fatalf("the owner without mailbox manage: %v, %d app passwords", err, appPasswordCount)
	}
}

// A server that serves no IMAP has nothing for Mail to sign in to, and is
// told so before an app password is spent.
func TestASetupProfileNeedsAnIMAPPort(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	fixture.configuration.Listen.IMAPS = ""
	fixture.configuration.Listen.IMAP = ""
	_, appPasswordCount, err := fixture.make(test, fixture.owner, consoleRequest(), false)
	if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "IMAP") {
		test.Fatalf("no IMAP port: %v", err)
	}
	if appPasswordCount != 0 {
		test.Fatal("no IMAP port spent an app password")
	}
}

// The command line on the server reaches it at 127.0.0.1, which a phone
// cannot open: the link is built from the server's name instead, and without
// one it is refused unless the caller downloads the file itself.
func TestASetupProfileLinkIsOneTheDeviceCanOpen(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	created, _, err := fixture.make(test, fixture.owner, consoleRequest(), false)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.HasPrefix(created.DownloadLink, "https://mail.example.com/") {
		test.Fatalf("the console's link must use the server's name, not %q", created.DownloadLink)
	}
	if until := time.Until(created.ExpiresAt); until <= 0 || until > ceremony.Lifetime {
		test.Fatalf("it must expire within the ceremony lifetime, not in %s", until)
	}

	// The dashboard opened over https at a name: that name, which reached
	// this server.
	dashboard := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	dashboard.Host = "mail.example.net:8443"
	dashboard.RemoteAddr = "192.0.2.10:4000"
	dashboard.TLS = &tls.ConnectionState{}
	created, _, err = fixture.make(test, fixture.owner, dashboard, false)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.HasPrefix(created.DownloadLink, "https://mail.example.net:8443/") {
		test.Fatalf("the dashboard's link must use the name it was opened at, not %q", created.DownloadLink)
	}

	fixture.configuration.Server.Name = ""
	fixture.configuration.IMAP.Host = "imap.example.com"
	fixture.configuration.SMTP.Submission.Host = "smtp.example.com"
	_, appPasswordCount, err := fixture.make(test, fixture.owner, consoleRequest(), false)
	if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "--output") {
		test.Fatalf("a loopback link with no name to use instead: %v", err)
	}
	if appPasswordCount != 2 {
		test.Fatalf("a refused link spent an app password: %d", appPasswordCount)
	}
	created, _, err = fixture.make(test, fixture.owner, consoleRequest(), true)
	if err != nil {
		test.Fatalf("a caller that downloads the file itself needs no name: %v", err)
	}
	if response := downloadSetupProfile(fixture.world, created.DownloadPath); response.Code != http.StatusOK {
		test.Fatalf("downloading it through the API: %d", response.Code)
	}
}

// Made through the resolver, the profile carries the new app password and
// downloads once.
func TestAMadeSetupProfileDownloadsOnce(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	created, appPasswordCount, err := fixture.make(test, fixture.owner, consoleRequest(), false)
	if err != nil {
		test.Fatal(err)
	}
	if appPasswordCount != 1 || created.AppPassword == nil || created.AppPassword.Name != "Phone" {
		test.Fatalf("one app password named for the device: %d, %+v", appPasswordCount, created.AppPassword)
	}
	first := downloadSetupProfile(fixture.world, created.DownloadPath)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "owner@example.com") || !strings.Contains(first.Body.String(), "mail.example.com") {
		test.Fatalf("the first download must be the profile: %d %q", first.Code, first.Body.String())
	}
	if again := downloadSetupProfile(fixture.world, created.DownloadPath); again.Code != http.StatusNotFound {
		test.Fatalf("a second download must fail, got %d", again.Code)
	}
}
