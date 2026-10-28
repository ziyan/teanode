package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/db"
)

// instanceNameWait is how long a start waits for its name to be free. A
// restart's previous process lets go of it when it exits, which a deploy
// that stops one container and starts the next takes a few seconds to do.
var instanceNameWait = 30 * time.Second

// instanceNamePoll is how often the name is tried while waiting.
var instanceNamePoll = time.Second

// holdInstanceName takes the lock on this instance's name for as long as the
// process runs, and refuses to start under a name another running instance
// holds.
//
// The name is what an instance tells its own events, commands and claims
// apart by. Two instances sharing one drop each other's events as their own,
// and the second to start puts the first one's running jobs back in the
// queue, where they run twice. Nothing about either would look wrong.
func holdInstanceName(ctx context.Context, database db.Database, instanceName string) (func(), error) {
	key := db.LockKey("instance:" + instanceName)
	deadline := time.Now().Add(instanceNameWait)
	for {
		release, isLocked, err := database.TryLockSession(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("cannot take the instance name %q: %w", instanceName, err)
		}
		if isLocked {
			return release, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another instance named %q is running against this database. Every instance "+
				"needs a name of its own: set %sINSTANCE_ID to tell them apart", instanceName, bootstrap.Prefix)
		}
		log.Noticef("waiting for the instance named %q to stop before starting under its name", instanceName)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(instanceNamePoll):
		}
	}
}

// isInstanceRunning says whether an instance of a name holds its name now. A
// name that cannot be asked about counts as running, which leaves its files.
func (self *server) isInstanceRunning(instanceName string) bool {
	release, isLocked, err := self.database.TryLockSession(context.Background(), db.LockKey("instance:"+instanceName))
	if err != nil {
		return true
	}
	if isLocked {
		release()
	}
	return !isLocked
}

// migrateOneAtATime migrates holding the migration lock, so that two
// instances starting together do not both apply the same migration. The one
// that waits reads what is applied after the first is done, and finds nothing
// left to do.
func migrateOneAtATime(ctx context.Context, database migratingDatabase, upgradeDirectory string) error {
	release, err := database.LockSession(ctx, db.LockKey("migrate"))
	if err != nil {
		return fmt.Errorf("cannot take the migration lock: %w", err)
	}
	defer release()
	return migrate(database, upgradeDirectory)
}

// migratingDatabase is what migrateOneAtATime needs: the migrations, and the
// lock it takes them under.
type migratingDatabase interface {
	migrator
	LockSession(ctx context.Context, key int64) (func(), error)
}

// acmeChallengeStore keeps the answers to certificate challenges in the
// acme_challenge table, where every instance finds them.
//
// Anybody can ask: a TLS handshake offering the challenge protocol, or a
// request under /.well-known/acme-challenge/, reaches a lookup before any
// sign-in. So a lookup reads what was outstanding a moment ago, read again at
// most once every acmeChallengesFresh, and a flood of made-up tokens costs the
// database one query a second. The authority retries for longer than that.
type acmeChallengeStore struct {
	database db.Database

	mutex       sync.Mutex
	outstanding map[db.AcmeChallengeKey]string
	readAt      time.Time
}

// acmeChallengesFresh is how long the outstanding challenges read from the
// database are answered from.
const acmeChallengesFresh = time.Second

func (self *acmeChallengeStore) Put(ctx context.Context, challengeKind, challengeKey, keyAuthorization string, expiresAt time.Time) error {
	defer self.forget()
	return self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.PutAcmeChallenge(challengeKey, challengeKind, keyAuthorization, expiresAt, time.Now())
	})
}

func (self *acmeChallengeStore) Delete(ctx context.Context, challengeKey string) error {
	defer self.forget()
	return self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.DeleteAcmeChallenge(challengeKey)
	})
}

func (self *acmeChallengeStore) Find(ctx context.Context, challengeKind, challengeKey string) (string, bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	now := time.Now()
	if self.outstanding == nil || now.Sub(self.readAt) >= acmeChallengesFresh {
		var outstanding map[db.AcmeChallengeKey]string
		if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			outstanding, err = tx.ListAcmeChallenges(now)
			return err
		}); err != nil {
			return "", false, err
		}
		self.outstanding, self.readAt = outstanding, now
	}
	keyAuthorization, isOutstanding := self.outstanding[db.AcmeChallengeKey{ChallengeKind: challengeKind, ChallengeKey: challengeKey}]
	return keyAuthorization, isOutstanding, nil
}

// forget drops what was read, so that this instance's own challenge is
// answered as soon as it is put.
func (self *acmeChallengeStore) forget() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.outstanding = nil
}
