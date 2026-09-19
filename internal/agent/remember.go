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

	// alreadySaidCandidates is how much of a page is read back to see
	// whether it already states a sentence. The same bound the nightly
	// pass reads a page with (firstSayingIt), because the two are asking
	// the same question at either end of the night.
	alreadySaidCandidates = 500
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
	// The last message this run was actually given, so the mark never
	// stands past something nobody read.
	read := unread[len(unread)-1]
	// The mark moves in the same transaction as the writes it is a
	// promise about. It says that everything behind it has been filed,
	// and it was moved separately and unconditionally: a fact whose write
	// failed was logged and stepped over, the run reported success, and
	// the mark went past the whole window. Those messages were never read
	// again. A window now lands whole or not at all, and a run that
	// cannot write leaves the mark where it was for the next one -- the
	// job is queued again, and the deferral below drains what is left.
	//
	// The loop wrote the transcript as it went; what is left is to say
	// what the run turned out to be.
	filed, err := self.fileWhatWasLearned(ctx, run, answer, theirWords, models.EvidenceConversation, shown,
		func(tx db.Transaction, filed whatWasFiled) error {
			note := "Filed nothing from this conversation"
			if filed.Filed > 0 {
				note = fmt.Sprintf("Filed %d thing(s) from %q", filed.Filed, conversation.Title)
			}
			// How much of what it filed could not be shown to have been
			// said. On the row rather than in a log line, because the
			// person reading the runs is the one who would want to know.
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
			return tx.MarkAgentConversationRemembered(conversation.ID, read.ID, time.Now())
		})
	if err != nil {
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

// preparedFact is one fact of an answer on its way to a page: worked out
// as far as it can be before anything at all is written.
//
// It exists so that everything slow happens first. An embedding is an
// HTTP call to another service, and both the page's meaning and the
// fact's are needed before either can be filed; made with the writing
// transaction open they would hold a database connection, and every row
// that transaction had locked, for as long as the provider took.
type preparedFact struct {
	// Text is what the model said, trimmed, and MessageID and Quote what
	// it cited for it.
	Text      string
	Kind      models.AgentFactKind
	MessageID string
	Quote     string
	Happened  *time.Time

	// AskedPath is the path the writer actually wrote, before this was
	// tidied into PagePath. A link in the same answer names a page by
	// that path rather than by whatever tidying it went through, so it is
	// kept to join the two.
	AskedPath string

	// The page this belongs on, as it would be called, and what that name
	// means -- which is how a page already there under another name is
	// found rather than a second one made beside it.
	PagePath  string
	PageKind  models.AgentNodeKind
	PageName  string
	PageSense *meaning

	// Node is that page once it has been opened.
	Node *models.AgentNode

	// Fact is the row to write, or nil where the page already said this
	// and there is nothing to write. Such an item still counts as filed:
	// the run did its job with it, and it turned out to be nothing new.
	Fact  *models.AgentFact
	Sense *meaning

	// Outcome is what the evidence check made of the citation.
	Outcome evidenceOutcome
}

// fileWhatWasLearned writes the run's answer onto the graph and says how
// much it kept.
//
// What the model was shown is handed in by id -- the batch's messages, or
// the documents as the reading rendered them -- because a fact may only
// cite something that was in front of it, and its quote may only be words
// that were there.
//
// Everything a window learned is written in one transaction, and finish
// is whatever the caller wants done in that same transaction once it has
// been: for a conversation, the mark saying how far it has been read.
// That is the point of the shape. Each fact used to be written on its
// own, a failure logged and stepped over, and the run reported success
// anyway -- so the caller moved its mark past the whole window and the
// messages behind it were marked read without ever having been read.
// Nothing came back for them, because the mark is a promise that
// everything behind it has been filed. The supersessions ran later still,
// in transactions of their own, so a replacement that failed left the
// page with the old line struck and nothing standing in its place.
//
// Now a window is all or none: a failure leaves the graph and the mark
// exactly as they were, and the job is queued again.
func (self *Agent) fileWhatWasLearned(ctx context.Context, run *Run, answer *RememberAnswer, theirWords map[string]bool, evidenceKind models.EvidenceKind, shown map[string]string, finish func(tx db.Transaction, filed whatWasFiled) error) (whatWasFiled, error) {
	tally := whatWasFiled{}
	if answer == nil {
		answer = &RememberAnswer{}
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

	// What each fact would say, and what its page would be called.
	prepared := make([]*preparedFact, 0, len(answer.Facts))
	for index, wanted := range answer.Facts {
		if index >= rememberFacts {
			break
		}
		text := strings.TrimSpace(wanted.Text)
		if text == "" {
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
		pagePath, pageKind, pageName := pageIdentity(path,
			models.AgentNodeKind(strings.ToLower(strings.TrimSpace(wanted.NodeKind))),
			strings.TrimSpace(wanted.NodeName))
		prepared = append(prepared, &preparedFact{
			AskedPath: models.NormalizePath(wanted.Path),
			Text:      text, Kind: kind,
			// The digest marks each item "[id]", and a model that copies
			// the marker whole is answering as asked.
			MessageID: strings.Trim(strings.TrimSpace(wanted.MessageID), "[]"),
			Quote:     strings.TrimSpace(wanted.Quote),
			Happened:  whenHappened(run, wanted.Happened),
			PagePath:  pagePath, PageKind: pageKind, PageName: pageName,
			PageSense: self.meaningOf(ctx, agentId, "remember", pageName),
		})
	}

	// The pages, before the facts, because a fact needs one to point at
	// and because what a page is called is part of what its facts mean.
	// A page opened for a window that then fails to write is an empty
	// page, which the nightly pass takes away after two days; a fact
	// written without the strike that was supposed to replace it is a
	// page saying two contradictory things, which is what must not
	// happen.
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorRemember)
		for _, ready := range prepared {
			// The page this belongs on, which is the one already there
			// under any of its names rather than a second one beside it.
			// Without this the graph fragments quietly: two half people
			// called Alice, and answers from whichever is found first.
			node, err := self.resolvePage(tx, agentId, ready.PagePath, ready.PageKind, ready.PageName, ready.PageSense)
			if err != nil {
				return fmt.Errorf("opening the page for %q: %w", ready.Text, err)
			}
			if node == nil {
				continue
			}
			ready.Node = node
			// A page about a person is a person the address book should
			// know: one list of people, not two.
			if node.Kind == models.NodePerson && node.ContactID == "" {
				if err := self.bindContact(tx, run.Owner, node); err != nil {
					log.Warningf("cannot keep a contact for %q: %s", node.Path, err)
				}
			}
		}
		return nil
	}); err != nil {
		return tally, err
	}

	// The pages this answer opened, under every path that names them, so
	// that a link in the same answer lands on the page its fact did.
	// Without this the links quietly halve: the page a fact asked for is
	// not always the page it got -- the owner's own name routes to
	// `self`, a bare path gains the folder its kind lives under, and a
	// page already there under another name keeps the path it has -- and
	// a link naming the path the writer wrote found nothing at it and was
	// dropped in silence. That is most of an authorship map, since most
	// of what a person's own checkouts contain was written by them.
	opened := make(map[string]*models.AgentNode, 2*len(prepared))
	for _, ready := range prepared {
		if ready.Node == nil {
			continue
		}
		opened[ready.AskedPath] = ready.Node
		opened[ready.PagePath] = ready.Node
	}

	// The rows themselves, with their evidence checked and their meaning
	// worked out.
	//
	// A word list stood here, of the words a sentence saying only that a
	// page exists is made of, and a fact left with nothing else was
	// refused. It refused real ones too -- "This project is private" is
	// four words that were all on the list -- and a refusal was silent,
	// so nobody ever saw what the person's agent had been told and did
	// not keep. A dull line is cheaper: the nightly run merges it or it
	// sinks.
	for _, ready := range prepared {
		if ready.Node == nil {
			continue
		}
		ready.Fact = &models.AgentFact{
			AgentID: agentId, NodeID: ready.Node.ID, Kind: ready.Kind,
			Text:       cutRunes(ready.Text, models.FactLength),
			HappenedAt: ready.Happened,
			Confidence: 1,
			Evidence: []models.Evidence{{
				Kind:  evidenceKind,
				ID:    ready.MessageID,
				Quote: cutRunes(ready.Quote, models.QuoteLength),
			}},
			Audiences: []models.AgentAudience{models.AudienceAsk},
		}
		ready.Outcome = checkTheEvidence(ready.Fact, shown)
		ready.Sense = self.meaningOf(ctx, agentId, "remember", factText(ready.Fact, ready.Node.Path, ready.Node.Name))
	}

	// What the run kept, counted before the write so that the caller's
	// own work in the transaction -- the run's title, the conversation's
	// mark -- can say it.
	for _, ready := range prepared {
		tally.Filed++
		switch ready.Outcome {
		case evidenceQuoteNotFound:
			tally.WithoutQuote++
		case evidenceCitesNothing:
			tally.WithoutEvidence++
		}
	}

	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorRemember)
		for _, ready := range prepared {
			if ready.Fact == nil {
				continue
			}
			// A sentence the page already states does not go on it
			// again. What the second saying brought that the first did
			// not is its evidence, and that goes on the fact that is
			// there. See whatThePageAlreadySays for why this is asked
			// before the row is written rather than after.
			standing, err := whatThePageAlreadySays(tx, agentId, ready.Node, ready.Fact.Text)
			if err != nil {
				return fmt.Errorf("reading what %q already says: %w", ready.Node.Path, err)
			}
			if standing != nil {
				if _, err := takeTheEvidenceOf(tx, standing, ready.Fact); err != nil {
					return fmt.Errorf("giving what %q brought to the fact that says it: %w", ready.Text, err)
				}
				continue
			}
			written, err := tx.AddAgentFact(ready.Fact)
			if err != nil {
				return fmt.Errorf("filing %q: %w", ready.Text, err)
			}
			if _, err := self.foldIntoWhatThePageSays(tx, written, ready.Node, ready.Sense); err != nil {
				return fmt.Errorf("folding %q into the page: %w", ready.Text, err)
			}
		}
		if err := linkWhatWasLearned(tx, agentId, answer.Links, opened, run.Owner, selfPage); err != nil {
			return err
		}
		if err := supersedeWhatWasReplaced(tx, agentId, answer.Supersedes); err != nil {
			return err
		}
		if finish == nil {
			return nil
		}
		return finish(tx, tally)
	}); err != nil {
		return whatWasFiled{}, err
	}
	return tally, nil
}

// linkWhatWasLearned draws the links an answer asked for. A link whose
// either end is not a page the agent has is not a failure: the model
// named something it did not file, and there is nothing to join.
//
// opened is the pages this same answer just made, by every path that
// names them, because a link names a page the way its fact did and the
// fact does not always end up at the path it asked for.
func linkWhatWasLearned(tx db.Transaction, agentId string, links []RememberedLink, opened map[string]*models.AgentNode, owner *models.User, selfPage *models.AgentNode) error {
	for _, link := range links {
		from := pathOfLinkEnd(link.From, owner, selfPage)
		to := pathOfLinkEnd(link.To, owner, selfPage)
		relation := models.AgentEdgeRelation(strings.ToLower(strings.TrimSpace(link.Relation)))
		if from == "" || to == "" || !models.IsAgentEdgeRelation(relation) {
			continue
		}
		fromNode, err := pageOfLinkEnd(tx, agentId, from, opened)
		if err != nil {
			return fmt.Errorf("reading %q: %w", from, err)
		}
		toNode, err := pageOfLinkEnd(tx, agentId, to, opened)
		if err != nil {
			return fmt.Errorf("reading %q: %w", to, err)
		}
		if fromNode == nil || toNode == nil {
			continue
		}
		// A page joined to itself, which PutAgentEdge refuses with an
		// error rather than a shrug -- and an error here loses the whole
		// window, since everything a run learned is written in one
		// transaction. Two ends that are two spellings of the person now
		// both route to `self`, so this is reachable in a way it was not
		// when an unfound page simply meant no link.
		if fromNode.ID == toNode.ID {
			continue
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agentId, FromID: fromNode.ID, ToID: toNode.ID, Relation: relation,
			Note: strings.TrimSpace(link.Note),
		}); err != nil {
			return fmt.Errorf("linking %s to %s: %w", from, to, err)
		}
	}
	return nil
}

// pathOfLinkEnd is one end of a link as a path: cleaned, and routed to
// `self` where it names the person whose agent this is. The same routing
// a fact's path gets, for the same reason -- what the agent knows about
// them lives on one page, and a link to a second page for them joins
// nothing to nothing.
func pathOfLinkEnd(path string, owner *models.User, selfPage *models.AgentNode) string {
	path = models.NormalizePath(path)
	if models.IsThePerson(path, owner, selfPage) {
		return models.PathSelf
	}
	return path
}

// pageOfLinkEnd is the page one end of a link names: the one this answer
// just opened under that path, or the one already there.
func pageOfLinkEnd(tx db.Transaction, agentId, path string, opened map[string]*models.AgentNode) (*models.AgentNode, error) {
	if node := opened[path]; node != nil {
		return node, nil
	}
	return tx.GetAgentNode(agentId, path)
}

// supersedeWhatWasReplaced strikes the facts an answer says it has
// replaced.
//
// In the same transaction as the facts that replace them, because a
// strike that lands without its replacement is the one shape of this
// that loses something: the page stops saying the old thing and never
// starts saying the new one.
func supersedeWhatWasReplaced(tx db.Transaction, agentId string, supersedes []SupersededFact) error {
	for _, superseded := range supersedes {
		path := models.NormalizePath(superseded.Path)
		if path == "" || superseded.Number <= 0 {
			continue
		}
		node, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return fmt.Errorf("reading %q: %w", path, err)
		}
		if node == nil {
			continue
		}
		fact, err := tx.GetAgentFact(agentId, node.ID, superseded.Number)
		if err != nil {
			return fmt.Errorf("reading %s#%d: %w", path, superseded.Number, err)
		}
		if fact == nil {
			continue
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
		if _, err := tx.StrikeAgentFact(agentId, fact.ID, "a later conversation replaced it"); err != nil {
			return fmt.Errorf("superseding %s#%d: %w", path, superseded.Number, err)
		}
	}
	return nil
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
//
// What it will not do is guess. Folding with nobody watching is only
// safe where the two are the same sentence twice; anything that reads
// alike but says something different is left standing beside its twin,
// so that the newer is recalled as well as the older. See whatToFold.
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
	switch whatToFold(written, twin) {
	case foldKeepBoth:
		return written, nil

	case foldTheOlderBehindTheNewer:
		if _, err := tx.FoldAgentFact(written.AgentID, twin.ID, written.ID,
			"a later statement of the same thing replaced it"); err != nil {
			return written, err
		}
		return written, nil
	}

	older, err := takeTheEvidenceOf(tx, twin, written)
	if err != nil {
		return written, err
	}
	if _, err := tx.FoldAgentFact(written.AgentID, written.ID, older.ID,
		"it says what another fact on the page already says"); err != nil {
		return written, err
	}
	return older, nil
}

// whatThePageAlreadySays is the fact the page states in these very
// words, or nil.
//
// Asked before a fact is written, where the fold behind it is asked
// after. The fold does the same job and cannot do it any earlier: the
// row has to exist before it can be put behind another one. So a
// re-statement cost a row and a number even when the words were
// identical -- AddAgentFact takes the page's next number, writes the
// line, and the fold puts it straight back. On the live graph that is
// 3,427 rows filed and folded in the same breath, one page reaching
// number 104 in two days with thirteen rows saying one sentence, and a
// page history that is mostly the record of undoing this.
//
// Nothing is lost by not writing it. A second saying of a sentence
// carries exactly one thing the first does not -- where it was read --
// and that is evidence, which goes on the fact that is already there;
// the fold's own surviving branch does no more than that.
//
// Held to the same words, exactly as the fold is (see whatToFold): a
// rewording may be this sentence again or may be the next thing the page
// has to say, and telling those apart is a judgement, which belongs to
// the nightly pass that asks a model. And held to what the page still
// states: a line struck or folded away is not something the page says,
// so a run that reads it again is learning it rather than repeating it.
func whatThePageAlreadySays(tx db.Transaction, agentId string, node *models.AgentNode, text string) (*models.AgentFact, error) {
	if node == nil || strings.TrimSpace(text) == "" {
		return nil, nil
	}
	facts, err := tx.ListAgentFacts(agentId, node.ID, false, alreadySaidCandidates)
	if err != nil {
		return nil, err
	}
	// By number ascending, which is what ListAgentFacts gives: the lowest
	// number wins, because that is the one anything else cites and the
	// one the fold would have kept.
	for _, fact := range facts {
		if saysItInTheSameWords(fact.Text, text) {
			return fact, nil
		}
	}
	return nil, nil
}

// takeTheEvidenceOf puts what a second saying of a sentence brought onto
// the fact that already says it, and answers with that fact as it now
// stands.
//
// The fact that survives is the one already on the page, so where the
// second saying stood on firmer ground the first takes that with it.
// Otherwise re-filing a sentence the person stated, over a copy the
// agent had inferred, would leave the page saying at half confidence
// something it had been told.
func takeTheEvidenceOf(tx db.Transaction, standing, said *models.AgentFact) (*models.AgentFact, error) {
	return tx.UpdateAgentFact(standing.AgentID, standing.ID, func(older *models.AgentFact) error {
		older.Evidence = append(older.Evidence, said.Evidence...)
		if len(older.Evidence) > models.EvidenceCount {
			older.Evidence = older.Evidence[:models.EvidenceCount]
		}
		if atLeastAsWellEvidenced(said, older) {
			older.Inferred = said.Inferred
			if said.Confidence > older.Confidence {
				older.Confidence = said.Confidence
			}
		}
		return nil
	})
}

// foldChoice is what the write boundary does with a new fact and the one
// already on the page that came back as its twin.
type foldChoice int

const (
	// foldKeepBoth leaves both rows on the page, which is the answer
	// whenever the two are not provably the same statement. It is the
	// zero value, so a path that does not decide keeps what it has.
	foldKeepBoth foldChoice = iota

	// foldTheNewerBehindTheOlder is the ordinary fold: the same sentence
	// filed twice. The older keeps its number, because that is what
	// anything else cites, and gains the newer's evidence.
	foldTheNewerBehindTheOlder

	// foldTheOlderBehindTheNewer is a negation: "she prefers tea" and
	// "she no longer prefers tea". The later statement is what the page
	// says and the earlier one stays behind it.
	foldTheOlderBehindTheNewer
)

// whatToFold decides between a new fact and its twin, and its whole job
// is to refuse.
//
// The twin search is a vector floor and a name check, and it was trusted
// to mean "these two say the same thing". It does not. "The rent is 4200
// a month from March" and "the rent is 3100 a month from March" clear
// both, carry no negation, and before this the newer one went dormant
// behind the older: the page kept last year's figure, normal recall
// never carried this year's, and nothing in the conversation said so. A
// change of amount, date, frequency or who is responsible is exactly the
// kind of thing a person tells their agent, and exactly the kind the
// fold was quietly dropping.
//
// So an automatic fold now needs the two to be the same sentence written
// twice -- see saysItInTheSameWords -- where there is provably nothing
// to lose. A paraphrase is left standing beside its twin; the nightly
// pass that puts a page to a model (consolidatePage) is where a judgment
// like that belongs, and until it runs the person hears both rather than
// only the older.
func whatToFold(written, twin *models.AgentFact) foldChoice {
	// "She prefers tea" and "she no longer prefers tea" share every name
	// and sit on top of each other in the vector space, so neither the
	// cosine nor the name check can keep them apart -- and they are the
	// pair it matters most not to lose one of. Both rows stay, and the
	// newer statement is the one the page states.
	if negates(written.Text, twin.Text) {
		if !laterThan(written, twin) {
			return foldKeepBoth
		}
		// And only where the newer one stands on ground at least as firm.
		// A fact whose quote could not be found in what the run was shown
		// is marked inferred at half confidence precisely because the
		// model may have composed it; letting that supersede something
		// the person said would have the agent's own paraphrase win an
		// argument with its source, with no one present to object.
		if !atLeastAsWellEvidenced(written, twin) {
			return foldKeepBoth
		}
		return foldTheOlderBehindTheNewer
	}
	if saysItInTheSameWords(written.Text, twin.Text) {
		return foldTheNewerBehindTheOlder
	}
	return foldKeepBoth
}

// atLeastAsWellEvidenced says whether one fact stands on ground at least
// as firm as another's: stated where the other is stated, and no less
// sure of itself.
func atLeastAsWellEvidenced(fact, than *models.AgentFact) bool {
	if fact.Inferred && !than.Inferred {
		return false
	}
	return fact.Confidence >= than.Confidence
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

// twinOf is the fact already on this page that a new one may be a second
// saying of, or nil.
//
// A candidate and not a verdict: whether the two are really one
// statement is whatToFold's to decide, and this only narrows the page
// down to what is worth asking about.
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
		if !sharesAName(fact.Text, candidate.Text, itsOwn...) {
			continue
		}
		// A line that says a different amount, a different date or a
		// different how-often is not this one said twice, however near
		// the two sit: it is the next thing the page has to say, and the
		// reason the person was talking to their agent at all. Not this
		// fact's twin, and the search goes on to the next candidate
		// rather than stopping at it.
		if differsInQuantity(fact.Text, candidate.Text) {
			continue
		}
		return candidate
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
