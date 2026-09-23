package apigraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/sources"
)

// applySourceType makes a source one of an installed type: its settings
// checked against what the type declares, and the source's kind, format
// and fields set from them. A type that names a reader built into TeaNode
// is read by that reader exactly as a source without a type is -- the
// type is how it was added and what its form shows -- and any other is
// read by running the type on the source's computer.
func (self *graph) applySourceType(ctx context.Context, tx db.Transaction, source *models.AgentKnowledgeSource, name string, settingsJSON json.RawMessage) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = source.Specification.Type
	}
	installed, err := tx.GetAgentSourceType(name)
	if err != nil {
		return err
	}
	if installed == nil {
		return fmt.Errorf("%w: no source type called %q is installed", api.ErrInvalidArguments, name)
	}
	parsed, err := sources.Parse([]byte(installed.Content))
	if err != nil {
		return fmt.Errorf("the installed %s cannot be read: %w", name, err)
	}
	given := settingsJSON
	if len(bytes.TrimSpace(given)) == 0 || string(bytes.TrimSpace(given)) == "null" {
		// Settings not given keep what the source had, for its own type.
		given = nil
		if source.Specification.Type == name {
			given = source.Specification.Settings
		}
	}
	values, err := sources.DecodeSettings(given)
	if err != nil {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	previousType := source.Specification.Type
	if err := parsed.Specify(source, values); err != nil {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	// Another type's secrets are not this one's: what was filled in for
	// the old type is forgotten rather than sent where the new one goes.
	if source.ID != "" && previousType != "" && previousType != parsed.Name {
		if err := tx.DeleteAgentSourceSecret(source.AgentID, source.ID, ""); err != nil {
			return err
		}
	}
	if parsed.Reader == sources.ReaderSent {
		// Theirs, and one that exists, as for a sent source added by its
		// mailbox: the reader opens whatever mailbox it is handed.
		if _, err := self.requireMailbox(ctx, models.PermissionMailRead, source.Specification.MailboxID); err != nil {
			return err
		}
	}
	return nil
}
