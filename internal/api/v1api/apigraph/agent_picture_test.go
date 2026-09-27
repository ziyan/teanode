package apigraph

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A picture in an answer is fetched only when a tool showed the agent its
// address in that conversation, as it stands; an address the agent put
// together is refused before anything is fetched, and so is anybody else's
// conversation. What is fetched goes through the same guarded client as
// the mail image proxy, which refuses this test's loopback address: a
// shown picture therefore gets as far as the fetch and fails there.
func TestAPictureIsFetchedOnlyWhenAToolShowedIt(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	picture := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer picture.Close()
	shownAddress := picture.URL + "/boxes/twenty.png?size=large&tone=light"

	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		owner, ownersAgent, _ := anAgentWithASource(test, tx, "picture-owner", "Alice Example")
		grantAgentUse(test, tx, owner)
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: ownersAgent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			test.Fatalf("CreateAgentConversation: %s", err)
		}
		// A browser tool's answer, written as JSON: & comes out as &.
		answer := `[{"alt":"a box of twenty","src":"` + strings.ReplaceAll(shownAddress, "&", `&`) + `"}]`
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "tool", Name: "browser", ToolCallID: "call_1", Content: answer}); err != nil {
			test.Fatalf("AppendAgentMessage: %s", err)
		}
		// The agent's own words, which may name any address at all.
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "assistant", Content: "![boxes](" + picture.URL + "/made-up.png?note=secret)"}); err != nil {
			test.Fatalf("AppendAgentMessage: %s", err)
		}
		stranger, _, _ := anAgentWithASource(test, tx, "picture-stranger", "Carol Example")
		grantAgentUse(test, tx, stranger)
	})

	router := mux.NewRouter()
	resolver := &graph{database: database}
	router.Path(api.PathAgentPicture).Methods(http.MethodGet).HandlerFunc(resolver.agentPictureView)
	fetch := func(username, conversationId, address string) *httptest.ResponseRecorder {
		path := strings.Replace(api.PathAgentPicture, "{conversationId}", conversationId, 1) + "?url=" + url.QueryEscape(address)
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if username != "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, username)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	if shown := fetch("picture-owner", conversation.ID, shownAddress); shown.Code != http.StatusBadGateway {
		test.Errorf("a picture a tool showed should get as far as the fetch, answered %d: %s", shown.Code, shown.Body.String())
	}
	if madeUp := fetch("picture-owner", conversation.ID, picture.URL+"/made-up.png?note=secret"); madeUp.Code != http.StatusNotFound {
		test.Errorf("an address only the agent wrote answered %d, want 404", madeUp.Code)
	}
	if theirs := fetch("picture-stranger", conversation.ID, shownAddress); theirs.Code != http.StatusNotFound {
		test.Errorf("another person's conversation answered %d, want 404", theirs.Code)
	}
	if nobody := fetch("", conversation.ID, shownAddress); nobody.Code != http.StatusUnauthorized {
		test.Errorf("a caller who is not signed in answered %d, want 401", nobody.Code)
	}
	if local := fetch("picture-owner", conversation.ID, "file:///etc/passwd"); local.Code != http.StatusBadRequest {
		test.Errorf("an address that is not http answered %d, want 400", local.Code)
	}
}
