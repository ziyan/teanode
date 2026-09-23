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
	values := map[string]any{}
	given := settingsJSON
	if len(bytes.TrimSpace(given)) == 0 || string(bytes.TrimSpace(given)) == "null" {
		// Settings not given keep what the source had, for its own type.
		given = nil
		if source.Specification.Type == name {
			given = source.Specification.Settings
		}
	}
	if len(given) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(given))
		decoder.UseNumber()
		if err := decoder.Decode(&values); err != nil {
			return fmt.Errorf("%w: settings are a JSON object: %s", api.ErrInvalidArguments, err)
		}
	}
	checked, err := parsed.CheckSettings(values)
	if err != nil {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	encoded, err := json.Marshal(checked)
	if err != nil {
		return err
	}
	specification := &source.Specification
	specification.Type, specification.Settings = parsed.Name, encoded
	text := func(name string) string {
		value, _ := checked[name].(string)
		return value
	}
	list := func(name string) []string {
		var found []string
		for _, each := range asAnyList(checked[name]) {
			if value, ok := each.(string); ok {
				found = append(found, value)
			}
		}
		return found
	}
	number := func(name string) int {
		value, _ := checked[name].(int)
		return value
	}
	flag := func(name string) bool {
		value, _ := checked[name].(bool)
		return value
	}
	switch parsed.Reader {
	case sources.ReaderFiles, sources.ReaderJournal:
		if source.Kind != models.SourceArchive {
			source.Kind = models.SourceComputer
		}
		specification.Path = text("path")
		specification.Format = models.FormatFiles
		if parsed.Reader == sources.ReaderJournal {
			specification.Format = models.FormatJournal
		}
		specification.Include, specification.Exclude = list("include"), list("exclude")
		specification.ReadEveryCheckout = flag("readEveryCheckout")
		specification.OwnCommitsAtLeast = number("ownCommitsAtLeast")
		specification.CommitsPerPass = number("commitsPerPass")
	case sources.ReaderSent:
		// Theirs, and one that exists, as for a sent source added by its
		// mailbox: the reader opens whatever mailbox it is handed.
		mailbox := text("mailbox")
		if _, err := self.requireMailbox(ctx, models.PermissionMailRead, mailbox); err != nil {
			return err
		}
		source.Kind, specification.MailboxID = models.SourceSent, mailbox
	case sources.ReaderWeb:
		source.Kind = models.SourceWeb
		specification.Start, specification.Allow, specification.Depth = text("start"), list("allow"), number("depth")
	case "":
		if !parsed.RunsOn(sources.RunsComputer) {
			return fmt.Errorf("%w: %s runs only on the server, which does not read source types yet", api.ErrInvalidArguments, parsed.Name)
		}
		source.Kind = models.SourceComputer
		specification.Format, specification.Path = models.FormatTyped, ""
		if strings.TrimSpace(specification.Computer) == "" {
			return fmt.Errorf("%w: %s runs on a computer; say which", api.ErrInvalidArguments, parsed.Name)
		}
	default:
		return fmt.Errorf("%w: %s names the reader %q, which this server does not have", api.ErrInvalidArguments, parsed.Name, parsed.Reader)
	}
	return nil
}

func asAnyList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		list := make([]any, len(typed))
		for index, each := range typed {
			list[index] = each
		}
		return list
	}
	return nil
}
