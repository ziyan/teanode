package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/lib/pq"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/util/aggregate"
)

type Settings struct {
	Host     string
	Port     uint16
	User     string
	Password string
	DBName   string
	SSLMode  string

	// SSLRootCertificate is the PEM file the server's certificate is verified
	// against. Only meaningful for the verify-ca and verify-full modes.
	SSLRootCertificate string

	// LogQueries echoes every SQL statement to the log. Very noisy; off
	// unless the operator asks for it.
	LogQueries bool

	BackendID string
}

type database struct {
	db       *gorm.DB
	settings *Settings

	// dsn is kept for the LISTEN connection, which is a connection of its
	// own rather than one from the pool: a listening connection is held for
	// as long as the server runs.
	dsn string
	// listenDsnOnce settles listenDsnChosen, the listener's own variant.
	listenDsnOnce   sync.Once
	listenDsnChosen string

	// sealer encrypts the domain table's secrets. Set once the server
	// secret has been read from the settings; nil before, which is the
	// first run's brief window.
	sealerMutex sync.RWMutex
	sealer      *sealer
}

// Open database.
func Open(settings *Settings) (Database, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s", settings.Host, settings.Port, settings.User, settings.Password, settings.DBName)
	if settings.SSLMode != "" {
		dsn += " sslmode=" + settings.SSLMode
	}
	if settings.SSLRootCertificate != "" {
		dsn += " sslrootcert=" + settings.SSLRootCertificate
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if settings.LogQueries {
		db = db.Debug()
	}
	return &database{
		db:       db,
		settings: settings,
		dsn:      dsn,
	}, nil
}

// ListenFolderChanges delivers the id of every folder any instance changes,
// for as long as the context lives. The channel closes when it ends. A
// listener that lost its connection reconnects on its own; what it missed
// while away is caught by the next poll, which is why every idler also
// polls on a clock.
func (self *database) ListenFolderChanges(ctx context.Context) (<-chan string, error) {
	return self.listen(ctx, FolderChangedChannel)
}

// AgentEventChannel is what an instance LISTENs on to hear the events of
// the turns running on the other instances.
const AgentEventChannel = "agent_event"

// AgentCommandChannel is what an instance LISTENs on for a word to a turn
// it runs, given on another instance.
const AgentCommandChannel = "agent_command"

// NotifyAgentEvent is outside any transaction: an event is not a change
// to keep, only a word to the instances listening now.
func (self *database) NotifyAgentEvent(payload string) error {
	return self.db.Exec("SELECT pg_notify(?, ?)", AgentEventChannel, payload).Error
}

func (self *database) NotifyAgentCommand(payload string) error {
	return self.db.Exec("SELECT pg_notify(?, ?)", AgentCommandChannel, payload).Error
}

func (self *database) ListenAgentCommands(ctx context.Context) (<-chan string, error) {
	return self.listen(ctx, AgentCommandChannel)
}

// ListenAgentEvents delivers every payload any instance notifies, for as
// long as the context lives. A listener that lost its connection reconnects
// on its own, and what was said while it was away is gone: a drawer that
// missed an event reads the transcript again when the turn is over.
func (self *database) ListenAgentEvents(ctx context.Context) (<-chan string, error) {
	return self.listen(ctx, AgentEventChannel)
}

// listenConnectWait is how long listen waits for the LISTEN to be in place
// before handing back the channel regardless: the listener keeps trying on
// its own, and whoever is waiting has a poll to fall back on.
const listenConnectWait = 10 * time.Second

// listen is a LISTEN on one channel, on a connection of its own, delivering
// each notification's payload until the context ends.
func (self *database) listen(ctx context.Context, channel string) (<-chan string, error) {
	listener := pq.NewListener(self.listenDsn(), time.Second, time.Minute, func(event pq.ListenerEventType, err error) {
		if err != nil {
			log.Warningf("the LISTEN connection for %q: %s", channel, err)
		}
	})
	listening := make(chan error, 1)
	go func() { listening <- listener.Listen(channel) }()
	select {
	case err := <-listening:
		if err != nil {
			_ = listener.Close()
			return nil, err
		}
	case <-time.After(listenConnectWait):
		log.Warningf("still waiting to LISTEN on %q after %s; carrying on without it for now", channel, listenConnectWait)
	case <-ctx.Done():
		_ = listener.Close()
		return nil, ctx.Err()
	}
	payloads := make(chan string, 256)
	go func() {
		defer close(payloads)
		defer func() { _ = listener.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case notification, ok := <-listener.Notify:
				if !ok {
					return
				}
				if notification == nil {
					// A reconnection; whatever was missed, the polls will find.
					continue
				}
				select {
				case payloads <- notification.Extra:
				default:
					// A slow consumer loses a wake-up, not a change: the
					// change is in the database and the next poll finds it.
				}
			}
		}
	}()
	return payloads, nil
}

// listenDsn is the connection string for the listening connection. The
// pool's driver tries TLS and falls back when the server has none; the
// listener's driver has no such fallback and reads an unset mode as
// "require", which never connects to a server without TLS. So when no
// mode is set, the listener finds out once which it is.
func (self *database) listenDsn() string {
	if self.settings.SSLMode != "" {
		return self.dsn
	}
	self.listenDsnOnce.Do(func() {
		self.listenDsnChosen = self.dsn + " sslmode=disable"
		probe, err := sql.Open("postgres", self.dsn+" sslmode=require")
		if err != nil {
			return
		}
		defer func() { _ = probe.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := probe.PingContext(ctx); err == nil {
			self.listenDsnChosen = self.dsn + " sslmode=require"
		}
	})
	return self.listenDsnChosen
}

func (self *database) Close() error {
	sqlDb, err := self.db.DB()
	if err != nil {
		return err
	}
	return sqlDb.Close()
}

type transaction struct {
	tx       *gorm.DB
	database *database

	// ctx carries who is acting, for the audit rows the writes produce.
	ctx context.Context
}

func (self *database) Transaction(f func(Transaction) error) error {
	return self.TransactionContext(context.Background(), f)
}

func (self *database) TransactionContext(ctx context.Context, f func(Transaction) error) error {
	tx := &transaction{
		database: self,
		ctx:      ctx,
	}
	defer tx.rollback()

	if err := tx.begin(); err != nil {
		return err
	}
	if err := f(tx); err != nil {
		return err
	}
	if err := tx.commit(); err != nil {
		return err
	}
	return nil
}

func (self *transaction) begin() error {
	tx := self.database.db.Begin()
	if err := tx.Error; err != nil {
		return err
	}
	self.tx = tx
	return nil
}

func (self *transaction) commit() error {
	if err := self.tx.Commit().Error; err != nil {
		return err
	}
	self.tx = nil
	return nil
}

func (self *transaction) rollback() {
	if self.tx != nil {
		self.tx.Rollback()
	}
}

func (self *transaction) Commit() error {
	if err := self.commit(); err != nil {
		return err
	}
	return self.begin()
}

func (self *transaction) query(model interface{}, options *Options) *gorm.DB {
	query := self.tx.Model(model)

	// The pipeline's own sort wins when it has one; identifiers sort by time,
	// so the default puts the newest first.
	ordered := false
	if options != nil {
		var err error
		query, ordered, err = applyAggregations(query, options)
		if err != nil {
			// Refuse the query rather than quietly returning unfiltered rows,
			// which would look like a filter that matched everything.
			_ = query.AddError(err)
			return query
		}
	}
	if !ordered {
		query = query.Order("\"id\" DESC")
	}

	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit))
	}
	if options != nil && options.Offset > 0 {
		query = query.Offset(int(options.Offset))
	}
	if options != nil && options.Cursor != "" {
		query = query.Where("\"id\" < ?", options.Cursor)
	}
	return query
}

// applyAggregations folds the pipeline into the query, in the order the
// caller wrote its stages, and reports whether it set an order.
func applyAggregations(query *gorm.DB, options *Options) (*gorm.DB, bool, error) {
	ordered := false
	for _, stage := range options.Aggregations {
		if stage == nil {
			continue
		}
		if err := stage.Validate(); err != nil {
			return query, ordered, err
		}

		switch {
		case stage.Match != nil:
			condition, err := aggregate.BuildFilter(stage.Match, options.Columns)
			if err != nil {
				return query, ordered, err
			}
			query = query.Where(condition.SQL, condition.Values...)

		case len(stage.Sort) > 0:
			clause, err := aggregate.BuildSort(stage.Sort, options.Columns)
			if err != nil {
				return query, ordered, err
			}
			query = query.Order(clause)
			ordered = true

		case len(stage.Distinct) > 0:
			// Distinct is answered by CountDistinct rather than here: it
			// changes what a row is, and the callers of this build a list of
			// entities.
			return query, ordered, fmt.Errorf("db: a distinct stage belongs in a facet query, not a list")
		}
	}
	return query, ordered, nil
}
