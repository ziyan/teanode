package agent

import (
	"strings"
	"testing"
)

// The three kinds draw: a frame, the labels, a shape per point, and the
// title escaped, so a label with a bracket in it is a label.
func TestChartsDraw(t *testing.T) {
	arguments := &chartArguments{Title: "Mail by sender <2026>", Labels: []string{"Sam", "Maria", "Bob & co"}, Series: []chartSeries{{Name: "messages", Values: []float64{3, 1, 2}}}, Unit: "messages"}
	bars := drawBars(arguments)
	for _, want := range []string{"<svg", "Mail by sender &lt;2026&gt;", "Bob &amp; co", `<rect x=`, "</svg>", "messages"} {
		if !strings.Contains(bars, want) {
			t.Fatalf("bars lack %q", want)
		}
	}
	if strings.Count(bars, "<rect x=") < 3 {
		t.Fatal("a bar per point")
	}
	lines := drawLines(arguments)
	if !strings.Contains(lines, "<polyline") || strings.Count(lines, "<circle") != 3 {
		t.Fatal("a line through three points")
	}
	pie := drawPie(arguments)
	if strings.Count(pie, "<path") != 3 || !strings.Contains(pie, "(50%)") {
		t.Fatalf("three slices with shares: %s", pie)
	}
	if niceMax(37) != 50 || niceMax(3) != 5 || niceMax(0) != 1 || niceMax(100) != 100 {
		t.Fatalf("niceMax: %v %v %v %v", niceMax(37), niceMax(3), niceMax(0), niceMax(100))
	}
}

func TestReachesOut(t *testing.T) {
	if reachesOut(`<html><body><svg><rect/></svg><script>draw()</script></body></html>`) != "" {
		t.Fatal("an inline page is fine")
	}
	if reachesOut(`<script src="https://cdn.example.net/d3.v5.min.js"></script>`) == "" {
		t.Fatal("a library from elsewhere never loads")
	}
	if reachesOut(`<link rel="stylesheet" href="https://x/y.css">`) == "" {
		t.Fatal("a stylesheet from elsewhere never loads")
	}
}
