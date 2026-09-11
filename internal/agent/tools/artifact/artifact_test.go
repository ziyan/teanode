package artifact

import (
	"strings"
	"testing"
)

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
	page := `<link href="/assets/artifact.css" rel="stylesheet" /><script src="/assets/echarts.min.js"></script><script defer src='/assets/artifact.js'></script><div class="chart"></div><script>teanode.chart(document.querySelector('.chart'), {})</script>`
	if reason := reachesOut(page); reason != "" {
		t.Fatalf("what this server offers is not reaching out: %s", reason)
	}
	if reachesOut(`<script src="/assets/other.js"></script>`) == "" {
		t.Fatal("only the offered files")
	}
	if reachesOut(`<script src="data:text/javascript,alert(1)"></script>`) == "" {
		t.Fatal("a data address is not this server's either")
	}
	for _, leaving := range []string{`<meta http-equiv="refresh" content="0;url=https://x/">`, `<script>location.href='https://x/?'+document.body.innerText</script>`, `<a href="https://x/">out</a>`, `<form action="https://x/"></form>`} {
		if reachesOut(leaving) == "" {
			t.Fatalf("a page may not leave: %s", leaving)
		}
	}
}

// A page gets the look on its own, and the chart library when it draws a
// chart, at the head of the document; a page that linked them keeps its
// own links.
func TestCompletePage(t *testing.T) {
	plain := completePage(`<html><head><title>x</title></head><body><p>hi</p></body></html>`)
	if !strings.HasPrefix(plain, `<html><head><link rel="stylesheet" href="/assets/artifact.css"><title>`) || strings.Contains(plain, "echarts") {
		t.Fatalf("a plain page gets the look and nothing else: %s", plain)
	}
	chart := completePage(`<body><div class="chart"></div><script>teanode.chart(document.querySelector('.chart'), {})</script></body>`)
	if !strings.HasPrefix(chart, `<head><link rel="stylesheet" href="/assets/artifact.css"><script src="/assets/echarts.min.js"></script><script src="/assets/artifact.js"></script></head><body>`) {
		t.Fatalf("a chart page gets the library before the body: %s", chart)
	}
	linked := `<html><head><link href="/assets/artifact.css" rel="stylesheet"><script src="/assets/echarts.min.js"></script><script src="/assets/artifact.js"></script></head><body></body></html>`
	if completePage(linked) != linked {
		t.Fatal("a page that linked them keeps its own links")
	}
}

func TestSafeFilename(t *testing.T) {
	for input, want := range map[string]string{"Mooring fees, Q3 2026": "mooring-fees-q3-2026", "  ": "artifact", "Chart/Plot!": "chartplot"} {
		if got := safeFilename(input); got != want {
			t.Fatalf("safeFilename(%q) = %q, want %q", input, got, want)
		}
	}
}
