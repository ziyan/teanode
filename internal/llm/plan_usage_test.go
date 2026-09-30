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
	if provider.planUsageNow(time.Now()) != nil {
		t.Fatal("nothing said yet")
	}
	header := http.Header{}
	header.Set("X-Codex-Primary-Used-Percent", "5")
	header.Set("X-Codex-Primary-Window-Minutes", "300")
	provider.notePlanUsage(header)
	if usage := provider.planUsageNow(time.Now()); usage == nil || usage.Windows[0].UsedPercent != 5 {
		t.Fatalf("usage kept: %+v", usage)
	}
}

func TestPlanUsageKeepsWhatALaterAnswerLeavesOut(t *testing.T) {
	now := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	first := http.Header{}
	first.Set("X-Codex-Primary-Used-Percent", "40")
	first.Set("X-Codex-Primary-Window-Minutes", "300")
	first.Set("X-Codex-Primary-Reset-After-Seconds", "7200")
	first.Set("X-Codex-Secondary-Used-Percent", "10")
	first.Set("X-Codex-Secondary-Window-Minutes", "10080")
	first.Set("X-Codex-Secondary-Reset-At", "2026-01-09T00:00:00Z")
	usage := planUsageOf(first, nil, now)

	// An answer that gives the use alone: each window keeps its length
	// and its reset time, and only the use moves.
	later := now.Add(10 * time.Minute)
	useOnly := http.Header{}
	useOnly.Set("X-Codex-Primary-Used-Percent", "45")
	useOnly.Set("X-Codex-Secondary-Used-Percent", "11")
	usage = planUsageOf(useOnly, usage, later)
	if len(usage.Windows) != 2 {
		t.Fatalf("both windows kept: %+v", usage.Windows)
	}
	shortest, longest := usage.Windows[0], usage.Windows[1]
	if shortest.WindowMinutes != 300 || shortest.UsedPercent != 45 || !shortest.ResetsAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("shortest window %+v", shortest)
	}
	if longest.WindowMinutes != 10080 || longest.UsedPercent != 11 ||
		!longest.ResetsAt.Equal(time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("longest window, reset read as RFC 3339: %+v", longest)
	}

	// An answer that gives a length and a use but no reset keeps the
	// reset time said before.
	noReset := http.Header{}
	noReset.Set("X-Codex-Primary-Used-Percent", "50")
	noReset.Set("X-Codex-Primary-Window-Minutes", "300")
	usage = planUsageOf(noReset, usage, later)
	if shortest := usage.Windows[0]; shortest.UsedPercent != 50 || !shortest.ResetsAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("reset time kept: %+v", shortest)
	}

	// Once the short window's reset has passed, an answer that says
	// nothing of it does not carry its old use past the reset.
	afterReset := now.Add(3 * time.Hour)
	usage = planUsageOf(http.Header{"X-Codex-Secondary-Used-Percent": {"12"}}, usage, afterReset)
	if shortest := usage.Windows[0]; shortest.UsedPercent != 0 || !shortest.ResetsAt.IsZero() {
		t.Fatalf("use from before the reset dropped: %+v", shortest)
	}
}

func TestPlanUsageShowsAPassedResetAsUnused(t *testing.T) {
	provider := &codex{}
	header := http.Header{}
	header.Set("X-Codex-Primary-Used-Percent", "80")
	header.Set("X-Codex-Primary-Window-Minutes", "300")
	header.Set("X-Codex-Primary-Reset-After-Seconds", "60")
	provider.notePlanUsage(header)
	resetsAt := provider.planUsageNow(time.Now()).Windows[0].ResetsAt

	before := provider.planUsageNow(resetsAt.Add(-time.Second)).Windows[0]
	if before.UsedPercent != 80 {
		t.Fatalf("before the reset: %+v", before)
	}
	after := provider.planUsageNow(resetsAt.Add(time.Second)).Windows[0]
	if after.UsedPercent != 0 || !after.ResetsAt.Equal(resetsAt) {
		t.Fatalf("after the reset, unused and still saying when: %+v", after)
	}
}

func TestPlanUsageIgnoresGarbledValues(t *testing.T) {
	now := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	for _, garbled := range []string{"NaN", "Inf", "-Inf", "+Inf", "abc"} {
		header := http.Header{}
		header.Set("X-Codex-Primary-Used-Percent", garbled)
		header.Set("X-Codex-Primary-Window-Minutes", garbled)
		header.Set("X-Codex-Primary-Reset-At", garbled)
		header.Set("X-Codex-Primary-Reset-After-Seconds", garbled)
		if usage := planUsageOf(header, nil, now); len(usage.Windows) != 0 {
			t.Fatalf("%q: no window from garbled headers: %+v", garbled, usage.Windows)
		}

		// Over a window already known, garbled values change nothing.
		known := &PlanUsage{Windows: []PlanWindow{{Name: "primary", UsedPercent: 30, WindowMinutes: 300, ResetsAt: now.Add(time.Hour)}}}
		usage := planUsageOf(header, known, now)
		if len(usage.Windows) != 1 || usage.Windows[0] != known.Windows[0] {
			t.Fatalf("%q: window kept as it was: %+v", garbled, usage.Windows)
		}
		if _, isSaid := usedPercent(header, "X-Codex-Primary-Used-Percent"); isSaid {
			t.Fatalf("%q read as a percent", garbled)
		}
	}
}
