package sources

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Specify makes a source one of this type: its settings checked, and its
// kind, format and fields set from them. A type that names a reader built
// into TeaNode is read by that reader exactly as a source without a type
// is -- the type is how it was added and what its form shows -- and any
// other is read by running the type on the source's computer.
//
// A sent-mail source is given the mailbox its settings name; whether the
// person may read that mailbox is the caller's to check, since only the
// caller knows who is asking.
func (self *Type) Specify(source *models.AgentKnowledgeSource, values map[string]any) error {
	checked, err := self.CheckSettings(values)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(checked)
	if err != nil {
		return err
	}
	specification := &source.Specification
	specification.Type, specification.Settings = self.Name, encoded
	// What the reader is given is every setting, a default where the
	// source said nothing; what is stored is only what it said, so a
	// default the type later changes is the new one.
	effective := map[string]any{}
	for _, setting := range self.Settings {
		if value, ok := checked[setting.Name]; ok {
			effective[setting.Name] = value
		} else if setting.Default != nil {
			effective[setting.Name] = setting.Default
		}
	}
	text := func(name string) string {
		value, _ := effective[name].(string)
		return value
	}
	list := func(name string) []string {
		var found []string
		for _, each := range asList(effective[name]) {
			if value, ok := each.(string); ok {
				found = append(found, value)
			}
		}
		return found
	}
	number := func(name string) int {
		value, _ := effective[name].(int)
		return value
	}
	flag := func(name string) bool {
		value, _ := effective[name].(bool)
		return value
	}
	switch self.Reader {
	case ReaderFiles, ReaderJournal:
		if source.Kind != models.SourceArchive {
			source.Kind = models.SourceComputer
		}
		specification.Path = text("path")
		specification.Format = models.FormatFiles
		if self.Reader == ReaderJournal {
			specification.Format = models.FormatJournal
		}
		specification.Include, specification.Exclude = list("include"), list("exclude")
		specification.ReadEveryCheckout = flag("readEveryCheckout")
		specification.OwnCommitsAtLeast = number("ownCommitsAtLeast")
		specification.CommitsPerPass = number("commitsPerPass")
	case ReaderSent:
		source.Kind, specification.MailboxID = models.SourceSent, text("mailbox")
	case ReaderWeb:
		source.Kind = models.SourceWeb
		specification.Start, specification.Allow, specification.Depth = text("start"), list("allow"), number("depth")
	case "":
		if !self.RunsOn(RunsComputer) {
			return fmt.Errorf("%s runs only on the server, which does not read source types yet", self.Name)
		}
		source.Kind = models.SourceComputer
		specification.Format, specification.Path = models.FormatTyped, ""
		if strings.TrimSpace(specification.Computer) == "" {
			return fmt.Errorf("%s runs on a computer; say which", self.Name)
		}
	default:
		return fmt.Errorf("%s names the reader %q, which this server does not have", self.Name, self.Reader)
	}
	return nil
}

// DecodeSettings reads a source's settings, numbers kept as they were
// written.
func DecodeSettings(raw json.RawMessage) (map[string]any, error) {
	values := map[string]any{}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return values, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("settings are a JSON object: %w", err)
	}
	return values, nil
}
