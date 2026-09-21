// Package db provides the database interface.
package db

import (
	"context"
	"errors"
	"github.com/ziyan/teanode/internal/util/aggregate"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("db")

var (
	ErrInvalidOptions   = errors.New("db: invalid options")
	ErrAlreadyExists    = errors.New("db: already exists")
	ErrInvalidEmail     = errors.New("db: invalid email")
	ErrInvalidArguments = errors.New("db: invalid arguments")
	ErrNotFound         = errors.New("db: not found")

	// ErrTooMuchAsked is a question whose answer is larger than this server
	// will build: a stretch of time holding more times something happens
	// than it will describe at once. The asker is told, because the
	// alternative -- the first hundred thousand, silently -- answers "when
	// is this person busy" with a window that stops in the middle.
	ErrTooMuchAsked = errors.New("db: more was asked for than this server answers at once")
)

type Options struct {
	// if supplied, limit the number of rows to return
	Limit uint64

	// if supplied, offset the returned rows
	Offset uint64

	// if supplied, offset the returned rows
	Cursor string

	// Aggregations is the filter, sort and distinct pipeline the caller
	// asked for. Running it here rather than in the browser is the point:
	// the browser only has the rows it fetched, and "which domains have
	// mail" is a question about all of them.
	Aggregations aggregate.Pipeline

	// Columns says which fields the pipeline may name, for the table being
	// queried. A query with a pipeline and no columns refuses everything,
	// which is the safe direction to be wrong in.
	Columns aggregate.Columns
}

// Facet is one value of a column and how many rows carry it: what fills a
// filter menu, and the number beside each option.
type Facet struct {
	Value string
	Count int
}

type Database interface {
	// The settings, read on start and on every change made elsewhere.
	ConfigurationOperation

	// SetSecret hands over the server secret the domain table's secrets are
	// sealed with, once the settings that hold it have been read.
	SetSecret(secret []byte) error

	// Users are looked up on every authenticated request, outside any
	// transaction.
	UserLookup

	// MailExists says whether a stored message still has a row, for the
	// spool sweep deciding what it may remove.
	MailExists(mailId string) (bool, error)

	// ListenFolderChanges delivers the id of each folder any instance
	// changes, until the context ends.
	ListenFolderChanges(ctx context.Context) (<-chan string, error)

	// NotifyAgentEvent tells every instance about an event of a turn, and
	// ListenAgentEvents delivers what the others said, until the context
	// ends. The payload is the instance's to shape; what a slow listener
	// misses is not kept.
	NotifyAgentEvent(payload string) error
	ListenAgentEvents(ctx context.Context) (<-chan string, error)

	// NotifyAgentCommand and ListenAgentCommands are the same for a word
	// to a turn — stop, an answer, a decision on a card — from an instance
	// that is not running it.
	NotifyAgentCommand(payload string) error
	ListenAgentCommands(ctx context.Context) (<-chan string, error)

	// Sessions, API tokens and passkeys are read and written outside a
	// transaction: every authenticated request looks one up, and wrapping
	// that in a transaction would buy nothing.
	SessionOperation
	TokenOperation
	PasskeyOperation

	// Media is read on every request for a picture in a sent message, which
	// arrives from a mail program with no session and no transaction to join.
	MediaOperation
	MediaLinkOperation

	// The built-in spam filter reads its learned counts while scoring a
	// message, outside any transaction the delivery is in: an advisory score
	// must not be able to hold a delivery's transaction open.
	SpamOperation

	// VectorIndexing says whether the database ranks vectors itself, and
	// EnsureVectorIndex builds the index one table needs for one model.
	// Both are outside any transaction: the first is asked once at start,
	// the second is DDL. See database_vector.go.
	VectorIndexing() bool
	EnsureVectorIndex(table VectorTable, model string, dimension int) error

	// migrate database schema
	Migrate() error
	UnknownMigrations() ([]string, error)

	// close opened database
	Close() error

	// Transaction runs a function in a transaction, as the server itself.
	Transaction(func(Transaction) error) error

	// TransactionContext runs a function in a transaction on behalf of
	// whoever the context says is acting, so that every audited write in it
	// names them. See ContextWithAuditPrincipal.
	TransactionContext(ctx context.Context, function func(Transaction) error) error
}

type Transaction interface {
	Commit() error

	// AfterCommit queues a non-durable in-memory update after successful SQL
	// commit. Rollback discards it, including rollback of a nested command.
	// Callbacks must be short in-memory operations and must not panic or use
	// the completed transaction. Durable work belongs in database queue records.
	AfterCommit(callback func())

	// TransactionContext runs one command under a savepoint on this connection.
	// A failure rolls back only that command; success remains subject to the
	// outer transaction's commit. The nested transaction cannot commit early.
	TransactionContext(ctx context.Context, function func(Transaction) error) error

	// TryAdvisoryLock takes an advisory lock for the rest of the
	// transaction, or says another transaction holds it.
	TryAdvisoryLock(key int64) (bool, error)

	DomainOperation
	AliasOperation
	CredentialOperation
	UserOperation
	RoleOperation
	GroupOperation
	AuditOperation
	MailboxOperation
	SubscriptionQuery
	BimiQuery
	IdentityOperation
	AgentOperation
	InsightOperation
	AttachmentOperation
	ReplyOperation
	EmbeddingOperation
	VectorOperation
	GraphOperation
	KnowledgeOperation
	DreamOperation
	RevisionOperation
	MemoryOperation
	ConnectionOperation
	ChannelOperation

	DomainUsageOperation
	AliasUsageOperation
	CredentialUsageOperation
	MailOperation
	SubmissionOperation
	MediaCompositionOperation
	DeliveryOperation
	ReportOperation
	LayoutOperation
	TemplateOperation
}
