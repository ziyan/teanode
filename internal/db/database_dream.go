package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/version"
)

// What the nightly run did, and the two queries it needs to know where it
// got to.
//
// The important property is that nothing here lets a night skip work. A
// document is marked read only after it has been; a page is marked
// rewritten only after it has been. A night that dies halfway leaves both
// marks where they were, so tomorrow does the rest rather than starting
// after it.

// DreamOperation is the nightly run's own store.
type DreamOperation interface {
	StartAgentDream(dream *models.AgentDream) (*models.AgentDream, error)
	FinishAgentDream(dream *models.AgentDream) error

	AdvanceAgentDreamProgress(dream *models.AgentDream, digestedCount, filedCount int) error
	ListAgentDreams(agentId string, limit int) ([]*models.AgentDream, error)

	// CountAgentDocumentsReading is how far the night has got, by the same
	// rule that decides what a night reads: every document but a chat
	// unit, and a chat unit only when the person was in it and it was a
	// conversation.
	CountAgentDocumentsReading(agentId string, names []string) (*AgentReadingCounts, error)

	// ListAgentDocumentsToDigest is what has been indexed and not yet
	// read, in the order a night should read it, and how much is waiting
	// altogether.
	//
	// The order is the priority the person would choose: what they wrote
	// themselves, then what they took part in, then the rest newest
	// first. The count beside it is what the log reports, so a backlog is
	// a number the person can see rather than work silently dropped.
	//
	// names are what the person is called in chat: a thread they took
	// part in is read before anything else, and a chat they were not in
	// is not read at all -- it stays searchable, and is not counted as
	// waiting.
	ListAgentDocumentsToDigest(agentId string, names []string, limit int) ([]*models.AgentDocument, int64, error)
	MarkAgentDocumentsDigested(documentIds []string, at time.Time) error

	// MarkAgentDocumentsGarbled says the night could not make sense of
	// what came back about these, and counts it; AgentDocumentsGivenUpOn
	// says how often that has happened to each.
	MarkAgentDocumentsGarbled(documentIds []string, at time.Time) error
	AgentDocumentsGivenUpOn(documentIds []string) (map[string]int, error)

	// MeasureAgentDocuments is how many characters of text each of these
	// documents actually holds, by id, leaving out the ones that hold
	// none.
	//
	// The night asks before deciding that something is too slight to be
	// worth a call. A document's recorded size is the size of a file on
	// somebody's disk, and the kinds that are not files -- a commit, a
	// message -- never had one, so a night that read the size took them
	// all for empty. The text is in the passages, so the passages are
	// what is measured.
	MeasureAgentDocuments(agentId string, documentIds []string) (map[string]int64, error)

	// ListAgentAttachmentsToDecide is the pictures and files a record came
	// with that nobody has read and the night has not decided about:
	// newest first, and only the ones whose bytes this server actually
	// holds, since a file it cannot fetch is not one it can decide to
	// open.
	//
	// Apart from ListAgentDocumentsToDigest because it is the opposite
	// question. That one asks what to read; this one asks what is worth
	// opening at all, which is asked of what has no text and therefore
	// nothing to read.
	ListAgentAttachmentsToDecide(agentId string, limit int) ([]*models.AgentDocument, error)

	// MarkAgentDocumentsDeclined records that the night decided against
	// opening these, and why, so that it is not paid for twice.
	MarkAgentDocumentsDeclined(documentIds []string, reason string, at time.Time) error

	// UnmarkAgentDocumentsDigested puts back into the queue everything
	// marked read since the given time: for a night that marked what it
	// never read. Says how many.
	UnmarkAgentDocumentsDigested(agentId string, since time.Time) (int64, error)

	// ListAgentNodesToConsolidate is the pages whose facts have changed
	// since their summary was written.
	ListAgentNodesToConsolidate(agentId string, limit int) ([]*models.AgentNode, error)
	MarkAgentNodeConsolidated(nodeId string, at time.Time) error

	// ListAgentNodesEmpty is the pages that say nothing: no opening, no
	// facts, nothing under them, no links, and made before the given
	// time. The roots and the period pages are never among them.
	ListAgentNodesEmpty(agentId string, before time.Time, limit int) ([]*models.AgentNode, error)

	// ListAgentMonthsToWriteUp is the months that hold at least so much of
	// the person's own record -- facts placed in them, their commits,
	// threads they took part in -- and have no page yet, or a page that
	// is blank or reads like a guess (matches the pattern), as
	// "2025/08". Months with no page come first, then most recent first.
	ListAgentMonthsToWriteUp(agentId string, names []string, least, limit int, guessed string) ([]string, error)

	// ListAgentNodesCrowded is the pages holding more than so many facts,
	// most crowded first, never a folder.
	ListAgentNodesCrowded(agentId string, above, limit int) ([]*models.AgentNode, error)

	// ListAgentFactsSaidTwice is every fact whose page already carries
	// the same words on a lower number: the later copies, never the
	// first.
	ListAgentFactsSaidTwice(agentId string, limit int) ([]*models.AgentFact, error)

	// FirstAgentFactSayingIt is the live fact on the same page that says
	// the same words on a lower number, or nothing where there is none.
	//
	// Asked of the database, because the page is where the pair is and a
	// page may hold more facts than any caller wants to carry. Scanning a
	// listing instead meant a duplicate the query above had found could
	// not be folded, and on a page with more facts than the listing took
	// it never could.
	FirstAgentFactSayingIt(agentId, nodeId, text string, below int) (*models.AgentFact, error)

	// RecomputeAgentImportance rewrites what the index is ordered by, and
	// RetireAgentFacts marks what has not been wanted in a long time
	// dormant. Neither deletes anything.
	RecomputeAgentImportance(agentId string, now time.Time) (int64, error)
	RetireAgentFacts(agentId string, before time.Time) (int, error)

	// The deterministic half of a night: links strengthened by being used
	// together and weakened by not being, a threshold that rises as the
	// graph grows, and the pages that fall under it.
	StrengthenAgentEdges(agentId string, since time.Time, rise, factor float64) (int64, error)
	AgentImportanceThreshold(agentId string, target int) (float64, error)
	RetireAgentNodes(agentId string, threshold float64, before time.Time) (int, error)

	// Walking the graph, for the pass that looks for relations nobody
	// wrote down.
	WalkAgentGraph(agentId, fromId string, steps int) ([]*models.AgentNode, error)
	ListAgentNodesForWalking(agentId string, limit int) ([]*models.AgentNode, error)

	// ListAgentFactsWrittenBefore is what an older build of this program
	// filed, oldest first, so a newer one can go back over it under the
	// rules it has now. See migration 0073.
	ListAgentFactsWrittenBefore(agentId, version string, limit int) ([]*models.AgentFact, error)

	// MarkAgentFactsSeen stamps rows with the build that has looked at
	// them, so the same pass does not look again.
	MarkAgentFactsSeen(agentId string, factIds []string) error
}

// AgentReadingCounts is how far the night has got through what was
// indexed, and what became of the pictures and files a record came with.
//
// The last three are apart from the first two because two of them are
// neither waiting nor read. An attachment with no text -- a picture whose
// bytes are kept and which nothing has described -- is not waiting for a
// night that could do nothing with it, and calling it read would be a
// lie: a source of fifty thousand screenshots would say it was all read
// and nothing would have been.
type AgentReadingCounts struct {
	// Waiting has been indexed and not read yet; Read has been.
	Waiting int64
	Read    int64

	// Undecided is a file the night has not yet looked at the outside of,
	// and Declined one it looked at and decided against opening. Described
	// is one it opened and made text of, which is a document like any
	// other and is counted in Waiting or Read as well -- it is here so
	// that a source's page can say how many of its pictures were read
	// rather than only how many were not.
	Undecided int64
	Declined  int64
	Described int64
}

type agentDreamModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	AgentID    string     `gorm:"column:agent_id"`
	StartedAt  time.Time  `gorm:"column:started_at"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
	JobID      string     `gorm:"column:job_id"`
	Digested   int        `gorm:"column:digested"`
	Filed      int        `gorm:"column:filed"`
	Merged     int        `gorm:"column:merged"`
	Rewritten  int        `gorm:"column:rewritten"`
	Moved      int        `gorm:"column:moved"`
	Dormant    int        `gorm:"column:dormant"`
	Embedded   int        `gorm:"column:embedded"`
	Backlog    int        `gorm:"column:backlog"`
	Coarse     bool       `gorm:"column:coarse"`

	Revised      int `gorm:"column:revised"`
	Strengthened int `gorm:"column:strengthened"`
	Associated   int `gorm:"column:associated"`
	Rehearsed    int `gorm:"column:rehearsed"`
	Gaps         int `gorm:"column:gaps"`
	Unknown      int `gorm:"column:unknown"`

	Proposals []byte `gorm:"column:proposals;type:jsonb"`
	Tokens    int64  `gorm:"column:tokens"`
	Notes     string `gorm:"column:notes"`
	LastError string `gorm:"column:last_error"`
}

func (agentDreamModel) TableName() string { return "agent_dream" }

// dreamCutShort is what a dream says when the server restarted under it.
const dreamCutShort = "the server restarted before the dream was over"

func (self *transaction) StartAgentDream(dream *models.AgentDream) (*models.AgentDream, error) {
	if dream.AgentID == "" {
		return nil, fmt.Errorf("db: a dream needs an agent")
	}
	created := *dream
	created.ID = newID()
	if created.StartedAt.IsZero() {
		created.StartedAt = time.Now()
	}
	created.StartedAt = created.StartedAt.Truncate(time.Microsecond)
	// One night at a time. An earlier one still marked as working was cut
	// short by a restart, since the night that ended it would have written
	// its finish. Left as it is, it says "still working" forever.
	if err := self.tx.Model(&agentDreamModel{}).
		Where("\"agent_id\" = ? AND \"finished_at\" IS NULL", created.AgentID).
		Updates(map[string]any{"finished_at": created.StartedAt, "last_error": dreamCutShort}).Error; err != nil {
		return nil, err
	}
	row := &agentDreamModel{
		ID: created.ID, AgentID: created.AgentID, StartedAt: created.StartedAt,
		JobID: created.JobID, Proposals: []byte("[]"),
	}
	if err := self.tx.Create(row).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) FinishAgentDream(dream *models.AgentDream) error {
	proposals := dream.Proposals
	if proposals == nil {
		proposals = []models.DreamProposal{}
	}
	encoded, err := json.Marshal(proposals)
	if err != nil {
		return err
	}
	return self.tx.Model(&agentDreamModel{}).Where(`"id" = ?`, dream.ID).Updates(map[string]any{
		"finished_at": dream.FinishedAt, "digested": dream.Digested, "filed": dream.Filed,
		"merged": dream.Merged, "rewritten": dream.Rewritten, "moved": dream.Moved,
		"dormant": dream.Dormant, "embedded": dream.Embedded, "backlog": dream.Backlog,
		"coarse": dream.Coarse, "proposals": encoded, "tokens": dream.Tokens,
		"strengthened": dream.Strengthened, "associated": dream.Associated, "revised": dream.Revised,
		"rehearsed": dream.Rehearsed, "gaps": dream.Gaps, "unknown": dream.Unknown,
		"notes": dream.Notes, "last_error": dream.LastError,
	}).Error
}

// AdvanceAgentDreamProgress adds one committed batch without overwriting another
// concurrent batch's counts. The caller also marks its documents in this transaction.
func (self *transaction) AdvanceAgentDreamProgress(dream *models.AgentDream, digestedCount, filedCount int) error {
	if dream == nil || dream.ID == "" {
		return nil
	}
	update := self.tx.Exec(`UPDATE agent_dream SET digested = digested + ?, filed = filed + ?,
		backlog = ?, tokens = GREATEST(tokens, ?) WHERE id = ? AND agent_id = ?`,
		digestedCount, filedCount, dream.Backlog, dream.Tokens, dream.ID, dream.AgentID)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("the dream no longer exists")
	}
	return nil
}

func (self *transaction) ListAgentDreams(agentId string, limit int) ([]*models.AgentDream, error) {
	if limit <= 0 {
		limit = 30
	}
	var rows []agentDreamModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Order(`"started_at" DESC`).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	dreams := make([]*models.AgentDream, 0, len(rows))
	for index := range rows {
		row := &rows[index]
		dream := &models.AgentDream{
			ID: row.ID, AgentID: row.AgentID, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
			JobID:    row.JobID,
			Digested: row.Digested, Filed: row.Filed, Merged: row.Merged, Rewritten: row.Rewritten,
			Moved: row.Moved, Dormant: row.Dormant, Embedded: row.Embedded, Backlog: row.Backlog,
			Coarse: row.Coarse, Tokens: row.Tokens, Notes: row.Notes, LastError: row.LastError,
			Strengthened: row.Strengthened, Associated: row.Associated, Revised: row.Revised,
			Rehearsed: row.Rehearsed, Gaps: row.Gaps, Unknown: row.Unknown,
			Proposals: []models.DreamProposal{},
		}
		if len(row.Proposals) > 0 {
			if err := json.Unmarshal(row.Proposals, &dream.Proposals); err != nil {
				return nil, err
			}
		}
		dreams = append(dreams, dream)
	}
	return dreams, nil
}

// ListAgentDocumentsToDigest is what is waiting to be read, best first.
//
// "Best" is the person's own priority: what they wrote, then what they
// took part in, then everything else newest first. A night gets through
// as much as its budget allows and the rest waits, which is why the
// second return value -- how much is waiting -- is reported and shown.
func (self *transaction) ListAgentDocumentsToDigest(agentId string, names []string, limit int) ([]*models.AgentDocument, int64, error) {
	if limit <= 0 {
		limit = 400
	}
	if len(names) == 0 {
		names = []string{""}
	}
	// jsonb_exists and jsonb_exists_any rather than the ? and ?| operators:
	// ? is how a parameter is written, so the driver read the operator as
	// one and substituted the next argument into it.
	//
	// A chat unit is read only when the person was in it and it is a
	// conversation rather than a remark: three posts or more. The rest of
	// an archive -- other people's channels, a quarter of a million of
	// them -- is searched when a question needs it and never read on its
	// own; reading it at four hundred a night would take years and file
	// other people's business.
	//
	// A file with no passages is not offered either. An attachment is
	// filed before anything can read it -- that is the point of keeping
	// its bytes -- and a night handed one would show the model a heading
	// and silence, learn nothing, and mark it read, which is the one
	// state it must not reach: read means read. It becomes eligible the
	// moment something gives it passages.
	const eligible = `"agent_id" = ? AND NOT jsonb_exists("metadata", 'digested')
		AND ("kind" <> 'chat' OR (
			jsonb_exists_any("metadata"->'participants', ?::text[])
			AND coalesce(("metadata"->>'posts')::int, 0) >= 3))
		AND ("kind" <> 'attachment' OR EXISTS (
			SELECT 1 FROM "agent_chunk" WHERE "document_id" = "agent_document"."id"))`
	var total []int64
	if err := self.tx.Raw(`SELECT count(*) FROM "agent_document" WHERE `+eligible,
		agentId, pq.Array(names)).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	var backlog int64
	if len(total) > 0 {
		backlog = total[0]
	}
	documents, err := self.documentsFrom(self.tx.Raw(`
		SELECT * FROM "agent_document"
		WHERE `+eligible+`
		ORDER BY
			CASE "kind" WHEN 'chat' THEN 0 WHEN 'journal' THEN 1 WHEN 'commit' THEN 2
				WHEN 'file' THEN CASE WHEN lower("title") ~ ? THEN 3 ELSE 5 END
				ELSE 4 END,
			"happened_at" DESC NULLS LAST
		LIMIT ?`, agentId, pq.Array(names), proseFile, limit))
	return documents, backlog, err
}

// MeasureAgentDocuments is how much text each of these documents holds.
//
// One query for the whole night's list rather than one per document:
// this is asked of everything waiting, which on a first ingest is
// thousands of rows.
func (self *transaction) MeasureAgentDocuments(agentId string, documentIds []string) (map[string]int64, error) {
	measured := map[string]int64{}
	if len(documentIds) == 0 {
		return measured, nil
	}
	var rows []struct {
		DocumentID string `gorm:"column:document_id"`
		Characters int64  `gorm:"column:characters"`
	}
	// char_length rather than octet_length: what the model is shown is
	// runes, and a page of Chinese counted in bytes is three times the
	// text it is.
	if err := self.tx.Raw(`
		SELECT "document_id", sum(char_length("text")) AS "characters"
		FROM "agent_chunk"
		WHERE "agent_id" = ? AND "document_id" = ANY(?)
		GROUP BY "document_id"`, agentId, pq.Array(documentIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		measured[row.DocumentID] = row.Characters
	}
	return measured, nil
}

func (self *transaction) CountAgentDocumentsReading(agentId string, names []string) (*AgentReadingCounts, error) {
	if len(names) == 0 {
		names = []string{""}
	}
	var counts []AgentReadingCounts
	// "Nothing has read it yet" is an attachment that has no passages:
	// its bytes are kept and no text has been made of them. The text of
	// a document lives in its chunks, so having none is the question, and
	// asking it of every kind would count the entries a reader refused as
	// well -- which are refusals, not files waiting for a reader.
	//
	// Such a file is set aside, and which of the two kinds of aside it is
	// depends on whether the night has decided about it. Both are outside
	// the reading: one waits for the night to look at what it would cost,
	// the other has been looked at and passed over, and neither moves on
	// its own.
	if err := self.tx.Raw(`SELECT
			count(*) FILTER (WHERE NOT "aside" AND NOT jsonb_exists("metadata", 'digested')) AS waiting,
			count(*) FILTER (WHERE NOT "aside" AND jsonb_exists("metadata", 'digested')) AS read,
			count(*) FILTER (WHERE "aside" AND NOT "declined") AS undecided,
			count(*) FILTER (WHERE "aside" AND "declined") AS declined,
			count(*) FILTER (WHERE "attachment" AND NOT "aside" AND NOT "declined") AS described
		FROM (
			SELECT d."metadata" AS "metadata",
				(d."kind" = ?) AS "attachment",
				jsonb_exists(d."metadata", 'declined') AS "declined",
				(d."kind" = ? AND NOT EXISTS (
					SELECT 1 FROM "agent_chunk" WHERE "document_id" = d."id")) AS "aside"
			FROM "agent_document" d
			WHERE d."agent_id" = ?
			AND (d."kind" <> 'chat' OR (
				jsonb_exists_any(d."metadata"->'participants', ?::text[])
				AND coalesce((d."metadata"->>'posts')::int, 0) >= 3))) AS "documents"`,
		string(models.DocumentAttachment), string(models.DocumentAttachment),
		agentId, pq.Array(names)).Scan(&counts).Error; err != nil {
		return nil, err
	}
	if len(counts) == 0 {
		return &AgentReadingCounts{}, nil
	}
	return &counts[0], nil
}

// ListAgentAttachmentsToDecide is what the night has yet to decide about.
//
// Only what this server holds the bytes of. A document filed while the
// person's machine was busy has no key yet and the next pass of its
// source fills one in; putting it to a model tonight would buy a decision
// about a file nothing could then open.
func (self *transaction) ListAgentAttachmentsToDecide(agentId string, limit int) ([]*models.AgentDocument, error) {
	if limit <= 0 {
		limit = 200
	}
	return self.documentsFrom(self.tx.Raw(`
		SELECT * FROM "agent_document"
		WHERE "agent_id" = ? AND "kind" = ? AND "storage_key" <> ''
		  AND NOT jsonb_exists("metadata", 'declined')
		  AND NOT jsonb_exists("metadata", 'digested')
		  AND NOT EXISTS (
			SELECT 1 FROM "agent_chunk" WHERE "document_id" = "agent_document"."id")
		ORDER BY "happened_at" DESC NULLS LAST
		LIMIT ?`, agentId, string(models.DocumentAttachment), limit))
}

// MarkAgentDocumentsDeclined says the night looked at what these would
// cost to open and decided against it.
//
// In the metadata beside 'digested', and for the same reason: it is a
// fact about this deployment's nightly run rather than about the
// document. A separate key rather than the same one, because declined is
// not read -- nothing has read it -- and a person who disagrees with what
// the agent passed over should be able to clear this without disturbing
// what really was read.
func (self *transaction) MarkAgentDocumentsDeclined(documentIds []string, reason string, at time.Time) error {
	if len(documentIds) == 0 {
		return nil
	}
	// The reason is most of why the row is kept rather than the file
	// deleted, so a caller that gives none still leaves something a
	// person can read and disagree with.
	if strings.TrimSpace(reason) == "" {
		reason = "the night decided against opening it"
	}
	return self.tx.Exec(
		`UPDATE "agent_document" SET "metadata" = "metadata" || jsonb_build_object('declined', ?::text, 'declinedAt', ?::text) WHERE "id" = ANY(?)`,
		reason, at.Format(time.RFC3339), pq.Array(documentIds)).Error
}

// proseFile is a file somebody wrote to be read: a readme, a note, a
// document. It is read before source code, which is the bulk of any
// checkout and says almost nothing about the person: a night that read
// four hundred files of Go filed two facts, with a hundred thousand more
// files behind them. The code stays indexed for search, and is read only
// once everything written in words has been.
const proseFile = `(^|/)(readme|changelog|contributing|notes?|todo)$|\.(md|markdown|txt|rst|adoc|org|tex|pdf|docx?|pptx?|xlsx?|odt|html?|eml)$`

// MarkAgentDocumentsGarbled says the night read these and the answer that
// came back could not be made sense of, and how many times that has now
// happened to each.
//
// A batch whose answer will not parse used to be marked read regardless,
// because a batch that blocks is a batch that blocks every night after it
// and the documents behind it are never reached. That is true and it is
// not a reason to lose them silently: what it cost was the whole batch,
// with nothing anywhere saying so.
//
// So the first unreadable answer is remembered and the documents are left
// waiting, which gives them one more night. The second marks them read,
// because two is enough to say it is not the weather -- and the count
// stays on the row, so what was given up on can be found and put back.
func (self *transaction) MarkAgentDocumentsGarbled(documentIds []string, at time.Time) error {
	if len(documentIds) == 0 {
		return nil
	}
	return self.tx.Exec(
		`UPDATE "agent_document" SET "metadata" = "metadata" || jsonb_build_object(
			'garbled', COALESCE(("metadata"->>'garbled')::int, 0) + 1,
			'garbledAt', ?::text) WHERE "id" = ANY(?)`,
		at.Format(time.RFC3339), pq.Array(documentIds)).Error
}

// AgentDocumentsGivenUpOn is how many of these the night has already
// failed to make sense of, by id, so a second failure can be told from a
// first.
func (self *transaction) AgentDocumentsGivenUpOn(documentIds []string) (map[string]int, error) {
	given := map[string]int{}
	if len(documentIds) == 0 {
		return given, nil
	}
	var rows []struct {
		ID    string `gorm:"column:id"`
		Times int    `gorm:"column:times"`
	}
	if err := self.tx.Raw(
		`SELECT "id", COALESCE(("metadata"->>'garbled')::int, 0) AS "times"
		 FROM "agent_document" WHERE "id" = ANY(?)`, pq.Array(documentIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		given[row.ID] = row.Times
	}
	return given, nil
}

// MarkAgentDocumentsDigested says these have been read.
//
// Written into the document's own metadata rather than a column of its
// own, because it is a fact about this deployment's nightly run rather
// than about the document, and a column added for one boolean is a
// migration everyone else pays for.
func (self *transaction) MarkAgentDocumentsDigested(documentIds []string, at time.Time) error {
	if len(documentIds) == 0 {
		return nil
	}
	return self.tx.Exec(
		`UPDATE "agent_document" SET "metadata" = "metadata" || jsonb_build_object('digested', ?::text) WHERE "id" = ANY(?)`,
		at.Format(time.RFC3339), pq.Array(documentIds)).Error
}

// ListAgentNodesToConsolidate is the pages whose facts have moved on
// since their summary was written.
func (self *transaction) ListAgentNodesToConsolidate(agentId string, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 100
	}
	// Either a fact has moved since the opening was written, or the page
	// has an opening and no facts at all.
	//
	// The second is a state that can only be wrong: an opening is written
	// from the facts, so with none left there is nothing it could have
	// come from. It happens when the last fact is struck -- by a person,
	// or by the pass that goes back over what an older build wrote -- and
	// without this clause such a page is never looked at again, because
	// the test for "due" was the existence of a fact that had changed.
	return self.nodesFrom(self.tx.Raw(`
		SELECT n.* FROM "agent_node" n
		WHERE n."agent_id" = ? AND NOT n."dormant"
		  AND (
			EXISTS (
				SELECT 1 FROM "agent_fact" f
				WHERE f."node_id" = n."id" AND NOT f."dormant" AND f."superseded_by" IS NULL
				  AND f."modified_at" > COALESCE(n."consolidated_at", to_timestamp(0))
			)
			OR (
				n."summary" <> ''
				AND NOT EXISTS (
					SELECT 1 FROM "agent_fact" f
					WHERE f."node_id" = n."id" AND NOT f."dormant"
				)
			)
		  )
		ORDER BY n."pinned" DESC, n."used_at" DESC NULLS LAST, n."modified_at" DESC
		LIMIT ?`, agentId, limit))
}

func (self *transaction) MarkAgentNodeConsolidated(nodeId string, at time.Time) error {
	// The zero time means "never", which is how a caller says a page is
	// due a fresh reading. Written as NULL rather than as the year one,
	// because the listing already reads NULL that way and a timestamp in
	// the year one in a person's notes is a thing somebody has to
	// explain.
	if at.IsZero() {
		return self.tx.Exec(`UPDATE "agent_node" SET "consolidated_at" = NULL WHERE "id" = ?`, nodeId).Error
	}
	return self.tx.Exec(`UPDATE "agent_node" SET "consolidated_at" = ? WHERE "id" = ?`, at, nodeId).Error
}

// RecomputeAgentImportance rewrites what the index is ordered by.
//
// A blend of how lately a page was wanted, how much it says, and how much
// else points at it, with the person's own page and the people they keep
// in their address book lifted. Arithmetic, in one statement, costing
// nothing: which is why it can be done every night and why nothing else
// is allowed to touch the ordering.
func (self *transaction) RecomputeAgentImportance(agentId string, now time.Time) (int64, error) {
	// Four things a page can be worth, and one it can be owed.
	//
	// The last term is novelty, and it exists to break a trap the other
	// three make between them: a page written last night has never been
	// used, so it scores nothing for use, so it is not in the index, so
	// nothing can use it, so it never will be. A fortnight's grace is
	// enough for something genuinely new to come up in conversation and
	// earn its place properly, and small enough that it cannot hold a
	// page there on its own.
	result := self.tx.Exec(`
		UPDATE "agent_node" n SET "importance" =
			  LEAST(1.0, (SELECT count(*) FROM "agent_fact" f WHERE f."node_id" = n."id" AND NOT f."dormant") / 12.0) * 0.30
			+ LEAST(1.0, (SELECT count(*) FROM "agent_edge" e WHERE e."from_id" = n."id" OR e."to_id" = n."id") / 6.0) * 0.15
			+ CASE WHEN n."used_at" IS NULL THEN 0.0
			       ELSE GREATEST(0.0, 1.0 - EXTRACT(EPOCH FROM (? - n."used_at")) / (90 * 86400.0)) END * 0.25
			+ CASE WHEN n."kind" = 'self' THEN 1.0
			       WHEN n."contact_id" IS NOT NULL THEN 0.6
			       WHEN n."kind" = 'period' THEN 0.4
			       WHEN n."kind" = 'folder' THEN 0.1
			       ELSE 0.3 END * 0.20
			+ GREATEST(0.0, 1.0 - EXTRACT(EPOCH FROM (? - n."created_at")) / (14 * 86400.0)) * 0.10
		WHERE n."agent_id" = ?`, now, now, agentId)
	return result.RowsAffected, result.Error
}

// RetireAgentFacts marks what has not been wanted in a long time dormant.
//
// Dormant, not deleted: it leaves the index and stays searchable, so a
// question about something from four years ago still finds it. A
// preference or a decision never retires -- those are asked for by name
// and are the whole point of keeping anything.
func (self *transaction) RetireAgentFacts(agentId string, before time.Time) (int, error) {
	result := self.tx.Exec(`
		UPDATE "agent_fact" SET "dormant" = true
		WHERE "agent_id" = ? AND NOT "dormant"
		  AND "kind" NOT IN ('preference', 'decision')
		  AND COALESCE("used_at", "created_at") < ?`, agentId, before)
	return int(result.RowsAffected), result.Error
}

// --- the deterministic half of a night ---------------------------------

// StrengthenAgentEdges raises the weight of every link whose two ends were
// both wanted since a moment, and lowers every link by a factor.
//
// This is synaptic homeostasis, and it is the difference between an edge
// weight meaning "somebody once made this link" and "this link is worth
// something". Two pages read in the same conversation are related in a
// way nobody wrote down; a link nothing has touched in months is not
// wrong, it is just no longer the first thing to say.
//
// The factor and the window are two readings of one interval, the stretch
// since this last ran, and both are the caller's. A constant instead made
// the fade depend on how often the night ran rather than on how long it
// had been, which is not what anybody meant by it.
//
// Deterministic and one statement each, so it costs nothing and can run
// every night. Nothing is deleted: a link decays towards a floor and
// stays readable.
func (self *transaction) StrengthenAgentEdges(agentId string, since time.Time, rise, factor float64) (int64, error) {
	// Down first, then up: an edge used today should end the night above
	// where it started, and doing it the other way round would shave the
	// rise off again.
	if err := self.tx.Exec(
		`UPDATE "agent_edge" SET "weight" = GREATEST(0.05, "weight" * ?) WHERE "agent_id" = ?`,
		factor, agentId).Error; err != nil {
		return 0, err
	}
	result := self.tx.Exec(`
		UPDATE "agent_edge" e SET "weight" = LEAST(4.0, e."weight" + ?), "used_at" = ?
		WHERE e."agent_id" = ?
		  AND EXISTS (SELECT 1 FROM "agent_node" n WHERE n."id" = e."from_id" AND n."used_at" >= ?)
		  AND EXISTS (SELECT 1 FROM "agent_node" n WHERE n."id" = e."to_id" AND n."used_at" >= ?)`,
		rise, time.Now(), agentId, since, since)
	return result.RowsAffected, result.Error
}

// AgentImportanceThreshold is the score below which a page is not worth
// keeping in the index, computed from the graph rather than fixed.
//
// The bar rises as the graph grows: mean importance minus a standard
// deviation scaled by how far past its target size the graph is. A
// constant would need re-tuning at every order of magnitude; this does
// not, and it says something true -- what counts as unimportant depends
// on what else there is.
func (self *transaction) AgentImportanceThreshold(agentId string, target int) (float64, error) {
	if target <= 0 {
		target = 400
	}
	var rows []struct {
		Mean   float64 `gorm:"column:mean"`
		Spread float64 `gorm:"column:spread"`
		Total  float64 `gorm:"column:total"`
	}
	if err := self.tx.Raw(`
		SELECT COALESCE(avg("importance"), 0) AS mean,
		       COALESCE(stddev_pop("importance"), 0) AS spread,
		       count(*) AS total
		FROM "agent_node" WHERE "agent_id" = ? AND NOT "dormant" AND NOT "pinned"`,
		agentId).Scan(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 || rows[0].Total <= float64(target) {
		return 0, nil // nothing to do until the graph is past its size
	}
	threshold := rows[0].Mean - rows[0].Spread*(rows[0].Total/float64(target))
	if threshold < 0.05 {
		threshold = 0.05
	}
	return threshold, nil
}

// RetireAgentNodes takes the least important pages out of the index.
//
// Out of the index, not out of the graph: a dormant page is still found
// by searching for it, and its facts are still read when it is. What it
// loses is the right to take up room in every prompt.
func (self *transaction) RetireAgentNodes(agentId string, threshold float64, before time.Time) (int, error) {
	if threshold <= 0 {
		return 0, nil
	}
	result := self.tx.Exec(`
		UPDATE "agent_node" SET "dormant" = true
		WHERE "agent_id" = ? AND NOT "dormant" AND NOT "pinned"
		  AND "kind" NOT IN ('self', 'folder', 'period')
		  AND "importance" < ?
		  AND COALESCE("used_at", "modified_at") < ?`, agentId, threshold, before)
	return int(result.RowsAffected), result.Error
}

// WalkAgentGraph is a path through the graph from a page, following the
// strongest links.
//
// What a REM-like pass needs: a handful of pages that are connected but
// not obviously so, to be asked whether the two ends have anything real
// to do with each other. Following weight rather than choosing at random
// means the walk goes where the graph is dense, which is where an unstated
// relation is most likely to be hiding.
func (self *transaction) WalkAgentGraph(agentId, fromId string, steps int) ([]*models.AgentNode, error) {
	if steps <= 0 {
		steps = 5
	}
	visited := map[string]bool{fromId: true}
	path := make([]*models.AgentNode, 0, steps)
	current := fromId
	for index := 0; index < steps; index++ {
		var next []struct {
			ID string `gorm:"column:id"`
		}
		// The strongest link out of here that the walk has not taken,
		// in either direction: a link is a relation, not an arrow.
		//
		// Weighted rather than strictly ordered, because a walk that is
		// deterministic takes the same path every night and asks the
		// same question of the same pair for ever. The jitter is small
		// enough that a strong link is still usually the one taken and
		// large enough that a night eventually sees the second-strongest.
		if err := self.tx.Raw(`
			SELECT CASE WHEN e."from_id" = ? THEN e."to_id" ELSE e."from_id" END AS id
			FROM "agent_edge" e
			WHERE e."agent_id" = ? AND (e."from_id" = ? OR e."to_id" = ?)
			ORDER BY e."weight" * (0.5 + random()) DESC
			LIMIT 8`, current, agentId, current, current).Scan(&next).Error; err != nil {
			return nil, err
		}
		moved := false
		for _, candidate := range next {
			if visited[candidate.ID] {
				continue
			}
			visited[candidate.ID] = true
			node, err := self.GetAgentNodeByID(agentId, candidate.ID)
			if err != nil {
				return nil, err
			}
			if node == nil {
				continue
			}
			path = append(path, node)
			current = candidate.ID
			moved = true
			break
		}
		if !moved {
			break
		}
	}
	return path, nil
}

// ListAgentNodesForWalking is where a REM-like pass starts: the pages
// that matter most and have something to walk from.
func (self *transaction) ListAgentNodesForWalking(agentId string, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 10
	}
	return self.nodesFrom(self.tx.Raw(`
		SELECT n.* FROM "agent_node" n
		WHERE n."agent_id" = ? AND NOT n."dormant" AND n."kind" NOT IN ('folder')
		  AND EXISTS (SELECT 1 FROM "agent_edge" e WHERE e."from_id" = n."id" OR e."to_id" = n."id")
		ORDER BY n."importance" DESC, n."used_at" DESC NULLS LAST
		LIMIT ?`, agentId, limit))
}

// ListAgentFactsWrittenBefore is what a build other than this one wrote.
//
// "Other than", not "older than": versions do not compare as strings
// once there are two digits in them, and what matters is only whether a
// row has been looked at by the build running now. A row this build has
// already been over carries its version and is skipped; everything else
// is offered exactly once, and the pass that looks at it stamps it
// whether or not it changed anything.
//
// A superseded fact is left out and so is never stamped, which looks
// like a row the pass cannot finish with and is not: it has already been
// merged into another and nothing reads it. Going back over it would
// change nothing anybody sees.
func (self *transaction) ListAgentFactsWrittenBefore(agentId, version string, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 200
	}
	return self.factsFrom(self.tx.Raw(`
		SELECT * FROM "agent_fact"
		WHERE "agent_id" = ? AND "version" <> ? AND "superseded_by" IS NULL
		ORDER BY "created_at" ASC LIMIT ?`, agentId, version, limit))
}

// MarkAgentFactsSeen records that this build has been over these rows.
//
// A write of its own rather than a no-op update, for two reasons. An
// update revalidates the whole row, and a row an older build wrote may
// not satisfy a rule this one has -- which would leave it unstamped and
// offered again every night, for ever. And nothing about the fact has
// changed, so it does not belong in the page's history.
func (self *transaction) MarkAgentFactsSeen(agentId string, factIds []string) error {
	if len(factIds) == 0 {
		return nil
	}
	return self.tx.Exec(
		`UPDATE "agent_fact" SET "version" = ? WHERE "agent_id" = ? AND "id" = ANY(?)`,
		version.Version(), agentId, pq.Array(factIds)).Error
}

func (self *transaction) ListAgentNodesEmpty(agentId string, before time.Time, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 100
	}
	return self.nodesFrom(self.tx.Raw(`
		SELECT n.* FROM "agent_node" n
		WHERE n."agent_id" = ? AND n."created_at" < ?
		  AND n."kind" NOT IN (?, ?) AND n."parent_id" IS NOT NULL
		  AND btrim(n."summary") = ''
		  AND NOT EXISTS (SELECT 1 FROM "agent_fact" f WHERE f."node_id" = n."id")
		  AND NOT EXISTS (SELECT 1 FROM "agent_node" c WHERE c."parent_id" = n."id")
		  AND NOT EXISTS (SELECT 1 FROM "agent_edge" e WHERE e."from_id" = n."id" OR e."to_id" = n."id")
		ORDER BY n."created_at"
		LIMIT ?`, agentId, before, models.NodeFolder, models.NodePeriod, limit))
}

func (self *transaction) ListAgentFactsSaidTwice(agentId string, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 100
	}
	return self.factsFrom(self.tx.Raw(`
		SELECT f.* FROM "agent_fact" f
		WHERE f."agent_id" = ? AND f."superseded_by" IS NULL
		  AND EXISTS (
			SELECT 1 FROM "agent_fact" g
			WHERE g."node_id" = f."node_id" AND g."number" < f."number"
			  AND g."superseded_by" IS NULL
			  AND lower(btrim(g."text")) = lower(btrim(f."text"))
		  )
		ORDER BY f."created_at"
		LIMIT ?`, agentId, limit))
}

func (self *transaction) FirstAgentFactSayingIt(agentId, nodeId, text string, below int) (*models.AgentFact, error) {
	wanted := strings.ToLower(strings.TrimSpace(text))
	if agentId == "" || nodeId == "" || wanted == "" {
		return nil, nil
	}
	// The same test the search for duplicates makes, the other way about:
	// there it asks whether an earlier twin exists, here it asks which one.
	// The two must agree, or a fact is found and cannot be folded.
	facts, err := self.factsFrom(self.tx.Raw(`
		SELECT f.* FROM "agent_fact" f
		WHERE f."agent_id" = ? AND f."node_id" = ? AND f."number" < ?
		  AND f."superseded_by" IS NULL
		  AND lower(btrim(f."text")) = ?
		ORDER BY f."number" ASC
		LIMIT 1`, agentId, nodeId, below, wanted))
	if err != nil || len(facts) == 0 {
		return nil, err
	}
	return facts[0], nil
}

func (self *transaction) ListAgentNodesCrowded(agentId string, above, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 5
	}
	return self.nodesFrom(self.tx.Raw(`
		SELECT n.* FROM "agent_node" n
		WHERE n."agent_id" = ? AND n."kind" <> ? AND NOT n."dormant"
		  AND (SELECT count(*) FROM "agent_fact" f WHERE f."node_id" = n."id" AND NOT f."dormant" AND f."superseded_by" IS NULL) > ?
		ORDER BY (SELECT count(*) FROM "agent_fact" f WHERE f."node_id" = n."id" AND NOT f."dormant" AND f."superseded_by" IS NULL) DESC
		LIMIT ?`, agentId, models.NodeFolder, above, limit))
}

func (self *transaction) UnmarkAgentDocumentsDigested(agentId string, since time.Time) (int64, error) {
	result := self.tx.Exec(
		`UPDATE "agent_document" SET "metadata" = "metadata" - 'digested'
		 WHERE "agent_id" = ? AND jsonb_exists("metadata", 'digested')
		   AND ("metadata"->>'digested')::timestamptz >= ?`, agentId, since)
	return result.RowsAffected, result.Error
}

func (self *transaction) ListAgentMonthsToWriteUp(agentId string, names []string, least, limit int, guessed string) ([]string, error) {
	if limit <= 0 {
		limit = 3
	}
	if len(names) == 0 {
		names = []string{""}
	}
	var months []string
	if err := self.tx.Raw(`
		WITH record AS (
			SELECT to_char("happened_at", 'YYYY/MM') AS month, count(*) AS how_many
			FROM "agent_fact"
			WHERE "agent_id" = ? AND "happened_at" IS NOT NULL AND "superseded_by" IS NULL
			GROUP BY 1
			UNION ALL
			SELECT to_char("happened_at", 'YYYY/MM'), count(*)
			FROM "agent_document"
			WHERE "agent_id" = ? AND "happened_at" IS NOT NULL
			  AND ("kind" = 'commit' OR ("kind" = 'chat' AND jsonb_exists_any("metadata"->'participants', ?::text[])))
			GROUP BY 1
		)
		, owed AS (
			SELECT month, EXISTS (SELECT 1 FROM "agent_node" n WHERE n."agent_id" = ? AND n."path" = 'time/' || month) AS written
			FROM record
			GROUP BY month
			HAVING sum(how_many) >= ?
		)
		SELECT month FROM owed
		WHERE NOT written
		   OR EXISTS (SELECT 1 FROM "agent_node" n WHERE n."agent_id" = ? AND n."path" = 'time/' || month
		              AND (btrim(n."summary") = '' OR (? <> '' AND n."summary" ~* ?)))
		ORDER BY written ASC, month DESC
		LIMIT ?`, agentId, agentId, pq.Array(names), agentId, least, agentId, guessed, guessed, limit).Scan(&months).Error; err != nil {
		return nil, err
	}
	return months, nil
}
