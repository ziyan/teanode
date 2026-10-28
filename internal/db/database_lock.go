package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"hash/fnv"
	"time"
)

// LockKey is the advisory lock key for a name: what makes one piece of work
// one instance's at a time, whichever instance asks. A name rather than a
// number chosen by hand, so that two callers cannot pick the same number
// without seeing that they picked the same name.
func LockKey(lockName string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte("teanode:" + lockName))
	return int64(hash.Sum64())
}

// sessionLockRelease is how long releasing a session lock may take before the
// connection is closed anyway, which releases it too.
const sessionLockRelease = 5 * time.Second

// LockSession waits for a session-level advisory lock and holds it until
// release is called, on a connection of its own: the work done under it may
// use any connection and any number of transactions. A connection that drops
// releases the lock with it.
func (self *database) LockSession(ctx context.Context, key int64) (func(), error) {
	connection, err := self.lockConnection(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := connection.ExecContext(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return releaseSessionLock(connection, key), nil
}

// TryLockSession is LockSession that does not wait: isLocked is false, and
// release nil, when another session holds the lock.
func (self *database) TryLockSession(ctx context.Context, key int64) (func(), bool, error) {
	connection, err := self.lockConnection(ctx)
	if err != nil {
		return nil, false, err
	}
	var isLocked bool
	if err := connection.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&isLocked); err != nil {
		_ = connection.Close()
		return nil, false, err
	}
	if !isLocked {
		_ = connection.Close()
		return nil, false, nil
	}
	return releaseSessionLock(connection, key), true, nil
}

// WhileLocked does work holding the session lock, and says whether it did:
// false, with nothing done, when another instance is doing it. For a sweep
// too long to hold a transaction open across.
func (self *database) WhileLocked(ctx context.Context, key int64, work func(context.Context) error) (bool, error) {
	release, isLocked, err := self.TryLockSession(ctx, key)
	if err != nil || !isLocked {
		return false, err
	}
	defer release()
	return true, work(ctx)
}

func (self *database) lockConnection(ctx context.Context) (*sql.Conn, error) {
	pool, err := self.db.DB()
	if err != nil {
		return nil, fmt.Errorf("db: no connection pool for a lock: %w", err)
	}
	return pool.Conn(ctx)
}

func releaseSessionLock(connection *sql.Conn, key int64) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), sessionLockRelease)
		defer cancel()
		if _, err := connection.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", key); err != nil {
			// Back in the pool still holding the lock, it would hold it for as
			// long as the connection lives. Thrown away, it lets go.
			_ = connection.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = connection.Close()
	}
}

// DescribeSessionLockHolder says who holds a session lock, as the database
// sees the session: its client address and when it connected. Empty when
// nobody holds it, or the database will not say.
//
// A host that lost power can leave its session open in the database for a
// long while, and the lock with it, until the database notices the connection
// is gone; this is how a start refused for it can say so.
func (self *database) DescribeSessionLockHolder(ctx context.Context, key int64) (string, error) {
	var holders []struct {
		ClientAddress string
		BackendStart  time.Time
	}
	// An advisory lock on one 64-bit key is kept as its two halves.
	err := self.db.WithContext(ctx).Raw(`SELECT coalesce(host(activity.client_addr), 'a local socket') AS client_address, activity.backend_start AS backend_start
		FROM pg_locks lock JOIN pg_stat_activity activity ON activity.pid = lock.pid
		WHERE lock.locktype = 'advisory' AND lock.granted AND lock.objsubid = 1
			AND lock.classid = ((?::bigint >> 32) & 4294967295)::oid AND lock.objid = (?::bigint & 4294967295)::oid`, key, key).Scan(&holders).Error
	if err != nil || len(holders) == 0 {
		return "", err
	}
	return fmt.Sprintf("a session from %s, connected %s", holders[0].ClientAddress, holders[0].BackendStart.Format(time.RFC3339)), nil
}
