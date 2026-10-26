package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	stdlog "log"
	"os"
	"sync"
	"time"

	"github.com/lib/pq"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ziyan/teanode/internal/models"
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

	// vectorIndexing says whether this database can rank vectors itself,
	// settled once by asking for the extension. See database_vector.go.
	vectorOnce     sync.Once
	vectorIndexing bool
}

// newLogger is GORM's own default logger (to standard output, warnings and
// errors, slow statements at 200 milliseconds, in color) with one change: it
// prints a statement with its placeholders and not the values bound to them.
// A statement that fails or runs slowly is logged whole, and a batch insert of
// readings or a route of thousands of points would otherwise put what a person
// keeps, Health above all, into the server log.
func newLogger(writer logger.Writer) logger.Interface {
	return logger.New(writer, logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: false,
		Colorful:                  true,
		ParameterizedQueries:      true,
	})
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
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: newLogger(stdlog.New(os.Stdout, "\r\n", stdlog.LstdFlags))})
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

// ChatEventChannel is what an instance LISTENs on to hear what happened in
// the chats of the people the other instances serve: a message said, edited
// or taken back, a reaction, a read mark, somebody typing. A channel of its
// own, because chats exist on a server that runs no agent, which listens on
// AgentEventChannel only when it has a model.
const ChatEventChannel = "chat_event"

// NotifyChatEvent is outside any transaction, like NotifyAgentEvent: a chat
// event is not a change to keep, only a word to the instances listening now.
// The change itself is in the chat's tables, and a client that missed the word
// reads the conversation again.
func (self *database) NotifyChatEvent(payload string) error {
	return self.db.Exec("SELECT pg_notify(?, ?)", ChatEventChannel, payload).Error
}

// ListenChatEvents delivers every payload any instance notifies, for as long
// as the context lives. A listener that lost its connection reconnects on its
// own, and what was said while it was away is gone: a drawer that missed an
// event reads the chat again when it follows it again.
func (self *database) ListenChatEvents(ctx context.Context) (<-chan string, error) {
	return self.listen(ctx, ChatEventChannel)
}

// LocationEventChannel is what an instance LISTENs on to hear that somebody's
// location was reported on another instance, so that a live map open on this
// one moves. A channel of its own, because reports arrive whether or not the
// server runs an agent.
const LocationEventChannel = "location_event"

// LocationEvent is what travels on LocationEventChannel: whose location
// changed and nothing else. Coordinates never go through NOTIFY; a listener
// reads the latest position from the tables, checking what the viewer may see.
type LocationEvent struct {
	UserID string `json:"userId"`

	// EndedForUserIDs are people who could read this person's location until
	// now and cannot any longer, because a share ended. They are told too,
	// though they may not see the location any more, so that a map they have
	// open reads it again and drops it.
	EndedForUserIDs []string `json:"endedForUserIds,omitempty"`
}

// NotifyLocationEvent is outside any transaction, like NotifyChatEvent: the
// report itself is stored already, and this is only a word to the instances
// listening now. A viewer that missed the word reads the location again.
func (self *database) NotifyLocationEvent(userId string, endedForUserIds ...string) error {
	// A notice carries at most 8000 bytes and an ID is 32 characters, so the
	// people a share stopped reaching go in as many notices as it takes, each
	// naming the owner. Every notice also moves the owner's own maps.
	for start := 0; start == 0 || start < len(endedForUserIds); start += locationEventEndedMost {
		end := min(start+locationEventEndedMost, len(endedForUserIds))
		payload, err := json.Marshal(&LocationEvent{UserID: userId, EndedForUserIDs: endedForUserIds[start:end]})
		if err != nil {
			return err
		}
		if err := self.db.Exec("SELECT pg_notify(?, ?)", LocationEventChannel, string(payload)).Error; err != nil {
			return err
		}
	}
	return nil
}

// locationEventEndedMost is how many people one location event names, so that
// its payload stays far below the 8000 bytes a notice may carry.
const locationEventEndedMost = 100

// ListenLocationEvents delivers the payload (a LocationEvent as JSON) of every
// report any instance announces, for as long as the context lives. A listener
// that lost its connection reconnects on its own, and what was said while it
// was away is gone: a map that missed an event shows the old position until
// the next report or its next read.
func (self *database) ListenLocationEvents(ctx context.Context) (<-chan string, error) {
	return self.listen(ctx, LocationEventChannel)
}

// ShoppingEventChannel is what an instance LISTENs on to hear that a
// shopping list changed on another instance, so that a list open on this one
// ticks as somebody checks it off in the store.
const ShoppingEventChannel = "shopping_event"

// ShoppingEvent is what travels on ShoppingEventChannel: which list changed.
// What changed is read from the tables by whoever may.
type ShoppingEvent struct {
	ShoppingListID string `json:"shoppingListId"`

	// EndedForUserIDs are people who could read the list until now and
	// cannot any longer (a share ended, the list was deleted), told so that
	// a screen they have open reads again and drops it.
	EndedForUserIDs []string `json:"endedForUserIds,omitempty"`
}

// NotifyShoppingEvent is outside any transaction, like NotifyLocationEvent:
// the change is stored already, and this is a word to the instances
// listening now. A screen that missed it reads the list again later.
func (self *database) NotifyShoppingEvent(shoppingListId string, endedForUserIds ...string) error {
	for start := 0; start == 0 || start < len(endedForUserIds); start += locationEventEndedMost {
		end := min(start+locationEventEndedMost, len(endedForUserIds))
		payload, err := json.Marshal(&ShoppingEvent{ShoppingListID: shoppingListId, EndedForUserIDs: endedForUserIds[start:end]})
		if err != nil {
			return err
		}
		if err := self.db.Exec("SELECT pg_notify(?, ?)", ShoppingEventChannel, string(payload)).Error; err != nil {
			return err
		}
	}
	return nil
}

// ListenShoppingEvents delivers the payload (a ShoppingEvent as JSON) of
// every change any instance announces, for as long as the context lives.
func (self *database) ListenShoppingEvents(ctx context.Context) (<-chan string, error) {
	return self.listen(ctx, ShoppingEventChannel)
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

	// ctx bounds SQL work and carries who is acting for audit rows.
	ctx context.Context

	// actor is who the graph writes in this transaction are by, for the
	// history a page keeps. See database_revision.go.
	actor models.RevisionActor

	isNested    bool
	rollbackErr error

	// isPasswordsBusy says a change of Passwords in this transaction gave up
	// waiting for a lock. The rest of the transaction's changes of Passwords
	// are refused at once, because the lock is still held by someone else and
	// each would wait the whole time again. See withPasswordsSavepoint.
	isPasswordsBusy bool
	afterCommit     []func()
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
	tx := self.database.db.WithContext(self.ctx).Begin()
	if err := tx.Error; err != nil {
		return err
	}
	self.tx = tx
	return nil
}

func (self *transaction) commit() error {
	if self.rollbackErr != nil {
		return self.rollbackErr
	}
	if err := self.tx.Commit().Error; err != nil {
		return err
	}
	self.tx = nil
	callbacks := self.afterCommit
	self.afterCommit = nil
	for _, callback := range callbacks {
		callback()
	}
	return nil
}

func (self *transaction) rollback() {
	self.afterCommit = nil
	if self.tx != nil {
		self.tx.Rollback()
	}
}

func (self *transaction) AfterCommit(callback func()) {
	if callback != nil {
		self.afterCommit = append(self.afterCommit, callback)
	}
}

func (self *transaction) Commit() error {
	if self.isNested {
		return fmt.Errorf("cannot commit a nested command transaction")
	}
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
