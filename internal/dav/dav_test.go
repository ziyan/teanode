package dav_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

const appPassword = "correct-horse-battery-staple"

const aCard = "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:ada\r\nFN:Ada Lovelace\r\n" +
	"N:Lovelace;Ada;;;\r\nEMAIL:ada@example.com\r\nEND:VCARD\r\n"

// world is a server with one account, one mailbox with one address and one
// app password, and an address book.
type world struct {
	server   *httptest.Server
	database db.Database
	userID   string
	other    string
	bookID   string
	address  string
}

func newWorld(t *testing.T) (*world, func()) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)

	here := &world{database: database, address: "alice@example.com"}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		// A role that may keep an address book, and an account in it.
		role, err := tx.CreateRole(&models.Role{
			Name:        "member",
			Permissions: []models.Permission{models.PermissionContactsUse, models.PermissionMailRead},
		})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		here.userID = owner.ID
		if _, err := tx.CreateGroup(&models.Group{
			Name: "people", RoleIDs: []string{role.ID}, UserIDs: []string{owner.ID},
		}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		stranger, err := tx.CreateUser(&models.User{Username: "bertie"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		here.other = stranger.ID

		// A domain, a mailbox, an address that delivers to it, and the app
		// password a device signs in with.
		domain, err := tx.CreateDomain(&models.Domain{ID: "example.com", Domain: "example.com"})
		if err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if _, err := tx.CreateAlias(&models.Alias{
			DomainID: domain.ID, Pattern: "^alice$", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID,
		}); err != nil {
			t.Fatalf("CreateAlias: %s", err)
		}
		hash, err := security.HashPassword(appPassword)
		if err != nil {
			t.Fatalf("HashPassword: %s", err)
		}
		if _, err := tx.CreateAppPassword(&models.MailboxAppPassword{
			MailboxID: mailbox.ID, Name: "phone", PasswordHash: string(hash),
		}); err != nil {
			t.Fatalf("CreateAppPassword: %s", err)
		}
		book, err := tx.CreateAddressBook(&models.AddressBook{UserID: owner.ID, Name: "Contacts"})
		if err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		here.bookID = book.ID
	})

	component, err := dav.New(database, config.NewMemoryStore(config.Default()), nil)
	if err != nil {
		t.Fatalf("dav.New: %s", err)
	}
	// The same router the real server uses, StrictSlash and all, because
	// the trailing-slash behaviour is one of the things under test.
	router := mux.NewRouter().StrictSlash(true)
	if err := component.AddRoutes(router); err != nil {
		t.Fatalf("AddRoutes: %s", err)
	}
	here.server = httptest.NewServer(router)
	return here, func() {
		here.server.Close()
		closeDatabase()
	}
}

// ask makes one request, signed in unless told otherwise.
func (self *world) ask(t *testing.T, method, path, body string, headers ...string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, self.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %s", err)
	}
	request.SetBasicAuth(self.address, appPassword)
	// A body sent with PROPFIND or REPORT is XML, and the library refuses
	// one that does not say so. Real clients always send this.
	if body != "" && (method == "PROPFIND" || method == "REPORT") {
		request.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	for index := 0; index+1 < len(headers); index += 2 {
		if headers[index] == "" {
			request.Header.Del("Authorization")
			continue
		}
		request.Header.Set(headers[index], headers[index+1])
	}
	// No redirects followed: a redirect is the bug, not a step on the way.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	answer, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %s", method, path, err)
	}
	return answer
}

func text(t *testing.T, answer *http.Response) string {
	t.Helper()
	body, _ := io.ReadAll(answer.Body)
	_ = answer.Body.Close()
	return string(body)
}

const propfindETags = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getetag/></d:prop></d:propfind>`

// Nobody reaches an address book without signing in, and the refusal says how.
func TestSigningInIsRequiredAndSaysHow(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	answer := here.ask(t, "PROPFIND", dav.Prefix+"/", "", "", "")
	if answer.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a credential: %d", answer.StatusCode)
	}
	if !strings.HasPrefix(answer.Header.Get("WWW-Authenticate"), "Basic ") {
		t.Fatalf("a client is told how to sign in: %q", answer.Header.Get("WWW-Authenticate"))
	}
	_ = text(t, answer)

	// A wrong password is the same answer as an unknown address, so that a
	// guess learns nothing about which addresses have mailboxes.
	request, _ := http.NewRequest("PROPFIND", here.server.URL+dav.Prefix+"/", nil)
	request.SetBasicAuth(here.address, "not-the-password")
	wrong, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request: %s", err)
	}
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password: %d", wrong.StatusCode)
	}
	_ = text(t, wrong)

	request, _ = http.NewRequest("PROPFIND", here.server.URL+dav.Prefix+"/", nil)
	request.SetBasicAuth("nobody@example.com", appPassword)
	unknown, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request: %s", err)
	}
	if unknown.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an unknown address: %d", unknown.StatusCode)
	}
	_ = text(t, unknown)
}

// A client that knows only the server finds its way to the address book.
func TestAClientFindsItsWayFromTheMount(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	// RFC 6764: a client given only a mail address looks here.
	wellKnown := here.ask(t, "PROPFIND", "/.well-known/carddav", "")
	if wellKnown.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("/.well-known/carddav sends a client onward: %d", wellKnown.StatusCode)
	}
	if place := wellKnown.Header.Get("Location"); place != dav.Prefix+"/" {
		t.Fatalf("onward to: %q", place)
	}
	_ = text(t, wellKnown)

	principal := `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`
	answer := here.ask(t, "PROPFIND", dav.Prefix+"/", principal, "Depth", "0")
	body := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("the mount answers who you are: %d\n%s", answer.StatusCode, body)
	}
	if !strings.Contains(body, dav.Prefix+"/"+here.userID+"/") {
		t.Fatalf("and names the principal:\n%s", body)
	}

	// From the principal to the home set.
	homeSet := `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:prop><c:addressbook-home-set/></d:prop></d:propfind>`
	answer = here.ask(t, "PROPFIND", dav.Prefix+"/"+here.userID+"/", homeSet, "Depth", "0")
	body = text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus || !strings.Contains(body, "/contacts/") {
		t.Fatalf("the principal names the home set: %d\n%s", answer.StatusCode, body)
	}

	// And from the home set to the book itself.
	answer = here.ask(t, "PROPFIND", dav.Prefix+"/"+here.userID+"/contacts/", propfindETags, "Depth", "1")
	body = text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus || !strings.Contains(body, here.bookID) {
		t.Fatalf("the home set lists the book: %d\n%s", answer.StatusCode, body)
	}
}

// Somebody else's address book is refused, not hidden: that other accounts
// exist is not a secret, and a 404 sends some clients into a retry loop.
func TestAnotherAccountsAddressBookIsRefused(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	answer := here.ask(t, "PROPFIND", dav.Prefix+"/"+here.other+"/contacts/", propfindETags, "Depth", "1")
	if answer.StatusCode != http.StatusForbidden {
		t.Fatalf("another account's books: %d", answer.StatusCode)
	}
	_ = text(t, answer)
}

// A collection asked for without its trailing slash must be answered, not
// redirected. The router is built with StrictSlash, which would redirect; an
// HTTP client turns that redirect into a GET, so a PROPFIND would arrive as a
// GET and be refused. This test is here because that cost real time to find.
func TestACollectionIsNeverRedirected(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	for _, path := range []string{
		dav.Prefix,
		dav.Prefix + "/" + here.userID,
		dav.Prefix + "/" + here.userID + "/contacts",
		dav.Prefix + "/" + here.userID + "/contacts/" + here.bookID,
	} {
		answer := here.ask(t, "PROPFIND", path, propfindETags, "Depth", "0")
		if answer.StatusCode == http.StatusMovedPermanently || answer.StatusCode == http.StatusPermanentRedirect {
			t.Errorf("%s was redirected (%d); a redirect turns PROPFIND into GET", path, answer.StatusCode)
		}
		if answer.StatusCode != http.StatusMultiStatus {
			t.Errorf("%s answered %d, wanted 207", path, answer.StatusCode)
		}
		_ = text(t, answer)
	}
}

// The whole of what a device does: put a contact, list it, read it back,
// change it, and delete it.
func TestADeviceKeepsContactsInStep(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	where := fmt.Sprintf("%s/%s/contacts/%s/ada.vcf", dav.Prefix, here.userID, here.bookID)

	put := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard")
	if put.StatusCode != http.StatusCreated && put.StatusCode != http.StatusNoContent && put.StatusCode != http.StatusOK {
		t.Fatalf("putting a contact: %d %s", put.StatusCode, text(t, put))
	}
	etag := put.Header.Get("ETag")
	_ = text(t, put)
	if etag == "" {
		t.Fatal("a kept contact has an etag, which is how a device knows it is the one it wrote")
	}

	// Listed, with that etag: this is how a device finds what changed.
	listed := here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/%s/", dav.Prefix, here.userID, here.bookID),
		propfindETags, "Depth", "1")
	body := text(t, listed)
	if listed.StatusCode != http.StatusMultiStatus || !strings.Contains(body, "ada.vcf") {
		t.Fatalf("the listing has the contact: %d\n%s", listed.StatusCode, body)
	}
	if !strings.Contains(body, strings.Trim(etag, `"`)) {
		t.Fatalf("and its etag:\n%s", body)
	}

	// Read back as a vCard, with what was sent still on it.
	got := here.ask(t, http.MethodGet, where, "")
	card := text(t, got)
	if got.StatusCode != http.StatusOK || !strings.Contains(card, "FN:Ada Lovelace") {
		t.Fatalf("reading it back: %d\n%s", got.StatusCode, card)
	}
	if !strings.Contains(card, "ada@example.com") {
		t.Fatalf("with the address:\n%s", card)
	}

	// Changed, and the etag moves with it.
	changed := strings.Replace(aCard, "Ada Lovelace", "Ada King", 1)
	second := here.ask(t, http.MethodPut, where, changed, "Content-Type", "text/vcard", "If-Match", etag)
	_ = text(t, second)
	if second.StatusCode >= 400 {
		t.Fatalf("a conditional write with the right etag: %d", second.StatusCode)
	}
	if moved := second.Header.Get("ETag"); moved == etag {
		t.Fatal("the etag moves when the card changes")
	}

	// Gone, and no longer listed, which is how a device learns of a
	// deletion without any tombstone to read.
	removed := here.ask(t, http.MethodDelete, where, "")
	_ = text(t, removed)
	if removed.StatusCode >= 400 {
		t.Fatalf("deleting: %d", removed.StatusCode)
	}
	listed = here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/%s/", dav.Prefix, here.userID, here.bookID),
		propfindETags, "Depth", "1")
	body = text(t, listed)
	if strings.Contains(body, "ada.vcf") {
		t.Fatalf("a deleted contact stops being listed:\n%s", body)
	}
}

// Two devices editing the same person at once must not silently overwrite one
// another.
func TestAWriteOverSomebodyElsesIsRefused(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	where := fmt.Sprintf("%s/%s/contacts/%s/ada.vcf", dav.Prefix, here.userID, here.bookID)
	first := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard")
	_ = text(t, first)

	// A device writing back the version it read, which somebody else has
	// since changed.
	stale := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard", "If-Match", `"not-the-one-you-read"`)
	_ = text(t, stale)
	if stale.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("a stale If-Match: %d, wanted 412", stale.StatusCode)
	}

	// A device creating somebody who is already there.
	exists := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard", "If-None-Match", "*")
	_ = text(t, exists)
	if exists.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("If-None-Match over an existing contact: %d, wanted 412", exists.StatusCode)
	}

	// And a card too large to keep is refused with the status that makes a
	// client stop resending rather than retry for ever.
	huge := strings.Replace(aCard, "END:VCARD", "NOTE:"+strings.Repeat("x", 1<<21)+"\r\nEND:VCARD", 1)
	big := here.ask(t, http.MethodPut, where, huge, "Content-Type", "text/vcard")
	_ = text(t, big)
	if big.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("an oversized card: %d, wanted 507", big.StatusCode)
	}
}

// The last segment of a contact's URL is the client's to choose, and clients
// choose differently. iOS and macOS name a card after its UID, which is a
// thirty-six character UUID; a server that only accepted its own
// thirty-two character identifiers refused every contact either of them ever
// made, with a 500 and no explanation.
func TestAClientNamesTheFileAndThisServerTakesIt(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	for _, name := range []string{
		"A1B2C3D4-E5F6-4789-ABCD-0123456789AB", // what iOS uses
		"ada",                                  // short, which some clients use
		strings.Repeat("n", 255),               // the longest that fits
	} {
		card := strings.Replace(aCard, "urn:uuid:ada", "urn:uuid:"+name, 1)
		where := fmt.Sprintf("%s/%s/contacts/%s/%s.vcf", dav.Prefix, here.userID, here.bookID, name)
		put := here.ask(t, http.MethodPut, where, card, "Content-Type", "text/vcard")
		body := text(t, put)
		if put.StatusCode >= 400 {
			t.Errorf("a client naming a card %q: %d %s", name, put.StatusCode, body)
			continue
		}
		got := here.ask(t, http.MethodGet, where, "")
		back := text(t, got)
		if got.StatusCode != http.StatusOK || !strings.Contains(back, "FN:Ada Lovelace") {
			t.Errorf("reading %q back: %d\n%s", name, got.StatusCode, back)
		}
	}

	// And a name this server cannot keep is refused with a reason, rather
	// than becoming a 500 from the column underneath. (A name with a
	// control character in it is refused too, but no test can send one:
	// Go will not build a URL containing one, which is itself most of the
	// protection.)
	tooLong := strings.Repeat("n", 256)
	where := fmt.Sprintf("%s/%s/contacts/%s/%s.vcf", dav.Prefix, here.userID, here.bookID, tooLong)
	answer := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusBadRequest {
		t.Errorf("a name of %d characters: %d, wanted 400", len(tooLong), answer.StatusCode)
	}
}
