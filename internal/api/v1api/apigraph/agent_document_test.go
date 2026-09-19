package apigraph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A picture a record came with is a document like any other -- read by
// the night, quoted as evidence on a page -- and until this there was no
// way for a browser to see the thing itself. It is served to the person
// whose archive it is, shown in the page when it is a picture and handed
// over as a file when it is not, and it is nobody else's to fetch.
func TestAnIndexedFileIsServedToItsOwnerAndToNobodyElse(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatalf("storage.Open: %s", err)
	}
	defer func() {
		_ = files.Close()
	}()
	const picture = "\x89PNG\r\n\x1a\nthe container is empty"
	const key = "e3b0c44298fc1c149afbf4c8996fb924"
	if err := files.PutFile(context.Background(), key, []byte(picture)); err != nil {
		test.Fatalf("PutFile: %s", err)
	}

	var screenshot, report, unkept *models.AgentDocument
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		owner, ownersAgent, source := anAgentWithASource(test, tx, "file-owner", "Alice Example")
		grantAgentUse(test, tx, owner)
		screenshot = anAttachment(test, tx, ownersAgent, source, "cell-stopped.png", key, map[string]any{
			"contentType": "image/png", "channel": "#support", "thread": "the cell stopped again",
		})
		report = anAttachment(test, tx, ownersAgent, source, "shift-report.pdf", key, map[string]any{
			"contentType": "application/pdf",
		})
		// Filed while the machine was busy: the row is there and the
		// bytes are not, which the next pass of the source puts right.
		unkept = anAttachment(test, tx, ownersAgent, source, "not-fetched.png", "", map[string]any{
			"contentType": "image/png",
		})

		stranger, _, _ := anAgentWithASource(test, tx, "file-stranger", "Carol Example")
		grantAgentUse(test, tx, stranger)
	})

	router := mux.NewRouter()
	resolver := &graph{database: database, storage: files}
	router.Path(api.PathAgentDocumentFile).Methods(http.MethodGet).HandlerFunc(resolver.agentDocumentFileView)

	fetch := func(username, documentId string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, api.AgentDocumentFilePath(documentId), nil)
		if username != "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, username)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	// A picture is shown in the page, under the type the source recorded.
	shown := fetch("file-owner", screenshot.ID)
	switch {
	case shown.Code != http.StatusOK:
		test.Fatalf("the owner's own picture answered %d: %s", shown.Code, shown.Body.String())
	case shown.Body.String() != picture:
		test.Errorf("the bytes served are not the bytes stored: %q", shown.Body.String())
	case shown.Header().Get("Content-Type") != "image/png":
		test.Errorf("a picture is served as %q", shown.Header().Get("Content-Type"))
	case shown.Header().Get("Content-Disposition") != `inline; filename="cell-stopped.png"`:
		test.Errorf("a picture is shown rather than saved: %q", shown.Header().Get("Content-Disposition"))
	case shown.Header().Get("X-Content-Type-Options") != "nosniff":
		test.Errorf("the type served is the type meant: %q", shown.Header().Get("X-Content-Type-Options"))
	}

	// Anything else is handed over as a file, so a browser never runs
	// what somebody's archive happened to contain.
	saved := fetch("file-owner", report.ID)
	switch {
	case saved.Code != http.StatusOK:
		test.Fatalf("a file that is not a picture answered %d", saved.Code)
	case saved.Header().Get("Content-Type") != "application/octet-stream":
		test.Errorf("a file that is not a picture is served as %q", saved.Header().Get("Content-Type"))
	case saved.Header().Get("Content-Disposition") != `attachment; filename="shift-report.pdf"`:
		test.Errorf("a file that is not a picture is saved rather than shown: %q", saved.Header().Get("Content-Disposition"))
	}

	// Somebody else's archive is somebody else's, and a caller who is
	// nobody is nobody.
	if theirs := fetch("file-stranger", screenshot.ID); theirs.Code != http.StatusNotFound {
		test.Errorf("another person's file answered %d, want 404", theirs.Code)
	}
	if nobody := fetch("", screenshot.ID); nobody.Code != http.StatusUnauthorized {
		test.Errorf("a caller who is not signed in answered %d, want 401", nobody.Code)
	}
	// A document whose bytes were never kept, and one that does not
	// exist: both are nothing to fetch rather than a failure.
	if missing := fetch("file-owner", unkept.ID); missing.Code != http.StatusNotFound {
		test.Errorf("a document with no file kept for it answered %d, want 404", missing.Code)
	}
	if nothing := fetch("file-owner", "a-document-nobody-has"); nothing.Code != http.StatusNotFound {
		test.Errorf("a document that does not exist answered %d, want 404", nothing.Code)
	}
}

// A fact read out of a screenshot is worth little to somebody who cannot
// see the screenshot. The page carries the files its facts cite, with
// where each came from, and the address to fetch it by -- and only for a
// file this server holds the bytes of.
func TestAPageCarriesTheFilesItsFactsWereReadFrom(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var owner *models.User
	var screenshot, unkept, thread *models.AgentDocument
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var ownersAgent *models.Agent
		var source *models.AgentKnowledgeSource
		owner, ownersAgent, source = anAgentWithASource(test, tx, "page-files-owner", "Alice Example")
		screenshot = anAttachment(test, tx, ownersAgent, source, "cell-stopped.png", "hash-of-the-screenshot", map[string]any{
			"contentType": "image/png", "channel": "#support", "thread": "the cell stopped again",
		})
		unkept = anAttachment(test, tx, ownersAgent, source, "not-fetched.png", "", map[string]any{
			"contentType": "image/png",
		})
		var err error
		if thread, err = tx.PutAgentDocument(&models.AgentDocument{
			AgentID: ownersAgent.ID, SourceID: source.ID, ExternalID: "posts/support.jsonl#12",
			Kind: models.DocumentChat, Title: "support, 16 October",
		}); err != nil {
			test.Fatalf("PutAgentDocument: %s", err)
		}
		if err := tx.EnsureAgentRoots(ownersAgent.ID); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: ownersAgent.ID, Path: "projects/carlisle", Kind: models.NodeProject, Name: "Carlisle",
		})
		if err != nil {
			test.Fatalf("PutAgentNode: %s", err)
		}
		// One fact citing the picture and the thread it was posted in,
		// and one citing a file whose bytes are not here.
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: ownersAgent.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text: "The cell stopped with the container empty on 16 October.",
			Evidence: []models.Evidence{
				{Kind: models.EvidenceDocument, ID: screenshot.ID, Quote: "Container is empty"},
				{Kind: models.EvidenceChat, ID: thread.ID},
			},
		}); err != nil {
			test.Fatalf("AddAgentFact: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: ownersAgent.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text:     "The shift changed at six.",
			Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: unkept.ID}},
		}); err != nil {
			test.Fatalf("AddAgentFact: %s", err)
		}
	})

	resolver := &graph{database: database}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asAgentPerson(owner)), tx)
		page, err := resolver.AgentGraphPage(ctx, AgentGraphPageArguments{Path: "projects/carlisle"})
		if err != nil {
			test.Fatalf("AgentGraphPage: %s", err)
		}
		if len(page.Attachments) != 2 {
			test.Fatalf("the page carries its two files, and carries %d: %+v", len(page.Attachments), page.Attachments)
		}
		files := map[string]*AgentAttachmentFile{}
		for _, file := range page.Attachments {
			files[file.DocumentID] = file
		}
		shown := files[screenshot.ID]
		switch {
		case shown == nil:
			test.Fatalf("the picture the first fact was read from is not on the page: %+v", page.Attachments)
		case shown.Name != "cell-stopped.png":
			test.Errorf("the file is named %q", shown.Name)
		case shown.ContentType != "image/png":
			test.Errorf("the file is a %q", shown.ContentType)
		case shown.Channel != "#support" || shown.Thread != "the cell stopped again":
			test.Errorf("the file says where it came from: %+v", shown)
		case shown.Path != api.AgentDocumentFilePath(screenshot.ID):
			test.Errorf("the file is fetched from %q", shown.Path)
		}
		// A file whose bytes are not here has no address, so nothing
		// links to a picture that would not load.
		if file := files[unkept.ID]; file == nil || file.Path != "" {
			test.Errorf("a file with no bytes kept for it is offered at %+v", file)
		}
		// And a chat unit cited beside the picture is not a file: it has
		// nothing to show.
		if _, cited := files[thread.ID]; cited {
			test.Errorf("a chat thread was carried as though it were a file")
		}
	})
}

// An answer in the drawer carries text and nothing else, so a screenshot
// it was read out of cannot be attached to it. It does not have to: the
// agent cites what it used, as "projects/carlisle#1", and a citation
// resolves to its fact and from there to the file behind it. That is what
// lets the conversation show the evidence rather than ask for it to be
// taken on trust.
func TestACitationResolvesToTheFileTheFactWasReadFrom(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var owner, stranger *models.User
	var screenshot *models.AgentDocument
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var ownersAgent *models.Agent
		var source *models.AgentKnowledgeSource
		owner, ownersAgent, source = anAgentWithASource(test, tx, "citation-owner", "Alice Example")
		screenshot = anAttachment(test, tx, ownersAgent, source, "cell-stopped.png", "hash-of-the-screenshot", map[string]any{
			"contentType": "image/png", "channel": "#support", "thread": "the cell stopped again",
		})
		if err := tx.EnsureAgentRoots(ownersAgent.ID); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: ownersAgent.ID, Path: "projects/carlisle", Kind: models.NodeProject, Name: "Carlisle",
		})
		if err != nil {
			test.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: ownersAgent.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text:     "The cell stopped with the container empty on 16 October.",
			Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: "[" + screenshot.ID + "]"}},
		}); err != nil {
			test.Fatalf("AddAgentFact: %s", err)
		}
		// A second fact, read out of words rather than out of a picture.
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: ownersAgent.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text:     "The shift changed at six.",
			Evidence: []models.Evidence{{Kind: models.EvidenceConversation, ID: "a-conversation"}},
		}); err != nil {
			test.Fatalf("AddAgentFact: %s", err)
		}
		stranger, _, _ = anAgentWithASource(test, tx, "citation-stranger", "Carol Example")
	})

	resolver := &graph{database: database}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asAgentPerson(owner)), tx)
		cited, err := resolver.AgentCitedAttachments(ctx, AgentCitedAttachmentsArguments{Citations: []string{
			"projects/carlisle#1",
			// The same one again, a fact read out of words, a fact that
			// is not there, a page that is not there, and something that
			// is not a citation at all: none of them is an answer, and
			// none of them is a failure.
			"projects/carlisle#1",
			"projects/carlisle#2",
			"projects/carlisle#99",
			"projects/nowhere#1",
			"not a citation",
		}})
		if err != nil {
			test.Fatalf("AgentCitedAttachments: %s", err)
		}
		if len(cited) != 1 {
			test.Fatalf("one citation has a file behind it, and the answer is %+v", cited)
		}
		switch {
		case cited[0].Citation != "projects/carlisle#1":
			test.Errorf("the answer says which citation it is for: %q", cited[0].Citation)
		case len(cited[0].Files) != 1:
			test.Fatalf("the citation has one file behind it: %+v", cited[0].Files)
		case cited[0].Files[0].DocumentID != screenshot.ID:
			test.Errorf("the file behind it is %q", cited[0].Files[0].DocumentID)
		case cited[0].Files[0].Name != "cell-stopped.png":
			test.Errorf("the file is named %q", cited[0].Files[0].Name)
		case cited[0].Files[0].Thread != "the cell stopped again":
			test.Errorf("the file says where it came from: %+v", cited[0].Files[0])
		case cited[0].Files[0].Path != api.AgentDocumentFilePath(screenshot.ID):
			test.Errorf("the file is fetched from %q", cited[0].Files[0].Path)
		}

		// Somebody else's citation reads their own graph, which has no
		// such page: a path out of another person's conversation is not a
		// way into their files.
		strangersContext := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asAgentPerson(stranger)), tx)
		theirs, err := resolver.AgentCitedAttachments(strangersContext, AgentCitedAttachmentsArguments{
			Citations: []string{"projects/carlisle#1"},
		})
		if err != nil || len(theirs) != 0 {
			test.Errorf("another person's citation resolved to %+v: %v", theirs, err)
		}
	})
}

// What the night decided against opening is kept rather than deleted, so
// that a person who disagrees can read the reason. This is the two
// answers the Dreams tab asks for: how many of each source's files became
// what, and which ones it passed over.
func TestTheFilesTheNightDecidedAgainstAreCountedAndListed(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	const reason = "an avatar in a social channel, not worth opening"
	var owner, stranger *models.User
	var source *models.AgentKnowledgeSource
	var avatar *models.AgentDocument
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var ownersAgent *models.Agent
		owner, ownersAgent, source = anAgentWithASource(test, tx, "declined-owner", "Alice Example")
		avatar = anAttachment(test, tx, ownersAgent, source, "avatar.png", "hash-of-the-avatar", map[string]any{
			"contentType": "image/png", "channel": "#random", "thread": "hello",
		})
		waiting := anAttachment(test, tx, ownersAgent, source, "cell-stopped.png", "hash-of-the-screenshot", map[string]any{
			"contentType": "image/png",
		})
		read := anAttachment(test, tx, ownersAgent, source, "dashboard.png", "hash-of-the-dashboard", map[string]any{
			"contentType": "image/png",
		})
		// What the night opened and made text of is a document like any
		// other from there on: it has passages.
		if err := tx.ReplaceAgentChunks(read, []*models.AgentChunk{
			{Text: "The status reads Container is empty.", Segmented: true},
		}); err != nil {
			test.Fatalf("ReplaceAgentChunks: %s", err)
		}
		if err := tx.MarkAgentDocumentsDeclined([]string{avatar.ID}, reason, time.Now()); err != nil {
			test.Fatalf("MarkAgentDocumentsDeclined: %s", err)
		}
		if waiting.ID == "" {
			test.Fatal("the waiting file was not filed")
		}
		stranger, _, _ = anAgentWithASource(test, tx, "declined-stranger", "Carol Example")
	})

	resolver := &graph{database: database}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asAgentPerson(owner)), tx)

		counted, err := resolver.ListAgentSourceAttachments(ctx)
		if err != nil {
			test.Fatalf("ListAgentSourceAttachments: %s", err)
		}
		if len(counted) != 1 || counted[0].SourceID != source.ID {
			test.Fatalf("one source carried files, and the answer is %+v", counted)
		}
		if counted[0].Undecided != 1 || counted[0].Declined != 1 || counted[0].Described != 1 {
			test.Errorf("one file of each of the three, and the answer is %+v", counted[0])
		}

		listed, err := resolver.ListAgentDeclinedAttachments(ctx, ListAgentDeclinedAttachmentsArguments{SourceID: source.ID})
		if err != nil {
			test.Fatalf("ListAgentDeclinedAttachments: %s", err)
		}
		if len(listed) != 1 || listed[0].DocumentID != avatar.ID {
			test.Fatalf("the one file it decided against is %+v", listed)
		}
		switch {
		case listed[0].Declined != reason:
			test.Errorf("the row says why it was passed over: %q", listed[0].Declined)
		case listed[0].Name != "avatar.png":
			test.Errorf("the row names the file: %q", listed[0].Name)
		case listed[0].Channel != "#random":
			test.Errorf("the row says where it came from: %+v", listed[0])
		case listed[0].Path != api.AgentDocumentFilePath(avatar.ID):
			test.Errorf("the row can be opened, at %q", listed[0].Path)
		}

		// Somebody else's source is somebody else's: they are told it is
		// not there, and their own agent has nothing of this one's.
		strangersContext := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asAgentPerson(stranger)), tx)
		if _, err := resolver.ListAgentDeclinedAttachments(strangersContext, ListAgentDeclinedAttachmentsArguments{
			SourceID: source.ID,
		}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person's source is not theirs to list: %v", err)
		}
		theirs, err := resolver.ListAgentDeclinedAttachments(strangersContext, ListAgentDeclinedAttachmentsArguments{})
		if err != nil || len(theirs) != 0 {
			test.Errorf("somebody else's list of declined files is %+v: %v", theirs, err)
		}
	})
}

// --- the setup these three share ---------------------------------------

// anAgentWithASource is a person, their agent, and one archive of records
// they have pointed it at.
func anAgentWithASource(test *testing.T, tx db.Transaction, username, name string) (*models.User, *models.Agent, *models.AgentKnowledgeSource) {
	test.Helper()
	person, err := tx.CreateUser(&models.User{Username: username, Name: name})
	if err != nil {
		test.Fatalf("CreateUser: %s", err)
	}
	theirAgent, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie"})
	if err != nil {
		test.Fatalf("CreateAgent: %s", err)
	}
	source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
		AgentID: theirAgent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
		Specification: models.AgentKnowledgeSpecification{
			Computer: "laptop", Path: "records", Format: models.FormatRecords,
		},
	})
	if err != nil {
		test.Fatalf("PutAgentSource: %s", err)
	}
	return person, theirAgent, source
}

// anAttachment is one picture or file a record came with, filed the way
// the scan files one: its bytes under a key, its name as its title, and
// what is known about it on its metadata.
func anAttachment(test *testing.T, tx db.Transaction, owner *models.Agent, source *models.AgentKnowledgeSource,
	name, storageKey string, metadata map[string]any) *models.AgentDocument {
	test.Helper()
	document, err := tx.PutAgentDocument(&models.AgentDocument{
		AgentID: owner.ID, SourceID: source.ID, ExternalID: "files/" + name,
		Kind: models.DocumentAttachment, Title: name, Hash: storageKey, StorageKey: storageKey,
		Bytes: 2048, Metadata: metadata,
	})
	if err != nil {
		test.Fatalf("PutAgentDocument(%s): %s", name, err)
	}
	return document
}

// grantAgentUse gives a person the permission the file endpoints ask for,
// the way a deployment does: a role in a group they are in.
func grantAgentUse(test *testing.T, tx db.Transaction, person *models.User) {
	test.Helper()
	role, err := tx.CreateRole(&models.Role{
		Name: "Agent " + person.Username, Permissions: []models.Permission{models.PermissionAgentUse},
	})
	if err != nil {
		test.Fatalf("CreateRole: %s", err)
	}
	if _, err := tx.CreateGroup(&models.Group{
		Name: "Group " + person.Username, UserIDs: []string{person.ID}, RoleIDs: []string{role.ID},
	}); err != nil {
		test.Fatalf("CreateGroup: %s", err)
	}
}

// asAgentPerson is the signed-in caller a resolver sees: the person, and
// the one permission everything on the graph asks for.
func asAgentPerson(person *models.User) *api.Principal {
	return &api.Principal{
		User: person,
		Permissions: models.NewEffectivePermissions([]models.Grant{
			{Permission: models.PermissionAgentUse},
		}),
	}
}
