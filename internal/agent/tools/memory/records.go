package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// A page's links to the person's own records, as the agent reads and makes
// them.
//
// A link names a record and holds nothing of it, so every read here finds
// the record again and asks whether the agent could read it anyway
// (recordlink.Reader.IsAgent): a granted address book or calendar, a
// granted mailbox, a switched-on finance source, the person's own
// recordings and conversations. A record it was not given is shown as a
// link it cannot open, without a title. What comes back from a record is
// written by outsiders -- a subject, an event name, a merchant -- and the
// result is marked untrusted so the loop fences it as data.

// recordLinkReader is the agent as the reader of its owner's records.
func recordLinkReader(run tools.Run) recordlink.Reader {
	return recordlink.Reader{UserID: run.Agent().UserID, AgentID: run.Agent().ID, IsAgent: true}
}

// linksAction lists the records a page is linked to, linksShown at a time.
func linksAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := ownPath(run, arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("links of which page? give a path, like people/alice-chen")
	}
	offset := max(arguments.LinkOffset, 0)
	var lines []string
	var linkCount int
	hasRecordText := false
	var passages []models.Evidence
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNode(run.Agent().ID, path)
		if err != nil {
			return err
		}
		if node == nil {
			return fmt.Errorf("there is no page at %s", path)
		}
		stored, err := tx.ListAgentRecordLinks(run.Agent().ID, node.ID)
		if err != nil {
			return err
		}
		// One row for a recording however many of its passages the page is
		// linked to, as the dashboard lists it.
		rows := recordlink.GroupPassages(stored)
		linkCount = len(rows)
		if offset >= len(rows) {
			return nil
		}
		for _, row := range rows[offset:min(offset+linksShown, len(rows))] {
			link := row.Link
			resolved, err := recordlink.Resolve(tx, recordLinkReader(run), link)
			if err != nil {
				return err
			}
			lines = append(lines, recordLinkLines(link, row.PassageCount, resolved)...)
			hasRecordText = hasRecordText || resolved.IsReadable()
			// What a recording says is not the person's word, and what a
			// run has read of one is marked so, as for a fact read out of one.
			if resolved.IsReadable() && link.RecordKind == models.RecordRecordingPassage {
				passages = append(passages, models.Evidence{Kind: models.EvidenceRecordingPassage, ID: link.RecordID})
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	switch {
	case linkCount == 0:
		return tools.TextResult("%s is not linked to any of the person's records", path), nil
	case offset >= linkCount:
		return tools.TextResult("… no links past the first %d; the page has %d, and links without link_offset starts at the first", offset, linkCount), nil
	}
	if next := offset + linksShown; next < linkCount {
		lines = append(lines, fmt.Sprintf("… %d more links; links again with link_offset: %d", linkCount-next, next))
	}
	result := tools.TextResult("%s", path+" is linked to:\n"+strings.Join(lines, "\n"))
	result.Untrusted = hasRecordText
	return tools.WithRecordingMemoryEvidence(ctx, run, result, nil, nil, passages)
}

// recordLinkLines is one link as the agent reads it: what the page is to
// the record, the record named so that another tool can open it, and what it
// is called when the agent may read it.
//
// A recording linked at many passages is one line, naming the first passage
// and how many there are; unlink with the recording alone, no range, takes
// them all.
func recordLinkLines(link *models.AgentRecordLink, passageCount int, resolved *recordlink.Resolved) []string {
	relation := string(link.RecordRelation)
	guess := ""
	if link.Proposed() {
		relation = "perhaps " + relation
		guess = " (the agent's guess)"
	}
	line := "- " + relation + " " + link.Reference() + guess
	if passageCount > 1 {
		line += fmt.Sprintf(" (%d passages, the first shown)", passageCount)
	}
	switch {
	case resolved.IsUnavailable:
		return []string{line + "  [no longer there]"}
	case resolved.IsWithheld:
		return []string{line + "  [the person has not shared this with you; you cannot open it]"}
	}
	if resolved.Title != "" {
		line += "  " + resolved.Title
	}
	if resolved.HappenedAt != nil {
		line += "  (" + resolved.HappenedAt.Format("2 Jan 2006") + ")"
	}
	lines := []string{line}
	if resolved.Contact != nil {
		lines = append(lines, contactCardLines(resolved.Contact, "    ")...)
	}
	return lines
}

// contactCardLines is the names, addresses, telephone numbers and
// organization of a card, and nothing of its notes.
func contactCardLines(contact *models.Contact, indent string) []string {
	var lines []string
	if name := recordlink.OneLine(contact.Name); name != "" {
		lines = append(lines, indent+"name: "+name)
	}
	if organization := recordlink.OneLine(contact.Organization); organization != "" {
		lines = append(lines, indent+"organization: "+organization)
	}
	if len(contact.Emails) > 0 {
		lines = append(lines, indent+"addresses: "+recordlink.OneLine(strings.Join(contact.Emails, ", ")))
	}
	if len(contact.Phones) > 0 {
		lines = append(lines, indent+"telephone: "+recordlink.OneLine(strings.Join(contact.Phones, ", ")))
	}
	return lines
}

// linkedContactLines is the card of the contact a person's page is about,
// for a get: the card when the agent may read it, and a line saying so when
// it may not.
func linkedContactLines(tx db.Transaction, run tools.Run, node *models.AgentNode) ([]string, bool, error) {
	if node.ContactID == "" || node.Kind != models.NodePerson {
		return nil, false, nil
	}
	resolved, err := recordlink.ResolveRecord(tx, recordLinkReader(run), models.RecordContact, node.ContactID, models.AgentRecordDetail{}, 0, 0)
	if err != nil {
		return nil, false, err
	}
	switch {
	case resolved.IsUnavailable:
		return nil, false, nil
	case resolved.IsWithheld:
		return []string{"contact card: this page is about a card in an address book the person has not shared with you"}, false, nil
	}
	return append([]string{"contact card:"}, contactCardLines(resolved.Contact, "  ")...), true, nil
}

// backlinksAction lists the pages that link to a record. It names pages of
// the agent's own memory and nothing of the record, so it needs no grant.
func backlinksAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	recordKind, recordId, startMS, endMS, err := models.ParseRecordReference(strings.TrimSpace(arguments.Record))
	if err != nil {
		return nil, err
	}
	var links []*models.AgentRecordLink
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		links, err = tx.ListAgentRecordBacklinks(run.Agent().ID, recordKind, recordId, startMS, endMS, searchLimit*5)
		return err
	}); err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return tools.TextResult("no page is linked to %s", arguments.Record), nil
	}
	lines := make([]string, 0, len(links))
	for _, link := range links {
		relation, guess := string(link.RecordRelation), ""
		if link.Proposed() {
			relation, guess = "perhaps "+relation, " (the agent's guess)"
		}
		line := "- " + link.NodePath + " " + relation + " this"
		if link.RecordKind == models.RecordRecordingPassage && (link.PassageStartMS != 0 || link.PassageEndMS != 0) {
			line += fmt.Sprintf(" at %d-%d ms", link.PassageStartMS, link.PassageEndMS)
		}
		lines = append(lines, line+guess)
	}
	return tools.TextResult("%s", "pages linked to "+arguments.Record+":\n"+strings.Join(lines, "\n")), nil
}

// isPersonsWord says whether the quote is in what the person last wrote in
// this conversation. A link or an unlink that changes what the person
// stated needs their words, and a run with nobody present has none: the
// agent may propose, never state.
func isPersonsWord(ctx context.Context, run tools.Run, quote string) (bool, error) {
	quote = strings.ToLower(strings.Join(strings.Fields(quote), " "))
	conversationId := tools.ConversationIDOf(run)
	if len(quote) < 8 || run.Headless() || conversationId == "" {
		return false, nil
	}
	var said string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		messageId, err := tx.LastAgentPersonMessageID(conversationId)
		if err != nil || messageId == "" {
			return err
		}
		message, err := tx.GetAgentMessage(messageId)
		if err != nil || message == nil {
			return err
		}
		said = message.Content
		return nil
	}); err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(strings.Join(strings.Fields(said), " ")), quote), nil
}

// recordLinkAction links a page to a record: proposed unless the person's
// own words, quoted, said so.
func recordLinkAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := ownPath(run, arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("link which page? give a path, like people/alice-chen")
	}
	recordKind, recordId, startMS, endMS, err := models.ParseRecordReference(strings.TrimSpace(arguments.Record))
	if err != nil {
		return nil, err
	}
	recordRelation := models.AgentRecordRelation(strings.ToLower(strings.TrimSpace(arguments.Relation)))
	link := &models.AgentRecordLink{
		AgentID: run.Agent().ID, RecordKind: recordKind, RecordID: recordId, RecordRelation: recordRelation,
		RecordDetail:   models.AgentRecordDetail{TranscriptRunID: strings.TrimSpace(arguments.TranscriptRun)},
		PassageStartMS: startMS, PassageEndMS: endMS,
		LinkStatus: models.EdgeProposed, Note: strings.TrimSpace(arguments.Text),
		Evidence: []models.Evidence{conversationEvidence(run, path+" "+string(recordRelation)+" "+arguments.Record)},
	}
	if recordRelation == "" {
		return nil, fmt.Errorf("what is the page to the record? relation is one of %s", relationNamesFor(recordKind))
	}
	if err := link.Validate(); err != nil {
		return nil, fmt.Errorf("%s; for a %s the relation is one of %s", err, recordKind, relationNamesFor(recordKind))
	}
	switch strings.ToLower(strings.TrimSpace(arguments.LinkStatus)) {
	case "", string(models.EdgeProposed):
	case string(models.EdgeStated):
		isTheirs, err := isPersonsWord(ctx, run, arguments.Quote)
		if err != nil {
			return nil, err
		}
		if !isTheirs {
			return nil, fmt.Errorf("a link is stated only on the person's own words: quote what they just asked for in quote, and it has to be in their last message. Without that, link it as proposed (the default), which reads as a guess")
		}
		link.LinkStatus = models.EdgeStated
		link.Evidence[0].Quote = tools.FirstWords(arguments.Quote, 20)
	default:
		return nil, fmt.Errorf("link_status is stated or proposed")
	}
	var node *models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		if node, err = tx.GetAgentNode(run.Agent().ID, path); err != nil {
			return err
		}
		if node == nil {
			return fmt.Errorf("there is no page at %s", path)
		}
		// Only what the agent could read anyway. A link to a record it
		// cannot open is a way to learn that the record is there, and
		// to be shown it by a page later.
		resolved, err := recordlink.ResolveRecord(tx, recordLinkReader(run), recordKind, recordId, link.RecordDetail, startMS, endMS)
		if err != nil {
			return err
		}
		switch {
		case resolved.IsUnavailable:
			return fmt.Errorf("there is no %s %q among the person's records", recordKind, recordId)
		case resolved.IsWithheld:
			return fmt.Errorf("the person has not shared that %s with you, so you cannot link a page to it", recordKind)
		}
		if recordKind == models.RecordRecordingPassage {
			if err := recordlink.ValidateTranscriptRun(tx, run.Agent().UserID, recordId, link.RecordDetail.TranscriptRunID); err != nil {
				return err
			}
		}
		link.NodeID = node.ID
		// A stated link to a card also names it on the page; a guess names
		// nothing. The database keeps that rule.
		_, err = tx.PutAgentRecordLink(link)
		return err
	}); err != nil {
		return nil, err
	}
	statement := path + " " + string(recordRelation) + " " + arguments.Record
	if link.LinkStatus == models.EdgeProposed {
		statement += " (proposed: it reads as a guess until the person confirms it)"
	}
	result := tools.TextResult("%s", statement)
	result.Note = "linked " + path + " to a record"
	return result, nil
}

// relationNamesFor is the relations that fit a kind of record.
func relationNamesFor(recordKind models.AgentRecordKind) string {
	names := []string{}
	for _, relation := range models.RecordRelationsOf(recordKind) {
		names = append(names, string(relation))
	}
	return strings.Join(names, ", ")
}

// recordUnlinkAction takes a page's links to a record away. Taking away
// one the person stated needs their words; a proposal is the agent's own to
// withdraw.
func recordUnlinkAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := ownPath(run, arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("unlink which page? give a path, like people/alice-chen")
	}
	recordKind, recordId, startMS, endMS, err := models.ParseRecordReference(strings.TrimSpace(arguments.Record))
	if err != nil {
		return nil, err
	}
	recordRelation := models.AgentRecordRelation(strings.ToLower(strings.TrimSpace(arguments.Relation)))
	var removed int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNode(run.Agent().ID, path)
		if err != nil {
			return err
		}
		if node == nil {
			return fmt.Errorf("there is no page at %s", path)
		}
		links, err := tx.ListAgentRecordLinks(run.Agent().ID, node.ID)
		if err != nil {
			return err
		}
		hasStated := false
		for _, link := range links {
			isMatch := link.RecordKind == recordKind && link.RecordID == recordId &&
				(recordRelation == "" || link.RecordRelation == recordRelation) &&
				(recordKind != models.RecordRecordingPassage || (startMS == 0 && endMS == 0) ||
					(link.PassageStartMS == startMS && link.PassageEndMS == endMS))
			hasStated = hasStated || (isMatch && !link.Proposed())
		}
		if hasStated {
			isTheirs, err := isPersonsWord(ctx, run, arguments.Quote)
			if err != nil {
				return err
			}
			if !isTheirs {
				return fmt.Errorf("the person stated that link, so it goes only on their own words: quote what they just asked for in quote")
			}
		}
		// Only a link the person stated is theirs to have removed, and is
		// remembered as removed. The agent taking back its own proposal is
		// not the person turning it down, so the dream may propose it again.
		// A card the page names goes with its is_contact link.
		if hasStated {
			removed, err = tx.DeleteAgentRecordLink(run.Agent().ID, node.ID, recordKind, recordId, recordRelation, startMS, endMS)
		} else {
			removed, err = tx.WithdrawAgentRecordLink(run.Agent().ID, node.ID, recordKind, recordId, recordRelation, startMS, endMS)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if removed == 0 {
		return tools.TextResult("%s was not linked to %s", path, arguments.Record), nil
	}
	result := tools.TextResult("%s is no longer linked to %s", path, arguments.Record)
	result.Note = "unlinked " + path + " from a record"
	return result, nil
}
