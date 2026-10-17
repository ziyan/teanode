package models

import (
	"strings"
	"time"
)

// A person shares a subtree of their pages with a circle or with one person
// in a circle they share, and the reader's agent and the reader read the
// owner's rows as they are now, never a copy.
// docs/decisions/20261016-shared-knowledge-is-read-where-it-lives.md says why,
// and docs/subsystems/memory.md ("Sharing") how it is read.
//
// Nothing here is the owner's graph row itself. What a reader gets are the
// views below, made in one place (internal/agent/shared_memory.go) so that
// what is left out is left out the same way for the memory tool, the
// dashboard, the command line and the prompt.

// AgentPageShare gives one person, or every member of one circle, a read-only
// view of a page and everything under it. Exactly one of GranteeUserID and
// GranteeCircleID is set.
type AgentPageShare struct {
	ID string `json:"id"`

	// AgentID is the owner's agent, whose pages are shared.
	AgentID string `json:"agentId"`

	// Path is the root of the shared subtree: the page at that path and every
	// page under it.
	Path string `json:"path"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	// IsQuoteShared says the words each fact was read in are shown too. Left
	// off, a reader sees what a fact says and where it was read, and not the
	// owner's own mail, chat or documents quoted back.
	IsQuoteShared bool `json:"isQuoteShared"`

	CreatedAt       time.Time `json:"createdAt"`
	CreatedByUserID string    `json:"createdByUserId,omitempty"`

	// The rest is read along with the row. The owner is the account the
	// agent belongs to; the grantee is a person or a circle, named for a
	// screen.
	OwnerUserID       string `json:"ownerUserId,omitempty"`
	OwnerUsername     string `json:"ownerUsername,omitempty"`
	OwnerName         string `json:"ownerName,omitempty"`
	OwnerUser         *User  `json:"-"`
	GranteeUsername   string `json:"granteeUsername,omitempty"`
	GranteeName       string `json:"granteeName,omitempty"`
	GranteeCircleName string `json:"granteeCircleName,omitempty"`
}

// Covers says whether the share reaches the page at this path: the shared
// path itself and every path under it.
func (self *AgentPageShare) Covers(pagePath string) bool {
	return PathIsWithin(pagePath, self.Path)
}

// PathIsWithin says whether a page path is the root or lies under it. The
// boundary is a slash: "people/a" contains "people/a" and "people/a/x" and
// never "people/ab", which is a different page whose name starts the same
// way.
func PathIsWithin(pagePath, root string) bool {
	if root == "" || pagePath == "" {
		return false
	}
	if pagePath == root {
		return true
	}
	return strings.HasPrefix(pagePath, root+"/")
}

// SharedFact is one fact of a shared page as the reader sees it.
type SharedFact struct {
	// Number is the fact's number on its page, so that it is cited as
	// people/a-grandparent#3 whoever reads it.
	Number   int           `json:"number"`
	FactKind AgentFactKind `json:"factKind"`
	Text     string        `json:"text"`

	HappenedAt        *time.Time `json:"happenedAt,omitempty"`
	HappenedPrecision string     `json:"happenedPrecision,omitempty"`

	// SaidAt is when the fact was first learned.
	SaidAt     *time.Time `json:"saidAt,omitempty"`
	IsInferred bool       `json:"isInferred"`

	// EvidenceSource names where the owner's agent read it in the owner's
	// terms ("from Ada's mail"), and nothing that identifies the source
	// itself. Empty when the fact has no evidence a reader may be told of.
	EvidenceSource string `json:"evidenceSource,omitempty"`

	// EvidenceQuote is the words it was read in, only when the share says
	// the quotes are shared.
	EvidenceQuote string `json:"evidenceQuote,omitempty"`
}

// Line is the fact as a page shows it, the same line a fact of one's own page
// is shown as.
func (self *SharedFact) Line() string {
	fact := &AgentFact{
		Text: self.Text, Inferred: self.IsInferred,
		HappenedAt: self.HappenedAt, HappenedPrecision: self.HappenedPrecision,
	}
	if self.SaidAt != nil {
		fact.Evidence = []Evidence{{At: self.SaidAt}}
	}
	return fact.Line()
}

// SharedEdge is a link between two shared pages. A link to a page that is not
// shared with the reader is not part of the view at all.
type SharedEdge struct {
	FromPath string            `json:"fromPath"`
	ToPath   string            `json:"toPath"`
	FromName string            `json:"fromName,omitempty"`
	ToName   string            `json:"toName,omitempty"`
	Relation AgentEdgeRelation `json:"relation"`
	Note     string            `json:"note,omitempty"`

	// IsProposed says the owner's agent only guessed the link.
	IsProposed bool `json:"isProposed"`
}

// SharedChild is a page under a shared page, which is shared with it.
type SharedChild struct {
	Path     string        `json:"path"`
	Name     string        `json:"name"`
	NodeKind AgentNodeKind `json:"nodeKind"`
	Opening  string        `json:"opening"`
}

// SharedOverviewNotice is said in every view of a shared page, so that a
// reader who knows pages have longer overviews does not take the missing one
// for a page without it. The overview is written by a model from the page's
// links, children and quotes, so it can name pages that are not shared and
// repeat quotes the owner did not share; it is not shown to a reader at all.
const SharedOverviewNotice = "The owner's longer overview of this page is not shared."

// SharedPage is one page the owner shared, as the reader sees it: its name,
// opening and facts, the links to other shared pages, and the pages under it.
// Not its overview: see SharedOverviewNotice.
type SharedPage struct {
	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`
	OwnerUser     *User  `json:"-"`

	Path     string        `json:"path"`
	Name     string        `json:"name"`
	NodeKind AgentNodeKind `json:"nodeKind"`
	Aliases  []string      `json:"aliases"`

	// Opening is what the page is about, the owner's summary of it. Empty
	// when the owner did not share quotes and the summary quotes somebody.
	Opening string `json:"opening"`

	// IsQuoteShared says at least one share reaching the reader for this
	// page includes the quotes.
	IsQuoteShared bool `json:"isQuoteShared"`

	Facts    []*SharedFact  `json:"facts"`
	Edges    []*SharedEdge  `json:"edges"`
	Children []*SharedChild `json:"children"`
}

// Reference is how a fact of this page is cited: the path and number, and
// whose page it is.
func (self *SharedPage) Reference(number int) string {
	return SharedReference(self.Path, number, self.OwnerName)
}

// SharedReference is the citation of a shared fact: "people/a-grandparent#3
// (shared by Ada)". The owner is named so that the person reading the answer
// knows whose memory it came from.
func SharedReference(pagePath string, number int, ownerName string) string {
	return pagePath + "#" + itoa(number) + " (shared by " + ownerName + ")"
}

// SharedRoot is a root of a shared subtree as a reader lists it.
type SharedRoot struct {
	// ShareIDs are the shares that give the reader this root; more than one
	// when it reaches them as a person and through a circle.
	ShareIDs []string `json:"shareIds"`

	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`
	OwnerUser     *User  `json:"-"`

	Path     string        `json:"path"`
	Name     string        `json:"name"`
	NodeKind AgentNodeKind `json:"nodeKind"`

	// Opening is the first sentence of the page's summary.
	Opening       string `json:"opening"`
	IsQuoteShared bool   `json:"isQuoteShared"`
}

// SharedHit is a page or a fact of a shared page that a search found.
type SharedHit struct {
	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`

	Path string `json:"path"`
	Name string `json:"name"`

	// Opening is the page's opening for a page, and empty for a fact.
	Opening string `json:"opening,omitempty"`

	// FactNumber is zero for a page.
	FactNumber int    `json:"factNumber"`
	Text       string `json:"text,omitempty"`
}
