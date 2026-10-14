package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// A page's links to the person's own records, as the dashboard and the
// command line reach them.
//
// The person reads every link of their own agent's pages. What the agent
// itself may read of the same records is decided separately, by the agent's
// grants (see package recordlink), and a link made here does not change it.

// AgentRecordLinkQuery reads the links between pages and records.
type AgentRecordLinkQuery interface {
	// A page's links to the person's own records, a part at a time, each
	// with a title, a date and a dashboard path found when it is read, or
	// marked unavailable when the record is gone. Needs agent:use.
	ListAgentRecordLinks(ctx context.Context, arguments ListAgentRecordLinksArguments) (*AgentRecordLinksResult, error)

	// The pages that link to one record: a contact card, a mail message,
	// an event, a transaction, a receipt, or for a recording the passage
	// that overlaps a range. Needs agent:use.
	ReadRecordMemoryLinks(ctx context.Context, arguments ReadRecordMemoryLinksArguments) ([]*AgentRecordLinkView, error)
}

// AgentRecordLinkMutation makes and removes them.
type AgentRecordLinkMutation interface {
	// Link a page to a record of the person's own, stated: what the
	// person asked for. Linking again what the agent only proposed
	// confirms it. The record has to be the person's and has to be there.
	// Needs agent:use.
	LinkAgentRecord(ctx context.Context, arguments LinkAgentRecordArguments) (*AgentRecordLinkView, error)

	// Take a page's links to a record away; every relation when none is
	// given, and for a recording every passage when no range is given.
	// Says whether there was one. Needs agent:use.
	UnlinkAgentRecord(ctx context.Context, arguments UnlinkAgentRecordArguments) (bool, error)

	// State the agent's guesses at a page's links to a record: every
	// passage of a recording the page is linked to in that relation, or
	// every relation when none is given. Says how many it confirmed. Needs
	// agent:use.
	ConfirmAgentRecord(ctx context.Context, arguments ConfirmAgentRecordArguments) (int, error)

	// Name the contact card a person's page is about, from one of the
	// person's own address books, and link the page to it. Empty clears it.
	// Never makes a card. Needs agent:use.
	SetAgentNodeContact(ctx context.Context, arguments SetAgentNodeContactArguments) (bool, error)
}

type ListAgentRecordLinksArguments struct {
	Path   string `json:"path"`
	First  int    `json:"first" graphapi:"nullable"`
	Offset int    `json:"offset" graphapi:"nullable"`
}

type ReadRecordMemoryLinksArguments struct {
	RecordKind string `json:"recordKind"`
	RecordID   string `json:"recordId"`

	// PassageStartMS and PassageEndMS narrow a recording to the passages
	// overlapping a range, in milliseconds; both left out is the whole
	// recording.
	PassageStartMS int64 `json:"passageStartMS" graphapi:"nullable"`
	PassageEndMS   int64 `json:"passageEndMS" graphapi:"nullable"`
}

type LinkAgentRecordArguments struct {
	Path           string `json:"path"`
	RecordKind     string `json:"recordKind"`
	RecordID       string `json:"recordId"`
	RecordRelation string `json:"recordRelation"`

	// PassageStartMS and PassageEndMS are the range of a recording passage
	// in milliseconds, and TranscriptRunID the transcript it is counted in.
	PassageStartMS  int64  `json:"passageStartMS" graphapi:"nullable"`
	PassageEndMS    int64  `json:"passageEndMS" graphapi:"nullable"`
	TranscriptRunID string `json:"transcriptRunId" graphapi:"nullable"`
	Note            string `json:"note" graphapi:"nullable"`
}

type UnlinkAgentRecordArguments struct {
	Path           string `json:"path"`
	RecordKind     string `json:"recordKind"`
	RecordID       string `json:"recordId"`
	RecordRelation string `json:"recordRelation" graphapi:"nullable"`
	PassageStartMS int64  `json:"passageStartMS" graphapi:"nullable"`
	PassageEndMS   int64  `json:"passageEndMS" graphapi:"nullable"`
}

type ConfirmAgentRecordArguments struct {
	Path           string `json:"path"`
	RecordKind     string `json:"recordKind"`
	RecordID       string `json:"recordId"`
	RecordRelation string `json:"recordRelation" graphapi:"nullable"`

	// PassageStartMS and PassageEndMS narrow a recording to one passage;
	// both left out is every passage the page has of it.
	PassageStartMS int64 `json:"passageStartMS" graphapi:"nullable"`
	PassageEndMS   int64 `json:"passageEndMS" graphapi:"nullable"`
}

type SetAgentNodeContactArguments struct {
	Path string `json:"path"`

	// ContactID is the card in one of the person's address books; empty
	// clears it.
	ContactID string `json:"contactId" graphapi:"nullable"`
}

// AgentRecordLinkView is a link with its record found again for the
// person: what to call it, when it was, and where it opens.
type AgentRecordLinkView struct {
	// ID is the link's own identifier.
	ID string `json:"id"`

	// NodePath, NodeName and NodeKind are the page the link is on.
	NodePath string               `json:"nodePath"`
	NodeName string               `json:"nodeName"`
	NodeKind models.AgentNodeKind `json:"nodeKind"`

	RecordKind     models.AgentRecordKind     `json:"recordKind"`
	RecordID       string                     `json:"recordId"`
	RecordRelation models.AgentRecordRelation `json:"recordRelation"`

	// LinkStatus is stated, or proposed where the agent only guessed.
	LinkStatus models.AgentEdgeStatus `json:"linkStatus"`

	// PassageStartMS, PassageEndMS and TranscriptRunID place a recording
	// passage; zero and empty for any other kind.
	PassageStartMS  int64  `json:"passageStartMS"`
	PassageEndMS    int64  `json:"passageEndMS"`
	TranscriptRunID string `json:"transcriptRunId"`

	// PassageCount is how many passage links of the recording a page lists
	// as this one row; the range above is the first of them. Zero for any
	// other kind.
	PassageCount int `json:"passageCount"`

	Note       string            `json:"note"`
	Evidence   []models.Evidence `json:"evidence"`
	HappenedAt *time.Time        `json:"happenedAt"`
	CreatedAt  time.Time         `json:"createdAt"`

	// RecordTitle and RecordHappenedAt are read from the record when the
	// link is, never stored. DashboardPath opens it. IsUnavailable says
	// the record is gone, and the link can only be removed.
	RecordTitle      string     `json:"recordTitle"`
	RecordHappenedAt *time.Time `json:"recordHappenedAt"`
	DashboardPath    string     `json:"dashboardPath"`
	IsUnavailable    bool       `json:"isUnavailable"`
}

// AgentRecordLinksResult is a part of a page's record links.
type AgentRecordLinksResult struct {
	RecordLinks []*AgentRecordLinkView `json:"recordLinks"`

	// RecordLinkCount is how many the page has altogether, and
	// NextOffset the offset of the next part, zero at the last.
	RecordLinkCount int `json:"recordLinkCount"`
	NextOffset      int `json:"nextOffset"`
}

// AgentEvidenceRecordView is a record that one piece of a fact's evidence
// names, found again so that the source of a fact opens the record.
type AgentEvidenceRecordView struct {
	// FactID and EvidenceIndex say which evidence of which fact it is.
	FactID        string `json:"factId"`
	EvidenceIndex int    `json:"evidenceIndex"`

	RecordKind      models.AgentRecordKind `json:"recordKind"`
	RecordID        string                 `json:"recordId"`
	PassageStartMS  int64                  `json:"passageStartMS"`
	PassageEndMS    int64                  `json:"passageEndMS"`
	TranscriptRunID string                 `json:"transcriptRunId"`

	RecordTitle   string `json:"recordTitle"`
	DashboardPath string `json:"dashboardPath"`
	IsUnavailable bool   `json:"isUnavailable"`
}

// recordLinksOnPage is how many of a page's record links the page read
// carries; ListAgentRecordLinks reads on from there.
const recordLinksOnPage = 50

// recordLinkPageMost is the most one ListAgentRecordLinks answers with.
const recordLinkPageMost = 200

// evidenceRecordsMost is how many pieces of evidence a page read looks
// up records for.
const evidenceRecordsMost = 200

func (self *graph) recordLinkReader(found *models.Agent) recordlink.Reader {
	return recordlink.Reader{UserID: found.UserID, AgentID: found.ID}
}

// recordLinkViews is these links with their records found for the person.
func (self *graph) recordLinkViews(tx db.Transaction, found *models.Agent, links []*models.AgentRecordLink, pageNode *models.AgentNode) ([]*AgentRecordLinkView, error) {
	views := make([]*AgentRecordLinkView, 0, len(links))
	for _, link := range links {
		resolved, err := recordlink.Resolve(tx, self.recordLinkReader(found), link)
		if err != nil {
			return nil, err
		}
		view := &AgentRecordLinkView{
			ID: link.ID, NodePath: link.NodePath, NodeName: link.NodeName, NodeKind: link.NodeKind,
			RecordKind: link.RecordKind, RecordID: link.RecordID, RecordRelation: link.RecordRelation,
			LinkStatus: link.LinkStatus, PassageStartMS: link.PassageStartMS, PassageEndMS: link.PassageEndMS,
			TranscriptRunID: link.RecordDetail.TranscriptRunID,
			Note:            link.Note, Evidence: link.Evidence, HappenedAt: link.HappenedAt, CreatedAt: link.CreatedAt,
			RecordTitle: resolved.Title, RecordHappenedAt: resolved.HappenedAt,
			DashboardPath: resolved.DashboardPath, IsUnavailable: resolved.IsUnavailable,
		}
		if link.RecordKind == models.RecordRecordingPassage {
			view.PassageCount = 1
		}
		if view.Evidence == nil {
			view.Evidence = []models.Evidence{}
		}
		if pageNode != nil {
			view.NodePath, view.NodeName, view.NodeKind = pageNode.Path, pageNode.Name, pageNode.Kind
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) ListAgentRecordLinks(ctx context.Context, arguments ListAgentRecordLinksArguments) (*AgentRecordLinksResult, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if arguments.First < 0 || arguments.Offset < 0 {
		return nil, fmt.Errorf("%w: first and offset cannot be negative", api.ErrInvalidArguments)
	}
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page?")
	}
	tx := self.transaction(ctx)
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("there is no page at %s", path)
	}
	stored, err := tx.ListAgentRecordLinks(found.ID, node.ID)
	if err != nil {
		return nil, err
	}
	rows := recordlink.GroupPassages(stored)
	first := arguments.First
	if first == 0 || first > recordLinkPageMost {
		first = recordLinkPageMost
	}
	result := &AgentRecordLinksResult{RecordLinkCount: len(rows)}
	end := min(arguments.Offset+first, len(rows))
	if arguments.Offset < len(rows) {
		rows = rows[arguments.Offset:end]
	} else {
		rows = nil
	}
	if end < result.RecordLinkCount {
		result.NextOffset = end
	}
	if result.RecordLinks, err = self.recordLinkRowViews(tx, found, rows, node); err != nil {
		return nil, err
	}
	return result, nil
}

// recordLinkRowViews is the rows of a page's links with their records
// found for the person, a recording's passages counted in one row.
func (self *graph) recordLinkRowViews(tx db.Transaction, found *models.Agent, rows []recordlink.Row, pageNode *models.AgentNode) ([]*AgentRecordLinkView, error) {
	views, err := self.recordLinkViews(tx, found, recordlink.RowLinks(rows), pageNode)
	if err != nil {
		return nil, err
	}
	for index, row := range rows {
		views[index].PassageCount = row.PassageCount
	}
	return views, nil
}

func (self *graph) ReadRecordMemoryLinks(ctx context.Context, arguments ReadRecordMemoryLinksArguments) ([]*AgentRecordLinkView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	recordKind := models.AgentRecordKind(arguments.RecordKind)
	if !models.IsAgentRecordKind(recordKind) {
		return nil, fmt.Errorf("%w: %q is not a kind of record", api.ErrInvalidArguments, arguments.RecordKind)
	}
	if strings.TrimSpace(arguments.RecordID) == "" {
		return nil, fmt.Errorf("%w: which record?", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	links, err := tx.ListAgentRecordBacklinks(found.ID, recordKind, strings.TrimSpace(arguments.RecordID),
		arguments.PassageStartMS, arguments.PassageEndMS, recordLinkPageMost)
	if err != nil {
		return nil, err
	}
	return self.recordLinkViews(tx, found, links, nil)
}

// requireOwnRecord is the record a person names, found as theirs, or why
// they cannot link it. Somebody else's record and one that does not exist
// are refused alike.
func (self *graph) requireOwnRecord(tx db.Transaction, found *models.Agent, recordKind models.AgentRecordKind, recordId string, detail models.AgentRecordDetail, startMS, endMS int64) error {
	resolved, err := recordlink.ResolveRecord(tx, self.recordLinkReader(found), recordKind, recordId, detail, startMS, endMS)
	if err != nil {
		return err
	}
	if resolved.IsUnavailable {
		return fmt.Errorf("%w: there is no %s %q among your records", api.ErrInvalidArguments, recordKind, recordId)
	}
	return nil
}

func (self *graph) LinkAgentRecord(ctx context.Context, arguments LinkAgentRecordArguments) (*AgentRecordLinkView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page?")
	}
	link := &models.AgentRecordLink{
		AgentID:    found.ID,
		RecordKind: models.AgentRecordKind(arguments.RecordKind), RecordID: strings.TrimSpace(arguments.RecordID),
		RecordRelation: models.AgentRecordRelation(arguments.RecordRelation),
		RecordDetail:   models.AgentRecordDetail{TranscriptRunID: arguments.TranscriptRunID},
		PassageStartMS: arguments.PassageStartMS, PassageEndMS: arguments.PassageEndMS,
		LinkStatus: models.EdgeStated, Note: strings.TrimSpace(arguments.Note),
		Evidence: []models.Evidence{{Kind: models.EvidencePerson}},
	}
	if err := link.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	tx := self.writing(ctx)
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("there is no page at %s", path)
	}
	if err := self.requireOwnRecord(tx, found, link.RecordKind, link.RecordID, link.RecordDetail, link.PassageStartMS, link.PassageEndMS); err != nil {
		return nil, err
	}
	if link.RecordKind == models.RecordRecordingPassage {
		if err := recordlink.ValidateTranscriptRun(tx, found.UserID, link.RecordID, link.RecordDetail.TranscriptRunID); err != nil {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
	}
	link.NodeID = node.ID
	stored, err := tx.PutAgentRecordLink(link)
	if err != nil {
		return nil, err
	}
	views, err := self.recordLinkViews(tx, found, []*models.AgentRecordLink{stored}, node)
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

func (self *graph) UnlinkAgentRecord(ctx context.Context, arguments UnlinkAgentRecordArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	recordKind := models.AgentRecordKind(arguments.RecordKind)
	if !models.IsAgentRecordKind(recordKind) {
		return false, fmt.Errorf("%w: %q is not a kind of record", api.ErrInvalidArguments, arguments.RecordKind)
	}
	recordRelation := models.AgentRecordRelation(arguments.RecordRelation)
	if recordRelation != "" && !models.IsAgentRecordRelation(recordRelation) {
		return false, fmt.Errorf("%w: %q is not a relation to a record", api.ErrInvalidArguments, arguments.RecordRelation)
	}
	path := models.NormalizePath(arguments.Path)
	tx := self.writing(ctx)
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return false, err
	}
	if node == nil {
		return false, fmt.Errorf("there is no page at %s", path)
	}
	removed, err := tx.DeleteAgentRecordLink(found.ID, node.ID, recordKind, strings.TrimSpace(arguments.RecordID),
		recordRelation, arguments.PassageStartMS, arguments.PassageEndMS)
	return removed > 0, err
}

func (self *graph) ConfirmAgentRecord(ctx context.Context, arguments ConfirmAgentRecordArguments) (int, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return 0, err
	}
	recordKind := models.AgentRecordKind(arguments.RecordKind)
	if !models.IsAgentRecordKind(recordKind) {
		return 0, fmt.Errorf("%w: %q is not a kind of record", api.ErrInvalidArguments, arguments.RecordKind)
	}
	recordRelation := models.AgentRecordRelation(arguments.RecordRelation)
	if recordRelation != "" && !models.IsAgentRecordRelation(recordRelation) {
		return 0, fmt.Errorf("%w: %q is not a relation to a record", api.ErrInvalidArguments, arguments.RecordRelation)
	}
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return 0, fmt.Errorf("which page?")
	}
	tx := self.writing(ctx)
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return 0, err
	}
	if node == nil {
		return 0, fmt.Errorf("there is no page at %s", path)
	}
	links, err := tx.ListAgentRecordLinks(found.ID, node.ID)
	if err != nil {
		return 0, err
	}
	// Stating a link again is how a person confirms it, one link at a
	// time, so a row that stands for many passages states each of them.
	confirmed := 0
	for _, link := range links {
		if !link.Proposed() || link.RecordKind != recordKind || link.RecordID != strings.TrimSpace(arguments.RecordID) ||
			(recordRelation != "" && link.RecordRelation != recordRelation) ||
			(recordKind == models.RecordRecordingPassage && (arguments.PassageStartMS != 0 || arguments.PassageEndMS != 0) &&
				(link.PassageStartMS != arguments.PassageStartMS || link.PassageEndMS != arguments.PassageEndMS)) {
			continue
		}
		stated := *link
		stated.LinkStatus = models.EdgeStated
		stated.Evidence = []models.Evidence{{Kind: models.EvidencePerson}}
		if _, err := tx.PutAgentRecordLink(&stated); err != nil {
			return 0, err
		}
		confirmed++
	}
	return confirmed, nil
}

func (self *graph) SetAgentNodeContact(ctx context.Context, arguments SetAgentNodeContactArguments) (bool, error) {
	person, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	path := models.NormalizePath(arguments.Path)
	tx := self.writing(ctx)
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return false, err
	}
	if node == nil {
		return false, fmt.Errorf("there is no page at %s", path)
	}
	switch node.Kind {
	case models.NodeSelf:
		return false, fmt.Errorf("%w: the card that is the person themselves is set with SetMyContact", api.ErrInvalidArguments)
	case models.NodePerson:
	default:
		return false, fmt.Errorf("%w: only a page about a person has a contact card", api.ErrInvalidArguments)
	}
	contactId := strings.TrimSpace(arguments.ContactID)
	if contactId != "" {
		// Theirs, and one that exists: a card in another person's book is
		// a way to read it.
		contact, err := self.contactOfUser(tx, person.UserID(), contactId)
		if err != nil {
			return false, err
		}
		if contact == nil {
			return false, fmt.Errorf("there is no contact %q in their address books", contactId)
		}
	}
	if node.ContactID != "" && node.ContactID != contactId {
		// Swapping a card is not the person removing the old link, so the
		// dream may propose that card again.
		if _, err := tx.WithdrawAgentRecordLink(found.ID, node.ID, models.RecordContact, node.ContactID, models.RecordRelationIsContact, 0, 0); err != nil {
			return false, err
		}
	}
	if err := tx.SetAgentNodeContact(found.ID, node.ID, contactId); err != nil {
		return false, err
	}
	if contactId == "" {
		return true, nil
	}
	_, err = tx.PutAgentRecordLink(&models.AgentRecordLink{
		AgentID: found.ID, NodeID: node.ID, RecordKind: models.RecordContact, RecordID: contactId,
		RecordRelation: models.RecordRelationIsContact, LinkStatus: models.EdgeStated,
		Evidence: []models.Evidence{{Kind: models.EvidencePerson}},
	})
	return err == nil, err
}

// evidenceRecordsOf is the records that these facts' evidence names, found
// for the person. Evidence of a kind of record names it by identifier;
// evidence of a document that is a sent message is mapped to the mail item
// it was read from, since the document is a copy and the message is what a
// person opens.
func (self *graph) evidenceRecordsOf(tx db.Transaction, found *models.Agent, facts []*models.AgentFact) ([]*AgentEvidenceRecordView, error) {
	views := []*AgentEvidenceRecordView{}
	type pending struct {
		fact     *models.AgentFact
		position int
	}
	var documentEvidence []pending
	var documentIds []string
	for _, fact := range facts {
		for position, evidence := range fact.Evidence {
			if len(views)+len(documentEvidence) >= evidenceRecordsMost {
				break
			}
			recordKind := models.AgentRecordKind(evidence.Kind)
			if evidence.Kind == models.EvidenceDocument {
				documentEvidence = append(documentEvidence, pending{fact, position})
				documentIds = append(documentIds, strings.Trim(strings.TrimSpace(evidence.ID), "[]"))
				continue
			}
			if !models.IsAgentRecordKind(recordKind) || evidence.ID == "" {
				continue
			}
			view, err := self.evidenceRecordView(tx, found, fact.ID, position, recordKind, evidence)
			if err != nil {
				return nil, err
			}
			views = append(views, view)
		}
	}
	if len(documentEvidence) == 0 {
		return views, nil
	}
	documents, err := tx.GetAgentDocuments(found.ID, documentIds)
	if err != nil {
		return nil, err
	}
	sources, err := tx.ListAgentSources(found.ID)
	if err != nil {
		return nil, err
	}
	isSentSource := map[string]bool{}
	for _, source := range sources {
		isSentSource[source.ID] = source.Kind == models.SourceSent
	}
	mailIds := []string{}
	mailIdOf := map[string]string{}
	for _, document := range documents {
		if isSentSource[document.SourceID] && document.ExternalID != "" {
			mailIdOf[document.ID] = document.ExternalID
			mailIds = append(mailIds, document.ExternalID)
		}
	}
	if len(mailIds) == 0 {
		return views, nil
	}
	itemIds, err := tx.FirstOwnedItemIDsByMail(found.UserID, mailIds)
	if err != nil {
		return nil, err
	}
	for _, entry := range documentEvidence {
		documentId := strings.Trim(strings.TrimSpace(entry.fact.Evidence[entry.position].ID), "[]")
		itemId := itemIds[mailIdOf[documentId]]
		if itemId == "" {
			continue
		}
		view, err := self.evidenceRecordView(tx, found, entry.fact.ID, entry.position, models.RecordMail, models.Evidence{ID: itemId})
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) evidenceRecordView(tx db.Transaction, found *models.Agent, factId string, position int, recordKind models.AgentRecordKind, evidence models.Evidence) (*AgentEvidenceRecordView, error) {
	detail := models.AgentRecordDetail{TranscriptRunID: evidence.TranscriptRunID}
	resolved, err := recordlink.ResolveRecord(tx, self.recordLinkReader(found), recordKind, evidence.ID, detail, evidence.StartMS, evidence.EndMS)
	if err != nil {
		return nil, err
	}
	return &AgentEvidenceRecordView{
		FactID: factId, EvidenceIndex: position, RecordKind: recordKind, RecordID: evidence.ID,
		PassageStartMS: evidence.StartMS, PassageEndMS: evidence.EndMS, TranscriptRunID: evidence.TranscriptRunID,
		RecordTitle: resolved.Title, DashboardPath: resolved.DashboardPath, IsUnavailable: resolved.IsUnavailable,
	}, nil
}
