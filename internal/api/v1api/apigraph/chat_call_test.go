package apigraph

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/call"
	"github.com/ziyan/teanode/internal/models"
)

const chatCallTestFields = `{ id conversationId chatTitle isGroup startedByName isGoing endReason askerRingState isAskerOn answeredAt
	transcriptionAskedByUserId members { userId name ringState isOn isVideoOn transcriptionConsent } }`

// callWorld is a chat world whose server has a media server.
func newCallWorld(t *testing.T) *chatWorld {
	t.Helper()
	world := newChatWorld(t)
	server, err := call.New(call.Settings{PublicAddress: "127.0.0.1", UDPPort: 0, TCPPort: 0})
	if err != nil {
		t.Fatalf("call.New: %s", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	world.graph.settings.Calls = server
	world.graph.settings.BackendID = "call-test"
	return world
}

func (self *chatWorld) callView(t *testing.T, as *models.User, mutation string) map[string]any {
	t.Helper()
	name := mutation[:strings.Index(mutation, "(")]
	return self.call(t, as, "mutation { "+mutation+" "+chatCallTestFields+" }")[name].(map[string]any)
}

func memberOf(view map[string]any, user *models.User) map[string]any {
	for _, each := range view["members"].([]any) {
		if member := each.(map[string]any); member["userId"] == user.ID {
			return member
		}
	}
	return nil
}

// A call rings the other person, who sees it going in the chat and joins;
// somebody outside the chat cannot see it; it ends when the last person
// leaves, and its line in the chat reads it.
func TestACallRingsIsJoinedAndEndsWhenEverybodyLeaves(t *testing.T) {
	world := newCallWorld(t)
	world.shareCircle(t, world.frank, "Neighbours", world.grace)
	frankConversation := world.openDirect(t, world.frank, world.grace)
	graceConversation := world.openDirect(t, world.grace, world.frank)

	started := world.callView(t, world.frank, fmt.Sprintf(`StartChatCall(conversationId: %q)`, frankConversation))
	callId := started["id"].(string)
	if started["isGoing"] != true || started["isAskerOn"] != true || started["askerRingState"] != "none" || started["conversationId"] != frankConversation {
		t.Fatalf("the call starts as %v", started)
	}
	if member := memberOf(started, world.grace); member == nil || member["ringState"] != "ringing" || member["isOn"] != false {
		t.Errorf("Grace is %v", member)
	}
	if again := world.callView(t, world.frank, fmt.Sprintf(`StartChatCall(conversationId: %q)`, frankConversation)); again["id"] != callId {
		t.Errorf("a second start made another call: %v", again)
	}

	going := world.call(t, world.grace, `query { ListGoingChatCalls `+chatCallTestFields+` }`)["ListGoingChatCalls"].([]any)
	if len(going) != 1 || going[0].(map[string]any)["askerRingState"] != "ringing" || going[0].(map[string]any)["conversationId"] != graceConversation {
		t.Fatalf("Grace's going calls: %v", going)
	}
	banner := world.call(t, world.grace, fmt.Sprintf(`query { ReadGoingChatCall(conversationId: %q) %s }`, graceConversation, chatCallTestFields))["ReadGoingChatCall"]
	if banner == nil || banner.(map[string]any)["id"] != callId || banner.(map[string]any)["startedByName"] != world.frank.Name {
		t.Errorf("Grace's chat shows %v", banner)
	}
	if refused := world.refused(t, world.heidi, fmt.Sprintf(`query { ReadChatCall(callId: %q) { call { id } } }`, callId)); refused == "" {
		t.Error("somebody outside the chat read the call")
	}

	joined := world.callView(t, world.grace, fmt.Sprintf(`JoinChatCall(callId: %q)`, callId))
	if joined["askerRingState"] != "answered" || joined["isAskerOn"] != true || joined["answeredAt"] == nil {
		t.Fatalf("after Grace joined: %v", joined)
	}
	if refused := world.refused(t, world.grace, fmt.Sprintf(`mutation { JoinChatCallMedia(callId: %q, offerSdp: "not an offer") { answerSdp } }`, callId)); refused == "" {
		t.Error("a device joined with no offer")
	}
	video := world.callView(t, world.grace, fmt.Sprintf(`SetChatCallVideo(callId: %q, isVideoOn: true)`, callId))
	if memberOf(video, world.grace)["isVideoOn"] != true {
		t.Errorf("Grace's camera: %v", video)
	}

	if left := world.callView(t, world.grace, fmt.Sprintf(`LeaveChatCall(callId: %q)`, callId)); left["isGoing"] != true {
		t.Fatalf("the call ended with Frank still on it: %v", left)
	}
	ended := world.callView(t, world.frank, fmt.Sprintf(`LeaveChatCall(callId: %q)`, callId))
	if ended["isGoing"] != false || ended["endReason"] != "ended" {
		t.Fatalf("after the last left: %v", ended)
	}
	if banner := world.call(t, world.grace, fmt.Sprintf(`query { ReadGoingChatCall(conversationId: %q) { id } }`, graceConversation))["ReadGoingChatCall"]; banner != nil {
		t.Errorf("an ended call is still shown going: %v", banner)
	}

	read := world.call(t, world.grace, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { messages { id content callId call { id endReason } } } }`, graceConversation))
	var line map[string]any
	for _, each := range read["ReadAgentConversation"].(map[string]any)["messages"].([]any) {
		if message := each.(map[string]any); message["callId"] == callId {
			line = message
		}
	}
	if line == nil || line["content"] != "" || line["call"].(map[string]any)["endReason"] != "ended" {
		t.Errorf("the call's line in Grace's chat: %v", line)
	}
	listed := world.call(t, world.grace, fmt.Sprintf(`query { ListChatCalls(conversationId: %q) { id endReason } }`, graceConversation))["ListChatCalls"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["id"] != callId {
		t.Errorf("Grace's list of calls: %v", listed)
	}
}

// A call nobody answers stops ringing after calls.ringSeconds and ends
// missed; one the only person rung declines ends declined.
func TestACallNobodyAnswersIsMissedAndOneDeclinedEnds(t *testing.T) {
	world := newCallWorld(t)
	world.shareCircle(t, world.frank, "Neighbours", world.grace)
	frankConversation := world.openDirect(t, world.frank, world.grace)

	started := world.callView(t, world.frank, fmt.Sprintf(`StartChatCall(conversationId: %q)`, frankConversation))
	if err := world.graph.TickChatCalls(context.Background(), time.Now()); err != nil {
		t.Fatalf("TickChatCalls: %s", err)
	}
	if still := world.call(t, world.grace, `query { ListGoingChatCalls { id } }`)["ListGoingChatCalls"].([]any); len(still) != 1 {
		t.Fatalf("a call stopped ringing at once: %v", still)
	}
	if err := world.graph.TickChatCalls(context.Background(), time.Now().Add(2*time.Minute)); err != nil {
		t.Fatalf("TickChatCalls: %s", err)
	}
	missed := world.call(t, world.grace, fmt.Sprintf(`query { ReadChatCall(callId: %q) { call %s } }`, started["id"], chatCallTestFields))["ReadChatCall"].(map[string]any)["call"].(map[string]any)
	if missed["isGoing"] != false || missed["endReason"] != "missed" || missed["askerRingState"] != "missed" {
		t.Fatalf("after the ringing stopped: %v", missed)
	}

	again := world.callView(t, world.frank, fmt.Sprintf(`StartChatCall(conversationId: %q)`, frankConversation))
	declined := world.callView(t, world.grace, fmt.Sprintf(`DeclineChatCall(callId: %q)`, again["id"]))
	if declined["isGoing"] != false || declined["endReason"] != "declined" || declined["askerRingState"] != "declined" {
		t.Fatalf("after Grace declined: %v", declined)
	}
}

// A transcript is turned on by somebody on the call, for everybody on it, and
// the first to turn it on stays its asker.
func TestATranscriptIsTurnedOnForEverybody(t *testing.T) {
	world := newCallWorld(t)
	world.shareCircle(t, world.frank, "Neighbours", world.grace)
	frankConversation := world.openDirect(t, world.frank, world.grace)
	started := world.callView(t, world.frank, fmt.Sprintf(`StartChatCall(conversationId: %q)`, frankConversation))
	callId := started["id"].(string)
	if refused := world.refused(t, world.grace, fmt.Sprintf(`mutation { AskChatCallTranscription(callId: %q) { id } }`, callId)); refused == "" {
		t.Error("somebody not on the call asked for a transcript")
	}
	world.callView(t, world.grace, fmt.Sprintf(`JoinChatCall(callId: %q)`, callId))
	asked := world.callView(t, world.frank, fmt.Sprintf(`AskChatCallTranscription(callId: %q)`, callId))
	if asked["transcriptionAskedByUserId"] != world.frank.ID || memberOf(asked, world.grace)["transcriptionConsent"] != "yes" {
		t.Fatalf("after Frank asked: %v", asked)
	}
	again := world.callView(t, world.grace, fmt.Sprintf(`AskChatCallTranscription(callId: %q)`, callId))
	if again["transcriptionAskedByUserId"] != world.frank.ID {
		t.Errorf("a second ask took it over: %v", again)
	}
}

// A device whose connection fails takes its person off the call, and the
// call ends when that leaves nobody on it.
func TestALostDeviceTakesItsPersonOffTheCall(t *testing.T) {
	world := newCallWorld(t)
	world.shareCircle(t, world.frank, "Neighbours", world.grace)
	frankConversation := world.openDirect(t, world.frank, world.grace)
	started := world.callView(t, world.frank, fmt.Sprintf(`StartChatCall(conversationId: %q)`, frankConversation))
	callId := started["id"].(string)
	world.callView(t, world.grace, fmt.Sprintf(`JoinChatCall(callId: %q)`, callId))

	world.graph.DeviceLost(callId, world.grace.ID)
	read := func() map[string]any {
		return world.call(t, world.frank, fmt.Sprintf(`query { ReadChatCall(callId: %q) { call %s } }`, callId, chatCallTestFields))["ReadChatCall"].(map[string]any)["call"].(map[string]any)
	}
	if after := read(); after["isGoing"] != true || memberOf(after, world.grace)["isOn"] != false {
		t.Fatalf("after Grace's device was lost: %v", after)
	}
	world.graph.DeviceLost(callId, world.frank.ID)
	if after := read(); after["isGoing"] != false || after["endReason"] != "ended" {
		t.Fatalf("after the last device was lost: %v", after)
	}
}
