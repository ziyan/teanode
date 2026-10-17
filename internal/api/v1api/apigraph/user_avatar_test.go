package apigraph

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/web"
)

// avatarWorld is a server with two ordinary people and an account manager.
type avatarWorld struct {
	graph    *graph
	router   *mux.Router
	database db.Database
	files    storage.Storage

	person, other, manager *models.User
}

func newAvatarWorld(t *testing.T) *avatarWorld {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	world := &avatarWorld{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		create := func(username string, permissions ...models.Permission) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			if len(permissions) > 0 {
				role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: permissions})
				if err != nil {
					t.Fatalf("CreateRole: %s", err)
				}
				if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
					t.Fatalf("CreateGroup: %s", err)
				}
			}
			return user
		}
		world.person = create("fixture-avatar-person")
		world.other = create("fixture-avatar-other")
		world.manager = create("fixture-avatar-manager", models.PermissionUserManage)
	})
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "teanode1.key"), []byte("not a real key"), 0o600); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	configuration := config.Example()
	configuration.Server.DataDirectory = directory
	store := config.NewMemoryStore(configuration)
	t.Cleanup(func() { _ = store.Close() })
	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("EnsureSecrets: %s", err)
	}
	authenticator, err := web.NewAuthenticator(store, database)
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	files, err := storage.Open(&storage.Settings{Directory: filepath.Join(directory, "spool")})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	t.Cleanup(func() { _ = files.Close() })
	world.files = files
	component, err := New(database, store, files, nil, nil, nil, nil, authenticator, nil, &api.Settings{})
	if err != nil {
		t.Fatalf("New: %s", err)
	}
	world.graph = component.(*graph)
	world.router = mux.NewRouter()
	world.router.Path(api.PathAvatarUser).Methods(http.MethodGet, http.MethodHead).HandlerFunc(world.graph.avatarView)
	return world
}

// ask sends GraphQL as the named account and returns the data and the error
// messages.
func (self *avatarWorld) ask(t *testing.T, user *models.User, query string) (map[string]any, []string) {
	t.Helper()
	body, _ := json.Marshal(graphRequest{Query: query})
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(api.AuthenticatedUsernameHeader, user.Username)
	response := httptest.NewRecorder()
	self.graph.graphView(response, request)
	var outcome struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &outcome)
	var messages []string
	for _, failure := range outcome.Errors {
		messages = append(messages, failure.Message)
	}
	return outcome.Data, messages
}

// fetch asks for a picture as the named account.
func (self *avatarWorld) fetch(user *models.User, address string, headers ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, address, nil)
	request.Header.Set(api.AuthenticatedUsernameHeader, user.Username)
	for index := 0; index+1 < len(headers); index += 2 {
		request.Header.Set(headers[index], headers[index+1])
	}
	response := httptest.NewRecorder()
	self.router.ServeHTTP(response, request)
	return response
}

// picture is a PNG, wide so that the crop has something to do, as the base64
// text the mutation takes.
func picture(t *testing.T, fill color.RGBA) string {
	t.Helper()
	wide := image.NewRGBA(image.Rect(0, 0, 90, 60))
	draw.Draw(wide, wide.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, wide); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(encoded.Bytes())
}

func setAvatar(userId, content string) string {
	if userId == "" {
		return `mutation { SetUserAvatar(content: "` + content + `") { id avatarUrl avatarColorIndex } }`
	}
	return `mutation { SetUserAvatar(userId: "` + userId + `", content: "` + content + `") { id avatarUrl avatarColorIndex } }`
}

func avatarAddress(t *testing.T, data map[string]any, operation string) string {
	t.Helper()
	user, _ := data[operation].(map[string]any)
	address, _ := user["avatarUrl"].(string)
	return address
}

func TestAPersonSetsAndRemovesTheirOwnPicture(t *testing.T) {
	world := newAvatarWorld(t)

	data, errors := world.ask(t, world.person, setAvatar("", picture(t, color.RGBA{R: 200, A: 255})))
	if len(errors) > 0 {
		t.Fatalf("SetUserAvatar: %v", errors)
	}
	address := avatarAddress(t, data, "SetUserAvatar")
	if !strings.HasPrefix(address, "/api/v1/avatar/user/"+world.person.ID+"?v=") {
		t.Fatalf("the picture's address is %q", address)
	}
	user, _ := data["SetUserAvatar"].(map[string]any)
	if _, ok := user["avatarColorIndex"].(float64); !ok {
		t.Errorf("the answer carries no color index: %v", user)
	}

	// Served as a square PNG at the stored size, and at 64 when asked.
	response := world.fetch(world.person, address)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("fetching the picture: %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	decoded, err := png.Decode(response.Body)
	if err != nil || decoded.Bounds().Dx() != 512 || decoded.Bounds().Dy() != 512 {
		t.Fatalf("the picture is %v, err %v; want 512x512", decoded.Bounds(), err)
	}
	if got := response.Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Errorf("Cache-Control is %q", got)
	}
	small := world.fetch(world.person, "/api/v1/avatar/user/"+world.person.ID+"?size=64")
	decoded, err = png.Decode(small.Body)
	if err != nil || decoded.Bounds().Dx() != 64 {
		t.Fatalf("the small picture is %v, err %v", decoded.Bounds(), err)
	}
	if world.fetch(world.person, "/api/v1/avatar/user/"+world.person.ID+"?size=100").Code != http.StatusBadRequest {
		t.Error("a size that is not offered was served")
	}

	// A browser that has it is told nothing changed.
	tag := response.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag")
	}
	if again := world.fetch(world.person, address, "If-None-Match", tag); again.Code != http.StatusNotModified {
		t.Errorf("a matching If-None-Match answered %d", again.Code)
	}

	// Replacing it makes a new address, and the old file is deleted.
	stored, err := world.database.GetUser(world.person.ID)
	if err != nil || stored.AvatarFileID == "" {
		t.Fatalf("the stored account has no picture: %v", err)
	}
	oldFile := stored.AvatarFileID
	if !strings.HasPrefix(oldFile, "avatar-user-"+world.person.ID+"-") {
		t.Errorf("the picture is stored as %q", oldFile)
	}
	_, errors = world.ask(t, world.person, setAvatar("", picture(t, color.RGBA{B: 200, A: 255})))
	if len(errors) > 0 {
		t.Fatalf("replacing: %v", errors)
	}
	stored, _ = world.database.GetUser(world.person.ID)
	if stored.AvatarFileID == oldFile {
		t.Error("the replacement reused the old file's name")
	}
	if _, err := world.files.GetFile(t.Context(), oldFile); err == nil {
		t.Error("the replaced picture's file is still there")
	}

	// Removing it deletes the file and the address goes back to null.
	data, errors = world.ask(t, world.person, `mutation { RemoveUserAvatar { id avatarUrl } }`)
	if len(errors) > 0 {
		t.Fatalf("RemoveUserAvatar: %v", errors)
	}
	if user, _ := data["RemoveUserAvatar"].(map[string]any); user["avatarUrl"] != nil {
		t.Errorf("a removed picture still has an address: %v", user)
	}
	if _, err := world.files.GetFile(t.Context(), stored.AvatarFileID); err == nil {
		t.Error("the removed picture's file is still there")
	}
	if got := world.fetch(world.person, "/api/v1/avatar/user/"+world.person.ID).Code; got != http.StatusNotFound {
		t.Errorf("a person without a picture answered %d, want 404", got)
	}
}

func TestAPictureThatCannotBeUsedIsRefusedInPlainWords(t *testing.T) {
	world := newAvatarWorld(t)
	for name, content := range map[string]string{
		"text":   base64.StdEncoding.EncodeToString([]byte("not a picture")),
		"base64": "***",
	} {
		_, errors := world.ask(t, world.person, setAvatar("", content))
		if len(errors) == 0 {
			t.Errorf("%s: accepted", name)
			continue
		}
		if name == "text" && !strings.Contains(errors[0], "the picture must be a PNG or JPEG of at most 20 megapixels") {
			t.Errorf("%s: the refusal reads %q", name, errors[0])
		}
	}
	stored, _ := world.database.GetUser(world.person.ID)
	if stored.AvatarFileID != "" {
		t.Error("a refused picture left a file on the account")
	}
}

func TestChangingSomebodyElsesPictureNeedsUserManage(t *testing.T) {
	world := newAvatarWorld(t)
	content := picture(t, color.RGBA{G: 200, A: 255})

	// An ordinary person is answered as if the account were not there.
	_, errors := world.ask(t, world.other, setAvatar(world.person.ID, content))
	if len(errors) == 0 {
		t.Fatal("somebody set another person's picture")
	}
	if _, errors := world.ask(t, world.other, `mutation { RemoveUserAvatar(userId: "`+world.person.ID+`") { id } }`); len(errors) == 0 {
		t.Fatal("somebody removed another person's picture")
	}

	// naming themselves is the same as naming nobody
	if _, errors := world.ask(t, world.other, setAvatar(world.other.ID, content)); len(errors) > 0 {
		t.Fatalf("a person naming themselves was refused: %v", errors)
	}

	// An account manager may.
	if _, errors := world.ask(t, world.manager, setAvatar(world.person.ID, content)); len(errors) > 0 {
		t.Fatalf("the manager was refused: %v", errors)
	}
	stored, _ := world.database.GetUser(world.person.ID)
	if stored.AvatarFileID == "" {
		t.Fatal("the manager's change did not reach the account")
	}
	if _, errors := world.ask(t, world.manager, `mutation { RemoveUserAvatar(userId: "`+world.person.ID+`") { id } }`); len(errors) > 0 {
		t.Fatalf("the manager could not remove it: %v", errors)
	}
}

func TestWhoMayFetchAPicture(t *testing.T) {
	world := newAvatarWorld(t)
	if _, errors := world.ask(t, world.person, setAvatar("", picture(t, color.RGBA{R: 90, A: 255}))); len(errors) > 0 {
		t.Fatalf("SetUserAvatar: %v", errors)
	}
	address := "/api/v1/avatar/user/" + world.person.ID

	for name, test := range map[string]struct {
		user *models.User
		want int
	}{
		"the person":         {world.person, http.StatusOK},
		"an account manager": {world.manager, http.StatusOK},
		// Anybody who shares no circle with them: not told apart from a
		// person with no picture. A circle-mate is in circle_test.go.
		"somebody else": {world.other, http.StatusNotFound},
	} {
		if got := world.fetch(test.user, address).Code; got != test.want {
			t.Errorf("%s: %d, want %d", name, got, test.want)
		}
	}
	if got := world.fetch(world.manager, "/api/v1/avatar/user/no-such-account").Code; got != http.StatusNotFound {
		t.Errorf("an account that does not exist answered %d", got)
	}
	anonymous := httptest.NewRequest(http.MethodGet, address, nil)
	response := httptest.NewRecorder()
	world.router.ServeHTTP(response, anonymous)
	if response.Code != http.StatusUnauthorized {
		t.Errorf("a request with no account answered %d", response.Code)
	}
}

func TestCanSeeAvatarIsTheOnePlaceThatDecides(t *testing.T) {
	world := newAvatarWorld(t)

	principalOf := func(user *models.User, permissions ...models.Grant) *api.Principal {
		return &api.Principal{User: user, Permissions: models.NewEffectivePermissions(permissions)}
	}
	own := principalOf(world.person)
	manager := principalOf(world.manager, models.Grant{Permission: models.PermissionUserManage})
	if err := world.database.Transaction(func(tx db.Transaction) error {
		for _, test := range []struct {
			name      string
			principal *api.Principal
			userId    string
			want      bool
		}{
			{"a person sees their own", own, world.person.ID, true},
			{"a person does not see a stranger's", own, world.other.ID, false},
			{"nobody has an empty id", own, "", false},
			{"an account manager sees every picture", manager, world.person.ID, true},
			{"nobody signed in sees nothing", nil, world.person.ID, false},
			{"the console is not in any circle", &api.Principal{Permissions: models.NewEffectivePermissions(nil)}, world.person.ID, false},
		} {
			got, err := canSeeAvatar(tx, test.principal, test.userId)
			if err != nil || got != test.want {
				t.Errorf("%s: %v, %v; want %v", test.name, got, err, test.want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A chat shows each member's name to the others, so the picture goes with it:
// for as long as they are both in it, whatever circles they share.
func TestPeopleInAChatTogetherSeeEachOthersPicture(t *testing.T) {
	world := newAvatarWorld(t)
	own := &api.Principal{User: world.other, Permissions: models.NewEffectivePermissions(nil)}
	canSee := func() bool {
		t.Helper()
		var got bool
		if err := world.database.Transaction(func(tx db.Transaction) (err error) {
			got, err = canSeeAvatar(tx, own, world.person.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if canSee() {
		t.Fatal("strangers see each other's pictures")
	}
	var chatId string
	if err := world.database.Transaction(func(tx db.Transaction) error {
		chat, err := tx.CreateGroupChat(world.person.ID, "Trip", []string{world.other.ID})
		if err != nil {
			return err
		}
		chatId = chat.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !canSee() {
		t.Error("two people in a chat together do not see each other's pictures")
	}
	if err := world.database.Transaction(func(tx db.Transaction) error {
		_, err := tx.RemoveChatMember(chatId, world.person.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if canSee() {
		t.Error("a person who left the chat is still seen")
	}
}

func TestTheSessionAndTheUserListCarryThePicture(t *testing.T) {
	world := newAvatarWorld(t)
	if _, errors := world.ask(t, world.person, setAvatar("", picture(t, color.RGBA{R: 90, A: 255}))); len(errors) > 0 {
		t.Fatalf("SetUserAvatar: %v", errors)
	}
	data, errors := world.ask(t, world.person, `{ GetSession { avatarUrl avatarColorIndex } GetCurrentUser { avatarUrl avatarColorIndex } }`)
	if len(errors) > 0 {
		t.Fatalf("query: %v", errors)
	}
	for _, name := range []string{"GetSession", "GetCurrentUser"} {
		entry, _ := data[name].(map[string]any)
		if address, _ := entry["avatarUrl"].(string); !strings.Contains(address, world.person.ID) {
			t.Errorf("%s carries the address %v", name, entry["avatarUrl"])
		}
	}
	data, errors = world.ask(t, world.manager, `{ ListUsers { id avatarUrl } }`)
	if len(errors) > 0 {
		t.Fatalf("ListUsers: %v", errors)
	}
	listed, _ := data["ListUsers"].([]any)
	withPicture := 0
	for _, entry := range listed {
		if entry.(map[string]any)["avatarUrl"] != nil {
			withPicture++
		}
	}
	if withPicture != 1 {
		t.Errorf("%d listed accounts have a picture, want 1", withPicture)
	}
}

// A photograph is bigger than the one megabyte a stranger's request may be,
// and the picture goes up inside the mutation, so a signed-in request has
// room for the eight megabytes a picture may be.
func TestAPhotographLargerThanAStrangersRequestGoesUp(t *testing.T) {
	world := newAvatarWorld(t)
	noise := image.NewNRGBA(image.Rect(0, 0, 700, 700))
	seed := uint32(1)
	for index := range noise.Pix {
		seed = seed*1664525 + 1013904223
		noise.Pix[index] = byte(seed >> 24)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, noise); err != nil {
		t.Fatal(err)
	}
	content := base64.StdEncoding.EncodeToString(encoded.Bytes())
	if len(content) <= maximumRequestSize {
		t.Fatalf("the test picture is only %d bytes of text", len(content))
	}
	// As a client sends it: the picture in the variables, not the document.
	body, _ := json.Marshal(graphRequest{
		Query:     `mutation ($content: String!) { SetUserAvatar(content: $content) { id } }`,
		Variables: map[string]any{"content": content},
	})
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(api.AuthenticatedUsernameHeader, world.person.Username)
	response := httptest.NewRecorder()
	world.graph.graphView(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "errors") {
		t.Fatalf("SetUserAvatar: %d %s", response.Code, response.Body.String())
	}
}

// Deleting an account deletes the file its picture was kept in: no row names
// it any more, and nothing else would.
func TestDeletingAnAccountDeletesItsPictureFile(t *testing.T) {
	world := newAvatarWorld(t)
	if _, errors := world.ask(t, world.other, setAvatar("", picture(t, color.RGBA{R: 200, A: 255}))); len(errors) > 0 {
		t.Fatalf("SetUserAvatar: %v", errors)
	}
	stored, err := world.database.GetUser(world.other.ID)
	if err != nil || stored.AvatarFileID == "" {
		t.Fatalf("the stored account has no picture: %v", err)
	}
	if _, err := world.files.GetFile(t.Context(), stored.AvatarFileID); err != nil {
		t.Fatalf("the picture's file is not there to begin with: %s", err)
	}
	if _, errors := world.ask(t, world.manager, fmt.Sprintf(`mutation { DeleteUser(userId: %q) }`, world.other.ID)); len(errors) > 0 {
		t.Fatalf("DeleteUser: %v", errors)
	}
	if _, err := world.files.GetFile(t.Context(), stored.AvatarFileID); err == nil {
		t.Error("the deleted account's picture file is still there")
	}
}
