package server

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/notes"
	"github.com/ziyan/teanode/internal/storage"
)

// Finding the notes a phone kept here before this server knew what a note was.
//
// A phone's Notes app keeps each note as a message it appends to a folder of
// the mail account, and those messages were stored as copies of sent mail.
// What says one is a note is in its headers, in storage, so this reads the
// headers of the messages that could be one -- appended copies in folders the
// owner made -- and marks the notes among them.
//
// Once: every message stored since has been looked at as it arrived, and each
// message looked at here is marked either way, so a start after the first
// finds nothing and costs one query.
const (
	// How many to read in one batch; the batches run one after another
	// until there are none left.
	noteBackfillBatch = 200
)

// backfillNotes reads one batch. It returns the number of messages examined,
// so the caller can tell "there was work" from "there was none".
func backfillNotes(ctx context.Context, database db.Database, store storage.Storage) (int, error) {
	var ids []string
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListMailNeedingNote(noteBackfillBatch)
		ids = found
		return err
	}); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	// Read outside a transaction, as the list headers are: these are network
	// calls when storage is S3.
	type examined struct {
		id             string
		noteIdentifier string
	}
	results := make([]examined, 0, len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			return len(results), ctx.Err()
		}
		headers, _, err := store.Get(ctx, id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				// Nothing more to learn about it, and leaving it unmarked
				// means reading it again on every start.
				results = append(results, examined{id: id})
				continue
			}
			return len(results), err
		}
		results = append(results, examined{id: id, noteIdentifier: notes.Identifier(headers)})
	}

	found := 0
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		found = 0
		for _, result := range results {
			if err := tx.SetMailNote(result.id, result.noteIdentifier); err != nil {
				return err
			}
			if result.noteIdentifier != "" {
				found++
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if found > 0 {
		log.Noticef("read the headers of %d stored messages; %d are notes", len(results), found)
	}
	return len(results), nil
}

// backfillAllNotes runs batches until one finds nothing, or the server
// stops. A batch that fails is tried again a minute later rather than given
// up on until the next start.
func backfillAllNotes(ctx context.Context, database db.Database, store storage.Storage) {
	for ctx.Err() == nil {
		examined, err := backfillNotes(ctx, database, store)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warningf("failed to look for notes among stored messages: %s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
			continue
		}
		if examined == 0 {
			return
		}
	}
}
