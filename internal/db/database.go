package db

import (
	"context"
	"fmt"
	"sync"

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

	// sealer encrypts the domain table's secrets. Set once the server
	// secret has been read from the settings; nil before, which is the
	// first run's brief window.
	sealerMutex sync.RWMutex
	sealer      *sealer
}

// Open database.
func Open(settings *Settings) (Database, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", settings.Host, settings.Port, settings.User, settings.Password, settings.DBName, settings.SSLMode)
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
	}, nil
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
