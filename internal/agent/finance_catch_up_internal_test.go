package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A finance source whose provider is still gathering its history syncs
// again within minutes rather than at its next scheduled time, and the pass
// over the whole history for transfers is not counted as done until the
// history is; once the provider says it is complete, the schedule is back.
func TestFinanceSyncCatchesUpWhileHistoryArrives(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	today := time.Now().UTC().Format(time.DateOnly)
	fixture.provider.result = &finance.SyncResult{
		Accounts:            []finance.Account{inventedAccount()},
		Added:               []finance.Transaction{inventedTransaction("transaction-1", today, "-12.00", "INVENTED CAFE", "Invented Cafe", "")},
		NextCursor:          "cursor-early",
		IsHistoryIncomplete: true,
	}
	before := time.Now()
	source := fixture.sync(t)
	if source.NextRunAt == nil || source.NextRunAt.After(before.Add(financeCatchUpEvery+time.Minute)) {
		t.Fatalf("a source still gathering its history syncs again soon: next %v", source.NextRunAt)
	}
	if isDetected, _ := source.Cursor[models.FinanceCursorIsTransferHistoryDetected].(bool); isDetected {
		t.Errorf("the whole-history transfer pass is counted as done while the history is still arriving")
	}

	fixture.provider.result.IsHistoryIncomplete = false
	fixture.provider.result.NextCursor = "cursor-complete"
	source = fixture.sync(t)
	scheduled := fixture.worker.nextRunOf(source, fixture.owner)
	if source.NextRunAt == nil || source.NextRunAt.Sub(scheduled).Abs() > time.Second {
		t.Errorf("a source whose history is complete keeps its schedule: next %v, scheduled %v", source.NextRunAt, scheduled)
	}
	if isDetected, _ := source.Cursor[models.FinanceCursorIsTransferHistoryDetected].(bool); !isDetected {
		t.Errorf("the whole-history transfer pass is counted as done once the history is complete")
	}
}
