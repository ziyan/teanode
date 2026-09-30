package llm

import (
	"net/http"
	"testing"
	"time"
)

func TestPlanUsageIsReadFromTheHeaders(t *testing.T) {
	now := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	header := http.Header{}
	header.Set("X-Codex-Plan-Type", "plus")
	header.Set("X-Codex-Primary-Used-Percent", "69.4")
	header.Set("X-Codex-Primary-Window-Minutes", "10080")
	header.Set("X-Codex-Primary-Reset-After-Seconds", "3600")
	// A second window the service names but gives no length is not
	// reported, rather than shown as unused.
	header.Set("X-Codex-Secondary-Used-Percent", "0")
	header.Set("X-Codex-Secondary-Window-Minutes", "0")

	usage := planUsageOf(header, nil, now)
	if usage.PlanName != "plus" || !usage.ObservedAt.Equal(now) {
		t.Fatalf("plan %+v", usage)
	}
	if len(usage.Windows) != 1 {
		t.Fatalf("one window reported: %+v", usage.Windows)
	}
	window := usage.Windows[0]
	if window.UsedPercent != 69 || window.WindowMinutes != 10080 || !window.ResetsAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("window %+v", window)
	}

	// A named reset time wins over the seconds remaining, and a later
	// answer that leaves out the plan and a window's use keeps them.
	later := http.Header{}
	later.Set("X-Codex-Primary-Window-Minutes", "10080")
	later.Set("X-Codex-Primary-Reset-At", "1767700000")
	later.Set("X-Codex-Primary-Reset-After-Seconds", "60")
	later.Set("X-Codex-Secondary-Used-Percent", "12")
	later.Set("X-Codex-Secondary-Window-Minutes", "300")
	usage = planUsageOf(later, usage, now)
	if usage.PlanName != "plus" || len(usage.Windows) != 2 {
		t.Fatalf("plan %+v", usage)
	}
	shortest, longest := usage.Windows[0], usage.Windows[1]
	if shortest.WindowMinutes != 300 || shortest.UsedPercent != 12 {
		t.Fatalf("shortest window first: %+v", usage.Windows)
	}
	if longest.UsedPercent != 69 || !longest.ResetsAt.Equal(time.Unix(1767700000, 0)) {
		t.Fatalf("longest window %+v", longest)
	}
}

func TestPlanUsageIsNilUntilThePlanHasAnswered(t *testing.T) {
	var registry *Registry
	if registry.PlanUsage("plan") != nil {
		t.Fatal("no registry, no usage")
	}
	provider := &codex{}
	if provider.planUsageNow() != nil {
		t.Fatal("nothing said yet")
	}
	header := http.Header{}
	header.Set("X-Codex-Primary-Used-Percent", "5")
	header.Set("X-Codex-Primary-Window-Minutes", "300")
	provider.notePlanUsage(header)
	if usage := provider.planUsageNow(); usage == nil || usage.Windows[0].UsedPercent != 5 {
		t.Fatalf("usage kept: %+v", usage)
	}
}
