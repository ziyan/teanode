package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// OverviewOperation is what the night's overview phase reads and writes.
//
// An overview is written from a page's inputs: its opening, its facts,
// the overviews of the pages under it and the openings of the pages it is
// linked to. What it was written from is kept as a hash of those inputs,
// and a page is due when the hash of its inputs as they are now is
// different. The hash is worked out here, in one statement, because every
// part of it is a column: a page nobody touched costs no model call and
// no read of its facts to find that out.
type OverviewOperation interface {
	// ListAgentNodesForOverview is the pages whose overview is due and
	// ready to be written, most important first. Due: a live page, not a
	// theme, with at least leastFactCount live facts or a live page under
	// it, as important as the median page with that many facts or more,
	// whose inputs have changed since its overview was written. Ready:
	// no page directly under it is due as well, so a page is written
	// after the pages it is written from, a night later, rather than
	// from what they said last week. See ListAgentThemesForOverview for
	// the themes.
	ListAgentNodesForOverview(agentId string, leastFactCount, limit int) ([]*models.AgentNode, error)

	// ListAgentThemesForOverview is the same for the theme pages: a theme
	// with members, or with a theme under it, whose inputs have changed,
	// and with no theme directly under it due. A theme does not wait for
	// its members: they are shown by their overview where they have one
	// and by their opening and facts where not, and a member written
	// later makes the theme due again. Its own listing so that a night
	// can write the themes, the top of the structure, before the pages.
	ListAgentThemesForOverview(agentId string, limit int) ([]*models.AgentNode, error)

	// AgentNodeOverviewInputs is the hash of a page's inputs as they are
	// now, which is what an overview written from them is marked with.
	AgentNodeOverviewInputs(agentId, nodeId string) (string, error)

	// IsAgentNodeOverviewStale says whether a page's inputs have changed
	// since its overview was written, or a rewrite was asked for.
	IsAgentNodeOverviewStale(agentId, nodeId string) (bool, error)

	// SetAgentNodeOverview writes a page's overview, what it cites, and
	// the hash of what it was written from. Nothing else of the page is
	// touched, and it is not a change to the page's words: the opening,
	// the facts and the links are what the history records, and the
	// overview is written from them.
	SetAgentNodeOverview(agentId, nodeId, overview string, evidence []models.Evidence, overviewInputs string, writtenAt time.Time) error

	// ClearAgentNodeOverviewInputs makes a page's overview due at the
	// next night, keeping the one it has until then.
	ClearAgentNodeOverviewInputs(agentId, nodeId string) error
}

// overviewInputsExpression is the hash of a page's inputs, over the page
// as "n". Every part is in a fixed order, so the same inputs always hash
// the same, and each part is only what the overview is written from:
//
//   - the opening;
//   - each live fact's number and when it last changed, which covers the
//     lines a checkout's profile keeps (its head, its build files, its
//     activity), since those are facts rewritten in place; but not a
//     reflection, which the night writes from the overview, so writing
//     one would otherwise make the overview it came from due;
//   - each live page under it and when its overview was written, so a
//     child written again makes its parent due;
//   - each link, which way it points, its relation and note, and the
//     opening of the page at the other end; but not a theme's link to one
//     of its members, seen from the member, since a theme is written from
//     its members and not the other way round;
//   - for a theme, each member no theme under it holds, and when its
//     overview was written, or for a member with no overview its opening
//     and its facts, which is what the theme's prompt shows of it. NULL
//     for any other page, which concat_ws leaves out, so the hash of a
//     page that is not a theme is what it was before themes existed.
//
// Not a link's weight, nor when anything was used: the night reweights
// every link and the index moves every day, and neither changes what an
// overview would say.
const overviewInputsExpression = `encode(sha256(convert_to(concat_ws(E'\n',
	n."summary",
	COALESCE((SELECT string_agg(f."number"::text || '@' || extract(epoch FROM f."modified_at")::text, ',' ORDER BY f."number")
		FROM "agent_fact" f
		WHERE f."node_id" = n."id" AND NOT f."dormant" AND f."superseded_by" IS NULL AND f."kind" <> 'reflection'), ''),
	COALESCE((SELECT string_agg(child."id" || '@' || COALESCE(extract(epoch FROM child."overview_written_at")::text, ''), ',' ORDER BY child."id")
		FROM "agent_node" child
		WHERE child."parent_id" = n."id" AND NOT child."dormant"), ''),
	COALESCE((SELECT string_agg(link.line, E'\n' ORDER BY link.line) FROM (
		SELECT concat_ws(' ', '>', e."relation", e."to_id", e."note", other."summary") AS line
			FROM "agent_edge" e JOIN "agent_node" other ON other."id" = e."to_id"
			WHERE e."from_id" = n."id"
		UNION ALL
		SELECT concat_ws(' ', '<', e."relation", e."from_id", e."note", other."summary") AS line
			FROM "agent_edge" e JOIN "agent_node" other ON other."id" = e."from_id"
			WHERE e."to_id" = n."id" AND other."path" NOT LIKE 'themes/%'
	) link), ''),
	CASE WHEN n."path" LIKE 'themes/%' THEN
		COALESCE((SELECT string_agg(member."id" || '@' || CASE
				WHEN member."overview_written_at" IS NOT NULL THEN extract(epoch FROM member."overview_written_at")::text
				ELSE md5(concat_ws(E'\n', member."summary", (SELECT string_agg(member_fact."number"::text || '@' || extract(epoch FROM member_fact."modified_at")::text, ',' ORDER BY member_fact."number")
					FROM "agent_fact" member_fact
					WHERE member_fact."node_id" = member."id" AND NOT member_fact."dormant" AND member_fact."superseded_by" IS NULL
					  AND member_fact."kind" <> 'reflection')))
				END, ',' ORDER BY member."id")
			FROM "agent_edge" e JOIN "agent_node" member ON member."id" = e."to_id"
			WHERE e."from_id" = n."id" AND e."relation" = 'about' AND NOT member."dormant"
			  AND NOT EXISTS (SELECT 1 FROM "agent_edge" held JOIN "agent_node" subtheme ON subtheme."id" = held."from_id"
				WHERE held."to_id" = member."id" AND held."relation" = 'about' AND subtheme."parent_id" = n."id" AND NOT subtheme."dormant")), '')
	END
), 'UTF8')), 'hex')`

// overviewFactCountExpression is how many live facts a page "n" has,
// reflections left out.
const overviewFactCountExpression = `(SELECT count(*) FROM "agent_fact" f
	WHERE f."node_id" = n."id" AND NOT f."dormant" AND f."superseded_by" IS NULL AND f."kind" <> 'reflection')`

func (self *transaction) ListAgentNodesForOverview(agentId string, leastFactCount, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 40
	}
	// The floor is the median importance of the pages with facts enough
	// for an overview: an overview is paid for, and the half of the graph
	// nobody reaches for is not worth one before the other half is.
	return self.nodesFrom(self.tx.Raw(`
		WITH "floor" AS (
			SELECT COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY n."importance"), 0) AS "importance"
			FROM "agent_node" n
			WHERE n."agent_id" = ? AND NOT n."dormant" AND n."path" NOT LIKE 'themes/%'
			  AND `+overviewFactCountExpression+` >= ?
		), "due" AS (
			SELECT n."id", n."parent_id" FROM "agent_node" n, "floor"
			WHERE n."agent_id" = ? AND NOT n."dormant" AND n."path" NOT LIKE 'themes/%'
			  AND n."importance" >= "floor"."importance"
			  AND (`+overviewFactCountExpression+` >= ?
				OR EXISTS (SELECT 1 FROM "agent_node" child WHERE child."parent_id" = n."id" AND NOT child."dormant"))
			  AND `+overviewInputsExpression+` <> n."overview_inputs"
		)
		SELECT n.* FROM "agent_node" n JOIN "due" ON "due"."id" = n."id"
		WHERE NOT EXISTS (SELECT 1 FROM "due" waiting WHERE waiting."parent_id" = n."id")
		ORDER BY n."importance" DESC, n."path" ASC
		LIMIT ?`, agentId, leastFactCount, agentId, leastFactCount, limit))
}

func (self *transaction) ListAgentThemesForOverview(agentId string, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 16
	}
	return self.nodesFrom(self.tx.Raw(`
		WITH "due" AS (
			SELECT n."id", n."parent_id" FROM "agent_node" n
			WHERE n."agent_id" = ? AND NOT n."dormant" AND n."path" LIKE 'themes/%'
			  AND (
				EXISTS (SELECT 1 FROM "agent_edge" e WHERE e."from_id" = n."id" AND e."relation" = 'about')
				OR EXISTS (SELECT 1 FROM "agent_node" child WHERE child."parent_id" = n."id" AND NOT child."dormant")
			  )
			  AND `+overviewInputsExpression+` <> n."overview_inputs"
		)
		SELECT n.* FROM "agent_node" n JOIN "due" ON "due"."id" = n."id"
		WHERE NOT EXISTS (SELECT 1 FROM "due" waiting WHERE waiting."parent_id" = n."id")
		ORDER BY n."importance" DESC, n."path" ASC
		LIMIT ?`, agentId, limit))
}

func (self *transaction) AgentNodeOverviewInputs(agentId, nodeId string) (string, error) {
	var overviewInputs string
	if err := self.tx.Raw(`SELECT `+overviewInputsExpression+` FROM "agent_node" n WHERE n."agent_id" = ? AND n."id" = ?`,
		agentId, nodeId).Scan(&overviewInputs).Error; err != nil {
		return "", err
	}
	return overviewInputs, nil
}

// IsAgentNodeOverviewStale says whether what a page's overview is written
// from has changed since it was written, or a rewrite was asked for: the
// next dream writes it again.
func (self *transaction) IsAgentNodeOverviewStale(agentId, nodeId string) (bool, error) {
	var isStale bool
	if err := self.tx.Raw(`SELECT `+overviewInputsExpression+` <> n."overview_inputs" FROM "agent_node" n WHERE n."agent_id" = ? AND n."id" = ?`,
		agentId, nodeId).Scan(&isStale).Error; err != nil {
		return false, err
	}
	return isStale, nil
}

func (self *transaction) SetAgentNodeOverview(agentId, nodeId, overview string, evidence []models.Evidence, overviewInputs string, writtenAt time.Time) error {
	if evidence == nil {
		evidence = []models.Evidence{}
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	update := self.tx.Exec(`UPDATE "agent_node" SET "overview" = ?, "overview_evidence" = ?, "overview_inputs" = ?, "overview_written_at" = ?
		WHERE "id" = ? AND "agent_id" = ?`, overview, string(encoded), overviewInputs, writtenAt.Truncate(time.Microsecond), nodeId, agentId)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("db: no page %q to write an overview on", nodeId)
	}
	return nil
}

func (self *transaction) ClearAgentNodeOverviewInputs(agentId, nodeId string) error {
	return self.tx.Exec(`UPDATE "agent_node" SET "overview_inputs" = '' WHERE "id" = ? AND "agent_id" = ?`, nodeId, agentId).Error
}
