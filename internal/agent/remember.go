package agent

import (
	"context"
	"encoding/json"
	"fmt"
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
	registry := run.Registry()
	if registry == nil {
		return fmt.Errorf("no model registry")
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
	if len(unread) > rememberMessages {
		unread = unread[len(unread)-rememberMessages:]
	}

	answer, prompt, response, modelName, err := self.askWhatWasLearned(ctx, run, conversation, unread)
	if err != nil {
		return err
	}
	theirWords := make(map[string]bool, len(unread))
	for _, message := range unread {
		if message.Role == string(llm.RoleUser) {
			theirWords[message.ID] = true
		}
	}
	filed, err := self.fileWhatWasLearned(ctx, run, answer, theirWords)
	if err != nil {
		return err
	}

	// The mark and the transcript in one transaction with nothing else:
	// a crash before this point re-reads, a crash after it does not
	// re-file.
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		note := "Filed nothing from this conversation"
		if filed > 0 {
			note = fmt.Sprintf("Filed %d thing(s) from %q", filed, conversation.Title)
		}
		if _, err := self.recordRun(tx, run, note, prompt, response, modelName); err != nil {
			return err
		}
		last := messages[len(messages)-1]
		return tx.MarkAgentConversationRemembered(conversation.ID, last.ID, time.Now())
	}); err != nil {
		return err
	}
	if filed > 0 {
		log.Debugf("filed %d fact(s) from conversation %s", filed, conversation.ID)
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
func (self *Agent) askWhatWasLearned(ctx context.Context, run *Run, conversation *models.AgentConversation, unread []*models.AgentMessage) (*RememberAnswer, string, *llm.ChatResponse, string, error) {
	configuration := run.Configuration()
	registry := run.Registry()
	provider, model, err := registry.ForWork(config.AgentWorkScan)
	if err != nil {
		return nil, "", nil, "", err
	}
	modelName := registry.Configuration().Models.ForWork(config.AgentWorkScan)

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
		return nil, "", nil, "", err
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
		return nil, "", nil, "", err
	}

	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model:     model,
		Messages:  []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}},
		MaxTokens: 2000,
	})
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobRemember), response.Usage)
	}
	if err != nil {
		return nil, prompt, response, modelName, fmt.Errorf("asking the model: %w", err)
	}
	extracted, err := llm.ExtractJSON(response.Message.Content)
	if err != nil {
		// A run that answered with prose taught nothing this time. Not a
		// failure: the mark still moves, and the next conversation is a
		// fresh try.
		log.Debugf("the filing run answered with no object: %s", err)
		return &RememberAnswer{}, prompt, response, modelName, nil
	}
	answer := &RememberAnswer{}
	if err := json.Unmarshal([]byte(extracted), answer); err != nil {
		log.Debugf("the filing run's object is not what was asked for: %s", err)
		return &RememberAnswer{}, prompt, response, modelName, nil
	}
	return answer, prompt, response, modelName, nil
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
		builder.WriteString(unclosable(cutRunes(message.Content, rememberMessageCharacters)))
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String())
}

// fileWhatWasLearned writes the run's answer onto the graph and says how
// much it kept.
func (self *Agent) fileWhatWasLearned(ctx context.Context, run *Run, answer *RememberAnswer, theirWords map[string]bool) (int, error) {
	if answer == nil {
		return 0, nil
	}
	filed := 0
	agentId := run.Agent.ID

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
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorRemember)
			// The page this belongs on, which is the one already there
			// under any of its names rather than a second one beside it.
			// Without this the graph fragments quietly: two half people
			// called Alice, and answers from whichever is found first.
			node, err := self.resolvePage(ctx, tx, agentId,
				path, models.AgentNodeKind(strings.ToLower(strings.TrimSpace(wanted.NodeKind))),
				strings.TrimSpace(wanted.NodeName))
			if err != nil {
				return err
			}
			if node == nil {
				return nil
			}
			// A page about a person is a person the address book should
			// know: one list of people, not two.
			if node.Kind == models.NodePerson && node.ContactID == "" {
				if err := self.bindContact(tx, run.Owner, node); err != nil {
					log.Warningf("cannot keep a contact for %q: %s", node.Path, err)
				}
			}
			// A line that only says what the page is says nothing: the
			// page already says it, and a page whose one fact is "X is a
			// project" reads like something was learned. See vacuous.go
			// for how much of a real graph this was.
			if !saysSomethingNew(text, node, run.Owner) {
				return nil
			}
			fact := &models.AgentFact{
				AgentID: agentId, NodeID: node.ID, Kind: kind,
				Text:       cutRunes(text, models.FactLength),
				HappenedAt: whenHappened(run, wanted.Happened),
				Confidence: 1,
				Evidence: []models.Evidence{{
					Kind:  models.EvidenceConversation,
					ID:    wanted.MessageID,
					Quote: cutRunes(strings.TrimSpace(wanted.Quote), models.QuoteLength),
				}},
				Audiences: []models.AgentAudience{models.AudienceAsk},
			}
			written, err := tx.AddAgentFact(fact)
			if err != nil {
				return err
			}
			_, err = self.FoldIntoWhatThePageSays(ctx, tx, written, node)
			return err
		}); err != nil {
			log.Warningf("cannot file %q: %s", text, err)
			continue
		}
		filed++
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
			_, err = tx.UpdateAgentFact(agentId, fact.ID, func(fact *models.AgentFact) error {
				fact.Dormant = true
				return nil
			})
			return err
		}); err != nil {
			log.Debugf("cannot supersede %s#%d: %s", path, superseded.Number, err)
		}
	}
	return filed, nil
}

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
// whatever evidence the new one brought, and the new one goes. What
// happened is in the page's history either way.
func (self *Agent) FoldIntoWhatThePageSays(ctx context.Context, tx db.Transaction, written *models.AgentFact, node *models.AgentNode) (*models.AgentFact, error) {
	if written == nil || node == nil {
		return written, nil
	}
	twin := self.twinOf(ctx, tx, written, node)
	if twin == nil {
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
	if err := tx.DeleteAgentFact(written.AgentID, written.ID); err != nil {
		return written, err
	}
	return older, nil
}

// twinOf is the fact already on this page that says what a new one says,
// or nil.
//
// Written after the fact rather than before it so that the vector is the
// one the store holds, and so that a deployment with no embedding model
// keeps everything rather than silently dropping what it cannot compare.
func (self *Agent) twinOf(ctx context.Context, tx db.Transaction, fact *models.AgentFact, node *models.AgentNode) *models.AgentFact {
	vectors, modelName, ok := self.embed(ctx, fact.AgentID, "remember", []string{factText(fact, node.Path, node.Name)})
	if !ok {
		return nil
	}
	if err := tx.PutAgentFactVector(fact.AgentID, fact.ID, modelName, vectors[0]); err != nil {
		log.Debugf("cannot keep a fact's vector: %s", err)
		return nil
	}
	scores, err := tx.Nearest(db.AgentFactTable, fact.AgentID, modelName, vectors[0], 6, db.VectorQuery{
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
