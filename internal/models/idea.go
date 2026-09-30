package models

import (
	"slices"
	"time"
)

// An idea is one offer of work the agent can do for the person, kept as a
// row they can browse, start, finish or dismiss. A catalog idea is written
// in this repository and offered while the person has every tool it needs;
// a personal idea is found in their own mail, memory and calendar, and
// carries what prompted it. docs/planning/agent-ideas-execplan.md is the
// design.

// AgentIdeaKind is where an idea came from.
type AgentIdeaKind string

const (
	IdeaCatalog  AgentIdeaKind = "catalog"
	IdeaPersonal AgentIdeaKind = "personal"
)

// AgentIdeaStatus is what became of an idea.
type AgentIdeaStatus string

const (
	// IdeaOpen is on offer, shown or not.
	IdeaOpen AgentIdeaStatus = "open"
	// IdeaStarted has a conversation carrying it out.
	IdeaStarted AgentIdeaStatus = "started"
	// IdeaDone was carried out.
	IdeaDone AgentIdeaStatus = "done"
	// IdeaDismissed is one the person does not want.
	IdeaDismissed AgentIdeaStatus = "dismissed"
	// IdeaExpired stopped mattering, or stopped being something the agent
	// can do, before anybody took it up.
	IdeaExpired AgentIdeaStatus = "expired"
)

// IsValid says whether a status is one of the five.
func (self AgentIdeaStatus) IsValid() bool {
	switch self {
	case IdeaOpen, IdeaStarted, IdeaDone, IdeaDismissed, IdeaExpired:
		return true
	}
	return false
}

// AgentIdeaExpiredReason is why an idea expired: what stopped it being on
// offer.
type AgentIdeaExpiredReason string

const (
	// IdeaPastDate is a personal idea whose date passed.
	IdeaPastDate AgentIdeaExpiredReason = "past_date"
	// IdeaMissingTool is a catalog idea needing a tool the person does not
	// have. It cannot be opened again until the tool is back, when it comes
	// back on its own.
	IdeaMissingTool AgentIdeaExpiredReason = "missing_tool"
	// IdeaAlreadyUsed is a catalog idea about something the person already
	// does. They may still open it again.
	IdeaAlreadyUsed AgentIdeaExpiredReason = "already_used"
)

// IsValid says whether a reason is one of the three, or none.
func (self AgentIdeaExpiredReason) IsValid() bool {
	switch self {
	case "", IdeaPastDate, IdeaMissingTool, IdeaAlreadyUsed:
		return true
	}
	return false
}

// AgentIdeaCategory is the area of life an idea is about.
type AgentIdeaCategory string

// IdeaCategory is an area, with the emoji an idea in it may carry: a fixed
// list, so that nothing reads as a joke beside a serious idea. The first
// is the area's own.
type IdeaCategory struct {
	IdeaCategory AgentIdeaCategory `json:"ideaCategory"`
	Emojis       []string          `json:"emojis"`
}

// ideaCategories is every area, in the order they are shown.
var ideaCategories = []IdeaCategory{
	{"mail", []string{"📬", "📥", "✉️", "🧹", "🔕", "📨"}},
	{"money", []string{"💰", "💳", "🧾", "📈", "🏦", "💵"}},
	{"paperwork", []string{"📄", "🗂️", "📋", "✍️", "🪪", "📑"}},
	{"home", []string{"🏠", "🔧", "🧰", "📦", "🔌", "🪴"}},
	{"family", []string{"👪", "🎂", "🧸", "🎁", "💬", "🐾"}},
	{"travel", []string{"✈️", "🧳", "🗺️", "🏨", "🚆", "🚗"}},
	{"shopping", []string{"🛒", "🏷️", "📦", "🔁", "🛍️", "🧾"}},
	{"health", []string{"🩺", "💊", "🏃", "🥗", "😴", "🦷"}},
	{"work", []string{"💼", "🗓️", "✅", "📊", "🧑‍💻", "📝"}},
	{"fun", []string{"🎟️", "🎶", "🎲", "📚", "🎬", "🏞️"}},
	{"assistant", []string{"✨", "🧭", "⚙️", "💡", "🔗", "🧠"}},
}

// IdeaCategories is every area, in the order they are shown.
func IdeaCategories() []IdeaCategory {
	return slices.Clone(ideaCategories)
}

// IdeaCategoryOf is the area with this name, and whether there is one.
func IdeaCategoryOf(name AgentIdeaCategory) (IdeaCategory, bool) {
	for _, category := range ideaCategories {
		if category.IdeaCategory == name {
			return category, true
		}
	}
	return IdeaCategory{}, false
}

// AgentIdeaEvidence is one thing in the person's data that prompted a
// personal idea.
type AgentIdeaEvidence struct {
	// EvidenceKind is message (a mailbox item), page (a memory page, by
	// its path) or conversation.
	EvidenceKind string `json:"evidenceKind"`
	// EvidenceID is the item's id, the page's path, or the conversation's
	// id.
	EvidenceID string `json:"evidenceId"`
	// EvidenceSummary says in a few words what it is.
	EvidenceSummary string `json:"evidenceSummary"`
}

// AgentIdea is one idea of one person's agent.
type AgentIdea struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	// IdeaKey names the idea for the agent: the catalog's key, or one made
	// for a personal idea.
	IdeaKey      string            `json:"ideaKey"`
	IdeaKind     AgentIdeaKind     `json:"ideaKind"`
	IdeaCategory AgentIdeaCategory `json:"ideaCategory"`
	Emoji        string            `json:"emoji"`

	// Headline is the offer in a line; Body what happens and where the
	// agent stops to ask.
	Headline string `json:"headline"`
	Body     string `json:"body"`

	// OpeningRequest is what the person would say to start it, put in the
	// reply box of the conversation that starting it opens.
	OpeningRequest string `json:"openingRequest"`

	// NeededToolNames are the tools it needs; a trailing * is any tool
	// whose name begins so.
	NeededToolNames []string `json:"neededToolNames"`

	// Evidence and SuggestionReason say why this person, for a personal
	// idea.
	Evidence         []AgentIdeaEvidence `json:"evidence"`
	SuggestionReason string              `json:"suggestionReason"`

	IdeaStatus AgentIdeaStatus `json:"ideaStatus"`

	// ExpiredReason is why an expired idea is not on offer, and empty for
	// an idea in any other status. An expired catalog idea keeps the reason
	// it is not offered now, which may have changed since it expired; one
	// that expired before reasons were kept, or whose key the catalog no
	// longer has, may have none.
	ExpiredReason AgentIdeaExpiredReason `json:"expiredReason"`

	// IsRestoredByPerson is a catalog idea the person opened again after it
	// expired because they already do what it offers. The check that found
	// that no longer expires it: they said they want it anyway, and it stays
	// until they dismiss it or finish it. A missing tool still expires it.
	IsRestoredByPerson bool `json:"isRestoredByPerson"`

	// RankScore orders open ideas, highest first.
	RankScore float64 `json:"rankScore"`

	// StartedConversationID is the conversation that carries it out.
	StartedConversationID string `json:"startedConversationId"`

	CreatedAt  time.Time  `json:"createdAt"`
	ModifiedAt time.Time  `json:"modifiedAt"`
	ShownAt    *time.Time `json:"shownAt" graphapi:"nullable"`
	StartedAt  *time.Time `json:"startedAt" graphapi:"nullable"`
	ClosedAt   *time.Time `json:"closedAt" graphapi:"nullable"`
	ExpiresAt  *time.Time `json:"expiresAt" graphapi:"nullable"`
}
