package db

import (
	"encoding/json"
	"fmt"
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
	ListAgentDreams(agentId string, limit int) ([]*models.AgentDream, error)

	// ListAgentDocumentsToDigest is what has been indexed and not yet
	// read, in the order a night should read it, and how much is waiting
	// altogether.
	//
	// The order is the priority the person would choose: what they wrote
	// themselves, then what they took part in, then the rest newest
	// first. The count beside it is what the log reports, so a backlog is
	// a number the person can see rather than work silently dropped.
	ListAgentDocumentsToDigest(agentId string, limit int) ([]*models.AgentDocument, int64, error)
	MarkAgentDocumentsDigested(documentIds []string, at time.Time) error

	// ListAgentNodesToConsolidate is the pages whose facts have changed
	// since their summary was written.
	ListAgentNodesToConsolidate(agentId string, limit int) ([]*models.AgentNode, error)
	MarkAgentNodeConsolidated(nodeId string, at time.Time) error

	// ListAgentNodesEmpty is the pages that say nothing: no opening, no
	// facts, nothing under them, no links, and made before the given
	// time. The roots and the period pages are never among them.
	ListAgentNodesEmpty(agentId string, before time.Time, limit int) ([]*models.AgentNode, error)

	// RecomputeAgentImportance rewrites what the index is ordered by, and
	// RetireAgentFacts marks what has not been wanted in a long time
	// dormant. Neither deletes anything.
	RecomputeAgentImportance(agentId string, now time.Time) (int64, error)
	RetireAgentFacts(agentId string, before time.Time) (int, error)

	// The deterministic half of a night: links strengthened by being used
	// together and weakened by not being, a threshold that rises as the
	// graph grows, and the pages that fall under it.
	StrengthenAgentEdges(agentId string, since time.Time, rise, decay float64) (int64, error)
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

type agentDreamModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	AgentID    string     `gorm:"column:agent_id"`
	StartedAt  time.Time  `gorm:"column:started_at"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
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

	Proposals []byte `gorm:"column:proposals;type:jsonb"`
	Tokens    int64  `gorm:"column:tokens"`
	Notes     string `gorm:"column:notes"`
	LastError string `gorm:"column:last_error"`
}

func (agentDreamModel) TableName() string { return "agent_dream" }

func (self *transaction) StartAgentDream(dream *models.AgentDream) (*models.AgentDream, error) {
	if dream.AgentID == "" {
		return nil, fmt.Errorf("db: a nightly run needs an agent")
	}
	created := *dream
	created.ID = newID()
	if created.StartedAt.IsZero() {
		created.StartedAt = time.Now()
	}
	created.StartedAt = created.StartedAt.Truncate(time.Microsecond)
	row := &agentDreamModel{
		ID: created.ID, AgentID: created.AgentID, StartedAt: created.StartedAt,
		Proposals: []byte("[]"),
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
		"rehearsed": dream.Rehearsed, "gaps": dream.Gaps,
		"notes": dream.Notes, "last_error": dream.LastError,
	}).Error
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
			Digested: row.Digested, Filed: row.Filed, Merged: row.Merged, Rewritten: row.Rewritten,
			Moved: row.Moved, Dormant: row.Dormant, Embedded: row.Embedded, Backlog: row.Backlog,
			Coarse: row.Coarse, Tokens: row.Tokens, Notes: row.Notes, LastError: row.LastError,
			Strengthened: row.Strengthened, Associated: row.Associated, Revised: row.Revised,
			Rehearsed: row.Rehearsed, Gaps: row.Gaps,
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
func (self *transaction) ListAgentDocumentsToDigest(agentId string, limit int) ([]*models.AgentDocument, int64, error) {
	if limit <= 0 {
		limit = 400
	}
	var total []int64
	if err := self.tx.Raw(
		// jsonb_exists rather than the ? operator: ? is how a parameter is
		// written, so the driver read the operator as one and substituted
		// the next argument into it. The count survived because it had no
		// other argument to take; the query below did not.
		`SELECT count(*) FROM "agent_document" WHERE "agent_id" = ? AND NOT jsonb_exists("metadata", 'digested')`,
		agentId).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	var backlog int64
	if len(total) > 0 {
		backlog = total[0]
	}
	documents, err := self.documentsFrom(self.tx.Raw(`
		SELECT * FROM "agent_document"
		WHERE "agent_id" = ? AND NOT jsonb_exists("metadata", 'digested')
		ORDER BY
			CASE "kind" WHEN 'journal' THEN 0 WHEN 'commit' THEN 1 WHEN 'chat' THEN 2
				WHEN 'file' THEN CASE WHEN lower("title") ~ ? THEN 3 ELSE 5 END
				ELSE 4 END,
			"happened_at" DESC NULLS LAST
		LIMIT ?`, agentId, proseFile, limit))
	return documents, backlog, err
}

// proseFile is a file somebody wrote to be read: a readme, a note, a
// document. It is read before source code, which is the bulk of any
// checkout and says almost nothing about the person: a night that read
// four hundred files of Go filed two facts, with a hundred thousand more
// files behind them. The code stays indexed for search, and is read only
// once everything written in words has been.
const proseFile = `(^|/)(readme|changelog|contributing|notes?|todo)$|\.(md|markdown|txt|rst|adoc|org|tex|pdf|docx?|pptx?|xlsx?|odt|html?|eml)$`

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
				WHERE f."node_id" = n."id" AND NOT f."dormant"
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
// both wanted today, and lowers every link a little.
//
// This is synaptic homeostasis, and it is the difference between an edge
// weight meaning "somebody once made this link" and "this link is worth
// something". Two pages read in the same conversation are related in a
// way nobody wrote down; a link nothing has touched in months is not
// wrong, it is just no longer the first thing to say.
//
// Deterministic and one statement each, so it costs nothing and can run
// every night. Nothing is deleted: a link decays towards a floor and
// stays readable.
func (self *transaction) StrengthenAgentEdges(agentId string, since time.Time, rise, decay float64) (int64, error) {
	// Down first, then up: an edge used today should end the night above
	// where it started, and doing it the other way round would shave the
	// rise off again.
	if err := self.tx.Exec(
		`UPDATE "agent_edge" SET "weight" = GREATEST(0.05, "weight" * ?) WHERE "agent_id" = ?`,
		decay, agentId).Error; err != nil {
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
