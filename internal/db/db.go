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

	DomainOperation
	AliasOperation
	CredentialOperation
	UserOperation
	RoleOperation
	GroupOperation
	AuditOperation

	DomainUsageOperation
	AliasUsageOperation
	CredentialUsageOperation
	MailOperation
	DeliveryOperation
	ReportOperation
	LayoutOperation
	TemplateOperation
}
