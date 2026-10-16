package agent_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Messages that arrive over a few days, each with the answer the model gave
// for it, are read and stored the way the extract run does. The follow-ups
// are written at once, one row to a situation, and a message that says the
// thing is done closes it. A sale that ends on a date comes back with
// nothing usable and leaves no trace.
func TestExtractedFollowUpsAreKeptAndClosed(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		created, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId = created.ID
	})

	type openedFollowUp struct {
		key, title, dueOn, amount string
	}
	steps := []struct {
		name   string
		mailId string
		answer string
		// What is on the open list afterwards, nearest day first.
		open []openedFollowUp
		// What the closed one says, by key, when this step closes one.
		closedKey, closedReason string
	}{
		{
			name: "a property tax bill", mailId: "mail-tax",
			answer: `{"events":[],"contacts":[],"follow_ups":[{"kind":"bill","title":"Property tax, first half","payee":"Riverbend County Tax Office","amount":"1,234.50","currency_code":"usd","due_on":"2026-11-01","key":"Riverbend County Tax Office 2026-11-01","resolves":false,"because":"First half is due November 1."}]}`,
			open:   []openedFollowUp{{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"}},
		},
		{
			name: "a shipping notice", mailId: "mail-shipped",
			answer: `{"follow_ups":[{"kind":"parcel","title":"Garden hose from Brightleaf Supply","carrier":"Fast Freight","tracking_number":"TN-48151623","due_on":"2026-10-21","key":"TN-48151623","resolves":false,"because":"Your order has shipped."}]}`,
			open: []openedFollowUp{
				{"parcel:tn-48151623", "Garden hose from Brightleaf Supply", "2026-10-21", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
		{
			name: "the same notice read twice changes nothing", mailId: "mail-shipped",
			answer: `{"follow_ups":[{"kind":"parcel","title":"Garden hose from Brightleaf Supply","carrier":"Fast Freight","tracking_number":"TN-48151623","due_on":"2026-10-21","key":"tn-48151623"}]}`,
			open: []openedFollowUp{
				{"parcel:tn-48151623", "Garden hose from Brightleaf Supply", "2026-10-21", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
		{
			name: "a permission slip", mailId: "mail-slip",
			answer: `{"follow_ups":[{"kind":"deadline","title":"Return the field trip permission slip","due_on":"2026-10-23","key":"Field trip permission slip 2026-10-23"}]}`,
			open: []openedFollowUp{
				{"parcel:tn-48151623", "Garden hose from Brightleaf Supply", "2026-10-21", ""},
				{"deadline:field trip permission slip 2026-10-23", "Return the field trip permission slip", "2026-10-23", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
		{
			name: "a street fair", mailId: "mail-fair",
			answer: `{"follow_ups":[{"kind":"event","title":"Maple Lane street fair","due_on":"2026-10-24","place":"Maple Lane","key":"maplelane.example 2026-10-24"}]}`,
			open: []openedFollowUp{
				{"parcel:tn-48151623", "Garden hose from Brightleaf Supply", "2026-10-21", ""},
				{"deadline:field trip permission slip 2026-10-23", "Return the field trip permission slip", "2026-10-23", ""},
				{"event:maplelane.example 2026-10-24", "Maple Lane street fair", "2026-10-24", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
		{
			name: "a sale that ends Friday", mailId: "mail-sale",
			answer: `{"events":[],"contacts":[],"follow_ups":[]}`,
			open: []openedFollowUp{
				{"parcel:tn-48151623", "Garden hose from Brightleaf Supply", "2026-10-21", ""},
				{"deadline:field trip permission slip 2026-10-23", "Return the field trip permission slip", "2026-10-23", ""},
				{"event:maplelane.example 2026-10-24", "Maple Lane street fair", "2026-10-24", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
		{
			name: "a sale the model wrongly called a bill is refused", mailId: "mail-sale-two",
			answer: `{"follow_ups":[{"kind":"bill","title":"Sale ends Friday","payee":"Brightleaf Supply","amount":"","due_on":"2026-10-23","key":"brightleaf sale"},{"kind":"coupon","title":"Ten percent off","due_on":"2026-10-23","key":"coupon"},{"kind":"deadline","title":"Offer ends","due_on":"Friday","key":"offer"}]}`,
			open: []openedFollowUp{
				{"parcel:tn-48151623", "Garden hose from Brightleaf Supply", "2026-10-21", ""},
				{"deadline:field trip permission slip 2026-10-23", "Return the field trip permission slip", "2026-10-23", ""},
				{"event:maplelane.example 2026-10-24", "Maple Lane street fair", "2026-10-24", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
		{
			name: "the delivery notice closes the parcel", mailId: "mail-delivered",
			answer: `{"follow_ups":[{"kind":"parcel","tracking_number":"TN-48151623","key":"TN-48151623","resolves":true,"because":"Your package was delivered."}]}`,
			open: []openedFollowUp{
				{"deadline:field trip permission slip 2026-10-23", "Return the field trip permission slip", "2026-10-23", ""},
				{"event:maplelane.example 2026-10-24", "Maple Lane street fair", "2026-10-24", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
			closedKey: "parcel:tn-48151623", closedReason: "Your package was delivered.",
		},
		{
			name: "a delivery notice for a parcel nobody was told about makes nothing", mailId: "mail-stranger",
			answer: `{"follow_ups":[{"kind":"parcel","tracking_number":"TN-99999999","key":"TN-99999999","resolves":true,"because":"Delivered."}]}`,
			open: []openedFollowUp{
				{"deadline:field trip permission slip 2026-10-23", "Return the field trip permission slip", "2026-10-23", ""},
				{"event:maplelane.example 2026-10-24", "Maple Lane street fair", "2026-10-24", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
		{
			name: "a late notice for the delivered parcel does not reopen it", mailId: "mail-late",
			answer: `{"follow_ups":[{"kind":"parcel","title":"Garden hose","tracking_number":"TN-48151623","due_on":"2026-10-21","key":"TN-48151623"}]}`,
			open: []openedFollowUp{
				{"deadline:field trip permission slip 2026-10-23", "Return the field trip permission slip", "2026-10-23", ""},
				{"event:maplelane.example 2026-10-24", "Maple Lane street fair", "2026-10-24", ""},
				{"bill:riverbend county tax office 2026-11-01", "Property tax, first half", "2026-11-01", "1234.5000"},
			},
		},
	}

	for _, step := range steps {
		answer, err := llm.Extract[agent.ExtractAnswer](step.answer)
		if err != nil {
			t.Fatalf("%s: the answer is not an object: %s", step.name, err)
		}
		findings := agent.InterpretFollowUps(&answer, agentId, step.mailId)
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if err := agent.StoreFollowUps(tx, findings); err != nil {
				t.Fatalf("%s: StoreFollowUps: %s", step.name, err)
			}
			open, err := tx.ListOpenAgentFollowUps(agentId)
			if err != nil {
				t.Fatalf("%s: ListOpenAgentFollowUps: %s", step.name, err)
			}
			if len(open) != len(step.open) {
				t.Fatalf("%s: open follow-ups: %+v", step.name, open)
			}
			for index, want := range step.open {
				got := open[index]
				if got.FollowUpKey != want.key || got.FollowUpTitle != want.title || got.DueOn != want.dueOn || got.Amount != want.amount {
					t.Errorf("%s: position %d: want %+v got %+v", step.name, index, want, got)
				}
			}
			if step.closedKey == "" {
				return
			}
			all, err := tx.ListAgentFollowUps(agentId, 50)
			if err != nil {
				t.Fatalf("%s: ListAgentFollowUps: %s", step.name, err)
			}
			for _, followUp := range all {
				if followUp.FollowUpKey == step.closedKey && (followUp.FollowUpState != models.AgentFollowUpClosed || followUp.ClosedReason != step.closedReason) {
					t.Errorf("%s: %+v", step.name, followUp)
				}
			}
		})
	}

	// The bill kept what a bill needs, and the parcel the messages that made
	// it.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		all, err := tx.ListAgentFollowUps(agentId, 50)
		if err != nil {
			t.Fatalf("ListAgentFollowUps: %s", err)
		}
		if len(all) != 4 {
			t.Fatalf("four situations in all, none for the sales: %+v", all)
		}
		for _, followUp := range all {
			switch followUp.FollowUpKind {
			case models.AgentFollowUpBill:
				if followUp.Payee != "Riverbend County Tax Office" || followUp.CurrencyCode != "USD" {
					t.Errorf("the bill: %+v", followUp)
				}
			case models.AgentFollowUpParcel:
				want := []string{"mail-shipped", "mail-delivered", "mail-late"}
				if followUp.Carrier != "Fast Freight" || len(followUp.SourceItemIDs) != len(want) {
					t.Fatalf("the parcel: %+v", followUp)
				}
				for index, mailId := range want {
					if followUp.SourceItemIDs[index] != mailId {
						t.Errorf("the parcel's messages: %v", followUp.SourceItemIDs)
					}
				}
			}
		}
	})
}

// The answer is read the way a rule would read it, whatever the model wrote.
func TestInterpretFollowUpsRefusesWhatIsNotUsable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		answer string
		count  int
	}{
		{"a bill whose amount is words", `{"follow_ups":[{"kind":"bill","title":"Water","due_on":"2026-11-02","amount":"see attached","key":"water"}]}`, 0},
		{"a bill with a zero amount", `{"follow_ups":[{"kind":"bill","title":"Water","due_on":"2026-11-02","amount":0,"key":"water"}]}`, 0},
		{"a bill with a number for an amount", `{"follow_ups":[{"kind":"bill","title":"Water","due_on":"2026-11-02","amount":48.2,"key":"water"}]}`, 1},
		{"a bill with a currency sign", `{"follow_ups":[{"kind":"bill","title":"Water","due_on":"2026-11-02","amount":"$1,048.20","key":"water"}]}`, 1},
		{"a deadline with no day", `{"follow_ups":[{"kind":"deadline","title":"Form","key":"form"}]}`, 0},
		{"an event with a day in words", `{"follow_ups":[{"kind":"event","title":"Fair","due_on":"next Saturday","key":"fair"}]}`, 0},
		{"a parcel with no day is fine", `{"follow_ups":[{"kind":"parcel","title":"Hose","tracking_number":"TN-1"}]}`, 1},
		{"no key and no tracking number", `{"follow_ups":[{"kind":"event","title":"Fair","due_on":"2026-10-24"}]}`, 0},
		{"no title", `{"follow_ups":[{"kind":"event","due_on":"2026-10-24","key":"fair"}]}`, 0},
		{"a kind in capitals", `{"follow_ups":[{"kind":"Event","title":"Fair","due_on":"2026-10-24","key":"fair"}]}`, 1},
		{"no follow_ups at all", `{"events":[],"contacts":[]}`, 0},
	}
	for _, test := range cases {
		answer, err := llm.Extract[agent.ExtractAnswer](test.answer)
		if err != nil {
			t.Fatalf("%s: %s", test.name, err)
		}
		if findings := agent.InterpretFollowUps(&answer, "agent-1", "mail-1"); len(findings) != test.count {
			t.Errorf("%s: want %d got %+v", test.name, test.count, findings)
		}
	}
}
