package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/upgrade"
	"github.com/ziyan/teanode/internal/version"
)

// AllowMigrationRevert is the variable that says an older binary may undo what
// a newer one did to the database.
const AllowMigrationRevert = bootstrap.Prefix + "ALLOW_MIGRATION_REVERT"

// migrate brings the database up to date, and refuses to bring it backwards
// unless somebody has said to.
//
// Migrate reverts every migration it does not recognise. That is how a
// deliberate downgrade works here — see docs/coding/database-migrations.md —
// and the trouble is that it cannot tell a deliberate downgrade from an
// accidental one, while the two are told apart by what happens next: one loses
// nothing anybody wanted, and the other drops columns and everything in them.
//
// The accidental one has three roads into this program and they are all
// ordinary. A release installed from the dashboard migrates the database and
// then crashes before serving, so the next start refuses it by design and the
// image's older binary carries on. A second instance sharing the database
// never got the upgrade — its own was refused — and restarts for some
// unrelated reason. An operator pulls last week's image to test something.
//
// So it is opt-in now. Unknown migrations stop the program, and the message
// says which, what would be lost, and the variable to set. That turns a silent
// loss into a start that does not happen, which is the trade a mail server
// should make: the queue is on disk and senders retry, and a dropped column
// does not come back.
func migrate(database migrator, upgradeDirectory string) error {
	unknown, err := database.UnknownMigrations()
	if err != nil {
		return fmt.Errorf("cannot read which migrations this database has: %w", err)
	}
	if len(unknown) == 0 {
		if err := database.Migrate(); err != nil {
			return fmt.Errorf("cannot migrate the database: %w", err)
		}
		return nil
	}

	allowed, err := revertAllowed()
	if err != nil {
		return err
	}
	if allowed {
		log.Warningf("reverting %d migration(s) this version does not have, because %s is set: %s",
			len(unknown), AllowMigrationRevert, strings.Join(unknown, ", "))
		if err := database.Migrate(); err != nil {
			return fmt.Errorf("cannot migrate the database: %w", err)
		}
		return nil
	}

	return fmt.Errorf("this database was migrated by a newer version of teanode (%s), and this one does "+
		"not have those migrations. Nothing has been changed and nothing has been opened.\n\n"+
		"The way out that loses nothing is to run that newer version here — upgrade this instance, or "+
		"pull the image the rest of the deployment is on.%s\n\n"+
		"To go back to this version instead, set %s=true. Read that as what it is: those migrations are "+
		"reverted and whatever is in the columns they added is gone. If another instance is sharing this "+
		"database and is already running the newer version, do not do it at all — the columns would go "+
		"out from under it while it is serving",
		strings.Join(unknown, ", "), stagedAdvice(upgradeDirectory, version.Version()), AllowMigrationRevert)
}

// revertAllowed reads the variable that permits going backwards.
//
// A value it cannot read is an error rather than a false. The refusal below is
// the only guidance anybody has at that point, and it ends by naming this
// variable — so somebody who sets it to "yes" and is handed the same three
// paragraphs back has been told to do the thing they just did.
func revertAllowed() (bool, error) {
	value, ok := os.LookupEnv(AllowMigrationRevert)
	if !ok || strings.TrimSpace(value) == "" {
		return false, nil
	}
	allowed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s is %q, which is not true or false", AllowMigrationRevert, value)
	}
	return allowed, nil
}

// stagedAdvice adds the way out that does not lose anything, when this start
// is one remedy away from it: a newer binary is sitting in the staging
// directory and was held back only by the marker saying an earlier attempt did
// not get as far as serving.
//
// Only for that one reason. A staged binary is left in place when its version
// or checksum cannot be read, when the checksum does not match, when it is not
// executable, and when the permissions are wrong — and in every one of those,
// removing the marker changes nothing and the server still will not start.
// Telling somebody that is the way out, when it is not, is worse than saying
// nothing: they do it, it fails again, and now they distrust the message that
// was going to tell them the truth about reverting.
func stagedAdvice(upgradeDirectory, current string) string {
	if !upgrade.HeldBackByMarker(upgradeDirectory, current) {
		return ""
	}
	return fmt.Sprintf(" There is one at %s: it was tried and did not get as far as serving, so this "+
		"start left it alone. Remove %s to let it try again.",
		upgrade.Staged(upgradeDirectory), upgrade.PendingMarker(upgradeDirectory))
}

// migrator is the two things migrate needs of a database. Narrow so that the
// refusal above can be exercised without one.
type migrator interface {
	UnknownMigrations() ([]string, error)
	Migrate() error
}
