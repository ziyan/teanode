package models

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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

	// PathThemes is where the night keeps the themes it finds by
	// clustering the links: groups of pages more linked to one another
	// than to the rest. Not one of the roots every graph starts with; the
	// night makes it the first time it has a theme to put there.
	PathThemes = "themes"

	// PathReflections is the person's own page of the night's
	// observations across their themes, written about once a week.
	PathReflections = "self/reflections"
)

// IsThemePath says whether a path is a theme's page: anything under
// themes, not the folder itself.
func IsThemePath(path string) bool {
	return strings.HasPrefix(path, PathThemes+"/")
}

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

	// Path is the address, and the hierarchy: "work/northwind/dev/portal" is
	// under "work/northwind/dev". ParentID is the same thing as a link, so a
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

	// Overview is how the thing works, in several short markdown sections,
	// written by the night from the page's facts, the overviews of the
	// pages under it and the openings of the pages it is linked to. The
	// opening says what a page is and every prompt's index carries it; the
	// overview is longer and is read only when the page itself is.
	//
	// Written only by the night's overview phase, and never by writing the
	// page, so a page saved from a copy read before an overview was written
	// does not put the old one back.
	Overview          string     `json:"overview,omitempty"`
	OverviewWrittenAt *time.Time `json:"overviewWrittenAt,omitempty"`

	// OverviewInputs is a hash of what the overview was written from. The
	// night writes it again when the hash of the page's inputs as they are
	// now is different; empty means it is due.
	OverviewInputs string `json:"-"`

	// OverviewEvidence is the pages (kind memory, the path as the quote)
	// and the files (kind document) the overview cites.
	OverviewEvidence []Evidence `json:"overviewEvidence,omitempty"`
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

	// FactReflection is an observation the night made over a theme --
	// a pattern, a tension, a trend, a risk, a question -- citing the
	// pages and facts it rests on. Nobody said it; the night worked it out
	// from what the notes say, so only the night writes one.
	FactReflection AgentFactKind = "reflection"

	// FactLesson is what worked, or what to avoid, when doing a piece of
	// work, read from a conversation in which a command showed it worked:
	// when it applies, the approach, what failed, how it was verified. Only
	// the pass that reads conversations for lessons writes one, and only
	// with that command's result as its evidence.
	FactLesson AgentFactKind = "lesson"
)

// AgentFactKinds is every kind.
var AgentFactKinds = []AgentFactKind{FactPlain, FactPreference, FactDecision, FactEvent, FactHowTo, FactReflection, FactLesson}

// AgentFactKindsFiled is the kinds a conversation, a tool or a person
// files: every kind but a reflection.
var AgentFactKindsFiled = []AgentFactKind{FactPlain, FactPreference, FactDecision, FactEvent, FactHowTo}

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

// FromItsOwnReasoning says whether a kind is only ever written by the
// agent's own passes over what it knows: the dreams' reflections, and the
// lessons read from verified work. A model filing a conversation that
// calls its sentence a reflection or a lesson is filing a fact, and it is
// filed as one.
func (self AgentFactKind) FromItsOwnReasoning() bool {
	return self == FactReflection || self == FactLesson
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

	// EvidenceDream is the agent's own reasoning rather than anything it
	// read: the path a nightly walk took to reach a page. It has its own
	// kind so that no reader mistakes the walk for a document.
	EvidenceDream EvidenceKind = "dream"
)

// ReflectionEvidencePrefix begins the night's own line of evidence on a
// reflection, which names what kind of observation it is: "reflection:
// pattern", "reflection: risk".
const ReflectionEvidencePrefix = "reflection: "

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

	// HappenedAt is when it was true, which is not when it was learned, and
	// HappenedPrecision how precisely: HappenedDay, HappenedMonth or
	// HappenedYear, as the date was given; empty where it was not recorded.
	HappenedAt        *time.Time `json:"happenedAt,omitempty"`
	HappenedPrecision string     `json:"happenedPrecision,omitempty"`

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

	// EdgeDependsOn is a build-time dependency of one checkout on another,
	// read from its build files by a program. Not uses: that one is the
	// model's word for people and their tools, and a map of what builds on
	// what has to be told apart from it.
	EdgeDependsOn AgentEdgeRelation = "depends_on"
)

// AgentEdgeRelations is every relation.
var AgentEdgeRelations = []AgentEdgeRelation{
	EdgePartOf, EdgeWorksOn, EdgeMemberOf, EdgeKnows, EdgeOwns,
	EdgeUses, EdgeLocatedIn, EdgeRelatedTo, EdgeDecidedIn, EdgeAboutPlace,
	EdgeDependsOn,
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

// AgentEdgeStatus is how far a link may be asserted: something somebody
// stated, or something the agent worked out for itself.
type AgentEdgeStatus string

// The statuses. Stated is the default and what every writer but the
// nightly walk produces, so an empty status read out of an older row
// means stated.
const (
	EdgeStated   AgentEdgeStatus = "stated"
	EdgeProposed AgentEdgeStatus = "proposed"
)

// AgentEdge joins two pages.
type AgentEdge struct {
	AgentID  string            `json:"agentId"`
	FromID   string            `json:"fromId"`
	ToID     string            `json:"toId"`
	Relation AgentEdgeRelation `json:"relation"`
	Weight   float32           `json:"weight"`

	// Status separates what the agent may assert from what it may only
	// wonder about. The nightly walk guesses a link from two pages being
	// two steps apart, which is a hypothesis; the person drawing one in
	// the dashboard is not. Without the distinction the agent said both
	// in the same voice.
	Status AgentEdgeStatus `json:"status"`

	Evidence  []Evidence `json:"evidence"`
	CreatedAt time.Time  `json:"createdAt"`

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
	EdgeDependsOn:  {"depends on", "is depended on by"},
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
	EdgeDependsOn: {"depended on", "was depended on by"},
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

// SortAgentEdgesFrom puts a page's links in an order that holds from one
// read to the next, so that reading them a part at a time neither repeats
// a link nor skips one: the page's own links first, then those pointing at
// it, each by relation and then by the path at the other end.
func SortAgentEdgesFrom(path string, edges []*AgentEdge) {
	isOutgoing := func(edge *AgentEdge) bool { return edge.FromPath == path }
	otherPath := func(edge *AgentEdge) string {
		if isOutgoing(edge) {
			return edge.ToPath
		}
		return edge.FromPath
	}
	sort.SliceStable(edges, func(left, right int) bool {
		if isOutgoing(edges[left]) != isOutgoing(edges[right]) {
			return isOutgoing(edges[left])
		}
		if edges[left].Relation != edges[right].Relation {
			return edges[left].Relation < edges[right].Relation
		}
		return otherPath(edges[left]) < otherPath(edges[right])
	})
}

// Proposed says whether this link is the agent's own guess rather than
// something somebody stated.
func (self *AgentEdge) Proposed() bool {
	return self.Status == EdgeProposed
}

// Sentence is the edge as something to read, from the end given.
//
// "Alice Chen works on Portal — led the API rewrite". The names rather
// than the paths, because the sentence is for a reader; the path is
// carried beside it for whoever wants to follow the link.
//
// A proposed link is hedged in both directions: "perhaps" in front so the
// claim is never made flat, and who guessed it at the end so a reader
// knows whom to disagree with.
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
	phrase := self.Phrase(outward, stale)
	if self.Proposed() {
		// "perhaps related to Portal", not "perhaps is related to
		// Portal": the phrases are written to follow a subject, and
		// "perhaps" stands where the subject would.
		phrase = "perhaps " + strings.TrimPrefix(phrase, "is ")
	}
	sentence := phrase + " " + other
	if otherPath != "" && otherPath != other {
		sentence += " (" + otherPath + ")"
	}
	if note := strings.TrimSpace(self.Note); note != "" {
		sentence += " — " + note
	}
	if self.Proposed() {
		sentence += " (the agent's guess)"
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

	// NameLength and AliasCount bound what a page is called.
	NameLength = 200
	AliasCount = 16

	// EvidenceCount is how many places one fact may cite.
	//
	// A page's opening, a fact's text and a quote have no bound. Each
	// had one, and what was written past it was cut off where it was
	// stored, with nothing to say a sentence had ended early: the words
	// were gone for every reader. A prompt that cannot hold all of a
	// long text cuts it where it shows it, and says there is more and
	// how to read it.
	EvidenceCount = 12
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
	if len(self.Aliases) > AliasCount {
		errors.add("aliases", "at most %d", AliasCount)
	}
	return errors.ErrOrNil()
}

// MergeEvidence is a fact's evidence with another saying's added, each
// place (kind, id and quote) once and within EvidenceCount.
//
// A place said again moves to where the newest are, keeping the earlier
// of its two times, since SaidAt is the earliest; only the first entry,
// where the fact came from, stays where it is. Past EvidenceCount the
// middle goes: the first entry and the newest, which hold everything the
// merge just added and so whatever a rise in confidence rests on, are
// kept. Cutting the end instead dropped everything a full fact was told
// later, while its confidence rose on the strength of it.
func MergeEvidence(standing, added []Evidence) []Evidence {
	type place struct {
		evidenceKind EvidenceKind
		id, quote    string
	}
	placeOf := func(evidence Evidence) place { return place{evidence.Kind, evidence.ID, evidence.Quote} }
	keepEarlier := func(into *Evidence, at *time.Time) {
		if at != nil && (into.At == nil || at.Before(*into.At)) {
			into.At = at
		}
	}
	var addedOnce []Evidence
	addedIndex := map[place]int{}
	for _, evidence := range added {
		key := placeOf(evidence)
		if index, isListed := addedIndex[key]; isListed {
			keepEarlier(&addedOnce[index], evidence.At)
			continue
		}
		addedIndex[key] = len(addedOnce)
		addedOnce = append(addedOnce, evidence)
	}
	merged := make([]Evidence, 0, len(standing)+len(addedOnce))
	isListed := map[place]bool{}
	for _, evidence := range standing {
		key := placeOf(evidence)
		if isListed[key] {
			continue
		}
		isListed[key] = true
		index, isAdded := addedIndex[key]
		switch {
		case !isAdded:
			merged = append(merged, evidence)
		case len(merged) == 0:
			// The first entry stays first, said again or not.
			keepEarlier(&evidence, addedOnce[index].At)
			merged = append(merged, evidence)
		default:
			keepEarlier(&addedOnce[index], evidence.At)
			isListed[key] = false
		}
	}
	for _, evidence := range addedOnce {
		if key := placeOf(evidence); !isListed[key] {
			isListed[key] = true
			merged = append(merged, evidence)
		}
	}
	if len(merged) <= EvidenceCount {
		return merged
	}
	return append(merged[:1:1], merged[len(merged)-(EvidenceCount-1):]...)
}

// Validate reports everything wrong with a fact.
func (self *AgentFact) Validate() error {
	var errors ValidationErrors
	if strings.TrimSpace(self.Text) == "" {
		errors.add("text", "required")
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

// ReflectionKind is what kind of observation a reflection is -- a
// pattern, a tension, a trend, a risk, a question -- from the night's own
// line in its evidence; empty for any other fact.
func (self *AgentFact) ReflectionKind() string {
	if self.Kind != FactReflection {
		return ""
	}
	for _, evidence := range self.Evidence {
		if evidence.Kind == EvidenceDream && strings.HasPrefix(evidence.Quote, ReflectionEvidencePrefix) {
			return strings.TrimPrefix(evidence.Quote, ReflectionEvidencePrefix)
		}
	}
	return ""
}

// Citations is the pages and facts a reflection cites, as a reader names
// them: "projects/example-app", "projects/example-lib#2".
func (self *AgentFact) Citations() []string {
	var citations []string
	for _, evidence := range self.Evidence {
		if evidence.Kind == EvidenceMemory && evidence.Quote != "" {
			citations = append(citations, evidence.Quote)
		}
	}
	return citations
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
	happened := self.HappenedText()
	if happened != "" {
		notes = append(notes, happened)
	}
	// When it was learned, where that says something the date it happened
	// does not: a fact with no date of its own, or one reported later than
	// it happened. "What did we know on the 6th" is answered from this.
	if said := self.SaidAt(); said != nil {
		if text := said.Format("2 Jan 2006"); happened == "" || text != happened {
			notes = append(notes, "said "+text)
		}
	}
	if len(notes) > 0 {
		line += " (" + strings.Join(notes, ", ") + ")"
	}
	return line
}

// The precisions a fact's date can be given with.
const (
	HappenedDay   = "day"
	HappenedMonth = "month"
	HappenedYear  = "year"
)

// HappenedText is when the fact happened, as precisely as it is known: "14
// Jan 2023", "Jan 2023" or "2023". A date recorded before the precision
// was is shown to the day unless it falls on the first of a month, which
// is how a month given alone was stored.
func (self *AgentFact) HappenedText() string {
	if self.HappenedAt == nil {
		return ""
	}
	switch self.HappenedPrecision {
	case HappenedDay:
		return self.HappenedAt.Format("2 Jan 2006")
	case HappenedMonth:
		return self.HappenedAt.Format("Jan 2006")
	case HappenedYear:
		return self.HappenedAt.Format("2006")
	}
	if self.HappenedAt.Day() != 1 {
		return self.HappenedAt.Format("2 Jan 2006")
	}
	return self.HappenedAt.Format("Jan 2006")
}

// SaidAt is when the fact was first learned: the earliest time its
// evidence records, which is when its source was written or said. Nil
// where no evidence says.
func (self *AgentFact) SaidAt() *time.Time {
	var earliest *time.Time
	for index := range self.Evidence {
		if at := self.Evidence[index].At; at != nil && (earliest == nil || at.Before(*earliest)) {
			earliest = at
		}
	}
	return earliest
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
	// Counted and cut in characters, not bytes: a cut through a character
	// left half of it before the ellipsis, which a prompt shows as a
	// replacement mark. A sentence cut short ends with the ellipsis, so
	// that a model reading the line can tell the words go on.
	if remaining := width - utf8.RuneCountInString(line) - 3; remaining > 16 && summary != "" {
		if runes := []rune(summary); len(runes) > remaining {
			summary = strings.TrimSpace(string(runes[:remaining])) + "…"
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
