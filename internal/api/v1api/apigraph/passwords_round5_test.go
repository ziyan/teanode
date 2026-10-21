package apigraph

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A token that fills a waiting recipient's place with a key that opens nothing
// does not keep the owner's real key out: the owner's tap replaces it. What a
// tap stored is replaced by nobody, and a call that is not a tap replaces
// nothing.
func TestATokenCannotKeepTheRealKeyOutOfAWaitingRecipientsPlace(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	id := world.save(test, ada, "login")["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, nil))
	if waiting := world.run(test, world.ada, waitingPasswordSharesQuery, nil)["ListWaitingPasswordShares"].([]any); len(waiting) != 1 {
		test.Fatalf("expected ben waiting: %v", waiting)
	}
	bensKey := func() any {
		return world.list(test, world.ben, nil)["items"].([]any)[0].(map[string]any)["itemKey"]
	}
	addKey := func(credentialKind models.CredentialKind, sealed string) any {
		key := shareKeyInput(id, world.ben, ben)
		key["itemKeyByRecipientKey"] = sealedForKeyPairOf(sealed)
		data, failures := world.callWithCredential(test, world.ada, credentialKind, addPasswordShareKeysQuery, map[string]any{"keys": []any{key}})
		if len(failures) > 0 {
			test.Fatalf("AddPasswordShareKeys as %s: %v", credentialKind, failures)
		}
		return data["AddPasswordShareKeys"]
	}

	if stored := addKey(models.CredentialToken, "garbage from a token"); stored != float64(1) {
		test.Fatalf("the token's key was not stored: %v", stored)
	}
	if got := bensKey(); got != sealedForKeyPairOf("garbage from a token") {
		test.Fatalf("ben's key: %v", got)
	}
	if stored := addKey(models.CredentialToken, "more garbage"); stored != float64(0) {
		test.Fatalf("a token replaced a token's key: %v", stored)
	}
	// The owner's device seals the real key with a tap, and it is stored.
	if stored := addKey(models.CredentialSession, "the real key"); stored != float64(1) {
		test.Fatalf("the owner's tap did not replace the token's key: %v", stored)
	}
	if got := bensKey(); got != sealedForKeyPairOf("the real key") {
		test.Fatalf("ben's key after the tap: %v", got)
	}
	// And now nobody replaces it.
	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, models.CredentialSession} {
		if stored := addKey(credentialKind, "overwritten by "+string(credentialKind)); stored != float64(0) {
			test.Fatalf("a call as %s replaced a tap's key: %v", credentialKind, stored)
		}
	}
	if got := bensKey(); got != sealedForKeyPairOf("the real key") {
		test.Fatalf("ben's key at the end: %v", got)
	}
}

// SetPasswordItemShares names the item key its keys were sealed from, like
// AddPasswordShareKeys: a device that read the item before it was re-keyed
// stores nothing, and a key that names none is refused.
func TestSetPasswordItemSharesRefusesAKeyFromAnOldItemKey(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	item := world.save(test, ada, "login")
	id := item["id"].(string)
	// Another device re-keys the item.
	rekeyed := world.run(test, world.ada, rekeyPasswordItemQuery, rekeyInput(id, item["revision"], nil, nil))["RekeyPasswordItem"].(map[string]any)
	if rekeyed["itemKey"] == item["itemKey"] {
		test.Fatalf("the re-key left the item key as it was")
	}
	grantees := []any{map[string]any{"granteeUserId": world.ben.ID}}

	// The stale device shares with a key it sealed from the old item key.
	stale := shareSetKeyInput(id, world.ben, ben)
	if message := world.refusal(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, grantees, []any{stale})); !strings.Contains(message, "changed on another device") {
		test.Fatalf("a key from the old item key: %s", message)
	}
	missing := shareSetKeyInput(id, world.ben, ben)
	delete(missing, "itemKey")
	if message := world.refusal(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, grantees, []any{missing})); !strings.Contains(message, "itemKey") {
		test.Fatalf("a key that names no item key: %s", message)
	}
	current := world.list(test, world.ada, nil)["items"].([]any)[0].(map[string]any)
	if shares := current["shares"].([]any); len(shares) != 0 || current["revision"] != rekeyed["revision"] {
		test.Fatalf("refused calls left shares %v or moved the revision to %v", shares, current["revision"])
	}

	// With the current item key it is stored.
	fresh := shareSetKeyInput(id, world.ben, ben)
	fresh["itemKey"] = rekeyed["itemKey"]
	shared := world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, grantees, []any{fresh}))["SetPasswordItemShares"].(map[string]any)
	recipients := shared["shares"].([]any)[0].(map[string]any)["recipients"].([]any)
	if len(recipients) != 1 || recipients[0].(map[string]any)["hasKey"] != true {
		test.Fatalf("the key from the current item key: %v", recipients)
	}
}

// holdPasswordsTransaction runs the first step in a transaction that stays
// open, with its locks held, until the second, started once it is open, has
// had half a second to wait behind it.
func holdPasswordsTransaction(test *testing.T, world *passwordsWorld, first func(tx db.Transaction), second func()) {
	test.Helper()
	done := make(chan struct{})
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		first(tx)
		go func() {
			defer close(done)
			second()
		}()
		time.Sleep(500 * time.Millisecond)
	})
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		test.Fatalf("the call that waited did not finish after the transaction it waited for committed")
	}
}

// A token that waits behind the owner's unshare does not add the person back
// on the strength of a read made before: whether a call adds a grantee is
// decided once the locks are held.
func TestATokenWaitingBehindAnUnshareDoesNotAddThePersonBack(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	id := world.save(test, ada, "login")["id"].(string)
	grantees := []any{map[string]any{"granteeUserId": world.ben.ID}}
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, grantees, []any{shareSetKeyInput(id, world.ben, ben)}))

	var failures []string
	holdPasswordsTransaction(test, world,
		func(tx db.Transaction) {
			// The owner's unshare holds the locks and has not committed.
			if _, err := tx.SetPasswordItemShares(world.ada.ID, id, nil, nil, true); err != nil {
				test.Fatal(err)
			}
		},
		func() {
			_, failures = world.callWithCredential(test, world.ada, models.CredentialToken, setPasswordItemSharesQuery, sharesInput(id, grantees, nil))
		})
	if len(failures) == 0 || !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("the token waiting behind the unshare: %v", failures)
	}
	after := world.list(test, world.ada, nil)["items"].([]any)[0].(map[string]any)
	if shares := after["shares"].([]any); len(shares) != 0 {
		test.Fatalf("the owner unshared and the token added the person back: %v", shares)
	}
}

// One list at a time for a person: the place is held until the answer has been
// written to the reader, not only until the resolver returns, so slow readers
// cannot each hold a full answer in memory; and the writing has a deadline, so
// one slow reader cannot hold the place for ever.
func TestTheListingPlaceIsHeldUntilTheAnswerIsWritten(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	for step := 0; step < 70; step++ {
		input := ada.itemInput("note", nil)
		input["encryptedFields"] = sealedOfSize(500 * 1024)
		world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})
	}
	previousTimeout := passwordsListWriteTimeout
	passwordsListWriteTimeout = 3 * time.Second
	test.Cleanup(func() { passwordsListWriteTimeout = previousTimeout })

	var inFlight atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request.Header.Set(api.AuthenticatedUsernameHeader, world.ada.Username)
		request.Header.Set(api.CredentialKindHeader, string(models.CredentialToken))
		inFlight.Add(1)
		defer inFlight.Add(-1)
		world.graph.graphView(response, request)
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	body := `{"query":"query { ListPasswordItems { itemIds revision items { id itemKey encryptedFields } } }"}`
	var connections []net.Conn
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	ask := func() *bufio.Reader {
		test.Helper()
		connection, err := net.Dial("tcp", address)
		if err != nil {
			test.Fatal(err)
		}
		connections = append(connections, connection)
		if _, err := fmt.Fprintf(connection, "POST /graphql HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			test.Fatal(err)
		}
		if err := connection.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			test.Fatal(err)
		}
		return bufio.NewReader(connection)
	}

	// The first reader takes a few bytes and no more: its handler stays in the
	// write, holding the person's place.
	slow := ask()
	if _, err := slow.Peek(16); err != nil {
		test.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if inFlight.Load() != 1 {
		test.Fatalf("%d handlers are running", inFlight.Load())
	}
	// A second list is turned away while the first answer is still being written.
	secondResponse, err := http.ReadResponse(ask(), nil)
	if err != nil {
		test.Fatal(err)
	}
	answer, err := io.ReadAll(secondResponse.Body)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(string(answer), "already being read") {
		test.Fatalf("a second list while the first was being written: %.300s", answer)
	}
	if inFlight.Load() != 1 {
		test.Fatalf("%d handlers are holding an answer", inFlight.Load())
	}

	// The write has a deadline, so the slow reader loses its answer and the
	// place is free again.
	deadline := time.Now().Add(15 * time.Second)
	for inFlight.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if inFlight.Load() != 0 {
		test.Fatalf("the slow reader still holds an answer after the deadline")
	}
	third := ask()
	head, err := third.Peek(64)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(string(head), "already being read") {
		test.Fatalf("the place was not given back: %s", head)
	}
}

// The numbers a device sends for a revision are 64-bit: text is refused, a
// number past 32 bits is read whole, and one that is not the item's revision
// does not save over it.
func TestRevisionNumbersAreTakenAsSixtyFourBitIntegers(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	item := world.save(test, ada, "login")
	for _, since := range []any{"abc", true} {
		if _, failures := world.callWith(test, world.ada, listPasswordItemsQuery, map[string]any{"sinceRevision": since}); len(failures) == 0 {
			test.Errorf("sinceRevision %v was accepted", since)
		}
	}
	// Anything in range is a revision to continue from: a negative one reads
	// everything, one past the highest reads nothing.
	for since, wantedCount := range map[float64]int{-5: 1, 5_000_000_000: 0} {
		data, failures := world.callWith(test, world.ada, listPasswordItemsQuery, map[string]any{"sinceRevision": since})
		if len(failures) > 0 {
			test.Fatalf("sinceRevision %v: %v", since, failures)
		}
		if got := len(data["ListPasswordItems"].(map[string]any)["items"].([]any)); got != wantedCount {
			test.Errorf("sinceRevision %v read %d items, want %d", since, got, wantedCount)
		}
	}
	for _, base := range []any{1e30, "abc", item["revision"].(float64) + 5_000_000_000} {
		input := ada.namedItemInput(item["id"].(string), "login", base)
		if _, failures := world.callWith(test, world.ada, savePasswordItemQuery, map[string]any{"input": input}); len(failures) == 0 {
			test.Errorf("a save based on revision %v was accepted", base)
		}
		if _, failures := world.callWith(test, world.ada, rekeyPasswordItemQuery, rekeyInput(item["id"].(string), base, nil, nil)); len(failures) == 0 {
			test.Errorf("a re-key based on revision %v was accepted", base)
		}
	}
}
