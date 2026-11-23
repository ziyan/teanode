package db

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

func TestChangedAuditFields(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		resourceType models.AuditResourceType
		actorKind    models.AuditActorKind
		before       string
		after        string
		isChanged    bool
		wantBefore   string
		wantAfter    string
	}{
		{"a nil list and an empty list are the same", models.AuditResourceAgent, models.AuditActorUser,
			`{"categories":[],"confirm":null}`, `{"categories":null,"confirm":[]}`, false, "", ""},
		{"absent and null are the same", models.AuditResourceAgent, models.AuditActorUser,
			`{"name":"a"}`, `{"name":"a","nickname":null}`, false, "", ""},
		{"a dream finishing is bookkeeping", models.AuditResourceAgent, models.AuditActorSystem,
			`{"name":"a","dreamedAt":"2026-01-01T00:00:00Z","dreamBootstrap":false}`,
			`{"name":"a","dreamedAt":"2026-01-02T00:00:00Z","decayedAt":"2026-01-02T00:00:00Z","dreamBootstrap":true}`, false, "", ""},
		{"a person's change is kept alone, unchanged fields left out", models.AuditResourceAgent, models.AuditActorUser,
			`{"name":"a","enabled":true,"categories":[],"dreamedAt":"2026-01-01T00:00:00Z"}`,
			`{"name":"b","enabled":true,"categories":null,"dreamedAt":"2026-01-02T00:00:00Z"}`, true, `{"name":"a"}`, `{"name":"b"}`},
		{"a list that really changed is recorded", models.AuditResourceAgent, models.AuditActorUser,
			`{"confirm":[]}`, `{"confirm":["send"]}`, true, `{}`, `{"confirm":["send"]}`},
		{"the system's receipt matches are bookkeeping", models.AuditResourceFinanceReceipt, models.AuditActorSystem,
			`{"receiptMatches":[]}`, `{"receiptMatches":[{"matchedAmount":"1.00"}]}`, false, "", ""},
		{"a person's receipt matches are kept", models.AuditResourceFinanceReceipt, models.AuditActorUser,
			`{"receiptMatches":[]}`, `{"receiptMatches":[{"matchedAmount":"1.00"}]}`, true, `{}`, `{"receiptMatches":[{"matchedAmount":"1.00"}]}`},
		{"an encrypted item's revision is kept", models.AuditResourcePasswordItem, models.AuditActorSystem,
			`{"revision":1}`, `{"revision":2}`, true, `{"revision":1}`, `{"revision":2}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			gotBefore, gotAfter, isChanged, err := changedAuditFields(testCase.resourceType, testCase.actorKind, []byte(testCase.before), []byte(testCase.after))
			if err != nil {
				t.Fatalf("changedAuditFields: %s", err)
			}
			if isChanged != testCase.isChanged || string(gotBefore) != testCase.wantBefore || string(gotAfter) != testCase.wantAfter {
				t.Errorf("got %v %s %s, want %v %s %s", isChanged, gotBefore, gotAfter, testCase.isChanged, testCase.wantBefore, testCase.wantAfter)
			}
		})
	}
}
