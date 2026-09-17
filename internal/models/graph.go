package models

import (
	"strings"
	"time"
	"unicode"
)

// The graph: what the agent knows about the person, as pages addressed by
// a path, facts kept on those pages with the evidence they came from, and
// edges for what a path cannot say.
//
// The model reads and writes paths, never identifiers. "people/alice-chen"
// is an address it can guess, and a guess that lands on a page is a read
// that would otherwise have been a search; a ULID is an address it copies
// wrongly and never remembers between rounds. Identifiers stay in the
// database and in the API.

// AgentNodeKind is what a page is about.
type AgentNodeKind string

// The kinds. A folder holds other pages and says nothing itself; a period
// is a month or a year, which is how time becomes a place in the graph.
const (
	NodeSelf         AgentNodeKind = "self"
	NodePerson       AgentNodeKind = "person"
	NodeOrganization AgentNodeKind = "organization"
	NodeProject      AgentNodeKind = "project"
	NodeTopic        AgentNodeKind = "topic"
	NodePlace        AgentNodeKind = "place"
	NodeThing        AgentNodeKind = "thing"
	NodeFolder       AgentNodeKind = "folder"
	NodePeriod       AgentNodeKind = "period"
)

// AgentNodeKinds is every kind, for validation and for the tool's schema.
var AgentNodeKinds = []AgentNodeKind{
	NodeSelf, NodePerson, NodeOrganization, NodeProject,
	NodeTopic, NodePlace, NodeThing, NodeFolder, NodePeriod,
}

// IsAgentNodeKind says whether a word names a kind.
func IsAgentNodeKind(kind AgentNodeKind) bool {
	for _, known := range AgentNodeKinds {
		if known == kind {
			return true
		}
	}
	return false
}

// The roots every agent has, made the first time its graph is touched.
// "self" is the person; the rest are folders things are filed under.
const (
	PathSelf     = "self"
	PathPeople   = "people"
	PathProjects = "projects"
	PathPlaces   = "places"
	PathThings   = "things"
	PathTopics   = "topics"
	PathNotes    = "notes"
	PathTime     = "time"

	// PathWork is the child of "self" where the ingest files the person's
	// own span on each checkout. A hundred checkouts are a hundred lines,
	// which would bury the page that says who they are.
	PathWork = "self/work"
)

// IsThePerson says whether a path under people names the person whose
// agent this is: by their username, by any word of their name, by their
// name's slug, or by what their own page is called and also called. The
// account's name can be a first name alone -- "Ziyan" -- while a model
// files under the full one, so the self page's aliases, which carry every
// name the person has been found under, are part of the check. Such a
// page is a duplicate of "self", where what the agent knows about them
// lives, and everything that files or links routes it there.
func IsThePerson(path string, owner *User, self *AgentNode) bool {
	if owner == nil || !strings.HasPrefix(path, PathPeople+"/") {
		return false
	}
	last := strings.ToLower(LastSegment(path))
	names := []string{owner.Username, owner.Name}
	names = append(names, strings.Fields(owner.Name)...)
	if self != nil {
		names = append(names, self.Name)
		names = append(names, self.Aliases...)
	}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if len(name) < 3 || strings.Contains(name, "@") {
			continue
		}
		if last == name || last == strings.ToLower(Slug(name)) {
			return true
		}
	}
	return false
}

// AgentRoot is one of the roots and what it is called.
type AgentRoot struct {
	Path    string
	Kind    AgentNodeKind
	Name    string
	Summary string
}

// AgentRoots is the shape every graph starts with. Notes is where anything
// that has no better home goes, and where the memories of the flat list
// that came before land.
var AgentRoots = []AgentRoot{
	{PathSelf, NodeSelf, "Who they are", ""},
	{PathPeople, NodeFolder, "People", "Everyone they have told you about."},
	{PathProjects, NodeFolder, "Projects", "What they are working on."},
	{PathPlaces, NodeFolder, "Places", "Where things are."},
	{PathThings, NodeFolder, "Things", "Objects, accounts, machines, possessions."},
	{PathTopics, NodeFolder, "Topics", "Subjects that are not a person or a project."},
	{PathNotes, NodeFolder, "Notes", "Anything that does not belong anywhere else yet."},
	{PathTime, NodeFolder, "Time", "A page per month and per year, of what happened then."},
}

// AgentNode is a page about one thing.
type AgentNode struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	AgentID    string    `json:"agentId"`

	// Version is the build that last wrote this, so a later one can find
	// what an earlier one filed under rules that have since changed. See
	// migration 0073.
	Version string `json:"version,omitempty"`

	// Path is the address, and the hierarchy: "work/mujin/dev/portal" is
	// under "work/mujin/dev". ParentID is the same thing as a link, so a
	// query can walk what a string can only spell.
	Path     string `json:"path"`
	ParentID string `json:"parentId,omitempty"`

	Kind AgentNodeKind `json:"kind"`
	Name string        `json:"name"`

	// Aliases are what else the person calls it: "alice", "the fleet
	// team". Searched along with the name.
	Aliases []string `json:"aliases"`

	// Summary is the page: written by the person, or by the dream from
	// the facts below it.
	Summary string `json:"summary"`

	// ContactID is the address book entry this page is about, for a
	// person. The card that is the person themselves is on their account,
	// not here; the self page reads it live.
	ContactID string `json:"contactId,omitempty"`

	Pinned bool `json:"pinned"`

	// Importance orders the index every prompt carries. Recomputed by the
	// nightly run and by nothing else, so a prompt's cacheable part does
	// not move during the day.
	Importance float32 `json:"importance"`

	// Dormant is out of the index and still searchable.
	Dormant bool `json:"dormant"`

	UsedAt *time.Time `json:"usedAt,omitempty"`
}

// AgentFactKind is what sort of statement a fact is.
type AgentFactKind string

// The kinds. A preference or a decision may only come from the person's
// own words; the rest may be learned from what the agent read.
const (
	FactPlain      AgentFactKind = "fact"
	FactPreference AgentFactKind = "preference"
	FactDecision   AgentFactKind = "decision"
	FactEvent      AgentFactKind = "event"
	FactHowTo      AgentFactKind = "howto"
)

// AgentFactKinds is every kind.
var AgentFactKinds = []AgentFactKind{FactPlain, FactPreference, FactDecision, FactEvent, FactHowTo}

// IsAgentFactKind says whether a word names a kind.
func IsAgentFactKind(kind AgentFactKind) bool {
	for _, known := range AgentFactKinds {
		if known == kind {
			return true
		}
	}
	return false
}

// FromThePerson says whether a kind may only be written from what the
// person themselves said. An instruction inside a page, a mail or a tool's
// answer is not theirs, and a preference taken from one would have the
// agent following a stranger.
func (self AgentFactKind) FromThePerson() bool {
	return self == FactPreference || self == FactDecision
}

// EvidenceKind is where a fact came from.
type EvidenceKind string

// The kinds of evidence.
const (
	EvidenceConversation EvidenceKind = "conversation"
	EvidenceMail         EvidenceKind = "mail"
	EvidenceDocument     EvidenceKind = "document"
	EvidenceCommit       EvidenceKind = "commit"
	EvidenceChat         EvidenceKind = "chat"
	EvidenceContact      EvidenceKind = "contact"
	EvidenceRepository   EvidenceKind = "repository"
	EvidenceMemory       EvidenceKind = "memory"
	EvidencePerson       EvidenceKind = "person"
)

// Evidence is one place a fact came from, with the words it was read in.
type Evidence struct {
	Kind EvidenceKind `json:"kind"`
	ID   string       `json:"id,omitempty"`

	// Quote is the line it was read in, so a person looking at a page can
	// see why the agent believes it.
	Quote string `json:"quote,omitempty"`

	// At is when the evidence was written, where that is known and
	// different from when the fact was filed.
	At *time.Time `json:"at,omitempty"`
}

// AgentFact is one sentence kept on a page.
type AgentFact struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	AgentID    string    `json:"agentId"`
	NodeID     string    `json:"nodeId"`

	// Version is the build that last wrote this. What an old build filed
	// under rules that have since changed is found by asking for it. See
	// migration 0073.
	Version string `json:"version,omitempty"`

	// Number is stable within the node: "people/alice-chen#3".
	Number int `json:"number"`

	Kind AgentFactKind `json:"kind"`
	Text string        `json:"text"`

	// HappenedAt is when it was true, which is not when it was learned.
	HappenedAt *time.Time `json:"happenedAt,omitempty"`

	// Confidence is below one for anything assembled rather than said, and
	// Inferred marks it as such on the page and in the prompt. A "who did
	// that feature" put together from a chat thread and a commit is an
	// inference; filing it as a plain fact would make a guess permanent.
	Confidence float32 `json:"confidence"`
	Inferred   bool    `json:"inferred"`

	Evidence []Evidence `json:"evidence"`

	// Audiences is which unattended runs read it, as a memory's AppliesTo
	// was. The conversation always reads a fact on a page it is shown.
	Audiences []AgentAudience `json:"audiences"`

	// SupersededBy names the newer statement of the same thing. A
	// superseded fact leaves the page and stays readable.
	SupersededBy string `json:"supersededBy,omitempty"`

	Dormant bool       `json:"dormant"`
	UsedAt  *time.Time `json:"usedAt,omitempty"`
}

// AgentEdgeRelation is what one page is to another.
type AgentEdgeRelation string

// The relations. PartOf duplicates what the path already says, so that a
// query can walk the hierarchy without parsing strings.
const (
	EdgePartOf     AgentEdgeRelation = "part_of"
	EdgeWorksOn    AgentEdgeRelation = "works_on"
	EdgeMemberOf   AgentEdgeRelation = "member_of"
	EdgeKnows      AgentEdgeRelation = "knows"
	EdgeOwns       AgentEdgeRelation = "owns"
	EdgeUses       AgentEdgeRelation = "uses"
	EdgeLocatedIn  AgentEdgeRelation = "located_in"
	EdgeRelatedTo  AgentEdgeRelation = "related_to"
	EdgeDecidedIn  AgentEdgeRelation = "decided_in"
	EdgeAboutPlace AgentEdgeRelation = "about"
)

// AgentEdgeRelations is every relation.
var AgentEdgeRelations = []AgentEdgeRelation{
	EdgePartOf, EdgeWorksOn, EdgeMemberOf, EdgeKnows, EdgeOwns,
	EdgeUses, EdgeLocatedIn, EdgeRelatedTo, EdgeDecidedIn, EdgeAboutPlace,
}

// IsAgentEdgeRelation says whether a word names a relation.
func IsAgentEdgeRelation(relation AgentEdgeRelation) bool {
	for _, known := range AgentEdgeRelations {
		if known == relation {
			return true
		}
	}
	return false
}

// AgentEdge joins two pages.
type AgentEdge struct {
	AgentID   string            `json:"agentId"`
	FromID    string            `json:"fromId"`
	ToID      string            `json:"toId"`
	Relation  AgentEdgeRelation `json:"relation"`
	Weight    float32           `json:"weight"`
	Evidence  []Evidence        `json:"evidence"`
	CreatedAt time.Time         `json:"createdAt"`

	// Note is what this particular link is about, in the person's own
	// terms: "wrote the payload angle check", "since 2019", "her
	// daughter". The relation says what sort of link it is and this says
	// which one.
	//
	// Without it an edge is a word. A model shown "people/alice-chen
	// works_on projects/portal" knows there is a link and nothing about
	// it; shown "Alice Chen works on Portal — led the API rewrite" it
	// knows something worth having.
	Note string `json:"note,omitempty"`

	// HappenedAt is when the link was true and UsedAt when it was last
	// wanted. A join that has gone stale sinks rather than being
	// forgotten: somebody who left a project two years ago did work on
	// it, and that is no longer the first thing to say about them.
	HappenedAt *time.Time `json:"happenedAt,omitempty"`
	UsedAt     *time.Time `json:"usedAt,omitempty"`

	// FromPath and ToPath are filled when an edge is read for display or
	// for a prompt, where a path means something and an identifier does
	// not. Not stored. FromName and ToName likewise, so that an edge can
	// be read as a sentence about two things rather than two paths.
	FromPath string        `json:"fromPath,omitempty"`
	ToPath   string        `json:"toPath,omitempty"`
	FromName string        `json:"fromName,omitempty"`
	ToName   string        `json:"toName,omitempty"`
	FromKind AgentNodeKind `json:"fromKind,omitempty"`
	ToKind   AgentNodeKind `json:"toKind,omitempty"`
}

// relationPhrases is how each relation reads, forwards and backwards.
//
// Two phrases rather than one, because a link is read from whichever end
// the reader is standing at: on Alice's page the link to Portal reads
// "works on Portal", and on Portal's page the same row reads "Alice Chen
// works on this". One word, "works_on", cannot do both, and a page that
// lists its incoming links as "works_on" is a page nobody can read.
var relationPhrases = map[AgentEdgeRelation][2]string{
	EdgePartOf:     {"is part of", "contains"},
	EdgeWorksOn:    {"works on", "is worked on by"},
	EdgeMemberOf:   {"is at", "counts as one of its people"},
	EdgeKnows:      {"knows", "is known to"},
	EdgeOwns:       {"owns", "belongs to"},
	EdgeUses:       {"uses", "is used by"},
	EdgeLocatedIn:  {"is in", "is where"},
	EdgeRelatedTo:  {"is related to", "is related to"},
	EdgeDecidedIn:  {"was decided in", "is where the decision was made about"},
	EdgeAboutPlace: {"is about", "is the subject of"},
}

// pastPhrases are how a relation reads once it has stopped being true.
// "Alice worked on Portal" rather than "works on": a link the nightly run
// has decided is stale should not be stated in the present tense, because
// the present tense is a claim about now.
var pastPhrases = map[AgentEdgeRelation][2]string{
	EdgeWorksOn:   {"worked on", "was worked on by"},
	EdgeMemberOf:  {"was at", "counted as one of its people"},
	EdgeOwns:      {"owned", "belonged to"},
	EdgeUses:      {"used", "was used by"},
	EdgeLocatedIn: {"was in", "was where"},
}

// Phrase is how this relation reads from one end, in the tense the link
// deserves.
func (self *AgentEdge) Phrase(outward, stale bool) string {
	index := 0
	if !outward {
		index = 1
	}
	if stale {
		if phrases, found := pastPhrases[self.Relation]; found {
			return phrases[index]
		}
	}
	if phrases, found := relationPhrases[self.Relation]; found {
		return phrases[index]
	}
	return string(self.Relation)
}

// Sentence is the edge as something to read, from the end given.
//
// "Alice Chen works on Portal — led the API rewrite". The names rather
// than the paths, because the sentence is for a reader; the path is
// carried beside it for whoever wants to follow the link.
func (self *AgentEdge) Sentence(fromPath string, stale bool) string {
	outward := self.FromPath == fromPath
	other, otherPath := self.ToName, self.ToPath
	subject := self.FromName
	if !outward {
		other, otherPath = self.FromName, self.FromPath
		subject = self.ToName
	}
	if other == "" {
		other = otherPath
	}
	_ = subject
	sentence := self.Phrase(outward, stale) + " " + other
	if otherPath != "" && otherPath != other {
		sentence += " (" + otherPath + ")"
	}
	if note := strings.TrimSpace(self.Note); note != "" {
		sentence += " — " + note
	}
	return sentence
}

// The bounds a page and a fact are held to.
const (
	// PathSegments is how deep a path may go, and PathLength how long it
	// may be. Eight segments is more than any hierarchy a person keeps in
	// their head, and a path longer than this is a description rather than
	// an address.
	PathSegments = 8
	PathLength   = 500

	// SummaryLength is how long a page may be. Long enough for a page
	// about a person or a project written as prose; short enough that the
	// twenty of them a recall can reach do not fill a prompt.
	SummaryLength = 8000

	// FactLength is how long one statement may be. A fact that needs more
	// than this is two facts, or a page.
	FactLength = 1000

	// NameLength and AliasCount bound what a page is called.
	NameLength = 200
	AliasCount = 16

	// EvidenceCount is how many places one fact may cite, and QuoteLength
	// how much of each is kept.
	EvidenceCount = 12
	QuoteLength   = 400
)

// Slug is a name as a path segment: lowercase, letters digits and dashes,
// runs of anything else collapsed to one dash. Non-ASCII letters are kept
// -- a person whose name is not written in ASCII still gets a page they
// can read -- because the path is an address for a model and a person,
// not a file name.
func Slug(name string) string {
	var builder strings.Builder
	dashed := false
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			builder.WriteRune(character)
			dashed = false
		case builder.Len() > 0 && !dashed:
			builder.WriteByte('-')
			dashed = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if len([]rune(slug)) > 60 {
		slug = string([]rune(slug)[:60])
		slug = strings.Trim(slug, "-")
	}
	return slug
}

// JoinPath is a parent path and a segment, with the segment slugged.
func JoinPath(parent, segment string) string {
	segment = Slug(segment)
	if parent == "" {
		return segment
	}
	if segment == "" {
		return parent
	}
	return parent + "/" + segment
}

// ParentPath is everything above the last segment, and empty for a root.
func ParentPath(path string) string {
	if index := strings.LastIndexByte(path, '/'); index > 0 {
		return path[:index]
	}
	return ""
}

// LastSegment is the last part of a path.
func LastSegment(path string) string {
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}

// NormalizePath cleans a path the model or a person wrote: trims, drops
// leading and trailing slashes, slugs each segment, and drops empty ones.
// It does not report an error -- ValidPath does that -- so that a caller
// can normalize first and complain about what is left.
func NormalizePath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if slug := Slug(part); slug != "" {
			kept = append(kept, slug)
		}
	}
	return strings.Join(kept, "/")
}

// ValidPath reports everything wrong with a path.
func ValidPath(path string) error {
	var errors ValidationErrors
	if strings.TrimSpace(path) == "" {
		errors.add("path", "required")
		return errors.ErrOrNil()
	}
	if len(path) > PathLength {
		errors.add("path", "at most %d characters", PathLength)
	}
	parts := strings.Split(path, "/")
	if len(parts) > PathSegments {
		errors.add("path", "at most %d segments deep", PathSegments)
	}
	for _, part := range parts {
		if part == "" {
			errors.add("path", "has an empty segment")
			break
		}
		if part != Slug(part) {
			errors.add("path", "%q is not a slug; write it as %q", part, Slug(part))
			break
		}
	}
	return errors.ErrOrNil()
}

// Validate reports everything wrong with a page.
func (self *AgentNode) Validate() error {
	var errors ValidationErrors
	if err := ValidPath(self.Path); err != nil {
		if validation, ok := err.(ValidationErrors); ok {
			errors = append(errors, validation...)
		}
	}
	if !IsAgentNodeKind(self.Kind) {
		errors.add("kind", "%q is not a kind of page", self.Kind)
	}
	if len(self.Name) > NameLength {
		errors.add("name", "at most %d characters", NameLength)
	}
	if len(self.Summary) > SummaryLength {
		errors.add("summary", "at most %d characters", SummaryLength)
	}
	if len(self.Aliases) > AliasCount {
		errors.add("aliases", "at most %d", AliasCount)
	}
	return errors.ErrOrNil()
}

// Validate reports everything wrong with a fact.
func (self *AgentFact) Validate() error {
	var errors ValidationErrors
	if strings.TrimSpace(self.Text) == "" {
		errors.add("text", "required")
	}
	if len(self.Text) > FactLength {
		errors.add("text", "at most %d characters", FactLength)
	}
	if !IsAgentFactKind(self.Kind) {
		errors.add("kind", "%q is not a kind of fact", self.Kind)
	}
	if self.NodeID == "" {
		errors.add("nodeId", "required")
	}
	if len(self.Evidence) > EvidenceCount {
		errors.add("evidence", "at most %d", EvidenceCount)
	}
	for _, audience := range self.Audiences {
		if !IsAgentAudience(audience) {
			errors.add("audiences", "%q is not an audience", audience)
		}
	}
	return errors.ErrOrNil()
}

// Addressed says whether a fact is read by an unattended run of a kind.
func (self *AgentFact) Addressed(audience AgentAudience) bool {
	for _, candidate := range self.Audiences {
		if candidate == audience {
			return true
		}
	}
	return false
}

// Live says whether a fact is one the page still states: not superseded,
// not dormant.
func (self *AgentFact) Live() bool {
	return self.SupersededBy == "" && !self.Dormant
}

// Line is the fact as a page or a prompt shows it: the sentence, then what
// a reader needs to judge it -- that it was inferred, and when it was
// true.
func (self *AgentFact) Line() string {
	line := strings.TrimSpace(self.Text)
	var notes []string
	if self.Inferred {
		notes = append(notes, "inferred")
	}
	if self.HappenedAt != nil {
		notes = append(notes, self.HappenedAt.Format("Jan 2006"))
	}
	if len(notes) > 0 {
		line += " (" + strings.Join(notes, ", ") + ")"
	}
	return line
}

// IndexLine is a page as the index in a prompt carries it: the path, the
// name, and the first sentence of the summary if there is room.
func (self *AgentNode) IndexLine(width int) string {
	line := self.Path
	if name := strings.TrimSpace(self.Name); name != "" && !strings.EqualFold(name, LastSegment(self.Path)) {
		line += " — " + name
	}
	summary := strings.TrimSpace(self.Summary)
	if summary == "" {
		return line
	}
	// The first sentence, or the first line, whichever comes first: a page
	// is prose and the opening says what it is about.
	if index := strings.IndexAny(summary, ".\n"); index > 0 {
		summary = summary[:index]
	}
	summary = strings.TrimSpace(summary)
	if remaining := width - len(line) - 3; remaining > 16 && summary != "" {
		if len(summary) > remaining {
			summary = strings.TrimSpace(summary[:remaining]) + "…"
		}
		line += ": " + summary
	}
	return line
}

// Reference is how a fact is cited in a prompt and on a page.
func (self *AgentFact) Reference(path string) string {
	return path + "#" + itoa(self.Number)
}

// itoa is strconv.Itoa without the import, for one call.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		digits[index] = '-'
	}
	return string(digits[index:])
}

// --- history -----------------------------------------------------------

// RevisionKind is what sort of change was made.
type RevisionKind string

// The kinds. A page's own words, its name and where it sits; a fact put
// on it, changed or taken off; a link made or broken.
//
// Folded and struck are not removed, and the difference is the whole
// point: a fact the agent decided against on its own stays on the page
// as a dormant row, so the decision can be read and undone. Only the
// person's own forgetting leaves a RevisionFactGone.
const (
	RevisionPage       RevisionKind = "page"
	RevisionRenamed    RevisionKind = "renamed"
	RevisionMoved      RevisionKind = "moved"
	RevisionFactAdded  RevisionKind = "fact_added"
	RevisionFactEdited RevisionKind = "fact_edited"
	RevisionFactGone   RevisionKind = "fact_removed"
	RevisionFactMerged RevisionKind = "fact_merged"
	RevisionFactFolded RevisionKind = "fact_folded"
	RevisionFactStruck RevisionKind = "fact_struck"
	RevisionLinked     RevisionKind = "linked"
	RevisionUnlinked   RevisionKind = "unlinked"
	RevisionCreated    RevisionKind = "created"
)

// RevisionActor is who made a change.
//
// Worth recording because the answer changes what a reader should do
// about it: a sentence the person wrote is theirs and stands, one the
// nightly run wrote is a summary that can be rewritten, and one a source
// put there is only as good as the source.
type RevisionActor string

// The actors.
const (
	ActorPerson   RevisionActor = "person"
	ActorAgent    RevisionActor = "agent"
	ActorRemember RevisionActor = "remember"
	ActorDream    RevisionActor = "dream"
	ActorIngest   RevisionActor = "ingest"
)

// AgentRevision is one change to a page, kept so the page can be
// accounted for.
//
// Version is the build that made the change. A graph outlives the code
// that filled it, and "written before the rule existed" is a question
// only a version can answer.
type AgentRevision struct {
	ID       string `json:"id"`
	AgentID  string `json:"agentId"`
	NodeID   string `json:"nodeId"`
	Revision int    `json:"revision"`

	Kind  RevisionKind  `json:"kind"`
	Actor RevisionActor `json:"actor"`

	// Before and After are what changed. Both, because a change that only
	// records the new value cannot be undone or explained.
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`

	Reason    string    `json:"reason,omitempty"`
	Version   string    `json:"version,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Describe is the change in a line, for a history somebody reads: who,
// then what they did.
//
// Change is the same line without the who, for a reader that shows the
// actor beside it rather than in it -- the dashboard puts it in a badge,
// and saying it twice in one row reads as a stutter.
func (self *AgentRevision) Describe() string {
	who := map[RevisionActor]string{
		ActorPerson:   "you",
		ActorAgent:    "the agent",
		ActorRemember: "filing a conversation",
		ActorDream:    "a dream",
		ActorIngest:   "reading a source",
	}[self.Actor]
	if who == "" {
		who = "something"
	}
	line := who + " " + self.Change()
	if self.Reason != "" {
		line += " — " + self.Reason
	}
	return line
}

// Change is what happened, without who did it.
func (self *AgentRevision) Change() string {
	what := map[RevisionKind]string{
		RevisionCreated:    "made this page",
		RevisionPage:       "rewrote what this page says",
		RevisionRenamed:    "renamed it",
		RevisionMoved:      "filed it somewhere else",
		RevisionFactAdded:  "added a fact",
		RevisionFactEdited: "changed a fact",
		RevisionFactGone:   "took a fact off",
		RevisionFactMerged: "merged two facts that said the same thing",
		RevisionFactFolded: "folded a fact into another",
		RevisionFactStruck: "struck a fact that said nothing",
		RevisionLinked:     "linked it to something",
		RevisionUnlinked:   "removed a link",
	}[self.Kind]
	if what == "" {
		what = string(self.Kind)
	}
	// The other end of a link, or where a page went: without it a
	// history reads "linked it to something", which tells nobody
	// anything.
	switch self.Kind {
	case RevisionLinked, RevisionUnlinked:
		if path := self.PathAfter(); path != "" {
			what = strings.TrimSuffix(what, " to something") + " to " + path
			if self.Kind == RevisionUnlinked {
				what = "removed the link to " + path
			}
		}
	case RevisionMoved:
		if path := self.PathAfter(); path != "" {
			what = "filed it under " + path
		}
	}
	return what
}

// PathAfter is the page a change points at: where a page was moved to, or
// the other end of a link. For an unlink, which has no "after" at all, it
// is the page that was unlinked.
func (self *AgentRevision) PathAfter() string {
	if path := stringIn(self.After, "path"); path != "" {
		return path
	}
	return stringIn(self.Before, "path")
}

// TextBefore and TextAfter are the words a change moved, where it moved
// words at all. Empty for a change that moved something else.
func (self *AgentRevision) TextBefore() string { return stringIn(self.Before, "text") }
func (self *AgentRevision) TextAfter() string  { return stringIn(self.After, "text") }

func stringIn(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}
