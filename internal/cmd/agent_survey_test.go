package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

// backgroundWorkServer answers the background work documents: a survey
// started is running for the first askCount reads of it, and then has
// finished with finishedWork.
func backgroundWorkServer(test *testing.T, askCount int, finishedWork string) (*httptest.Server, func() []map[string]any) {
	test.Helper()
	var mutex sync.Mutex
	var asked []map[string]any
	readCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		asked = append(asked, document.Variables)
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "StartAgentSurvey"):
			_, _ = response.Write([]byte(`{"data":{"StartAgentSurvey":{"id":"work-1","workKind":"survey","workStatus":"queued","title":"Survey: the orchards","createdAt":"2026-09-29T10:00:00Z"}}}`))
		case strings.Contains(document.Query, "GetAgentBackgroundWork"):
			readCount++
			if readCount <= askCount {
				_, _ = response.Write([]byte(`{"data":{"GetAgentBackgroundWork":{"id":"work-1","workKind":"survey","workStatus":"running","createdAt":"2026-09-29T10:00:00Z"}}}`))
				return
			}
			_, _ = response.Write([]byte(`{"data":{"GetAgentBackgroundWork":` + finishedWork + `}}`))
		case strings.Contains(document.Query, "ListAgentBackgroundWork"):
			_, _ = response.Write([]byte(`{"data":{"ListAgentBackgroundWork":[{"id":"work-2","workKind":"subagent","workStatus":"running","title":"count the invoices","createdAt":"2026-09-29T10:00:00Z"},` +
				`{"id":"work-1","workKind":"survey","workStatus":"done","title":"Survey: the orchards","createdAt":"2026-09-29T09:00:00Z"}]}}`))
		case strings.Contains(document.Query, "StopAgentBackgroundWork"):
			_, _ = response.Write([]byte(`{"data":{"StopAgentBackgroundWork":{"id":"work-2","workKind":"subagent","workStatus":"stopped","title":"count the invoices","createdAt":"2026-09-29T10:00:00Z"}}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)
	return server, func() []map[string]any {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]map[string]any(nil), asked...)
	}
}

const finishedSurvey = `{"id":"work-1","workKind":"survey","workStatus":"done","createdAt":"2026-09-29T10:00:00Z",` +
	`"resultText":"## Strengths\n\nThe orchards are pruned (projects/orchard-north#1).\n\n---\n\nCovered: ` + "`projects/orchard-north`" + `.","runIds":["run-1","run-2"]}`

// The survey command starts a survey and asks for it until it has
// finished, rather than holding one request open, then prints the report
// and the runs it made.
func TestTheSurveyCommandStartsASurveyAndWaitsForItsReport(test *testing.T) {
	polled := surveyPollEvery
	surveyPollEvery = 10 * time.Millisecond
	defer func() { surveyPollEvery = polled }()
	server, asked := backgroundWorkServer(test, 2, finishedSurvey)

	printed := runAgainst(test, server, "survey", "--scope", "themes/orchard-work", "what are the strengths of the orchards?")
	if !strings.HasPrefix(printed, "## Strengths") || !strings.Contains(printed, "Covered: `projects/orchard-north`.") ||
		!strings.Contains(printed, "'teanode agent run show': run-1, run-2") {
		test.Errorf("the survey printed %q", printed)
	}
	sent := asked()
	if len(sent) != 4 || sent[0]["scopePath"] != "themes/orchard-work" || sent[0]["question"] != "what are the strengths of the orchards?" {
		test.Fatalf("one start and three reads: %v", sent)
	}
	for _, read := range sent[1:] {
		if read["id"] != "work-1" {
			test.Errorf("a read of another: %v", read)
		}
	}
}

// With --no-wait it prints the id and returns; a survey of everything
// sends no scope; one that failed says why.
func TestTheSurveyCommandReturnsAtOnceOrSaysWhyItFailed(test *testing.T) {
	polled := surveyPollEvery
	surveyPollEvery = 10 * time.Millisecond
	defer func() { surveyPollEvery = polled }()
	server, asked := backgroundWorkServer(test, 0, `{"id":"work-1","workKind":"survey","workStatus":"failed","errorMessage":"nothing in that scope has an overview yet","createdAt":"2026-09-29T10:00:00Z"}`)

	if printed := runAgainst(test, server, "survey", "--no-wait", "what", "is", "everything?"); strings.TrimSpace(printed) != "work-1" {
		test.Errorf("--no-wait printed %q", printed)
	}
	if sent := asked(); len(sent) != 1 || sent[0]["question"] != "what is everything?" {
		test.Fatalf("one start and no read: %v", sent)
	} else if _, isSent := sent[0]["scopePath"]; isSent {
		test.Errorf("a survey of everything sent a scope: %v", sent[0])
	}

	var written bytes.Buffer
	command := &cli.Command{
		Name: "fixture", Writer: &written,
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewAgentCommand()},
	}
	err := command.Run(test.Context(), []string{"fixture", "agent", "survey", "what went wrong?"})
	if err == nil || !strings.Contains(err.Error(), "failed") || !strings.Contains(err.Error(), "nothing in that scope has an overview yet") {
		test.Errorf("a failed survey says why: %v", err)
	}
}

// The background work is listed newest first, one line each, and one is
// stopped by its id.
func TestTheBackgroundCommandListsAndStops(test *testing.T) {
	server, asked := backgroundWorkServer(test, 0, finishedSurvey)
	listed := runAgainst(test, server, "background", "list")
	lines := strings.Split(strings.TrimSpace(listed), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "work-2  subagent  running") || !strings.Contains(lines[1], "work-1  survey  finished") ||
		!strings.HasSuffix(lines[1], "Survey: the orchards") {
		test.Errorf("listed %q", listed)
	}
	if stopped := runAgainst(test, server, "background", "stop", "work-2"); strings.TrimSpace(stopped) != "work-2: stopped" {
		test.Errorf("stopped %q", stopped)
	}
	if shown := runAgainst(test, server, "background", "show", "work-1"); !strings.HasPrefix(shown, "## Strengths") {
		test.Errorf("shown %q", shown)
	}
	if sent := asked(); sent[1]["id"] != "work-2" || sent[2]["id"] != "work-1" {
		test.Errorf("sent %v", sent)
	}
}
