package server

import (
	"context"
	"errors"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Reading the list headers of mail that arrived before this server knew what a
// subscription was.
//
// A message says which mailing list it came from in its headers, and those are
// in object storage rather than in the database — so the columns the
// subscriptions page reads are written when a message is stored. Every message
// stored before that has them empty, which on a mailbox that has been running
// for months is all of them.
//
// This walks that mail in batches, reads each message back, and records what
// it said. It is a background job rather than part of the migration because it
// is one storage read per message: a migration holds a transaction, and a
// hundred thousand small reads inside one is a way to be down for an hour.
const (
	// How many to read in one pass. Small enough that a pass is quick and the
	// work is spread over several minutes rather than taken all at once.
	listBackfillBatch = 200
)

// backfillMailLists reads one batch. It returns the number of messages
// examined, so the caller can tell "there was work" from "there was none".
func backfillMailLists(ctx context.Context, database db.Database, store storage.Storage) (int, error) {
	var ids []string
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListMailNeedingList(listBackfillBatch)
		ids = found
		return err
	}); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	// Read outside a transaction: these are network calls when storage is S3,
	// and a transaction held open across them is a transaction held open for
	// as long as the slowest of them.
	type examined struct {
		id     string
		parsed mailparse.ListInfo
	}
	results := make([]examined, 0, len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			return len(results), ctx.Err()
		}
		headers, _, err := store.Get(ctx, id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				// The message is gone from storage but its row is still here.
				// Mark it read anyway: there is nothing more to learn about
				// it, and leaving it unread means reading it again forever.
				results = append(results, examined{id: id})
				continue
			}
			return len(results), err
		}
		results = append(results, examined{
			id:     id,
			parsed: mailparse.ParseList(headers, mailparse.FindHeaderValue(headers, "From")),
		})
	}

	subscriptions := 0
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		subscriptions = 0
		for _, result := range results {
			if err := tx.SetMailList(result.id, result.parsed); err != nil {
				return err
			}
			if result.parsed.Subscription() {
				subscriptions++
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if subscriptions > 0 {
		log.Noticef("read the list headers of %d stored messages; %d belong to a mailing list",
			len(results), subscriptions)
	}
	return len(results), nil
}
