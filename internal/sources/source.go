// Package sources reads a source type: a YAML file saying how to read one
// kind of knowledge source, either by naming a reader built into TeaNode
// or by listing the commands and web requests that list and read it.
//
// The format is described in the teanode-sources registry's README and in
// docs/subsystems/sources.md; this package is the one place it is parsed,
// checked, and run, and both the server and teanode computer import it, so
// the two can never disagree about what a type means.
package sources

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/skills"
)

// The readers built into TeaNode that a type may name instead of saying
// how to call a tool.
const (
	ReaderFiles   = "files"
	ReaderJournal = "journal"
	ReaderSent    = "sent"
	ReaderWeb     = "web"
)

// Where a type can run.
const (
	RunsComputer = "computer"
	RunsServer   = "server"
)

// Type is one source type.
type Type struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Reader      string   `yaml:"reader"`
	Runs        []string `yaml:"runs"`
	Requires    []string `yaml:"requires"`

	Settings []Setting `yaml:"settings"`

	Secrets                []skills.Secret           `yaml:"secrets"`
	AuthenticationProfiles map[string]skills.Profile `yaml:"authenticationProfiles"`

	// Refresh is the commands run once at the start of every pass, before
	// anything is listed: a tool that keeps its own copy of a service
	// brings it up to date, and the type reads the copy.
	Refresh []Refresh `yaml:"refresh"`

	// Lookups are tables read from files once a pass, which templates
	// reach as lookup.<name>[key]: the people an archive names by
	// identifier, the files a post had with it.
	Lookups map[string]Lookup `yaml:"lookups"`

	Containers []Listing `yaml:"containers"`
	Records    []Reading `yaml:"records"`

	// Pace is the least time between two calls a type makes, for a service
	// that counts calls a minute, such as "100ms".
	Pace string `yaml:"pace"`

	// Prose is what follows the header, for people.
	Prose string `yaml:"-"`
}

// Setting is one thing a person fills in when adding a source of a type.
type Setting struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Type        string       `yaml:"type"`
	Items       *SettingItem `yaml:"items"`
	Pattern     string       `yaml:"pattern"`
	Default     any          `yaml:"default"`
	Minimum     *int         `yaml:"minimum"`
	Maximum     *int         `yaml:"maximum"`
}

// SettingItem is what each value of a setting that is a list has to be.
type SettingItem struct {
	Type    string `yaml:"type"`
	Pattern string `yaml:"pattern"`
}

// Refresh is one command run at the start of a pass, where its condition
// holds or it has none.
type Refresh struct {
	When    string   `yaml:"when"`
	Command []string `yaml:"command"`
}

// Lookup is a table read from a file, or from the names of the files in
// a directory, keyed by a template over each item.
type Lookup struct {
	File  string  `yaml:"file"`
	Files *Files  `yaml:"files"`
	Parse Parsing `yaml:"parse"`
	Key   string  `yaml:"key"`
	// Value is what each key holds; the whole item where it is left out.
	Value string `yaml:"value"`
	// Many keeps every item under a key, as a list, rather than the last.
	Many bool `yaml:"many"`
}

// Files is the files under a directory on the computer whose path,
// relative to it, matches a pattern: * is any part of one name, ** any
// number of directories. Names beginning with a dot are passed over.
type Files struct {
	In    string `yaml:"in"`
	Match string `yaml:"match"`
}

// Listing is one way of listing the containers a source holds.
type Listing struct {
	ID      string            `yaml:"id"`
	When    string            `yaml:"when"`
	Each    string            `yaml:"each"`
	Over    string            `yaml:"over"`
	Walk    *Walk             `yaml:"walk"`
	Fixed   []map[string]any  `yaml:"fixed"`
	Files   *Files            `yaml:"files"`
	Command []string          `yaml:"command"`
	Request *Request          `yaml:"request"`
	Parse   Parsing           `yaml:"parse"`
	Paging  Paging            `yaml:"paging"`
	Skip    string            `yaml:"skip"`
	Name    string            `yaml:"name"`
	Fields  map[string]string `yaml:"fields"`

	// Only is "parents" for a listing whose containers are there to be
	// listed over and are not read themselves.
	Only string `yaml:"only"`
}

// Walk lists a tree: each place is listed, and the items the branch
// condition picks out are places to list in turn.
type Walk struct {
	Start  map[string]string `yaml:"start"`
	Branch string            `yaml:"branch"`
	Child  map[string]string `yaml:"child"`

	// Step, where it is given, is a child's name within its parent: the
	// child's path is its parent's and this, and two children of one
	// parent with the same step are told apart by Distinct, which says
	// something of each that does not change, in brackets after it.
	Step     string `yaml:"step"`
	Distinct string `yaml:"distinct"`
}

// Reading is one way of reading the records of a container.
type Reading struct {
	Each    any      `yaml:"each"`
	Command []string `yaml:"command"`
	Request *Request `yaml:"request"`
	// File is one file on the computer to read, and Files several, each
	// parsed on its own with the file in scope as file.
	File   string  `yaml:"file"`
	Files  *Files  `yaml:"files"`
	Parse  Parsing `yaml:"parse"`
	Paging Paging  `yaml:"paging"`
	Skip   string  `yaml:"skip"`

	// DropWhenMostly leaves a whole container out when more than a share
	// of its items meet a condition: a channel that is an integration
	// talking to itself.
	DropWhenMostly *Mostly `yaml:"dropWhenMostly"`

	// Missing is "empty" where a command's not-found answer means there
	// is nothing to read rather than that something went wrong.
	Missing string `yaml:"missing"`

	Since  *Since `yaml:"since"`
	Unseen string `yaml:"unseen"`

	Record      map[string]string `yaml:"record"`
	Metadata    map[string]string `yaml:"metadata"`
	Detail      *Detail           `yaml:"detail"`
	Attachments Attachments       `yaml:"attachments"`
	MaxBytes    int64             `yaml:"maxBytes"`
}

// Mostly is a condition and the share of items above which it drops a
// container.
type Mostly struct {
	Items string  `yaml:"items"`
	Share float64 `yaml:"share"`
}

// Since reads only what changed since the last complete pass over a
// container.
type Since struct {
	First         string `yaml:"first"`
	Window        string `yaml:"window"`
	UnchangedWhen string `yaml:"unchangedWhen"`
}

// Detail is a command or request run for each item whose version changed,
// supplying its text. A command that is told where to write, with
// {{output}}, is read from that file; any other is read from what it
// prints.
type Detail struct {
	// When, where it is given, is the items it runs for.
	When    string   `yaml:"when"`
	Command []string `yaml:"command"`
	Request *Request `yaml:"request"`
	Parse   Parsing  `yaml:"parse"`
	Text    string   `yaml:"text"`
}

// Attachment is a command that writes one file of an item -- to
// {{output}}, or by printing it where the command is not told where --
// a file the computer already has, or text the reading already fetched.
type Attachment struct {
	When    string   `yaml:"when"`
	Each    string   `yaml:"each"`
	Command []string `yaml:"command"`
	Path    string   `yaml:"path"`
	// Content is a file's bytes where the reading already fetched them.
	Content  string `yaml:"content"`
	Name     string `yaml:"name"`
	Version  string `yaml:"version"`
	MaxBytes int64  `yaml:"maxBytes"`
}

// Attachments is one attachment command or a list of them.
type Attachments []Attachment

func (self *Attachments) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var list []Attachment
		if err := node.Decode(&list); err != nil {
			return err
		}
		*self = list
	case yaml.MappingNode:
		var one Attachment
		if err := node.Decode(&one); err != nil {
			return err
		}
		*self = Attachments{one}
	default:
		return fmt.Errorf("attachments is a command or a list of them")
	}
	return nil
}

// Request is a web request, in the shape a skill's http step has.
type Request struct {
	Method  string            `yaml:"method"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	Auth    string            `yaml:"auth"`
}

// Parsing is what a command or request prints and how its items are found.
type Parsing struct {
	// Kind is json, xml, jsonl, lines, markdown or text.
	Kind string
	// Items is where the list is, for json and xml: dotted keys, ".*"
	// for the values of a map, "a | b" to try each in turn.
	Items string
	// Pattern is the regular expression a line has to match, for lines.
	Pattern string
}

func (self *Parsing) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		self.Kind = node.Value
		return nil
	}
	var shaped map[string]struct {
		Items   string `yaml:"items"`
		Pattern string `yaml:"pattern"`
	}
	if err := node.Decode(&shaped); err != nil {
		return err
	}
	if len(shaped) != 1 {
		return fmt.Errorf("parse is one of json, xml, jsonl, lines, markdown or text")
	}
	for kind, shape := range shaped {
		self.Kind, self.Items, self.Pattern = kind, shape.Items, shape.Pattern
	}
	return nil
}

// Paging is how a command or request is asked for the next page.
type Paging struct {
	// Kind is none, all, token, limit, offset or link: token asks for the
	// next page with what the answer said, offset with how many came
	// before, and link at the address the answer gave, which for a request
	// must be on the same host.
	Kind  string
	Field string
	Flag  string
	Size  int
	// Base is where in the answer the address a relative link is read
	// against is, for link paging.
	Base string
}

func (self *Paging) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		self.Kind = node.Value
		return nil
	}
	var shaped map[string]struct {
		Field string `yaml:"field"`
		Flag  string `yaml:"flag"`
		Size  int    `yaml:"size"`
		Base  string `yaml:"base"`
	}
	if err := node.Decode(&shaped); err != nil {
		return err
	}
	if len(shaped) != 1 {
		return fmt.Errorf("paging is one of none, all, token, limit, offset or link")
	}
	for kind, shape := range shaped {
		self.Kind, self.Field, self.Flag, self.Size, self.Base = kind, shape.Field, shape.Flag, shape.Size, shape.Base
	}
	return nil
}

// namePattern is what a type's name may be.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Parse reads a type from its file: a YAML header between two lines of
// three dashes, and prose after it.
func Parse(content []byte) (*Type, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, fmt.Errorf("sources: the file carries a zero byte")
	}
	header, prose, err := split(content)
	if err != nil {
		return nil, err
	}
	var parsed Type
	if err := yaml.Unmarshal(header, &parsed); err != nil {
		return nil, fmt.Errorf("sources: the header is not readable: %w", err)
	}
	parsed.Prose = prose
	if err := parsed.validate(); err != nil {
		return nil, fmt.Errorf("sources: %s: %w", parsed.Name, err)
	}
	return &parsed, nil
}

// split takes the YAML header out from between the first two lines that
// are exactly three dashes.
func split(content []byte) ([]byte, string, error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	trimmed := strings.TrimLeft(text, "\n ")
	if !strings.HasPrefix(trimmed, "---\n") {
		return nil, "", fmt.Errorf("sources: the file does not begin with a --- header")
	}
	rest := trimmed[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, "", fmt.Errorf("sources: the --- header is never closed")
	}
	return []byte(rest[:end]), strings.TrimSpace(strings.TrimLeft(rest[end+len("\n---"):], "-\n")), nil
}

// RunsOn says whether a type can run where asked. A type that says
// nothing runs on a computer.
func (self *Type) RunsOn(where string) bool {
	if len(self.Runs) == 0 {
		return where == RunsComputer
	}
	for _, each := range self.Runs {
		if each == where {
			return true
		}
	}
	return false
}

// RunsCommands says whether any part of the type runs a command or reads
// a file, which only a computer can do.
func (self *Type) RunsCommands() bool {
	if len(self.Refresh) > 0 || len(self.Lookups) > 0 {
		return true
	}
	for _, listing := range self.Containers {
		if len(listing.Command) > 0 || listing.Files != nil {
			return true
		}
	}
	for _, reading := range self.Records {
		if len(reading.Command) > 0 || reading.File != "" || reading.Files != nil || len(reading.Attachments) > 0 || (reading.Detail != nil && len(reading.Detail.Command) > 0) {
			return true
		}
	}
	return false
}
