package models

import (
	"strings"
	"time"
)

// Knowledge: the places a person has pointed their agent at, and what it
// found there.
//
// A source is a standing grant of reach, which is why adding one is the
// person's decision even when the agent asks for it: the card names the
// computer and the path, and nothing is read until they say yes.

// AgentKnowledgeKind is what sort of place a source is.
type AgentKnowledgeKind string

// The kinds.
const (
	// SourceComputer is a directory on a machine the person runs
	// `teanode computer` on. Inside a repository the manifest is what git
	// tracks, which is how a checkout is read without its build output.
	SourceComputer AgentKnowledgeKind = "computer"

	// SourceArchive is an export of something, on the same machine, in a
	// shape the scan understands: a chat export, a folder of dated
	// notes.
	SourceArchive AgentKnowledgeKind = "archive"

	// SourceSkill pages an installed skill's tool -- a wiki, a forge, a
	// chat service -- with the person's own credentials.
	SourceSkill AgentKnowledgeKind = "skill"

	// SourceWeb crawls within an address prefix.
	SourceWeb AgentKnowledgeKind = "web"

	// SourceSent is the person's own sent mail, which is what makes the
	// agent able to write as they do.
	SourceSent AgentKnowledgeKind = "sent"
)

// AgentKnowledgeKinds is every kind.
var AgentKnowledgeKinds = []AgentKnowledgeKind{SourceComputer, SourceArchive, SourceSkill, SourceWeb, SourceSent}

// IsAgentKnowledgeKind says whether a word names a kind.
func IsAgentKnowledgeKind(kind AgentKnowledgeKind) bool {
	for _, known := range AgentKnowledgeKinds {
		if known == kind {
			return true
		}
	}
	return false
}

// The formats an archive or a computer source can be read in.
const (
	// FormatFiles is the default: a tree of files, git-aware.
	FormatFiles = "files"

	// FormatJournal is a folder of dated notes: a file per day or per
	// month, or one file with date headings.
	FormatJournal = "journal"

	// FormatRecords is a folder of JSON lines, one record a line, in the
	// one shape any script can write: what it is, when it happened, who
	// wrote it, what it says. It is how everything that is not one of the
	// shapes above gets in without a reader being written for it.
	FormatRecords = "records"
)

// AgentKnowledgeFormats is every format a source may be read in.
var AgentKnowledgeFormats = []string{FormatFiles, FormatJournal, FormatRecords}

// IsAgentKnowledgeFormat says whether a word names a format.
func IsAgentKnowledgeFormat(format string) bool {
	for _, known := range AgentKnowledgeFormats {
		if known == format {
			return true
		}
	}
	return false
}

// AgentKnowledgeSpecification is what to read. Which fields mean anything
// depends on the kind; the rest are empty.
type AgentKnowledgeSpecification struct {
	// Computer and Path: the device the person attached and where on it.
	Computer string `json:"computer,omitempty"`
	Path     string `json:"path,omitempty"`

	// Format is how to read what is there: files, journal, records.
	Format string `json:"format,omitempty"`

	// Include and Exclude are globs, for a files source.
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`

	// Tool is the skill tool to call, Arguments what to call it with, and
	// the rest say how to read what comes back.
	Tool        string         `json:"tool,omitempty"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	ItemPath    string         `json:"itemPath,omitempty"`
	IDField     string         `json:"idField,omitempty"`
	TextField   string         `json:"textField,omitempty"`
	TitleField  string         `json:"titleField,omitempty"`
	URLField    string         `json:"urlField,omitempty"`
	DateField   string         `json:"dateField,omitempty"`
	CursorField string         `json:"cursorField,omitempty"`

	// Start, Allow and Depth are a web crawl.
	Start string   `json:"start,omitempty"`
	Allow []string `json:"allow,omitempty"`
	Depth int      `json:"depth,omitempty"`

	// MailboxID is which mailbox a sent source reads.
	MailboxID string `json:"mailboxId,omitempty"`

	// MaxAttachmentBytes is the largest file this source carries off the
	// person's machine, in bytes. Zero means the server's own limit,
	// agent.limits.maxScannedAttachmentBytes, which is what nearly every
	// source wants; it is here for the one archive whose pictures are
	// bigger than everybody else's.
	MaxAttachmentBytes int64 `json:"maxAttachmentBytes,omitempty"`
}

// AgentKnowledgeSource is one standing grant of reach.
type AgentKnowledgeSource struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agentId"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	Kind          AgentKnowledgeKind          `json:"kind"`
	Name          string                      `json:"name"`
	Specification AgentKnowledgeSpecification `json:"specification"`

	// RootPath is where in the graph what it finds is filed.
	RootPath string `json:"rootPath"`

	Enabled bool   `json:"enabled"`
	Cron    string `json:"cron,omitempty"`

	// Cursor is where the last pass stopped. Opaque above the reader that
	// wrote it.
	Cursor map[string]any `json:"-"`

	// Instance is which server holds the socket to the device.
	Instance string `json:"-"`

	LastRunAt *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
	LastError string     `json:"lastError,omitempty"`

	DocumentCount int `json:"documentCount"`
	ChunkCount    int `json:"chunkCount"`
	RefusedCount  int `json:"refusedCount"`

	// More says the last pass left work behind and wants another.
	More bool `json:"more"`

	// UnknownAuthors are the commit addresses this source found that are
	// not on the card the person marked as themselves. Whether a checkout
	// is their own work is decided by that match, and when nothing
	// matches every answer built on it is empty -- so the addresses are
	// kept and shown rather than the silence.
	UnknownAuthors []string `json:"unknownAuthors"`
}

// Validate reports everything wrong with a source.
func (self *AgentKnowledgeSource) Validate() error {
	var errors ValidationErrors
	if !IsAgentKnowledgeKind(self.Kind) {
		errors.add("kind", "%q is not a kind of source", self.Kind)
	}
	switch self.Kind {
	case SourceComputer, SourceArchive:
		if strings.TrimSpace(self.Specification.Computer) == "" {
			errors.add("specification.computer", "required: which computer")
		}
		if strings.TrimSpace(self.Specification.Path) == "" {
			errors.add("specification.path", "required: where on it")
		}
	case SourceSkill:
		if strings.TrimSpace(self.Specification.Tool) == "" {
			errors.add("specification.tool", "required: which tool to call")
		}
	case SourceWeb:
		if strings.TrimSpace(self.Specification.Start) == "" {
			errors.add("specification.start", "required: where to start")
		}
	case SourceSent:
		if strings.TrimSpace(self.Specification.MailboxID) == "" {
			errors.add("specification.mailboxId", "required: which mailbox")
		}
	}
	// A format the daemon cannot read used to be found only by the daemon,
	// hours later, as a scan that failed on a word nobody could see any
	// more. A typo is refused here, where the person is still looking at
	// what they typed.
	if format := self.Specification.Format; format != "" && !IsAgentKnowledgeFormat(format) {
		errors.add("specification.format", "%q is not a format: %s, %s or %s",
			format, FormatFiles, FormatJournal, FormatRecords)
	}
	if self.RootPath != "" {
		if err := ValidPath(self.RootPath); err != nil {
			if validation, ok := err.(ValidationErrors); ok {
				errors = append(errors, validation...)
			}
		}
	}
	if len(self.Name) > 200 {
		errors.add("name", "at most 200 characters")
	}
	return errors.ErrOrNil()
}

// Describe is what a source is, in a line, for a card and for a list.
func (self *AgentKnowledgeSource) Describe() string {
	switch self.Kind {
	case SourceComputer, SourceArchive:
		where := self.Specification.Path + " on " + self.Specification.Computer
		if format := self.Specification.Format; format != "" && format != FormatFiles {
			return where + " (" + format + ")"
		}
		return where
	case SourceSkill:
		return self.Specification.Tool
	case SourceWeb:
		return self.Specification.Start
	case SourceSent:
		return "their own sent mail"
	}
	return string(self.Kind)
}

// AgentDocumentKind is what one thing from a source is.
type AgentDocumentKind string

// The kinds of document.
const (
	DocumentFile AgentDocumentKind = "file"

	// DocumentAttachment is a picture or a file a record came with: a
	// document whose meaning is in its bytes rather than in its text,
	// kept in object storage under its hash and read, if anything here
	// can read it, later.
	DocumentAttachment AgentDocumentKind = "attachment"
	DocumentPage       AgentDocumentKind = "page"
	DocumentPost       AgentDocumentKind = "post"
	DocumentMessage    AgentDocumentKind = "message"
	DocumentCommit     AgentDocumentKind = "commit"
	DocumentChat       AgentDocumentKind = "chat"
	DocumentRequest    AgentDocumentKind = "request"
	DocumentJournal    AgentDocumentKind = "journal"
)

// AgentDocument is one thing read from a source.
type AgentDocument struct {
	ID         string `json:"id"`
	AgentID    string `json:"agentId"`
	SourceID   string `json:"sourceId"`
	ExternalID string `json:"externalId"`

	Kind  AgentDocumentKind `json:"kind"`
	Title string            `json:"title"`
	URL   string            `json:"url,omitempty"`

	HappenedAt *time.Time `json:"happenedAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty"`

	Hash       string `json:"-"`
	Bytes      int64  `json:"bytes"`
	StorageKey string `json:"-"`

	Metadata map[string]any `json:"metadata,omitempty"`
	Private  bool           `json:"private"`

	// SeenAt is when the source last said it still had this. A pass that
	// walks the whole tree deletes what it did not see, so this is what
	// tells a thing that is gone from one that has merely not changed.
	SeenAt *time.Time `json:"-"`

	CreatedAt time.Time `json:"createdAt"`
}

// Cite is how a document is named in an answer, with the warning that it
// came from somewhere only the person can see.
func (self *AgentDocument) Cite() string {
	name := strings.TrimSpace(self.Title)
	if name == "" {
		name = self.ExternalID
	}
	if self.Private {
		name += " (private)"
	}
	return name
}

// Author is who wrote it, where the document says.
func (self *AgentDocument) Author() string {
	return self.metadataText("author")
}

// ContentType is what kind of file this is, where the source said: the
// media type without whatever parameters came with it, lowered, because
// what asks is code deciding whether anything here can open it.
func (self *AgentDocument) ContentType() string {
	value := self.metadataText("contentType")
	if index := strings.Index(value, ";"); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

// Channel is where the record this file came with was posted, and Thread
// is which conversation in it, where the source said.
//
// The two things that tell one screenshot from the fifty thousand beside
// it, so they are asked for in three places -- the night deciding what to
// open, the page showing a fact's evidence, and the agent looking at a
// picture again -- and are read off the document here rather than spelled
// out at each of them.
func (self *AgentDocument) Channel() string {
	return strings.TrimSpace(self.metadataText("channel"))
}

// Thread is which conversation in the channel this came from.
func (self *AgentDocument) Thread() string {
	return strings.TrimSpace(self.metadataText("thread"))
}

// Declined is why the night decided against opening this, and "" where it
// has not decided or decided to.
//
// Kept rather than acted on, so that a person who disagrees with what the
// agent passed over can read the reason and put the file back. Declined
// is not read: nothing has read it, and the two are separate marks so
// that clearing one leaves the other alone.
func (self *AgentDocument) Declined() string {
	return self.metadataText("declined")
}

func (self *AgentDocument) metadataText(key string) string {
	if self.Metadata == nil {
		return ""
	}
	if value, ok := self.Metadata[key].(string); ok {
		return value
	}
	return ""
}

// AgentChunk is a slice of a document, small enough to rank.
type AgentChunk struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agentId"`
	DocumentID string    `json:"documentId"`
	SourceID   string    `json:"sourceId"`
	Number     int       `json:"number"`
	Text       string    `json:"text"`
	Segmented  bool      `json:"segmented"`
	CreatedAt  time.Time `json:"createdAt"`
}

// AgentSymbol is one definition a code file declares.
type AgentSymbol struct {
	AgentID    string `json:"agentId"`
	DocumentID string `json:"documentId"`
	Symbol     string `json:"symbol"`
	Kind       string `json:"kind"`
	Line       int    `json:"line"`
}

// ChunkCharacters is how long a chunk may be, and ChunkOverlap how much of
// the one before it each carries so that a sentence cut in half is still
// found whole somewhere.
const (
	ChunkCharacters = 2000
	ChunkOverlap    = 200
)

// AgentDream is what the nightly run did, one row a night.
//
// Kept and shown because a run that works while nobody is watching has to
// be able to say what it did, and because the backlog below is how the
// person decides whether to give it more of the day or let it work
// coarsely.
type AgentDream struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agentId"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`

	// JobID is the job that ran it. Every model call a dream makes is a
	// run tagged with that job, so a dream's runs can be listed and each
	// opened, as a sorting run can.
	JobID string `json:"jobId,omitempty"`

	Digested  int `json:"digested"`
	Filed     int `json:"filed"`
	Merged    int `json:"merged"`
	Rewritten int `json:"rewritten"`
	Moved     int `json:"moved"`
	Dormant   int `json:"dormant"`
	Embedded  int `json:"embedded"`

	// What the two halves of the night did. Strengthened is links
	// reweighted by what was used together, which is arithmetic and
	// happens every night; Associated is links the night found by
	// walking the graph and asking, which is the only phase that adds a
	// relation nobody typed.
	Strengthened int `json:"strengthened"`
	Associated   int `json:"associated"`

	// Rehearsed is how many questions it asked itself, Gaps how many of
	// those memory could not answer, and Unknown how many it could not
	// try -- no embedding model, no budget left, or a model that did not
	// answer. The gaps are in Proposals, as questions, because filling
	// one means deciding what the answer is and a run with nobody present
	// must not do that.
	//
	// Three numbers rather than two because every failure used to count
	// as answered, so a night whose model was unreachable read as a night
	// with nothing missing.
	Rehearsed int `json:"rehearsed"`
	Gaps      int `json:"gaps"`
	Unknown   int `json:"unknown"`

	// Revised is what an older build of this program wrote that this one
	// went back over and struck. A graph outlives its code, and this is
	// the number that says a newer build is cleaning up after an older
	// one rather than the person having to.
	Revised int `json:"revised"`

	// Backlog is how much was still waiting when it stopped. A cap here
	// is pacing, never truncation: what is not read tonight is read
	// tomorrow, and this is the number that says how many nights that is.
	Backlog int `json:"backlog"`

	// Coarse says this night worked a stretch at a time rather than an
	// item at a time, because the backlog was larger than anybody would
	// wait for. Coverage is still total; only the detail is less, and a
	// later night can redo the stretch finely.
	Coarse bool `json:"coarse"`

	Proposals []DreamProposal `json:"proposals"`

	Tokens    int64  `json:"tokens"`
	Notes     string `json:"notes,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

// DreamProposal is something the night wants the person to decide: where
// a page belongs, what a folder should be called. Applied by a press,
// never by the run.
type DreamProposal struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	To     string `json:"to,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Describe is a night in a sentence, for the page and the command line.
func (self *AgentDream) Describe() string {
	var parts []string
	if self.Digested > 0 {
		parts = append(parts, plural(self.Digested, "thing", "things")+" read")
	}
	if self.Filed > 0 {
		parts = append(parts, plural(self.Filed, "fact", "facts")+" filed")
	}
	if self.Rewritten > 0 {
		parts = append(parts, plural(self.Rewritten, "page", "pages")+" rewritten")
	}
	if self.Moved > 0 {
		parts = append(parts, plural(self.Moved, "page", "pages")+" filed away")
	}
	if self.Dormant > 0 {
		parts = append(parts, plural(self.Dormant, "fact", "facts")+" retired")
	}
	if self.Embedded > 0 {
		parts = append(parts, plural(self.Embedded, "vector", "vectors")+" written")
	}
	if self.Associated > 0 {
		parts = append(parts, plural(self.Associated, "connection", "connections")+" noticed")
	}
	if self.Gaps > 0 {
		parts = append(parts, plural(self.Gaps, "question", "questions")+" it could not answer")
	}
	if self.Unknown > 0 {
		parts = append(parts, plural(self.Unknown, "question", "questions")+" it could not try")
	}
	if self.Revised > 0 {
		parts = append(parts, plural(self.Revised, "line", "lines")+" an older version left")
	}
	if self.FinishedAt == nil {
		// Still going. Saying "nothing needed doing" about a run that has
		// not got to the end of its first phase is the wrong answer to
		// the question somebody is asking.
		if len(parts) == 0 {
			return "Still working."
		}
		return "Still working: " + strings.Join(parts, ", ") + " so far."
	}
	if len(parts) == 0 {
		return "Nothing needed doing."
	}
	sentence := strings.Join(parts, ", ") + "."
	if self.Coarse {
		sentence += " Worked a stretch at a time, to keep up."
	}
	if self.Backlog > 0 {
		sentence += " " + plural(self.Backlog, "thing", "things") + " still waiting."
	}
	return strings.ToUpper(sentence[:1]) + sentence[1:]
}

func plural(count int, one, many string) string {
	word := many
	if count == 1 {
		word = one
	}
	return itoa(count) + " " + word
}
