package agent

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// A day's work is told from what was said in its coding sessions: the
// person's own requests, and the assistant's report at the end of each
// stretch of work. A session's name is only a label, and a session kept open
// for weeks keeps its name while what it works on changes every day.

// journalPostHeader begins a post in a coding transcript: "15:04 author: ".
var journalPostHeader = regexp.MustCompile(`(?m)^(\d{1,2}:\d{2}) ([^:\n]{1,80}): `)

const (
	// The most of a project's requests and reports the writer reads, spread
	// over the day, and how much of each.
	journalRequestsPerProjectMost = 10
	journalReportsPerProjectMost  = 5
	journalRequestCharacters      = 220
	journalReportCharacters       = 320

	// journalRequestCharactersLeast leaves out the requests that say nothing
	// on their own ("ok", "go ahead").
	journalRequestCharactersLeast = 12

	// journalWorkTalkCharactersMost bounds what the writer reads of the day's
	// work; the projects worked on longest come first.
	journalWorkTalkCharactersMost = 16000
)

// journalPost is one post of a transcript: its clock as written, its author
// and its words.
type journalPost struct {
	clock  string
	author string
	text   string
}

// journalPostsOf splits a transcript's chunks into its posts.
func journalPostsOf(chunkTexts []string) []journalPost {
	text := strings.Join(chunkTexts, "\n")
	headers := journalPostHeader.FindAllStringSubmatchIndex(text, -1)
	posts := make([]journalPost, 0, len(headers))
	for index, header := range headers {
		end := len(text)
		if index+1 < len(headers) {
			end = headers[index+1][0]
		}
		posts = append(posts, journalPost{
			clock:  text[header[2]:header[3]],
			author: text[header[4]:header[5]],
			text:   strings.TrimSpace(text[header[1]:end]),
		})
	}
	return posts
}

// spreadJournalLines keeps at most most lines, taken evenly from first to
// last so that the day's end is always among them.
func spreadJournalLines(lines []string, most int) []string {
	if len(lines) <= most {
		return lines
	}
	if most <= 1 {
		return lines[len(lines)-most:]
	}
	spread := make([]string, 0, most)
	for index := 0; index < most; index++ {
		spread = append(spread, lines[index*(len(lines)-1)/(most-1)])
	}
	return spread
}

// journalProjectTalk is what was said in a project's sessions that day, as
// the writer reads it: the person's requests and the assistant's reports, or
// "" when its transcripts say nothing.
func journalProjectTalk(project journal.WorkProject, reference string, chunkTexts map[string][]string, personNames []string) string {
	var requests, reports []string
	seen := map[string]bool{}
	for _, documentId := range project.StretchDocumentIDs {
		var report *journalPost
		posts := journalPostsOf(chunkTexts[documentId])
		for index := range posts {
			post := &posts[index]
			if !journal.IsPersonAuthor(post.author, personNames) {
				report = post
				continue
			}
			request := oneLineForModel(post.text, journalRequestCharacters)
			if utf8.RuneCountInString(request) < journalRequestCharactersLeast || seen[request] {
				continue
			}
			seen[request] = true
			requests = append(requests, "- "+post.clock+" "+request)
		}
		if report != nil {
			if text := oneLineForModel(report.text, journalReportCharacters); text != "" {
				reports = append(reports, "- "+report.clock+" "+text)
			}
		}
	}
	if len(requests) == 0 && len(reports) == 0 {
		return ""
	}
	lines := []string{"Project " + project.Name + ", " + reference + ":"}
	if requests = spreadJournalLines(requests, journalRequestsPerProjectMost); len(requests) > 0 {
		lines = append(lines, "Their requests:")
		lines = append(lines, requests...)
	}
	if reports = spreadJournalLines(reports, journalReportsPerProjectMost); len(reports) > 0 {
		lines = append(lines, "The assistant's reports at the end of a stretch of work:")
		lines = append(lines, reports...)
	}
	return strings.Join(lines, "\n")
}

// readJournalWorkTalk reads the transcripts of the day's coding stretches and
// says, project by project, what was asked for and done. Only the projects
// whose line the writer reads (a session's project) are told.
func readJournalWorkTalk(transaction db.Transaction, owner *models.User, agent *models.Agent, day *journal.Day) (string, error) {
	if agent == nil || len(day.WorkProjects) == 0 {
		return "", nil
	}
	var documentIds []string
	for _, project := range day.WorkProjects {
		documentIds = append(documentIds, project.StretchDocumentIDs...)
	}
	chunkTexts, err := transaction.ListAgentDocumentChunkTexts(agent.ID, documentIds)
	if err != nil {
		return "", err
	}
	personNames := reading.ChatNamesOf(owner)
	var told []string
	characters := 0
	for _, project := range day.WorkProjects {
		if project.DocumentID == "" {
			continue
		}
		talk := journalProjectTalk(project, string(models.RecordAgentDocument)+":"+project.DocumentID, chunkTexts, personNames)
		if talk == "" {
			continue
		}
		if characters+len(talk) > journalWorkTalkCharactersMost {
			break
		}
		characters += len(talk)
		told = append(told, talk)
	}
	return strings.Join(told, "\n\n"), nil
}
