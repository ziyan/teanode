package chat

import (
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The room used throughout: Ann's agent is on and nobody else may ask it,
// Ben's is on and he allowed the others, Cy's is off and he allowed them,
// Dee's is off and she did not, and Eve left.
func room() []AgentSeat {
	return []AgentSeat{
		{OwnerUserID: "ann", AgentID: "agent-ann", AgentName: "Annie", IsAgentActive: true},
		{OwnerUserID: "ben", AgentID: "agent-ben", AgentName: "Benny", IsAgentActive: true, IsAgentAskableByOthers: true},
		{OwnerUserID: "cy", AgentID: "agent-cy", AgentName: "Cyrus", IsAgentAskableByOthers: true},
		{OwnerUserID: "dee", AgentID: "agent-dee", AgentName: "Dee-Dee"},
		{OwnerUserID: "eve", AgentID: "agent-eve", AgentName: "Evie", IsAgentActive: true, IsAgentAskableByOthers: true, IsRemoved: true},
	}
}

func TestDecideAgentTurns(t *testing.T) {
	person := func(userId string) AgentMessageAuthor { return AgentMessageAuthor{UserID: userId, CanUseAgents: true} }
	cases := []struct {
		name      string
		author    AgentMessageAuthor
		addressed []string
		turns     []AgentTurn
		notes     []AgentNote
	}{
		{
			name:      "an owner addressing their own agent starts an owner turn",
			author:    person("ann"),
			addressed: []string{"agent-ann"},
			turns:     []AgentTurn{{AgentID: "agent-ann", AgentName: "Annie", OwnerUserID: "ann", AgentTurnKind: AgentTurnOwner}},
		},
		{
			name:      "an owner addressing their own agent when it is off is told it is off",
			author:    person("dee"),
			addressed: []string{"agent-dee"},
			notes:     []AgentNote{{AgentID: "agent-dee", AgentName: "Dee-Dee", NoteKind: models.NoteChatAgentOff}},
		},
		{
			name:      "somebody else's agent that was not made askable is only its owner's",
			author:    person("ben"),
			addressed: []string{"agent-ann"},
			notes:     []AgentNote{{AgentID: "agent-ann", AgentName: "Annie", NoteKind: models.NoteChatAgentOwnerOnly}},
		},
		{
			name:      "somebody else's agent that is askable starts a member turn",
			author:    person("ann"),
			addressed: []string{"agent-ben"},
			turns:     []AgentTurn{{AgentID: "agent-ben", AgentName: "Benny", OwnerUserID: "ben", AgentTurnKind: AgentTurnMember}},
		},
		{
			name:      "an askable agent that is switched off starts nothing and says so",
			author:    person("ann"),
			addressed: []string{"agent-cy"},
			notes:     []AgentNote{{AgentID: "agent-cy", AgentName: "Cyrus", NoteKind: models.NoteChatAgentOff}},
		},
		{
			name:      "an agent that is both off and not askable says only its owner can ask, not that it is off",
			author:    person("ann"),
			addressed: []string{"agent-dee"},
			notes:     []AgentNote{{AgentID: "agent-dee", AgentName: "Dee-Dee", NoteKind: models.NoteChatAgentOwnerOnly}},
		},
		{
			name:      "the agent of somebody who left is absent, askable or not",
			author:    person("ann"),
			addressed: []string{"agent-eve"},
			notes:     []AgentNote{{AgentID: "agent-eve", NoteKind: models.NoteChatAgentAbsent}},
		},
		{
			name:      "an id that is nobody's agent here is absent",
			author:    person("ann"),
			addressed: []string{"agent-stranger"},
			notes:     []AgentNote{{AgentID: "agent-stranger", NoteKind: models.NoteChatAgentAbsent}},
		},
		{
			name:      "an agent's message starts nothing, of any agent",
			author:    AgentMessageAuthor{UserID: "ann", IsAgent: true, CanUseAgents: true},
			addressed: []string{"agent-ben", "agent-ann"},
		},
		{
			name:      "somebody who is not in the chat starts nothing",
			author:    person("stranger"),
			addressed: []string{"agent-ben"},
		},
		{
			name:      "a member who left starts nothing",
			author:    person("eve"),
			addressed: []string{"agent-ben"},
		},
		{
			name:   "nobody addressed starts nothing",
			author: person("ann"),
		},
		{
			name:      "a blank id is ignored",
			author:    person("ann"),
			addressed: []string{"", "  "},
		},
		{
			name:      "an agent addressed twice is addressed once",
			author:    person("ann"),
			addressed: []string{"agent-ann", "agent-ann"},
			turns:     []AgentTurn{{AgentID: "agent-ann", AgentName: "Annie", OwnerUserID: "ann", AgentTurnKind: AgentTurnOwner}},
		},
		{
			name:      "somebody without agent:use cannot ask another member's askable agent",
			author:    AgentMessageAuthor{UserID: "ann"},
			addressed: []string{"agent-ben"},
			notes:     []AgentNote{{AgentID: "agent-ben", AgentName: "Benny", NoteKind: models.NoteChatAgentOwnerOnly}},
		},
		{
			name:      "two agents addressed start two turns, in the order named",
			author:    person("ann"),
			addressed: []string{"agent-ben", "agent-ann"},
			turns: []AgentTurn{
				{AgentID: "agent-ben", AgentName: "Benny", OwnerUserID: "ben", AgentTurnKind: AgentTurnMember},
				{AgentID: "agent-ann", AgentName: "Annie", OwnerUserID: "ann", AgentTurnKind: AgentTurnOwner},
			},
		},
		{
			name:      "a turn and a note can come from one message",
			author:    person("ann"),
			addressed: []string{"agent-ann", "agent-dee"},
			turns:     []AgentTurn{{AgentID: "agent-ann", AgentName: "Annie", OwnerUserID: "ann", AgentTurnKind: AgentTurnOwner}},
			notes:     []AgentNote{{AgentID: "agent-dee", AgentName: "Dee-Dee", NoteKind: models.NoteChatAgentOwnerOnly}},
		},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			decision := DecideAgentTurns(each.author, each.addressed, room())
			if len(each.turns) == 0 {
				each.turns = []AgentTurn{}
			}
			if len(each.notes) == 0 {
				each.notes = []AgentNote{}
			}
			if !reflect.DeepEqual(decision.Turns, each.turns) {
				t.Errorf("turns: got %+v, want %+v", decision.Turns, each.turns)
			}
			if !reflect.DeepEqual(decision.Notes, each.notes) {
				t.Errorf("notes: got %+v, want %+v", decision.Notes, each.notes)
			}
		})
	}
}

func TestDecideAgentTurnsIgnoresAgentsPastTheMost(t *testing.T) {
	seats := []AgentSeat{{OwnerUserID: "ann", AgentID: "a0", IsAgentActive: true}}
	addressed := []string{"a0"}
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5"} {
		seats = append(seats, AgentSeat{OwnerUserID: "u-" + id, AgentID: id, IsAgentActive: true, IsAgentAskableByOthers: true})
		addressed = append(addressed, id)
	}
	decision := DecideAgentTurns(AgentMessageAuthor{UserID: "ann", CanUseAgents: true}, addressed, seats)
	if len(decision.Turns) != MostAgentsAddressed {
		t.Fatalf("got %d turns, want %d", len(decision.Turns), MostAgentsAddressed)
	}
	for _, turn := range decision.Turns {
		if turn.AgentID == "a5" || turn.AgentID == "a4" {
			t.Errorf("%s is past the most and should have been ignored", turn.AgentID)
		}
	}
}

func TestSeatsOfNeedsTheOwnerToMayUseAgents(t *testing.T) {
	members := []*models.ChatMember{
		{UserID: "ann", AgentID: "agent-ann", AgentName: "Annie", IsAgentActive: true},
		{UserID: "ben", AgentID: "agent-ben", AgentName: "Benny", IsAgentActive: true, IsAgentAskableByOthers: true},
		{UserID: "", AgentID: "agent-gone", IsAgentActive: true},
		{UserID: "cy", AgentID: "", IsAgentActive: true},
	}
	seats := SeatsOf(members, func(member *models.ChatMember) bool { return member.UserID != "ben" })
	if len(seats) != 2 {
		t.Fatalf("a deleted account and a member with no agent have no seat: got %+v", seats)
	}
	if !seats[0].IsAgentActive {
		t.Error("ann may use agents and hers is on")
	}
	if seats[1].IsAgentActive {
		t.Error("ben may not use agents, so his is not active however it is switched")
	}
	if !seats[1].IsAgentAskableByOthers {
		t.Error("the switch for the others is carried over")
	}
}
