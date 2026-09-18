package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// The bytes of an attachment go into the store under their own hash and
// come back out of it with nothing else needed -- no computer attached,
// nobody awake -- which is the whole reason they are kept rather than
// fetched when they are wanted.
func TestAnAttachmentsBytesComeBackOutOfTheStore(t *testing.T) {
	store := &fakeFiles{files: map[string][]byte{}}
	picture := []byte("\x89PNG\r\n\x1a\na screenshot of a failing cell")
	sum := sha256.Sum256(picture)
	hash := hex.EncodeToString(sum[:])

	key, err := keepAttachmentBytes(context.Background(), store, hash, "", func(context.Context) ([]byte, error) {
		return picture, nil
	})
	if err != nil {
		t.Fatalf("keeping the bytes: %s", err)
	}
	if key != hash {
		t.Fatalf("kept under %q rather than its own hash", key)
	}

	document := &models.AgentDocument{
		Title: "shot.png", Kind: models.DocumentAttachment,
		Hash: hash, Bytes: int64(len(picture)), StorageKey: key,
	}
	content, err := DocumentBytes(context.Background(), store, document)
	if err != nil {
		t.Fatalf("reading them back: %s", err)
	}
	if string(content) != string(picture) {
		t.Fatalf("what came back is %q", content)
	}

	// A document whose bytes never arrived says so in words a person can
	// act on, rather than a panic or an empty answer that reads like an
	// empty file.
	without := &models.AgentDocument{Title: "shot.png", Kind: models.DocumentAttachment, Hash: hash}
	if _, err := DocumentBytes(context.Background(), store, without); err == nil {
		t.Fatal("a document with no key answered with something")
	} else if !strings.Contains(err.Error(), "shot.png") || !strings.Contains(err.Error(), "no stored bytes") {
		t.Fatalf("the reason does not say what is wrong: %s", err)
	}
	if _, err := DocumentBytes(context.Background(), store, nil); err == nil {
		t.Fatal("no document at all answered with something")
	}

	// And a key whose bytes are gone is the store's own answer, not a
	// silence.
	missing := &models.AgentDocument{Title: "gone.png", StorageKey: strings.Repeat("a", 64)}
	if _, err := DocumentBytes(context.Background(), store, missing); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("a key nothing is under: %v", err)
	}
}

// The same picture pasted into four threads is one set of bytes. Once
// something holds them, the computer is not asked again -- which on the
// archive this was written for is the difference between carrying
// twenty-four gigabytes once and carrying it every pass.
func TestAnAttachmentAlreadyStoredIsNotFetchedAgain(t *testing.T) {
	store := &fakeFiles{files: map[string][]byte{}}
	picture := []byte("the same screenshot")
	sum := sha256.Sum256(picture)
	hash := hex.EncodeToString(sum[:])
	asked := 0
	fetch := func(context.Context) ([]byte, error) {
		asked++
		return picture, nil
	}

	key, err := keepAttachmentBytes(context.Background(), store, hash, "", fetch)
	if err != nil || asked != 1 {
		t.Fatalf("the first time it has to be asked: key %q, asked %d, %v", key, asked, err)
	}
	again, err := keepAttachmentBytes(context.Background(), store, hash, key, fetch)
	if err != nil {
		t.Fatalf("keeping the bytes again: %s", err)
	}
	if again != key {
		t.Fatalf("the second thread's picture was kept under %q rather than %q", again, key)
	}
	if asked != 1 {
		t.Fatalf("the computer was asked %d times for bytes this server already had", asked)
	}

	// Bytes that are not the ones that were scanned are refused rather
	// than written: the key is the hash, and filing something else under
	// it would hand those bytes to every document with that hash.
	if _, err := keepAttachmentBytes(context.Background(), store, hash, "", func(context.Context) ([]byte, error) {
		return []byte("a different file entirely"), nil
	}); err == nil {
		t.Fatal("bytes that do not hash to the name they would be filed under were kept")
	}
}

// The bound on one file is the source's where it has one, the operator's
// where it does not, and the number this program was written with only
// where an operator has set neither.
func TestTheBoundOnAnAttachmentIsTheSourcesOrTheServers(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Limits.MaxScannedAttachmentBytes = 8 << 20
	source := &models.AgentKnowledgeSource{ID: "one", Kind: models.SourceArchive}

	if most := maxAttachmentBytes(configuration, source); most != 8<<20 {
		t.Fatalf("a source with nothing of its own takes the server's: %d", most)
	}
	source.Specification.MaxAttachmentBytes = 60 << 20
	if most := maxAttachmentBytes(configuration, source); most != 60<<20 {
		t.Fatalf("a source's own bound wins: %d", most)
	}
	source.Specification.MaxAttachmentBytes = 0
	if most := maxAttachmentBytes(configuration, source); most != 8<<20 {
		t.Fatalf("zero on a source is the server's bound, not no bound: %d", most)
	}

	// An operator who has set neither gets the one this program was
	// written with, which is also what a daemon told nothing falls back
	// to, so the two ends agree.
	configuration.Agent.Limits.MaxScannedAttachmentBytes = 0
	if most := maxAttachmentBytes(configuration, source); most != computer.DefaultMaxAttachmentBytes {
		t.Fatalf("with nothing set anywhere: %d", most)
	}
	if most := maxAttachmentBytes(nil, nil); most != computer.DefaultMaxAttachmentBytes {
		t.Fatalf("with no configuration at all: %d", most)
	}
}

// Filing one attachment: the row is written with the key its bytes are
// kept under, which is the first thing in this program ever to fill the
// column the original design reserved for them. A pass that meets the
// same picture again -- in this source or in another thread of it -- asks
// the computer for nothing.
func TestFilingAnAttachmentWritesTheKeyOnTheDocument(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	store := &fakeFiles{files: map[string][]byte{}}
	settings := &Settings{Database: database, Storage: store}
	worker, run := &Agent{settings: settings}, &Run{settings: settings}
	ctx := context.Background()

	var source *models.AgentKnowledgeSource
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		owner, err := tx.CreateUser(&models.User{
			Username: "ziyan-" + time.Now().Format("150405.000000000"), Name: "Ziyan Example",
		})
		if err != nil {
			return err
		}
		person, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			return err
		}
		source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: person.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/records", Format: models.FormatRecords,
			},
		})
		return err
	}); err != nil {
		t.Fatalf("making somebody with a source: %s", err)
	}

	picture := []byte("\x89PNG\r\n\x1a\na screenshot of a failing cell")
	sum := sha256.Sum256(picture)
	hash := hex.EncodeToString(sum[:])
	entry := computer.ScanEntry{
		ExternalID: "posts.jsonl#" + hash, Kind: computer.KindAttachment,
		Title: "shot.png", Hash: hash, Size: int64(len(picture)),
		Metadata: map[string]any{
			"path": "~/records/files/shot.png", "contentType": "image/png",
			"channel": "ops", "said": "look at this, the cell stopped again",
		},
	}
	asked := 0
	fetch := func(context.Context) ([]byte, error) {
		asked++
		return picture, nil
	}

	filed, err := worker.fileAttachment(ctx, run, source, entry, fetch)
	if err != nil || !filed || asked != 1 {
		t.Fatalf("filing it: filed %v, asked %d, %v", filed, asked, err)
	}
	if string(store.files[hash]) != string(picture) {
		t.Fatalf("the bytes are not in the store under their hash: %v", store.files)
	}
	var document *models.AgentDocument
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		document, err = tx.GetAgentDocumentByExternal(source.ID, entry.ExternalID)
		return err
	}); err != nil || document == nil {
		t.Fatalf("the document: %v %v", document, err)
	}
	if document.StorageKey != hash || document.Kind != models.DocumentAttachment {
		t.Fatalf("filed as %+v", document)
	}
	if document.Bytes != int64(len(picture)) || document.Title != "shot.png" {
		t.Fatalf("named and measured: %+v", document)
	}
	if said, _ := document.Metadata["said"].(string); said != "look at this, the cell stopped again" {
		t.Fatalf("what was said around it: %+v", document.Metadata)
	}

	// The next pass over the same archive. The daemon says it is
	// unchanged; the row is there with its bytes, so nothing is asked
	// and nothing is filed.
	unchanged := entry
	unchanged.Unchanged = true
	filed, err = worker.fileAttachment(ctx, run, source, unchanged, fetch)
	if err != nil || filed || asked != 1 {
		t.Fatalf("a second pass: filed %v, asked %d, %v", filed, asked, err)
	}

	// The same picture in another file of the same folder: a document of
	// its own, under the key this server already holds.
	elsewhere := entry
	elsewhere.ExternalID = "other.jsonl#" + hash
	filed, err = worker.fileAttachment(ctx, run, source, elsewhere, fetch)
	if err != nil || !filed {
		t.Fatalf("the same picture in another thread: filed %v, %v", filed, err)
	}
	if asked != 1 {
		t.Fatalf("the computer was asked %d times for bytes this server already had", asked)
	}
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		document, err = tx.GetAgentDocumentByExternal(source.ID, elsewhere.ExternalID)
		return err
	}); err != nil || document == nil || document.StorageKey != hash {
		t.Fatalf("the second document: %v %v", document, err)
	}
}

// A computer that cannot be reached is not the pass's failure. The
// document is filed without a key, so that what was there is known and
// the next pass fills the bytes in.
func TestAnAttachmentWhoseBytesCannotBeFetchedIsStillFiled(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	store := &fakeFiles{files: map[string][]byte{}}
	settings := &Settings{Database: database, Storage: store}
	worker, run := &Agent{settings: settings}, &Run{settings: settings}
	ctx := context.Background()

	var source *models.AgentKnowledgeSource
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		owner, err := tx.CreateUser(&models.User{
			Username: "ziyan-" + time.Now().Format("150405.000000000"), Name: "Ziyan Example",
		})
		if err != nil {
			return err
		}
		person, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			return err
		}
		source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: person.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/records", Format: models.FormatRecords,
			},
		})
		return err
	}); err != nil {
		t.Fatalf("making somebody with a source: %s", err)
	}

	hash := strings.Repeat("d", 64)
	entry := computer.ScanEntry{
		ExternalID: "posts.jsonl#" + hash, Kind: computer.KindAttachment,
		Title: "shot.png", Hash: hash, Size: 4096,
		Metadata: map[string]any{"path": "~/records/files/shot.png"},
	}
	filed, err := worker.fileAttachment(ctx, run, source, entry, func(context.Context) ([]byte, error) {
		return nil, errors.New("the computer went away")
	})
	if err != nil || !filed {
		t.Fatalf("a file whose bytes did not arrive: filed %v, %v", filed, err)
	}
	var document *models.AgentDocument
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		document, err = tx.GetAgentDocumentByExternal(source.ID, entry.ExternalID)
		return err
	}); err != nil || document == nil {
		t.Fatalf("the document: %v %v", document, err)
	}
	if document.StorageKey != "" {
		t.Fatalf("a key was written for bytes that never arrived: %q", document.StorageKey)
	}
	if len(store.files) != 0 {
		t.Fatalf("something was put in the store: %v", store.files)
	}
}
