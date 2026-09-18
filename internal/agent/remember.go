package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Filing what a conversation taught.
//
// This is the half of memory that was missing. The model was asked, in
// the conduct, to keep what it learned; on a real server over five days
// and four hundred and fifty turns it did so three times, twice about the
// same thing. A model in the middle of somebody's actual work does not
// stop to keep house, and a prompt that asks more loudly does not change
// that.
//
// So it is a run of its own, on the cheap model, reading the conversation
// after it has gone quiet, with nothing to do but file. It happens every
// time, which is the only property that matters.

// The bounds.
const (
	// rememberQuiet is how long a conversation rests before it is read.
	// Long enough that somebody thinking between messages is not filed
	// mid-thought; short enough that what they said this morning is on
	// the pages by lunch.
	rememberQuiet = 5 * time.Minute

	// rememberBatch is how many conversations one sweep queues.
	rememberBatch = 20

	// rememberEvery is how often the sweep looks.
	rememberEvery = time.Minute

	// rememberFacts is how many facts one run may file, and
	// rememberMessages how many messages it reads.
	rememberFacts    = 15
	rememberMessages = 60

	// rememberMessageCharacters is how much of one message goes in. A
	// tool's answer can be a page of JSON; what it taught is in the first
	// part of it.
	rememberMessageCharacters = 1500

	// rememberIndexTokens is how much of the index the run is shown, so
	// it files onto a page that exists rather than making a second one
	// beside it.
	rememberIndexTokens = 3000

	// rememberPages is how many of the pages the conversation already
	// touched are shown in full.
	rememberPages = 6
)

// RememberAnswer is what the run answers with.
type RememberAnswer struct {
	Facts      []RememberedFact `json:"facts"`
	Links      []RememberedLink `json:"links"`
	Supersedes []SupersededFact `json:"supersedes"`
}

// RememberedFact is one thing the conversation taught.
type RememberedFact struct {
	Path      string `json:"path"`
	NodeKind  string `json:"node_kind"`
	NodeName  string `json:"node_name"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Happened  string `json:"happened"`
	Quote     string `json:"quote"`
	MessageID string `json:"message_id"`
}

// RememberedLink joins two pages.
type RememberedLink struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`

	// Note is the sentence that justifies the link, in the words somebody
	// would use to say it. The relation says "works on"; this says "led
	// the controls work until 2025", which is the part a person reading
	// the page wants and the part a relation alone cannot carry.
	Note string `json:"note"`
}

// SupersededFact is a fact this conversation has replaced.
type SupersededFact struct {
	Path   string `json:"path"`
	Number int    `json:"number"`
}

// queueRemembering queues a remember job for every conversation that has
// gone quiet with something in it nobody has filed.
//
// Queued from the sweep rather than at the end of a turn so that a burst
// of messages coalesces into one run, and so that a turn started from a
// chat app, a terminal or the drawer is treated the same way.
func (self *Agent) queueRemembering(ctx context.Context, now time.Time) {
	if now.Sub(self.lastRemember) < rememberEvery || self.settings.Registry == nil {
		return
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "remember") || !FeatureAllowed(configuration, "ask") {
		return
	}
	self.lastRemember = now

	var due []*models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		due, err = tx.ListAgentConversationsToRemember(now.Add(-rememberQuiet), rememberBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list the conversations to file: %s", err)
		return
	}
	for _, conversation := range due {
		if ctx.Err() != nil {
			return
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := self.Enqueue(tx, models.AgentJobRemember, conversation.AgentID, "", conversation.ID)
			return err
		}); err != nil {
			log.Warningf("cannot queue the filing of conversation %q: %s", conversation.ID, err)
		}
	}
}

// runRemember is the handler for a remember job; its subject is the
// conversation.
func (self *Agent) runRemember(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "remember") {
		return nil
	}
	if !self.canThink(configuration) {
		return fmt.Errorf("no way to act as the person")
	}

	var conversation *models.AgentConversation
	var messages []*models.AgentMessage
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		found, err := tx.GetAgentConversation(run.Job.SubjectID)
		if err != nil || found == nil {
			return err
		}
		if found.AgentID != run.Agent.ID {
			return nil
		}
		conversation = found
		messages, err = tx.ListAgentMessages(found.ID, nil)
		return err
	}); err != nil {
		return err
	}
	if conversation == nil {
		return nil // gone since it was queued
	}

	// Everything after the mark, which is where the last run stopped.
	unread := messages
	if conversation.RememberedThrough != "" {
		for index, message := range messages {
			if message.ID == conversation.RememberedThrough {
				unread = messages[index+1:]
				break
			}
		}
	}
	unread = worthReading(unread)
	if len(unread) == 0 {
		// Nothing to file, but the mark still moves: otherwise a
		// conversation of nothing but tool chatter comes round every
		// minute for ever.
		return self.markRemembered(ctx, conversation, messages)
	}
	// The oldest sixty, not the newest sixty. Cutting from the end and
	// then moving the mark to the last message of the whole list said
	// that everything in between had been filed, and nothing ever came
	// back for it: a conversation with two hundred unread messages had
	// its first hundred and forty marked read without being read. A
	// backlog is worked through oldest first, sixty at a time, over as
	// many runs as it takes.
	backlog := 0
	if len(unread) > rememberMessages {
		backlog = len(unread) - rememberMessages
		unread = unread[:rememberMessages]
	}

	answer, transcript, err := self.askWhatWasLearned(ctx, run, conversation, unread)
	if err != nil {
		return err
	}
	theirWords := make(map[string]bool, len(unread))
	// What the run put in front of the model, which is the only thing a
	// fact from it may cite and the only words it may quote.
	shown := make(map[string]string, len(unread))
	for _, message := range unread {
		if message.Role == string(llm.RoleUser) {
			theirWords[message.ID] = true
		}
		shown[message.ID] = shownText(message)
	}
	filed, err := self.fileWhatWasLearned(ctx, run, answer, theirWords, models.EvidenceConversation, shown)
	if err != nil {
		return err
	}

	// The mark and the run's title in one transaction with nothing else:
	// a crash before this point re-reads, a crash after it does not
	// re-file. The loop wrote the transcript as it went; what is left is
	// to say what the run turned out to be.
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		note := "Filed nothing from this conversation"
		if filed.Filed > 0 {
			note = fmt.Sprintf("Filed %d thing(s) from %q", filed.Filed, conversation.Title)
		}
		// How much of what it filed could not be shown to have been said.
		// On the row rather than in a log line, because the person
		// reading the runs is the one who would want to know.
		if checked := filed.Describe(); checked != "" {
			note += ", " + checked
		}
		if transcript != nil {
			if _, err := tx.UpdateAgentConversation(transcript.ID, func(found *models.AgentConversation) error {
				found.Title = note
				return nil
			}); err != nil {
				return err
			}
		}
		// The last message this run was actually given, so the mark never
		// stands past something nobody read.
		read := unread[len(unread)-1]
		return tx.MarkAgentConversationRemembered(conversation.ID, read.ID, time.Now())
	}); err != nil {
		return err
	}
	if filed.Filed > 0 {
		log.Debugf("filed %d fact(s) from conversation %s", filed.Filed, conversation.ID)
	}
	if backlog > 0 {
		// Straight back into the queue rather than waiting for the sweep
		// to offer the conversation again, which it does once a minute.
		//
		// A deferral and not another Enqueue: one job per agent, kind and
		// subject is open at a time, and this job is the open one, so an
		// Enqueue from inside it hands back the row it is already running
		// and queues nothing at all.
		return &Deferral{
			Until:  time.Now(),
			Reason: fmt.Sprintf("%d more message(s) of this conversation are unread", backlog),
		}
	}
	return nil
}

// markRemembered moves the mark without filing anything.
func (self *Agent) markRemembered(ctx context.Context, conversation *models.AgentConversation, messages []*models.AgentMessage) error {
	if len(messages) == 0 {
		return nil
	}
	last := messages[len(messages)-1]
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkAgentConversationRemembered(conversation.ID, last.ID, time.Now())
	})
}

// worthReading is the messages a filing run has any use for: what the
// person said and what the agent answered. A tool's call and its result
// are left out -- they are how the answer was arrived at, not what was
// learned, and they are most of the tokens.
func worthReading(messages []*models.AgentMessage) []*models.AgentMessage {
	kept := make([]*models.AgentMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case string(llm.RoleUser), string(llm.RoleAssistant):
			if strings.TrimSpace(message.Content) != "" {
				kept = append(kept, message)
			}
		}
	}
	return kept
}

// askWhatWasLearned puts the conversation to the scan model.
func (self *Agent) askWhatWasLearned(ctx context.Context, run *Run, conversation *models.AgentConversation, unread []*models.AgentMessage) (*RememberAnswer, *models.AgentConversation, error) {

	var index []string
	var pages []string
	var unlearned []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(run.Agent.ID); err != nil {
			return err
		}
		nodes, err := tx.ListAgentIndex(run.Agent.ID, 300)
		if err != nil {
			return err
		}
		spent := 0
		for _, node := range nodes {
			if node.Kind == models.NodeFolder && node.Summary == "" && node.ParentID == "" {
				continue
			}
			line := node.IndexLine(140)
			cost := llm.EstimateTokens(line)
			if spent+cost > rememberIndexTokens {
				break
			}
			spent += cost
			index = append(index, line)
		}
		// The pages this conversation already touched, so the run adds
		// to them rather than saying again what they say.
		pages, err = self.pagesTouched(tx, run.Agent.ID, unread, rememberPages)
		if err != nil {
			return err
		}
		struck, err := tx.ListAgentFeedback(run.Agent.ID, []models.AgentFeedbackKind{models.FeedbackUnlearned}, 20)
		if err != nil {
			return err
		}
		for _, entry := range struck {
			unlearned = append(unlearned, entry.Said)
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}

	prompt, err := render("remember.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Index":      index,
		"Pages":      pages,
		"Unlearned":  unlearned,
		"Transcript": transcriptFor(unread),
		"Most":       rememberFacts,
	})
	if err != nil {
		return nil, nil, err
	}

	thinking, err := self.oneShot(ctx, run, fmt.Sprintf("Filing what %q taught", conversation.Title), prompt, models.AgentJobRemember, config.AgentWorkScan)
	if err != nil {
		return nil, nil, fmt.Errorf("asking the model: %w", err)
	}
	extracted, err := llm.ExtractJSON(thinking.Text)
	if err != nil {
		// A run that answered with prose taught nothing this time. Not a
		// failure: the mark still moves, and the next conversation is a
		// fresh try.
		log.Debugf("the filing run answered with no object: %s", err)
		return &RememberAnswer{}, thinking.Conversation, nil
	}
	answer := &RememberAnswer{}
	if err := json.Unmarshal([]byte(extracted), answer); err != nil {
		log.Debugf("the filing run's object is not what was asked for: %s", err)
		return &RememberAnswer{}, thinking.Conversation, nil
	}
	return answer, thinking.Conversation, nil
}

// pagesTouched is the pages the conversation's own words touch, in full,
// so that a run does not file what a page already says.
func (self *Agent) pagesTouched(tx db.Transaction, agentId string, messages []*models.AgentMessage, limit int) ([]string, error) {
	var words strings.Builder
	for _, message := range messages {
		if message.Role == string(llm.RoleUser) {
			words.WriteString(message.Content)
			words.WriteByte('\n')
		}
	}
	nodes, _, err := tx.SearchAgentGraph(agentId, words.String(), limit)
	if err != nil {
		return nil, err
	}
	pages := make([]string, 0, len(nodes))
	for _, node := range nodes {
		facts, err := tx.ListAgentFacts(agentId, node.ID, false, 20)
		if err != nil {
			return nil, err
		}
		block := node.Path
		if node.Name != "" {
			block += " — " + node.Name
		}
		if summary := strings.TrimSpace(node.Summary); summary != "" {
			block += "\n" + cutRunes(summary, 800)
		}
		for _, fact := range facts {
			block += "\n#" + fmt.Sprint(fact.Number) + " " + fact.Line()
		}
		pages = append(pages, block)
	}
	return pages, nil
}

// transcriptFor is the conversation as the run reads it, with each
// message's identifier in front so a fact can cite the message it came
// from.
func transcriptFor(messages []*models.AgentMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		who := "them"
		if message.Role == string(llm.RoleAssistant) {
			who = "you"
		}
		builder.WriteString("[" + message.ID + "] " + who + ": ")
		builder.WriteString(shownText(message))
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String())
}

// shownText is one message as the transcript shows it: cut to the bound,
// with anything that could close the block said rather than left to close
// it.
//
// One function because two callers need the same answer. What a fact may
// quote is what the model was shown, and the check held the quote against
// the whole message instead: words from past the cut, which the run never
// put in front of the model, passed as something it had been told.
func shownText(message *models.AgentMessage) string {
	return unclosable(cutRunes(message.Content, rememberMessageCharacters))
}

// whatWasFiled is what a filing run kept, and what the evidence check
// took off it on the way.
//
// The two counts are the measure of how often the model quotes something
// nobody said. They go in the run's own row rather than a log line,
// because the person reading the dream log is the one who would want to
// know that a night filed forty facts and could find the words for six.
type whatWasFiled struct {
	Filed           int
	WithoutQuote    int
	WithoutEvidence int
}

// Describe is what the check did, for the end of a run's title. Empty
// when every quote was where it was said to be, which is the usual case
// and does not need saying.
func (self *whatWasFiled) Describe() string {
	var parts []string
	if self.WithoutQuote > 0 {
		parts = append(parts, fmt.Sprintf("%d without their quote", self.WithoutQuote))
	}
	if self.WithoutEvidence > 0 {
		parts = append(parts, fmt.Sprintf("%d without evidence", self.WithoutEvidence))
	}
	return strings.Join(parts, ", ")
}

// evidenceSpace is every run of whitespace, which a quote and the text it
// came from may break differently without either being wrong.
var evidenceSpace = regexp.MustCompile(`\s+`)

// evidenceLikeness is the quote and the text it cites reduced to what
// they have to share for the quote to be that text's.
//
// Case, the width of the whitespace, and which of the several characters
// somebody used for a quotation mark or a dash are not the model
// inventing anything: a transcript is typed by people and rendered by
// programs, and a model asked to copy a line out of one will normalize it
// on the way. What it may not do is write words that are not there.
func evidenceLikeness(text string) string {
	text = strings.Map(func(letter rune) rune {
		switch letter {
		case '‘', '’', '‛', '`', '´':
			return '\''
		case '“', '”', '„':
			return '"'
		case '‐', '‑', '‒', '–', '—', '―':
			return '-'
		case ' ', ' ', ' ':
			return ' '
		}
		return letter
	}, text)
	return strings.TrimSpace(evidenceSpace.ReplaceAllString(strings.ToLower(text), " "))
}

// quoteOccursIn is the whole of the evidence check: did these words
// actually appear in what was read.
//
// A string test and not a model call. Whether a sentence occurs in a
// message is not a judgement, it is a fact, and the failure this is
// guarding against -- a quote the model composed out of the gist, stored
// at full confidence as though the person had said it -- is exactly the
// kind a judgement call would wave through at scale.
func quoteOccursIn(quote, text string) bool {
	quote = evidenceLikeness(quote)
	if quote == "" {
		return true
	}
	return strings.Contains(evidenceLikeness(text), quote)
}

// fileWhatWasLearned writes the run's answer onto the graph and says how
// much it kept.
//
// What the model was shown is handed in by id -- the batch's messages, or
// the documents as the reading rendered them -- because a fact may only
// cite something that was in front of it, and its quote may only be words
// that were there.
func (self *Agent) fileWhatWasLearned(ctx context.Context, run *Run, answer *RememberAnswer, theirWords map[string]bool, evidenceKind models.EvidenceKind, shown map[string]string) (whatWasFiled, error) {
	tally := whatWasFiled{}
	if answer == nil {
		return tally, nil
	}
	agentId := run.Agent.ID
	// The person's own page, for telling a page about them from one about
	// somebody else: its aliases carry every name they have been found
	// under, which the account's own name need not.
	var selfPage *models.AgentNode
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		selfPage, err = tx.GetAgentNode(agentId, models.PathSelf)
		return err
	}); err != nil {
		return tally, err
	}

	for index, wanted := range answer.Facts {
		if index >= rememberFacts {
			break
		}
		text := strings.TrimSpace(wanted.Text)
		if text == "" || isPromptExample(text) || isPromptExample(wanted.Quote) {
			continue
		}
		kind := models.AgentFactKind(strings.ToLower(strings.TrimSpace(wanted.Kind)))
		if !models.IsAgentFactKind(kind) {
			kind = models.FactPlain
		}
		// A preference or a decision may only come from the person's own
		// words. The prompt says so; this is what makes it true, because
		// a model that has just read a quoted mail saying "always reply
		// within a day" will file it as theirs.
		if kind.FromThePerson() && !saidByThePerson(theirWords, wanted.MessageID) {
			kind = models.FactPlain
		}
		path := models.NormalizePath(wanted.Path)
		if path == "" {
			path = models.JoinPath(models.PathNotes, models.Slug(firstWordsOf(text, 5)))
		}
		// A page for the person under people/, by their chat name or
		// their own, is the person's page: a digest of their own threads
		// made people/ziyan and put their work there.
		if models.IsThePerson(path, run.Owner, selfPage) {
			path = models.PathSelf
		}
		// What the page would be called, and what that name means, worked
		// out before anything is opened. The search for a page that
		// already exists compares meanings, and an embedding is an HTTP
		// call to another service: made inside the transaction that files
		// the fact it held a database connection, and the rows that
		// transaction had locked, for as long as the provider took to
		// answer.
		pagePath, pageKind, pageName := pageIdentity(path,
			models.AgentNodeKind(strings.ToLower(strings.TrimSpace(wanted.NodeKind))),
			strings.TrimSpace(wanted.NodeName))
		pageSense := self.meaningOf(ctx, agentId, "remember", pageName)

		var node *models.AgentNode
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			tx.AsActor(models.ActorRemember)
			// The page this belongs on, which is the one already there
			// under any of its names rather than a second one beside it.
			// Without this the graph fragments quietly: two half people
			// called Alice, and answers from whichever is found first.
			node, err = self.resolvePage(tx, agentId, pagePath, pageKind, pageName, pageSense)
			if err != nil || node == nil {
				return err
			}
			// A page about a person is a person the address book should
			// know: one list of people, not two.
			if node.Kind == models.NodePerson && node.ContactID == "" {
				if err := self.bindContact(tx, run.Owner, node); err != nil {
					log.Warningf("cannot keep a contact for %q: %s", node.Path, err)
				}
			}
			return nil
		}); err != nil {
			log.Warningf("cannot file %q: %s", text, err)
			continue
		}

		// What the check made of this one, counted only if the row is
		// written: a fact the page already said was never filed, and a
		// tally that counted it would overstate how much the model made
		// up.
		//
		// A line that only says what the page is says nothing: the page
		// already says it, and a page whose one fact is "X is a project"
		// reads like something was learned. See vacuous.go for how much
		// of a real graph this was.
		outcome := evidenceHolds
		if node != nil && saysSomethingNew(text, node, run.Owner) {
			fact := &models.AgentFact{
				AgentID: agentId, NodeID: node.ID, Kind: kind,
				Text:       cutRunes(text, models.FactLength),
				HappenedAt: whenHappened(run, wanted.Happened),
				Confidence: 1,
				Evidence: []models.Evidence{{
					Kind: evidenceKind,
					// The digest marks each item "[id]", and a model that
					// copies the marker whole is answering as asked.
					ID:    strings.Trim(strings.TrimSpace(wanted.MessageID), "[]"),
					Quote: cutRunes(strings.TrimSpace(wanted.Quote), models.QuoteLength),
				}},
				Audiences: []models.AgentAudience{models.AudienceAsk},
			}
			outcome = checkTheEvidence(fact, shown)
			// And what the fact itself means, for the fold into whatever
			// the page already says -- the second embedding this used to
			// make with the transaction open.
			factSense := self.meaningOf(ctx, agentId, "remember", factText(fact, node.Path, node.Name))
			if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
				tx.AsActor(models.ActorRemember)
				written, err := tx.AddAgentFact(fact)
				if err != nil {
					return err
				}
				_, err = self.foldIntoWhatThePageSays(tx, written, node, factSense)
				return err
			}); err != nil {
				log.Warningf("cannot file %q: %s", text, err)
				continue
			}
		}
		tally.Filed++
		switch outcome {
		case evidenceQuoteNotFound:
			tally.WithoutQuote++
		case evidenceCitesNothing:
			tally.WithoutEvidence++
		}
	}

	for _, link := range answer.Links {
		from := models.NormalizePath(link.From)
		to := models.NormalizePath(link.To)
		relation := models.AgentEdgeRelation(strings.ToLower(strings.TrimSpace(link.Relation)))
		if from == "" || to == "" || !models.IsAgentEdgeRelation(relation) || isPromptExample(link.Note) {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			fromNode, err := tx.GetAgentNode(agentId, from)
			if err != nil || fromNode == nil {
				return err
			}
			toNode, err := tx.GetAgentNode(agentId, to)
			if err != nil || toNode == nil {
				return err
			}
			return tx.PutAgentEdge(&models.AgentEdge{
				AgentID: agentId, FromID: fromNode.ID, ToID: toNode.ID, Relation: relation,
				Note: strings.TrimSpace(link.Note),
			})
		}); err != nil {
			log.Debugf("cannot link %s to %s: %s", from, to, err)
		}
	}

	for _, superseded := range answer.Supersedes {
		path := models.NormalizePath(superseded.Path)
		if path == "" || superseded.Number <= 0 {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			node, err := tx.GetAgentNode(agentId, path)
			if err != nil || node == nil {
				return err
			}
			fact, err := tx.GetAgentFact(agentId, node.ID, superseded.Number)
			if err != nil || fact == nil {
				return err
			}
			// Marked, never deleted: what it said is still readable, and
			// a page that was rewritten can be read back.
			//
			// Struck rather than updated to dormant. An ordinary update
			// that happens to set dormant files nothing in the page's
			// history -- that is what the nightly retirement pass wants
			// -- so a run that took a line off a page this way left no
			// trace of having done it, and a judgement the person cannot
			// see is one they cannot undo.
			_, err = tx.StrikeAgentFact(agentId, fact.ID, "a later conversation replaced it")
			return err
		}); err != nil {
			log.Debugf("cannot supersede %s#%d: %s", path, superseded.Number, err)
		}
	}
	return tally, nil
}

// evidenceOutcome is what the check made of one fact's citation.
type evidenceOutcome int

const (
	// evidenceHolds is the quote occurring in what was read, or no quote
	// offered at all, which is nothing to check rather than something to
	// doubt.
	evidenceHolds evidenceOutcome = iota

	// evidenceQuoteNotFound is words that are not in the thing they are
	// said to be from. The citation stays -- the fact did come out of
	// reading that message -- and the invented words do not.
	evidenceQuoteNotFound

	// evidenceCitesNothing is a citation of something the run never put
	// in front of the model, so there is nothing behind the fact at all.
	evidenceCitesNothing
)

// checkTheEvidence holds a fact's citation against what the run actually
// showed the model, and takes off it whatever cannot be supported.
//
// A fact that fails is kept and marked: it may well be true, and the
// model did read something. What it loses is the claim to have been told.
// Half confidence and Inferred put it under anything somebody said, in
// the ranking and on the page, which is where a paraphrase belongs.
//
// A citation known to the run but shown without its text -- a document a
// coarse night read by its title alone -- has its quote left alone.
// Nothing was shown to check against, and a check that cannot be made is
// not a check that failed.
func checkTheEvidence(fact *models.AgentFact, shown map[string]string) evidenceOutcome {
	if len(fact.Evidence) == 0 {
		return evidenceHolds
	}
	source, known := shown[fact.Evidence[0].ID]
	if !known {
		fact.Evidence = nil
		fact.Inferred = true
		fact.Confidence = evidenceInferredConfidence
		return evidenceCitesNothing
	}
	if source == "" || quoteOccursIn(fact.Evidence[0].Quote, source) {
		return evidenceHolds
	}
	fact.Evidence[0].Quote = ""
	fact.Inferred = true
	fact.Confidence = evidenceInferredConfidence
	return evidenceQuoteNotFound
}

// evidenceInferredConfidence is what a fact is worth once the check has
// taken its evidence off it: the agent's own reading of something, which
// is worth having and worth ranking under what somebody said. One
// constant, so that a guard found too strict is loosened in one place.
const evidenceInferredConfidence = 0.5

// FoldIntoWhatThePageSays merges a fact just written into the one already
// on the page that says the same thing, and reports which it became.
//
// Every writer needs this and for the same reason: nobody writing a fact
// has read the page first. A conversation that comes back to a subject is
// filed by two runs a day apart, neither knowing of the other, and both
// doing their job; a person adding a line by hand is not going to reread
// thirteen of them. Left to itself a page ends up saying one thing nine
// ways, which is worse than saying it once and worse than not saying it,
// because every answer drawn from it is a little different.
//
// The older one stays: its number is what anything else cites. It gains
// whatever evidence the new one brought, and the new one goes dormant
// behind it -- kept, searchable, out of the page -- so a fold the person
// disagrees with is there to be undone. What happened is in the page's
// history either way.
func (self *Agent) FoldIntoWhatThePageSays(ctx context.Context, tx db.Transaction, written *models.AgentFact, node *models.AgentNode) (*models.AgentFact, error) {
	if written == nil || node == nil {
		return written, nil
	}
	return self.foldIntoWhatThePageSays(tx, written, node,
		self.meaningOf(ctx, written.AgentID, "remember", factText(written, node.Path, node.Name)))
}

// FoldByWordsIntoWhatThePageSays is the fold without the fact's meaning:
// the twin is found by names and words alone. For a caller that is
// already inside a transaction it cannot leave -- a dashboard request,
// which runs whole inside one -- and must not hold it, and the page's
// row lock, through a provider call.
func (self *Agent) FoldByWordsIntoWhatThePageSays(tx db.Transaction, written *models.AgentFact, node *models.AgentNode) (*models.AgentFact, error) {
	return self.foldIntoWhatThePageSays(tx, written, node, nil)
}

// foldIntoWhatThePageSays is the same with the fact's meaning already
// worked out, for a caller that did it before opening its transaction.
func (self *Agent) foldIntoWhatThePageSays(tx db.Transaction, written *models.AgentFact, node *models.AgentNode, sense *meaning) (*models.AgentFact, error) {
	if written == nil || node == nil {
		return written, nil
	}
	twin := self.twinOf(tx, written, node, sense)
	if twin == nil {
		return written, nil
	}
	// "She prefers tea" and "she no longer prefers tea" share every name
	// and sit on top of each other in the vector space, so neither the
	// cosine nor the name check can keep them apart -- and they are the
	// pair it matters most not to lose one of. Both rows stay, and the
	// newer statement is the one the page states.
	if negates(written.Text, twin.Text) {
		if !laterThan(written, twin) {
			return written, nil
		}
		if _, err := tx.FoldAgentFact(written.AgentID, twin.ID, written.ID,
			"a later statement of the same thing replaced it"); err != nil {
			return written, err
		}
		return written, nil
	}
	older, err := tx.UpdateAgentFact(written.AgentID, twin.ID, func(older *models.AgentFact) error {
		older.Evidence = append(older.Evidence, written.Evidence...)
		if len(older.Evidence) > models.EvidenceCount {
			older.Evidence = older.Evidence[:models.EvidenceCount]
		}
		return nil
	})
	if err != nil {
		return written, err
	}
	if _, err := tx.FoldAgentFact(written.AgentID, written.ID, older.ID,
		"it says what another fact on the page already says"); err != nil {
		return written, err
	}
	return older, nil
}

// laterThan says whether one fact is the later statement of the two: by
// when it was true where both say, and by when it was filed otherwise.
//
// When it was true is asked first because a fact learned today about
// 2019 is a 2019 fact, and a conversation that corrects an old record
// after the fact would otherwise make the correction the older one.
func laterThan(fact, than *models.AgentFact) bool {
	if fact.HappenedAt != nil && than.HappenedAt != nil {
		return fact.HappenedAt.After(*than.HappenedAt)
	}
	return fact.CreatedAt.After(than.CreatedAt)
}

// twinOf is the fact already on this page that says what a new one says,
// or nil.
//
// Written after the fact rather than before it so that the vector is the
// one the store holds, and so that a deployment with no embedding model
// keeps everything rather than silently dropping what it cannot compare.
func (self *Agent) twinOf(tx db.Transaction, fact *models.AgentFact, node *models.AgentNode, sense *meaning) *models.AgentFact {
	if sense == nil {
		return nil
	}
	if err := tx.PutAgentFactVector(fact.AgentID, fact.ID, sense.ModelName, sense.Vector); err != nil {
		log.Debugf("cannot keep a fact's vector: %s", err)
		return nil
	}
	scores, err := tx.Nearest(db.AgentFactTable, fact.AgentID, sense.ModelName, sense.Vector, 6, db.VectorQuery{
		Where:     []string{`"fact_id" IN (SELECT "id" FROM "agent_fact" WHERE "node_id" = ? AND "id" <> ? AND NOT "dormant" AND "superseded_by" IS NULL)`},
		Arguments: []any{fact.NodeID, fact.ID},
		Floor:     twinFloor,
	})
	if err != nil {
		log.Debugf("cannot look for a twin: %s", err)
		return nil
	}
	candidates, err := tx.GetAgentFacts(fact.AgentID, idsOf(scores))
	if err != nil {
		return nil
	}
	// The page's own name is not evidence either way; see sharesAName.
	itsOwn := append([]string{node.Name}, node.Aliases...)
	for _, candidate := range orderFacts(candidates, idsOf(scores)) {
		if sharesAName(fact.Text, candidate.Text, itsOwn...) {
			return candidate
		}
	}
	return nil
}

// saidByThePerson says whether a message identifier names something the
// person themselves wrote.
//
// The whole point of the check: a run that has just read a quoted mail
// saying "always reply within a day" will file it as the person's own
// preference. It is not theirs unless they typed it.
func saidByThePerson(said map[string]bool, messageId string) bool {
	return messageId != "" && said[messageId]
}

// bindContact keeps a person the agent learned about in the address book,
// and points the page at them. The address book is the only list of
// people this server keeps (see the decision record of 2026-09-13), so a
// page about somebody and a card for them are two views of one thing.
func (self *Agent) bindContact(tx db.Transaction, owner *models.User, node *models.AgentNode) error {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		return nil
	}
	books, err := tx.ListAddressBooks(owner.ID)
	if err != nil || len(books) == 0 {
		return err
	}
	book := books[0]
	// Somebody they already keep, matched by name, is not written again.
	existing, err := tx.ListContacts(book.ID, name, 5)
	if err != nil {
		return err
	}
	for _, contact := range existing {
		if strings.EqualFold(strings.TrimSpace(contact.Name), name) {
			_, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: node.AgentID, Path: node.Path, Kind: node.Kind, Name: node.Name,
				Aliases: node.Aliases, Summary: node.Summary, ContactID: contact.ID,
				Pinned: node.Pinned, Importance: node.Importance,
			})
			return err
		}
	}
	// A card with a name and nothing else is a valid card. The page is
	// where what the agent learned lives; this is only the entry that
	// says the person exists.
	built, err := contacts.Build(nil, &contacts.Fields{Name: &name})
	if err != nil {
		return err
	}
	contact, err := tx.PutContact(&models.Contact{
		AddressBookID: book.ID, UID: built.UID, Name: built.Name, Card: string(built.Card),
	})
	if err != nil {
		return err
	}
	_, err = tx.PutAgentNode(&models.AgentNode{
		AgentID: node.AgentID, Path: node.Path, Kind: node.Kind, Name: node.Name,
		Aliases: node.Aliases, Summary: node.Summary, ContactID: contact.ID,
		Pinned: node.Pinned, Importance: node.Importance,
	})
	return err
}

// whenHappened reads the date the run gave, in the person's zone.
func whenHappened(run *Run, said string) *time.Time {
	said = strings.TrimSpace(said)
	if said == "" || strings.EqualFold(said, "null") {
		return nil
	}
	location := time.Local
	if run.Owner != nil && run.Owner.Timezone != "" {
		if loaded, err := time.LoadLocation(run.Owner.Timezone); err == nil {
			location = loaded
		}
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006", time.RFC3339} {
		if when, err := time.ParseInLocation(layout, said, location); err == nil {
			return &when
		}
	}
	return nil
}

// kindOfPath guesses what a page is about from where it was filed.
func kindOfPath(path string) models.AgentNodeKind {
	switch strings.Split(path, "/")[0] {
	case models.PathPeople:
		return models.NodePerson
	case "organizations":
		return models.NodeOrganization
	case models.PathProjects:
		return models.NodeProject
	case models.PathPlaces:
		return models.NodePlace
	case models.PathThings:
		return models.NodeThing
	case models.PathTime:
		return models.NodePeriod
	case models.PathSelf:
		return models.NodeSelf
	}
	return models.NodeTopic
}

// nameFromSlug is a path segment as a name.
func nameFromSlug(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "-", " "), " ")
	for index, word := range words {
		if word == "" {
			continue
		}
		runes := []rune(word)
		words[index] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(words, " ")
}

// firstWordsOf is the opening of a sentence, for naming a page nobody
// named.
func firstWordsOf(text string, count int) string {
	words := strings.Fields(text)
	if len(words) > count {
		words = words[:count]
	}
	return strings.Join(words, " ")
}
