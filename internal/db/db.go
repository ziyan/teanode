// Package db provides the database interface.
package db

import (
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

// ErrConfigurationChanged is returned when a change was made against a
// configuration that somebody else has since replaced. The caller reloads and
// tries again; it is not a failure, it is two people editing at once.
var ErrConfigurationChanged = errors.New("db: the configuration changed while this change was being made")

type Database interface {
	// ConfigurationVersion is what the stored configuration is at, for an
	// instance checking whether its copy is stale.
	ConfigurationVersion() (int64, error)

	// LoadConfiguration reads the whole configuration.
	LoadConfiguration() (*ConfigurationRows, error)

	// SaveConfiguration replaces it, refusing when the caller's copy is
	// stale, and returns the new version.
	SaveConfiguration(rows *ConfigurationRows) (int64, error)

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

	// migrate database schema
	Migrate() error
	UnknownMigrations() ([]string, error)

	// close opened database
	Close() error

	// run a function in transaction
	Transaction(func(Transaction) error) error
}

type Transaction interface {
	Commit() error

	DomainUsageOperation
	AliasUsageOperation
	CredentialUsageOperation
	MailOperation
	DeliveryOperation
	ReportOperation
	LayoutOperation
	TemplateOperation
}
