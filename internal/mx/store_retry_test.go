package mx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Storage that fails a set number of times before it works.
type flakyStorage struct {
	storage.Storage
	failures int
	attempts int
}

func (self *flakyStorage) Put(context.Context, string, []string, []byte) error {
	self.attempts++
	if self.attempts <= self.failures {
		return errors.New("the object store is not answering")
	}
	return nil
}

// A message accepted from a sender is not lost to one bad moment.
//
// The row is committed before the content is written, so a failure here
// cannot refuse the message -- it would be delivered twice when the sender
// tried again. What is left is to try more than once, which is what stands
// between a network that blinked and a message sitting in a mailbox that
// nobody can open.
func TestContentIsOfferedToStorageMoreThanOnce(t *testing.T) {
	defer shortenStoreWait(t)()

	mail := &models.Mail{ID: "m1", Headers: []string{"Subject: hello"}, Body: []byte("hello\r\n")}

	failing := &flakyStorage{failures: 2}
	(&exchange{storage: failing}).store(context.Background(), mail)
	if failing.attempts != 3 {
		t.Fatalf("it kept offering until one was taken: %d attempts", failing.attempts)
	}

	// Beyond what it will try, it gives up rather than holding the sender.
	hopeless := &flakyStorage{failures: 99}
	(&exchange{storage: hopeless}).store(context.Background(), mail)
	if hopeless.attempts != storeAttempts {
		t.Fatalf("bounded at %d attempts, made %d", storeAttempts, hopeless.attempts)
	}

	// Nothing to keep, so nothing is offered.
	empty := &flakyStorage{}
	(&exchange{storage: empty}).store(context.Background(), &models.Mail{ID: "m2"})
	if empty.attempts != 0 {
		t.Fatalf("a message refused before DATA is not stored: %d", empty.attempts)
	}
}

// A cancelled context stops the waiting rather than sleeping through a
// shutdown.
func TestGivingUpOnStorageDoesNotOutlastAShutdown(t *testing.T) {
	defer shortenStoreWait(t)()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hopeless := &flakyStorage{failures: 99}
	(&exchange{storage: hopeless}).store(ctx, &models.Mail{ID: "m3", Body: []byte("x")})
	if hopeless.attempts != 1 {
		t.Fatalf("it stopped when the server did: %d attempts", hopeless.attempts)
	}
}

// shortenStoreWait makes the backoff too short to notice, and puts it back.
// What is being tested is how many times it tries, not how long it waits.
func shortenStoreWait(t *testing.T) func() {
	t.Helper()
	was := storeFirstWait
	storeFirstWait = time.Millisecond
	return func() { storeFirstWait = was }
}
