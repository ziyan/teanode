package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// The pictures and files a record came with.
//
// This is the ingest's side of an attachment, and it is not attachment.go,
// which is about the files a person hands the agent in a conversation.
// What arrives here nobody handed anybody: a scan of somebody's chat
// archive found fifty thousand screenshots beside the messages, and the
// sentence that pointed at each one was indexed while the thing it
// pointed at was not.
//
// A scan reports such a file as an entry with no text, named by the hash
// of its bytes. This end decides whether it already holds those bytes,
// asks the computer for them when it does not, keeps them in object
// storage under that hash, and writes the key on the document. Nothing
// reads them yet; that is a later night's work, and the point of keeping
// them is that the night comes at three in the morning when the laptop is
// shut.

// maxAttachmentBytes is the largest file a source carries off a person's
// machine: its own limit where it has one, the operator's otherwise, and
// the daemon's own only where an operator has set neither.
//
// The daemon is told the number rather than knowing it, so that raising
// it is a configuration change on the server and not a release everybody
// has to install.
func maxAttachmentBytes(configuration *config.Configuration, source *models.AgentKnowledgeSource) int64 {
	if source != nil && source.Specification.MaxAttachmentBytes > 0 {
		return source.Specification.MaxAttachmentBytes
	}
	if configuration != nil {
		if limit := int64(configuration.Agent.Limits.MaxScannedAttachmentBytes.Bytes()); limit > 0 {
			return limit
		}
	}
	return computer.DefaultMaxAttachmentBytes
}

// attachmentKey is where an attachment's bytes are kept: its own hash, so
// that the same picture pasted into four threads is stored once and a
// second pass over an archive uploads nothing.
//
// Empty for anything that is not a hash. The key becomes a path in the
// store, and what a scan reports is a name of somebody else's choosing.
func attachmentKey(hash string) string {
	if len(hash) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return ""
	}
	return hash
}

// blobFetcher fetches an attachment's bytes from the machine they are on.
// A function rather than the computer itself, so that what is decided
// here -- already held, or worth asking for -- can be tested without one.
type blobFetcher func(ctx context.Context) ([]byte, error)

// keepAttachmentBytes answers the key an attachment's bytes are kept
// under, fetching them only when nothing holds them already.
func keepAttachmentBytes(ctx context.Context, files storage.Files, hash, stored string, fetch blobFetcher) (string, error) {
	if stored != "" {
		// Something already holds these bytes: the same picture in
		// another thread, in another source, or in this one before a
		// pass was interrupted. Asking the computer again would carry
		// twenty-five megabytes across the socket for a file this server
		// can already open.
		return stored, nil
	}
	key := attachmentKey(hash)
	if key == "" {
		return "", fmt.Errorf("%q is not the hash of anything", hash)
	}
	if files == nil {
		return "", errors.New("this server has nowhere to keep a file")
	}
	content, err := fetch(ctx)
	if err != nil {
		return "", err
	}
	// The key is the hash, so bytes that are not what they were said to
	// be would be filed under a name every other document with that hash
	// then reads. The daemon checks this too; it is checked again here
	// because this is the end that writes the name.
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != hash {
		return "", fmt.Errorf("what came back is not the file that was scanned")
	}
	if err := files.PutFile(ctx, key, content); err != nil {
		return "", err
	}
	return key, nil
}

// DocumentBytes is an attachment document's bytes, out of the store.
//
// The work is in the tools package, because the memory tool's `look` reads
// the same bytes for the person and a tool cannot import this one. Kept
// here as well so that every caller in this package and in the API says
// the same name for the same thing.
func DocumentBytes(ctx context.Context, files storage.Files, document *models.AgentDocument) ([]byte, error) {
	return tools.DocumentBytes(ctx, files, document)
}

// fileAttachment writes one attachment down and keeps its bytes.
//
// It answers whether a document was filed. A failure to fetch or to store
// is not the pass's failure: the document is written without a key, the
// next pass finds it without one and asks again, and one file that was
// busy does not stop a source of four hundred thousand.
func (self *Agent) fileAttachment(ctx context.Context, run *Run, source *models.AgentKnowledgeSource,
	entry computer.ScanEntry, fetch blobFetcher) (bool, error) {
	var existing *models.AgentDocument
	var stored string
	wasRead := true
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if existing, err = tx.GetAgentDocumentByExternal(source.ID, entry.ExternalID); err != nil {
			return err
		}
		// Asked only where the computer read something out of it this
		// time: whether it had been read before decides whether it is
		// filed again.
		if existing != nil && strings.TrimSpace(entry.Text) != "" {
			if wasRead, err = tx.HasAgentChunks(source.AgentID, existing.ID); err != nil {
				return err
			}
		}
		stored, err = tx.AgentDocumentStorageKey(source.AgentID, entry.Hash)
		return err
	}); err != nil {
		return false, err
	}
	// Filed already, with its bytes. An unchanged file is skipped by the
	// pass above; an unchanged attachment is only skipped here, because
	// "unchanged" says the text is the same and the thing that matters
	// about this one is whether its bytes ever arrived.
	//
	// Unless the computer can now read what it could not when the file was
	// filed -- a scan, once it has an OCR program, or a workbook LibreOffice
	// once ran out of time on. The bytes are the same, so the hash is, and
	// the text is the only thing that says so. Filed again, it takes that
	// text and loses the night's "not a picture" with the rest of its old
	// row, and the night reads it like any other document.
	if existing != nil && existing.Hash == entry.Hash && existing.StorageKey != "" && wasRead {
		return false, nil
	}
	key, err := keepAttachmentBytes(ctx, run.Storage(), entry.Hash, stored, fetch)
	if err != nil {
		log.Warningf("cannot keep the bytes of %q of source %q: %s", entry.ExternalID, source.ID, err)
		key = ""
	}
	if _, err := self.fileDocument(ctx, run, source, entry, key); err != nil {
		return false, err
	}
	return true, nil
}

// blobFrom asks a person's computer for one file's bytes.
//
// A request of its own rather than part of the scan: a page of a scan is
// bounded at a few megabytes and one attachment may be twenty-five, so
// the bytes are asked for one file at a time and only for what this
// server does not already hold.
func blobFrom(device *attachedComputer, entry computer.ScanEntry, most int64) blobFetcher {
	return func(ctx context.Context) ([]byte, error) {
		if device == nil {
			return nil, ErrDeviceDetached
		}
		path, _ := entry.Metadata["path"].(string)
		if path == "" {
			return nil, fmt.Errorf("the scan did not say where %q is", entry.Title)
		}
		answer, err := device.Ask(ctx, "blob", &computer.BlobArguments{
			Path: path, Hash: entry.Hash, MaxBytes: most,
		}, ingestDeviceWait)
		if err != nil {
			return nil, err
		}
		var blob computer.BlobResult
		if err := json.Unmarshal(answer, &blob); err != nil {
			return nil, fmt.Errorf("the computer's answer is not readable: %w", err)
		}
		content, err := base64.StdEncoding.DecodeString(blob.Base64)
		if err != nil {
			return nil, fmt.Errorf("the computer's answer is not base64: %w", err)
		}
		return content, nil
	}
}
