package chat

import (
	"slices"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Agents in a chat. A message in a chat goes to the people in it. It starts a
// turn of an agent only when its author addresses that agent, by choosing it
// from the composer's @ or by replying to one of its messages, so an agent's
// name in ordinary prose starts nothing. DecideAgentTurns is the whole rule,
// kept apart from the database and the model so that every branch of it is a
// unit test; docs/subsystems/conversations.md says why it is this way.

// AgentTurnKind says whose turn it is and so what it may do.
type AgentTurnKind string

// The two turns an addressed agent can take.
const (
	// AgentTurnOwner is the agent answering its own owner: its usual tools,
	// and its cards, which only the owner sees.
	AgentTurnOwner AgentTurnKind = "owner"

	// AgentTurnMember is the agent answering somebody else in the chat whom
	// its owner allowed to ask it here: held to what everybody in the chat
	// may see, with no card and no tool of the owner's.
	AgentTurnMember AgentTurnKind = "member"
)

// AgentSeat is an agent in a chat, as the decision sees it: the member whose
// agent it is, and whether it answers and may be asked by the others.
type AgentSeat struct {
	// OwnerUserID is the member the agent belongs to, and OwnerName what they
	// are called.
	OwnerUserID string
	OwnerName   string

	// AgentID and AgentName say which agent it is and what it is called.
	AgentID   string
	AgentName string

	// IsAgentActive is false for an agent that is switched off, or whose
	// owner may not use agents.
	IsAgentActive bool

	// IsAgentAskableByOthers is the owner's switch for this chat.
	IsAgentAskableByOthers bool

	// IsRemoved says the owner is no longer in the chat.
	IsRemoved bool
}

// AgentMessageAuthor is who wrote the message that addresses agents.
type AgentMessageAuthor struct {
	// UserID is the author, a member of the chat.
	UserID string

	// IsAgent says an agent wrote it, speaking for the author. Nothing an
	// agent writes starts a turn, of any agent.
	IsAgent bool

	// CanUseAgents says the author holds agent:use. Without it they cannot
	// ask anybody else's agent, whatever its owner allowed the others, since a
	// member turn spends the owner's agent on their word.
	CanUseAgents bool
}

// AgentTurn is a turn to start.
type AgentTurn struct {
	AgentID       string
	AgentName     string
	OwnerUserID   string
	AgentTurnKind AgentTurnKind
}

// AgentNote is what the author is told, privately, about an agent they
// addressed that starts no turn.
type AgentNote struct {
	AgentID   string
	AgentName string

	// OwnerName is what the agent's owner is called, told for the note that
	// only the owner can ask it.
	OwnerName string

	// NoteKind is one of the chat notes, and the note's detail is AgentName.
	NoteKind models.AgentNoteKind
}

// AgentDecision is what a message that addresses agents does.
type AgentDecision struct {
	Turns []AgentTurn
	Notes []AgentNote
}

// MostAgentsAddressed is how many agents one message can start at once; the
// rest are ignored. A room has few agents and a long list would only be a way
// to spend somebody's budget.
const MostAgentsAddressed = 4

// DecideAgentTurns says which turns a message starts and what its author is
// told about the agents it started none of. The rules, and no others:
//
//   - A message an agent wrote starts nothing, and neither does one by
//     somebody who is not in the chat.
//   - An agent that is addressed twice is addressed once, and an id that is
//     not an agent of anybody in the chat is told to the author as absent.
//   - The author's own agent, addressed: an owner turn, unless it is switched
//     off, which starts nothing and tells the author it is off.
//   - Another member's agent, addressed: if its owner has not allowed the
//     others to ask it here, or the author does not hold agent:use, nothing
//     starts and the author is told only its owner can ask it; if they have
//     and it is switched off, nothing starts and the author is told so;
//     otherwise a member turn.
func DecideAgentTurns(author AgentMessageAuthor, addressedAgentIds []string, seats []AgentSeat) AgentDecision {
	decision := AgentDecision{Turns: []AgentTurn{}, Notes: []AgentNote{}}
	if author.IsAgent || author.UserID == "" || !isInRoom(author.UserID, seats) {
		return decision
	}
	seen := []string{}
	for _, agentId := range addressedAgentIds {
		agentId = strings.TrimSpace(agentId)
		if agentId == "" || slices.Contains(seen, agentId) {
			continue
		}
		if len(seen) == MostAgentsAddressed {
			break
		}
		seen = append(seen, agentId)
		seat := seatOf(agentId, seats)
		switch {
		case seat == nil || seat.IsRemoved:
			decision.Notes = append(decision.Notes, AgentNote{AgentID: agentId, NoteKind: models.NoteChatAgentAbsent})
		case seat.OwnerUserID == author.UserID && !seat.IsAgentActive:
			decision.Notes = append(decision.Notes, noteFor(seat, models.NoteChatAgentOff))
		case seat.OwnerUserID == author.UserID:
			decision.Turns = append(decision.Turns, turnFor(seat, AgentTurnOwner))
		case !seat.IsAgentAskableByOthers || !author.CanUseAgents:
			decision.Notes = append(decision.Notes, noteFor(seat, models.NoteChatAgentOwnerOnly))
		case !seat.IsAgentActive:
			decision.Notes = append(decision.Notes, noteFor(seat, models.NoteChatAgentOff))
		default:
			decision.Turns = append(decision.Turns, turnFor(seat, AgentTurnMember))
		}
	}
	return decision
}

func isInRoom(userId string, seats []AgentSeat) bool {
	for _, seat := range seats {
		if seat.OwnerUserID == userId && !seat.IsRemoved {
			return true
		}
	}
	return false
}

func seatOf(agentId string, seats []AgentSeat) *AgentSeat {
	for index := range seats {
		if seats[index].AgentID == agentId {
			return &seats[index]
		}
	}
	return nil
}

func turnFor(seat *AgentSeat, agentTurnKind AgentTurnKind) AgentTurn {
	return AgentTurn{AgentID: seat.AgentID, AgentName: seat.AgentName, OwnerUserID: seat.OwnerUserID, AgentTurnKind: agentTurnKind}
}

func noteFor(seat *AgentSeat, noteKind models.AgentNoteKind) AgentNote {
	return AgentNote{AgentID: seat.AgentID, AgentName: seat.AgentName, OwnerName: seat.OwnerName, NoteKind: noteKind}
}

// SeatsOf are the agents of a chat's members who are still in it, and of
// those who are not, for the decision. isActive says whether a member's agent
// answers: it is on, and its owner may use agents.
func SeatsOf(members []*models.ChatMember, isActive func(member *models.ChatMember) bool) []AgentSeat {
	seats := make([]AgentSeat, 0, len(members))
	for _, member := range members {
		if member.UserID == "" || member.AgentID == "" {
			continue
		}
		seats = append(seats, AgentSeat{
			OwnerUserID:            member.UserID,
			OwnerName:              member.Name,
			AgentID:                member.AgentID,
			AgentName:              member.AgentName,
			IsAgentActive:          member.IsAgentActive && (isActive == nil || isActive(member)),
			IsAgentAskableByOthers: member.IsAgentAskableByOthers,
			IsRemoved:              member.IsRemoved,
		})
	}
	return seats
}
