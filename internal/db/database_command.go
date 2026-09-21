package db

import (
	"context"
	"errors"
	"time"
)

func (self *transaction) TransactionContext(ctx context.Context, function func(Transaction) error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if self.rollbackErr != nil {
		return self.rollbackErr
	}
	// The identifier is generated locally, never interpolated from a request.
	savepointName := "command_" + newID()
	if err := self.tx.WithContext(ctx).Exec("SAVEPOINT " + savepointName).Error; err != nil {
		return err
	}
	hasSucceeded := false
	defer func() {
		// Even if a command's shorter deadline expired, undo its writes before
		// allowing another command on the parent connection. Cleanup is bounded.
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), 5*time.Second)
		defer cancel()
		cleanup := self.tx.WithContext(cleanupContext)
		if !hasSucceeded {
			if rollbackErr := cleanup.Exec("ROLLBACK TO SAVEPOINT " + savepointName).Error; rollbackErr != nil {
				self.rollbackErr = rollbackErr
				err = errors.Join(err, rollbackErr)
				return
			}
		}
		if releaseErr := cleanup.Exec("RELEASE SAVEPOINT " + savepointName).Error; releaseErr != nil {
			self.rollbackErr = releaseErr
			err = errors.Join(err, releaseErr)
		}
	}()
	nested := &transaction{
		tx: self.tx.WithContext(ctx), database: self.database, ctx: ctx,
		actor: self.actor, isNested: true,
	}
	if err := function(nested); err != nil {
		return err
	}
	if nested.rollbackErr != nil {
		return nested.rollbackErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	hasSucceeded = true
	return nil
}
