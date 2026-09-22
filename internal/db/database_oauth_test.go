package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// registered is a client with one address, for the tests below.
func registered(test *testing.T, database db.Database) *models.OAuthClient {
	test.Helper()
	client, err := database.CreateOAuthClient(&models.OAuthClient{
		ID:           security.NewULID(),
		Name:         "a harness",
		RedirectURIs: []string{"http://127.0.0.1:7391/callback", "https://harness.example.com/callback"},
	})
	if err != nil {
		test.Fatalf("CreateOAuthClient: %s", err)
	}
	return client
}

func approval(test *testing.T, database db.Database, clientId, keyHash string, expires time.Time) *models.OAuthAuthorization {
	test.Helper()
	authorization, err := database.CreateOAuthAuthorization(&models.OAuthAuthorization{
		ID:            security.NewULID(),
		ClientID:      clientId,
		UserID:        "a-person",
		RedirectURI:   "http://127.0.0.1:7391/callback",
		CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		Resource:      "https://mail.example.com/api/v1/mcp",
		ExpiresAt:     expires,
	}, keyHash)
	if err != nil {
		test.Fatalf("CreateOAuthAuthorization: %s", err)
	}
	return authorization
}

// The addresses survive the round trip, and only the ones registered.
//
// This is the check that stops an approval being sent somewhere the program
// never named, so it is worth proving the stored list is the list that comes
// back rather than assuming the encoding holds.
func TestAClientAllowsOnlyTheAddressesItRegistered(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	created := registered(test, database)
	client, err := database.GetOAuthClient(created.ID)
	if err != nil {
		test.Fatalf("GetOAuthClient: %s", err)
	}
	if client == nil {
		test.Fatal("the client that was just registered cannot be read back")
	}
	if len(client.RedirectURIs) != 2 {
		test.Fatalf("stored %d addresses, read back %d", 2, len(client.RedirectURIs))
	}
	for _, allowed := range []string{"http://127.0.0.1:7391/callback", "https://harness.example.com/callback"} {
		if !client.AllowsRedirect(allowed) {
			test.Errorf("%q was registered and is not allowed", allowed)
		}
	}
	// The shapes somebody would try. A prefix of a registered address, and a
	// registered address with something appended, are the two that a careless
	// comparison lets through.
	for _, refused := range []string{
		"http://127.0.0.1:7391/callback.evil.example.com",
		"http://127.0.0.1:7391/",
		"https://harness.example.com/callback/../elsewhere",
		"https://harness.example.com.evil.example.com/callback",
		"",
	} {
		if client.AllowsRedirect(refused) {
			test.Errorf("%q was never registered and is allowed", refused)
		}
	}
}

// An approval is collectable exactly once.
//
// Two programs collecting the same approval at the same moment must not both
// be told yes. Taking it is a delete that returns the row, so the second
// caller finds nothing however close behind the first it arrives.
func TestAnApprovalIsSpentOnce(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	client := registered(test, database)
	created := approval(test, database, client.ID, "a-hash", time.Now().Add(5*time.Minute))

	first, hash, err := database.SpendOAuthAuthorization(created.ID, time.Now())
	if err != nil {
		test.Fatalf("SpendOAuthAuthorization: %s", err)
	}
	if first == nil {
		test.Fatal("the approval could not be collected the first time")
	}
	if hash != "a-hash" {
		test.Errorf("the stored hash came back as %q", hash)
	}
	if first.Resource != "https://mail.example.com/api/v1/mcp" {
		test.Errorf("the resource came back as %q", first.Resource)
	}

	second, _, err := database.SpendOAuthAuthorization(created.ID, time.Now())
	if err != nil {
		test.Fatalf("SpendOAuthAuthorization: %s", err)
	}
	if second != nil {
		test.Error("the same approval was collected twice")
	}
}

// An approval past its moment is not collectable at all.
func TestAnExpiredApprovalIsNotCollected(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	client := registered(test, database)
	created := approval(test, database, client.ID, "a-hash", time.Now().Add(-time.Second))

	spent, _, err := database.SpendOAuthAuthorization(created.ID, time.Now())
	if err != nil {
		test.Fatalf("SpendOAuthAuthorization: %s", err)
	}
	if spent != nil {
		test.Error("an approval that had expired was collected")
	}
}

// A registration nobody approved is swept; one in service is not.
//
// Registration is open, so without this the table grows with every stranger
// who ever pointed something at this server.
func TestUnusedRegistrationsAreSweptAndApprovedOnesAreKept(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	forgotten := registered(test, database)
	inService := registered(test, database)
	if err := database.TouchOAuthClientApproval(inService.ID, time.Now()); err != nil {
		test.Fatalf("TouchOAuthClientApproval: %s", err)
	}

	// A day and a half on, so both are older than the sweep's reach and only
	// the approval keeps one of them.
	if _, err := database.ScavengeOAuth(time.Now().Add(36 * time.Hour)); err != nil {
		test.Fatalf("ScavengeOAuth: %s", err)
	}

	gone, err := database.GetOAuthClient(forgotten.ID)
	if err != nil {
		test.Fatalf("GetOAuthClient: %s", err)
	}
	if gone != nil {
		test.Error("a registration nobody ever approved was kept")
	}

	kept, err := database.GetOAuthClient(inService.ID)
	if err != nil {
		test.Fatalf("GetOAuthClient: %s", err)
	}
	if kept == nil {
		test.Error("a registration somebody approved was swept")
	}
}

// A program's expired token is kept for its refresh window; a hand-minted one
// is swept the next day.
//
// The sweep used to remove every token a day after it expired, which made
// renewal after expiry impossible after a day however long the window was.
func TestAProgramsExpiredTokenIsKeptThroughItsRefreshWindow(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	now := time.Now()
	userId := dbtest.CreateUser(test, database, "token-holder")
	create := func(expiredFor time.Duration, refreshHash string) string {
		id := security.NewULID()
		token := &models.Token{ID: id, UserID: userId, Name: "a token", ExpiresAt: now.Add(-expiredFor)}
		var err error
		if refreshHash == "" {
			_, err = database.CreateToken(token, "a-hash")
		} else {
			token.ClientID, token.Resource = "a-client", "/api/v1/mcp"
			_, err = database.CreateAuthorizedToken(token, "a-hash", refreshHash)
		}
		if err != nil {
			test.Fatalf("creating a token: %s", err)
		}
		return id
	}
	handMinted := create(48*time.Hour, "")
	programRecent := create(48*time.Hour, "a-refresh-hash")
	programForgotten := create(models.RefreshWindow+time.Hour, "a-refresh-hash")

	if _, err := database.ScavengeTokens(now); err != nil {
		test.Fatalf("ScavengeTokens: %s", err)
	}
	for _, check := range []struct {
		describe string
		id       string
		kept     bool
	}{
		{"a hand-minted token two days expired", handMinted, false},
		{"a program's token two days expired", programRecent, true},
		{"a program's token past its refresh window", programForgotten, false},
	} {
		token, _, err := database.GetToken(check.id)
		if err != nil {
			test.Fatalf("GetToken: %s", err)
		}
		if kept := token != nil; kept != check.kept {
			test.Errorf("%s: kept was %v", check.describe, kept)
		}
	}
}

// Retiring a token says whether this call did it.
func TestRetiringATokenSaysWhoRetiredIt(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	id := security.NewULID()
	userId := dbtest.CreateUser(test, database, "token-holder")
	if _, err := database.CreateToken(&models.Token{ID: id, UserID: userId, Name: "a token"}, "a-hash"); err != nil {
		test.Fatalf("CreateToken: %s", err)
	}
	first, err := database.RetireToken(id, time.Now())
	if err != nil || !first {
		test.Fatalf("the first retirement: %v, %v", first, err)
	}
	second, err := database.RetireToken(id, time.Now())
	if err != nil || second {
		test.Errorf("a token already retired was retired again: %v, %v", second, err)
	}
}
