package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// saidByModel is one streamed round of a scripted model saying text.
func saidByModel(text string) string {
	content, _ := json.Marshal(text)
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, content)
}

// tipCatalogForTest is two invented features, the first already used.
func tipCatalogForTest() []*Tip {
	return []*Tip{
		{TipKey: "kite_flying", Feature: "Flying kites.", Where: "the kite page", IsUsed: func(db.Transaction, *models.Agent, *models.User) (bool, error) { return true, nil }},
		{TipKey: "bread_baking", Feature: "Baking bread.", Where: "the bread page", IsUsed: func(db.Transaction, *models.Agent, *models.User) (bool, error) { return false, nil }},
	}
}

// A model decides whether to give a tip and which, offered only what the
// person does not use; the tip it chose is what the turn is told about,
// and it is recorded. A model that says no tip, names one it was not
// offered, or answers something unreadable leaves the agent quiet.
func TestAModelDecidesWhetherToGiveATipAndWhich(t *testing.T) {
	for _, each := range []struct {
		name         string
		decision     string
		isSpeaking   bool
		wantTieInSay string
	}{
		{"a tip that fits", `{"shouldTip": true, "tipKey": "bread_baking", "tieIn": "they asked about flour twice", "decisionReason": "fits"}`, true, "they asked about flour twice"},
		{"no tip now", `{"shouldTip": false, "tipKey": "", "tieIn": "", "decisionReason": "too soon"}`, false, ""},
		{"a tip it was not offered", `{"shouldTip": true, "tipKey": "kite_flying", "tieIn": "", "decisionReason": "kites"}`, false, ""},
		{"nothing readable", `A tip about bread would be nice.`, false, ""},
	} {
		t.Run(each.name, func(t *testing.T) {
			database, release := dbtest.AcquireDatabase(t)
			defer release()
			provider := scriptedProvider([]string{saidByModel(each.decision)})
			defer provider.Close()
			worker, run := digestSplitWorld(t, database, provider.URL)
			saved := tipCatalog
			defer func() { tipCatalog = saved }()
			tipCatalog = tipCatalogForTest()

			now := time.Now()
			prepared, isSpeaking, err := worker.chooseTip(t.Context(), run, now)
			if err != nil || isSpeaking != each.isSpeaking {
				t.Fatalf("speaking %v, want %v: %v", isSpeaking, each.isSpeaking, err)
			}
			if !isSpeaking {
				return
			}
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				message, err := worker.tipReason().checkIn(t.Context(), tx, run.Agent, run.Owner, now, prepared)
				if err != nil || !strings.Contains(message, "Baking bread.") || !strings.Contains(message, each.wantTieInSay) || !strings.HasPrefix(message, models.SpeakFirstMarker) {
					t.Fatalf("the turn is told about bread, and why: %q %v", message, err)
				}
				tips, _ := tx.ListAgentTips(run.Agent.ID)
				if len(tips) != 1 || tips[0].TipKey != "bread_baking" {
					t.Fatalf("recorded: %+v", tips)
				}
			})
		})
	}
}

// Whether to ask the model at all is decided without one: tips on, the
// introduction over, the person quiet for five minutes, something they do
// not use and were not told, and no decision in the last day, whatever it
// was.
func TestATipDecisionIsMadeAtMostDaily(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{saidByModel("ok")})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	saved := tipCatalog
	defer func() { tipCatalog = saved }()
	tipCatalog = tipCatalogForTest()

	now := time.Now()
	reason := worker.tipReason()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent, _ := tx.GetAgent(run.Agent.ID)
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); isDue {
			t.Fatal("not before the introduction is over")
		}
		if err := tx.MarkAgentSpokeFirst(run.Agent.ID, nil, &now, nil); err != nil {
			t.Fatal(err)
		}
		agent, _ = tx.GetAgent(run.Agent.ID)
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, time.Minute, now); isDue {
			t.Fatal("not while they are busy")
		}
		if isDue, err := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); err != nil || !isDue {
			t.Fatalf("a quiet person with something to try: %v %v", isDue, err)
		}
		if _, err := worker.Enqueue(tx, models.AgentJobSpeakFirst, agent.ID, "", SpeakFirstTip); err != nil {
			t.Fatal(err)
		}
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); isDue {
			t.Fatal("one decision a day")
		}
		if err := tx.AddAgentTip(&models.AgentTip{AgentID: agent.ID, TipKey: "bread_baking"}); err != nil {
			t.Fatal(err)
		}
		if candidates, _ := tipsToGive(tx, agent, run.Owner); len(candidates) != 0 {
			t.Fatalf("nothing left once bread was told: %v", candidates)
		}
	})
}
