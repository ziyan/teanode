package apigraph

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A key sealed for a recipient or for the agent is a 32 byte item key and
// nothing else: the server cannot open it, so it holds a ciphertext of exactly
// 48 bytes. Kilobytes of garbage in its place would be carried in the
// recipient's list, which the recipient cannot refuse (the numbers are those of
// the audit: 5000 bytes of ciphertext on each of 30 items of 400 KB).
func TestAKeySealedForARecipientOrTheAgentHasTheSizeOfAnItemKey(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	id := world.save(test, ada, "login")["id"].(string)
	grantees := []any{map[string]any{"granteeUserId": world.ben.ID}}
	tooLong := func(size int) string {
		ephemeralPublicKey := append([]byte{4}, bytes.Repeat([]byte{9}, 64)...)
		return `{"version":1,"ephemeralPublicKey":"` + base64.StdEncoding.EncodeToString(ephemeralPublicKey) + `","nonce":"` + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 12)) +
			`","ciphertext":"` + base64.StdEncoding.EncodeToString(make([]byte, size)) + `"}`
	}
	for name, sealed := range map[string]string{
		"5000 bytes":             tooLong(5000),
		"49 bytes":               tooLong(49),
		"47 bytes":               tooLong(47),
		"only the tag":           tooLong(16),
		"a 31 byte item key":     sealedForKeyPairOfSize("short", 31),
		"a 33 byte item key":     sealedForKeyPairOfSize("long", 33),
		"a 4096 byte item key":   sealedForKeyPairOfSize("huge", 4096),
		"a plain sealed value":   sealedOf("not for a key pair"),
		"an empty ciphertext":    tooLong(0),
		"a version 2 sealed key": strings.Replace(sealedForKeyPairOf("key"), `"version":1`, `"version":2`, 1),
	} {
		key := shareSetKeyInput(id, world.ben, ben)
		key["itemKeyByRecipientKey"] = sealed
		if message := world.refusal(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, grantees, []any{key})); !strings.Contains(message, "invalid arguments") {
			test.Errorf("sharing with %s: %s", name, message)
		}
		added := shareKeyInput(id, world.ben, ben)
		added["itemKeyByRecipientKey"] = sealed
		if message := world.refusal(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{added}}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("adding a key of %s: %s", name, message)
		}
		if message := world.refusal(test, world.ada, rekeyPasswordItemQuery, rekeyInput(id, 1, []any{added}, nil)); !strings.Contains(message, "invalid arguments") {
			test.Errorf("re-keying with %s: %s", name, message)
		}
		if message := world.refusal(test, world.ada, passwordAgentUseQuery, map[string]any{"input": map[string]any{"itemId": id, "isUsableByAgent": true, "itemKeyByAgentKey": sealed}}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("an agent copy of %s: %s", name, message)
		}
	}
	// Nothing was stored for ben or for the agent.
	if got := dbtest.QueryString(test, world.database, `SELECT count(*) FROM password_share_key`); got != "0" {
		test.Fatalf("%s keys were stored", got)
	}
	// A key of the right size is stored, for a recipient and for the agent.
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, grantees, []any{shareSetKeyInput(id, world.ben, ben)}))
	world.run(test, world.ada, passwordAgentUseQuery, map[string]any{"input": map[string]any{"itemId": id, "isUsableByAgent": true, "itemKeyByAgentKey": sealedForKeyPairOf("item key for the agent")}})
}

// Another person's shares make up most of a person's own list, and the person
// cannot refuse them: what the sender can make of it is bounded by the keys
// being the size of a key, so fifteen items of 400 KB come to little more than
// their fields.
func TestAShareCannotInflateTheRecipientsListBeyondTheFields(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	const itemCount, fieldsBytes = 15, 400 * 1024
	for index := 0; index < itemCount; index++ {
		input := ada.itemInput("note", nil)
		input["encryptedFields"] = sealedOfSize(fieldsBytes)
		id := world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})["SavePasswordItem"].(map[string]any)["id"].(string)
		key := shareSetKeyInput(id, world.ben, ben)
		key["itemKeyByRecipientKey"] = sealedForKeyPairOfSize("a key", 5000)
		key["itemKey"] = input["itemKey"]
		if message := world.refusal(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{key})); !strings.Contains(message, "invalid arguments") {
			test.Fatalf("a key of 5000 bytes: %s", message)
		}
		key["itemKeyByRecipientKey"] = sealedForKeyPairOf("a key")
		world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{key}))
	}
	list := world.list(test, world.ben, nil)
	encoded, err := json.Marshal(list)
	if err != nil {
		test.Fatal(err)
	}
	if got := len(list["items"].([]any)); got != itemCount {
		test.Fatalf("ben lists %d items", got)
	}
	// The fields are base64 text of the sealed bytes, as they are in the JSON.
	if limit := itemCount * (fieldsBytes + 4096); len(encoded) > limit {
		test.Fatalf("ben's list is %d bytes, more than %d", len(encoded), limit)
	}
}

// The place for a listing is one for the person's own taps and one for every
// other way in, as the write and unlock limits are, so a program holding a token
// cannot keep the person's own dashboard from listing. The waiting list is
// under the same place as the list.
func TestTheListingPlaceIsKeptForTapsAndOtherCredentialsApart(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	world.save(test, ada, "login")
	token := &api.Principal{User: world.ada, CredentialKind: models.CredentialToken}
	session := &api.Principal{User: world.ada, CredentialKind: models.CredentialSession}
	app := &api.Principal{User: world.ada, CredentialKind: models.CredentialApp}

	releaseToken, err := world.graph.takePasswordsListing(token)
	if err != nil {
		test.Fatal(err)
	}
	// The token's place is taken: the token's list and waiting list are turned
	// away, the person's session and the phone app are not.
	for name, query := range map[string]string{"list": listPasswordItemsQuery, "waiting": waitingPasswordSharesQuery} {
		if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, query, nil); len(failures) == 0 || !strings.Contains(strings.Join(failures, "; "), "Try again in a moment") {
			test.Fatalf("the token's %s while it holds its place: %v", name, failures)
		}
		if _, failures := world.callWithCredential(test, world.ada, models.CredentialSession, query, nil); len(failures) > 0 {
			test.Fatalf("the session's %s while a token holds its place: %v", name, failures)
		}
	}
	releaseSession, err := world.graph.takePasswordsListing(session)
	if err != nil {
		test.Fatalf("the session's place while a token holds the token's: %v", err)
	}
	// The phone app is a tap too: it shares the session's place.
	if _, err := world.graph.takePasswordsListing(app); err == nil {
		test.Fatalf("the phone app took the place the session holds")
	}
	releaseSession()
	releaseToken()
	// Another person has a place of their own, and a place given back is free.
	ben := newPasswordsDevice(test, world.ben)
	world.setUp(test, ben)
	releaseAda, err := world.graph.takePasswordsListing(token)
	if err != nil {
		test.Fatal(err)
	}
	world.run(test, world.ben, waitingPasswordSharesQuery, nil)
	releaseAda()
	world.run(test, world.ada, waitingPasswordSharesQuery, nil)
	world.run(test, world.ada, listPasswordItemsQuery, nil)
}

// A token that reads its list slowly held the person's own place, so that the
// dashboard could not list (the numbers are those of the audit: 70 notes of
// 500 KB). The token's place is held while its answer is being written; a
// session asks at the same time and is answered.
func TestATokenReadingSlowlyDoesNotHoldThePersonsListingPlace(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	for step := 0; step < 70; step++ {
		input := ada.itemInput("note", nil)
		input["encryptedFields"] = sealedOfSize(500 * 1024)
		world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request.Header.Set(api.AuthenticatedUsernameHeader, world.ada.Username)
		request.Header.Set(api.CredentialKindHeader, request.Header.Get("X-Test-Kind"))
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
	ask := func(kind models.CredentialKind) *bufio.Reader {
		connection, err := net.Dial("tcp", address)
		if err != nil {
			test.Fatal(err)
		}
		connections = append(connections, connection)
		if _, err := fmt.Fprintf(connection, "POST /graphql HTTP/1.1\r\nHost: x\r\nX-Test-Kind: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", kind, len(body), body); err != nil {
			test.Fatal(err)
		}
		_ = connection.SetReadDeadline(time.Now().Add(30 * time.Second))
		return bufio.NewReader(connection)
	}
	slow := ask(models.CredentialToken)
	if _, err := slow.Peek(16); err != nil {
		test.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	// The token's second list is turned away while the first is being written.
	tokenResponse, err := http.ReadResponse(ask(models.CredentialToken), nil)
	if err != nil {
		test.Fatal(err)
	}
	tokenAnswer, _ := io.ReadAll(tokenResponse.Body)
	if !strings.Contains(string(tokenAnswer), "already being read") {
		test.Fatalf("a second list of the token while the first was being written: %.300s", tokenAnswer)
	}
	// The person's own session is answered with the list.
	sessionResponse, err := http.ReadResponse(ask(models.CredentialSession), nil)
	if err != nil {
		test.Fatal(err)
	}
	sessionAnswer, _ := io.ReadAll(sessionResponse.Body)
	if strings.Contains(string(sessionAnswer), "already being read") || !strings.Contains(string(sessionAnswer), `"itemIds"`) {
		test.Fatalf("the person's session while a token reads slowly: %.300s", sessionAnswer)
	}
}

// Bringing a deleted item back lets its old shares reach their recipients
// again, so it is a person's tap: a token is refused, and the item stays
// deleted.
func TestRestoringADeletedPasswordItemNeedsATap(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	id := world.save(test, ada, "login")["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{shareSetKeyInput(id, world.ben, ben)}))
	world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": id})
	for _, kind := range []models.CredentialKind{models.CredentialToken, ""} {
		if _, failures := world.callWithCredential(test, world.ada, kind, restorePasswordItemQuery, map[string]any{"id": id}); !isTapRefusal(strings.Join(failures, "; ")) {
			test.Fatalf("restoring with credential %q: %v", kind, failures)
		}
	}
	if got := dbtest.QueryString(test, world.database, `SELECT (deleted_at IS NOT NULL)::text FROM password_item WHERE id = '`+id+`'`); got != "true" {
		test.Fatalf("the item came back")
	}
	if waiting := world.run(test, world.ada, waitingPasswordSharesQuery, nil)["ListWaitingPasswordShares"].([]any); len(waiting) != 0 {
		test.Fatalf("ben waits on a deleted item: %v", waiting)
	}
	for _, kind := range []models.CredentialKind{models.CredentialSession, models.CredentialApp} {
		restored, failures := world.callWithCredential(test, world.ada, kind, restorePasswordItemQuery, map[string]any{"id": id})
		if len(failures) > 0 || restored["RestorePasswordItem"].(map[string]any)["deletedAt"] != nil {
			test.Fatalf("restoring with credential %q: %v %v", kind, restored, failures)
		}
		world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": id})
	}
}

// A list answers a circle's worth of recipients for each item, so the waiting
// list is cut at 2000 entries and says so; an owner whose items are shared
// with a circle of 40 people has 300 items waiting on each (the audit's
// numbers), and the first 2000 entries are those of the newest items.
func TestTheWaitingListIsCutAndSaysSo(test *testing.T) {
	world := newPasswordsWorld(test)
	const memberCount, itemCount = 40, 300
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	var members []*models.User
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		for index := 0; index < memberCount; index++ {
			user, err := tx.CreateUser(&models.User{Username: fmt.Sprintf("member%03d", index)})
			if err != nil {
				test.Fatal(err)
			}
			dbtest.GrantPermission(test, tx, user.ID, models.PermissionPasswordsUse)
			if _, err := tx.AddCircleMember(world.home, user.ID, models.MemberRoleMember, world.ada.ID); err != nil {
				test.Fatal(err)
			}
			members = append(members, user)
		}
	})
	for _, user := range members {
		world.setUp(test, newPasswordsDevice(test, user))
	}
	var lastID string
	for index := 0; index < itemCount; index++ {
		lastID = world.save(test, ada, "login")["id"].(string)
		world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(lastID, []any{map[string]any{"granteeCircleId": world.home}}, nil))
	}
	const waitingQuery = `query { ListWaitingPasswordShares { itemId hasMoreWaiting recipient { userId publicKey hasKey hasTooMuchShared } } }`
	waiting := world.run(test, world.ada, waitingQuery, nil)["ListWaitingPasswordShares"].([]any)
	// Ben is in the circle too, and has not set up Passwords: he never waits.
	if len(waiting) != 2000 {
		test.Fatalf("the waiting list has %d entries, want 2000 of %d", len(waiting), memberCount*itemCount)
	}
	for _, each := range waiting {
		entry := each.(map[string]any)
		if entry["hasMoreWaiting"] != true || entry["recipient"].(map[string]any)["hasTooMuchShared"] != false {
			test.Fatalf("an entry of the cut list: %v", entry)
		}
	}
	if waiting[0].(map[string]any)["itemId"] != lastID {
		test.Fatalf("the first entry is for %v, the newest item is %s", waiting[0].(map[string]any)["itemId"], lastID)
	}
	// Somebody with nothing waiting gets an empty list.
	short := world.run(test, world.ben, waitingQuery, nil)["ListWaitingPasswordShares"].([]any)
	if len(short) != 0 {
		test.Fatalf("ben waits on %d: %v", len(short), short)
	}
}
