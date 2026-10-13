package db

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// Links from a page to the person's own records. See
// models.AgentRecordLink for what they are and database_graph.go for the
// rest of the graph.
//
// Every call takes the agent, and a page is checked to be that agent's
// before a link is written to it, so a caller that mixes up whose graph it
// holds writes nothing rather than a link into somebody else's.

// maximumRecordLinkNoteLength is the longest note a link keeps, in runes.
const maximumRecordLinkNoteLength = 400

type agentRecordLinkModel struct {
	ID             string     `gorm:"column:id;primaryKey"`
	AgentID        string     `gorm:"column:agent_id"`
	NodeID         string     `gorm:"column:node_id"`
	RecordKind     string     `gorm:"column:record_kind"`
	RecordID       string     `gorm:"column:record_id"`
	RecordDetail   []byte     `gorm:"column:record_detail;type:jsonb"`
	PassageStartMS int64      `gorm:"column:passage_start_ms"`
	PassageEndMS   int64      `gorm:"column:passage_end_ms"`
	RecordRelation string     `gorm:"column:record_relation"`
	LinkStatus     string     `gorm:"column:link_status"`
	Evidence       []byte     `gorm:"column:evidence;type:jsonb"`
	Note           string     `gorm:"column:note"`
	HappenedAt     *time.Time `gorm:"column:happened_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	ModifiedAt     time.Time  `gorm:"column:modified_at"`
}

func (agentRecordLinkModel) TableName() string { return "agent_record_link" }

func (self *agentRecordLinkModel) toModel() (*models.AgentRecordLink, error) {
	link := &models.AgentRecordLink{
		ID: self.ID, AgentID: self.AgentID, NodeID: self.NodeID,
		RecordKind: models.AgentRecordKind(self.RecordKind), RecordID: self.RecordID,
		PassageStartMS: self.PassageStartMS, PassageEndMS: self.PassageEndMS,
		RecordRelation: models.AgentRecordRelation(self.RecordRelation),
		LinkStatus:     models.AgentEdgeStatus(self.LinkStatus),
		Note:           self.Note, HappenedAt: self.HappenedAt,
		CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt,
		Evidence: []models.Evidence{},
	}
	if link.LinkStatus == "" {
		link.LinkStatus = models.EdgeStated
	}
	if len(self.RecordDetail) > 0 {
		if err := json.Unmarshal(self.RecordDetail, &link.RecordDetail); err != nil {
			return nil, err
		}
	}
	if len(self.Evidence) > 0 {
		if err := json.Unmarshal(self.Evidence, &link.Evidence); err != nil {
			return nil, err
		}
	}
	return link, nil
}

func (self *transaction) PutAgentRecordLink(link *models.AgentRecordLink) (*models.AgentRecordLink, error) {
	if link == nil || link.AgentID == "" || link.NodeID == "" {
		return nil, fmt.Errorf("db: a record link needs an agent and a page")
	}
	written := *link
	if written.LinkStatus == "" {
		written.LinkStatus = models.EdgeStated
	}
	if err := written.Validate(); err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	node, err := self.GetAgentNodeByID(written.AgentID, written.NodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("db: no page %q to link a record to", written.NodeID)
	}
	if written.Evidence == nil {
		written.Evidence = []models.Evidence{}
	}
	evidence, err := json.Marshal(written.Evidence)
	if err != nil {
		return nil, err
	}
	detail, err := json.Marshal(written.RecordDetail)
	if err != nil {
		return nil, err
	}
	now := time.Now().Truncate(time.Microsecond)
	row := &agentRecordLinkModel{
		ID: newID(), AgentID: written.AgentID, NodeID: written.NodeID,
		RecordKind: string(written.RecordKind), RecordID: written.RecordID, RecordDetail: detail,
		PassageStartMS: written.PassageStartMS, PassageEndMS: written.PassageEndMS,
		RecordRelation: string(written.RecordRelation), LinkStatus: string(written.LinkStatus),
		Evidence: evidence, Note: truncateRunes(written.Note, maximumRecordLinkNoteLength),
		HappenedAt: written.HappenedAt, CreatedAt: now, ModifiedAt: now,
	}
	// Making a link by hand takes back an earlier removal. The dream, which
	// is the one writer that honors removals, never does.
	if self.actor != models.ActorDream {
		if err := self.tx.Where(recordLinkKeyWhere, row.AgentID, row.NodeID, row.RecordKind, row.RecordID,
			row.RecordRelation, row.PassageStartMS, row.PassageEndMS).Delete(&agentRecordLinkDismissalModel{}).Error; err != nil {
			return nil, err
		}
	}
	var existing []agentRecordLinkModel
	if err := self.tx.Where(recordLinkKeyWhere, row.AgentID, row.NodeID, row.RecordKind, row.RecordID,
		row.RecordRelation, row.PassageStartMS, row.PassageEndMS).Limit(1).Find(&existing).Error; err != nil {
		return nil, err
	}
	// A person stating the link again confirms what was only proposed. A
	// proposal made again never takes a statement back: the automatic
	// passes run every day, and the person's word is not theirs to undo.
	// Nor does a link said again with no note lose the one it has.
	err = self.tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "agent_id"}, {Name: "node_id"}, {Name: "record_kind"}, {Name: "record_id"},
			{Name: "record_relation"}, {Name: "passage_start_ms"}, {Name: "passage_end_ms"}},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "link_status"}, Value: gorm.Expr(
				`CASE WHEN "agent_record_link"."link_status" = 'stated' THEN 'stated' ELSE EXCLUDED."link_status" END`)},
			{Column: clause.Column{Name: "record_detail"}, Value: gorm.Expr(`EXCLUDED."record_detail"`)},
			{Column: clause.Column{Name: "evidence"}, Value: gorm.Expr(
				`CASE WHEN EXCLUDED."evidence" = '[]'::jsonb THEN "agent_record_link"."evidence" ELSE EXCLUDED."evidence" END`)},
			{Column: clause.Column{Name: "note"}, Value: gorm.Expr(
				`CASE WHEN EXCLUDED."note" = '' THEN "agent_record_link"."note" ELSE EXCLUDED."note" END`)},
			{Column: clause.Column{Name: "happened_at"}, Value: gorm.Expr(
				`COALESCE(EXCLUDED."happened_at", "agent_record_link"."happened_at")`)},
			{Column: clause.Column{Name: "modified_at"}, Value: gorm.Expr(`EXCLUDED."modified_at"`)},
		},
	}).Create(row).Error
	if err != nil {
		return nil, err
	}
	var stored agentRecordLinkModel
	if err := self.tx.Where(recordLinkKeyWhere, row.AgentID, row.NodeID, row.RecordKind, row.RecordID,
		row.RecordRelation, row.PassageStartMS, row.PassageEndMS).Take(&stored).Error; err != nil {
		return nil, err
	}
	if len(existing) == 0 || existing[0].LinkStatus != stored.LinkStatus {
		self.note(written.AgentID, written.NodeID, models.RevisionLinked, nil,
			map[string]any{"path": written.Reference(), "relation": string(written.RecordRelation), "text": row.Note}, "")
	}
	return stored.toModel()
}

// recordLinkKeyWhere is the unique key of a link.
const recordLinkKeyWhere = `"agent_id" = ? AND "node_id" = ? AND "record_kind" = ? AND "record_id" = ? AND "record_relation" = ? AND "passage_start_ms" = ? AND "passage_end_ms" = ?`

func (self *transaction) DeleteAgentRecordLink(agentId, nodeId string, recordKind models.AgentRecordKind, recordId string, recordRelation models.AgentRecordRelation, startMS, endMS int64) (int64, error) {
	query := self.tx.Where(`"agent_id" = ? AND "node_id" = ? AND "record_kind" = ? AND "record_id" = ?`,
		agentId, nodeId, string(recordKind), recordId)
	if recordRelation != "" {
		query = query.Where(`"record_relation" = ?`, string(recordRelation))
	}
	if recordKind == models.RecordRecordingPassage && (startMS != 0 || endMS != 0) {
		query = query.Where(`"passage_start_ms" = ? AND "passage_end_ms" = ?`, startMS, endMS)
	}
	// Remembered before the links go, so that the automatic passes do not
	// make again what somebody just took away.
	var removed []agentRecordLinkModel
	if err := query.Session(&gorm.Session{}).Find(&removed).Error; err != nil {
		return 0, err
	}
	now := time.Now().Truncate(time.Microsecond)
	for index := range removed {
		if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&agentRecordLinkDismissalModel{
			AgentID: removed[index].AgentID, NodeID: removed[index].NodeID,
			RecordKind: removed[index].RecordKind, RecordID: removed[index].RecordID,
			RecordRelation: removed[index].RecordRelation,
			PassageStartMS: removed[index].PassageStartMS, PassageEndMS: removed[index].PassageEndMS,
			CreatedAt: now,
		}).Error; err != nil {
			return 0, err
		}
	}
	result := query.Delete(&agentRecordLinkModel{})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected > 0 {
		self.note(agentId, nodeId, models.RevisionUnlinked,
			map[string]any{"path": models.FormatRecordReference(recordKind, recordId, startMS, endMS), "relation": string(recordRelation)}, nil, "")
	}
	return result.RowsAffected, nil
}

type agentRecordLinkDismissalModel struct {
	AgentID        string    `gorm:"column:agent_id;primaryKey"`
	NodeID         string    `gorm:"column:node_id;primaryKey"`
	RecordKind     string    `gorm:"column:record_kind;primaryKey"`
	RecordID       string    `gorm:"column:record_id;primaryKey"`
	RecordRelation string    `gorm:"column:record_relation;primaryKey"`
	PassageStartMS int64     `gorm:"column:passage_start_ms;primaryKey"`
	PassageEndMS   int64     `gorm:"column:passage_end_ms;primaryKey"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (agentRecordLinkDismissalModel) TableName() string { return "agent_record_link_dismissal" }

// ListAgentRecordLinkDismissals is the links the person took off pages of
// this agent, as references, for the automatic passes to leave alone.
func (self *transaction) ListAgentRecordLinkDismissals(agentId string) (map[string]bool, error) {
	var rows []agentRecordLinkDismissalModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Find(&rows).Error; err != nil {
		return nil, err
	}
	dismissed := make(map[string]bool, len(rows))
	for _, row := range rows {
		dismissed[models.RecordLinkKey(row.NodeID, models.AgentRecordKind(row.RecordKind), row.RecordID,
			models.AgentRecordRelation(row.RecordRelation), row.PassageStartMS, row.PassageEndMS)] = true
	}
	return dismissed, nil
}

func (self *transaction) GetAgentRecordLink(agentId, linkId string) (*models.AgentRecordLink, error) {
	var rows []agentRecordLinkModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, linkId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].toModel()
}

func (self *transaction) ListAgentRecordLinks(agentId, nodeId string) ([]*models.AgentRecordLink, error) {
	var rows []agentRecordLinkModel
	if err := self.tx.Where(`"agent_id" = ? AND "node_id" = ?`, agentId, nodeId).
		Order(`"record_kind" ASC, "happened_at" DESC NULLS LAST, "created_at" DESC, "id" ASC`).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.recordLinksOf(agentId, rows, false)
}

func (self *transaction) ListAgentRecordBacklinks(agentId string, recordKind models.AgentRecordKind, recordId string, startMS, endMS int64, limit int) ([]*models.AgentRecordLink, error) {
	if limit <= 0 {
		limit = 200
	}
	query := self.tx.Where(`"agent_id" = ? AND "record_kind" = ? AND "record_id" = ?`, agentId, string(recordKind), recordId)
	// A moment of a recording is asked about by its range, and answered by
	// every passage that overlaps it; without a range, by every passage of
	// the recording.
	if recordKind == models.RecordRecordingPassage && (startMS != 0 || endMS != 0) {
		if endMS < startMS {
			return nil, fmt.Errorf("db: a range cannot end before it starts")
		}
		query = query.Where(`"passage_start_ms" <= ? AND "passage_end_ms" >= ?`, endMS, startMS)
	}
	var rows []agentRecordLinkModel
	if err := query.Order(`"passage_start_ms" ASC, "record_relation" ASC, "created_at" ASC, "id" ASC`).
		Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.recordLinksOf(agentId, rows, true)
}

// recordLinksOf is stored links as the model has them, with the page at
// the other end named when asked.
func (self *transaction) recordLinksOf(agentId string, rows []agentRecordLinkModel, withPages bool) ([]*models.AgentRecordLink, error) {
	links := make([]*models.AgentRecordLink, 0, len(rows))
	nodeIds := make([]string, 0, len(rows))
	for index := range rows {
		link, err := rows[index].toModel()
		if err != nil {
			return nil, err
		}
		links = append(links, link)
		nodeIds = append(nodeIds, link.NodeID)
	}
	if !withPages || len(links) == 0 {
		return links, nil
	}
	nodes, err := self.GetAgentNodes(agentId, nodeIds)
	if err != nil {
		return nil, err
	}
	byId := make(map[string]*models.AgentNode, len(nodes))
	for _, node := range nodes {
		byId[node.ID] = node
	}
	for _, link := range links {
		if node := byId[link.NodeID]; node != nil {
			link.NodePath, link.NodeName, link.NodeKind = node.Path, node.Name, node.Kind
		}
	}
	return links, nil
}

func (self *transaction) SetAgentNodeContact(agentId, nodeId, contactId string) error {
	var value any
	if contactId != "" {
		value = contactId
	}
	result := self.tx.Model(&agentNodeModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, nodeId).Update("contact_id", value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("db: no page %q to name a contact on", nodeId)
	}
	return nil
}

// moveAgentRecordLinks re-points a merged page's record links at the page
// that survives, leaving behind any the survivor already has.
func (self *transaction) moveAgentRecordLinks(agentId, fromNodeId, intoNodeId string) error {
	if err := self.tx.Exec(`UPDATE "agent_record_link" AS moved SET "node_id" = ?
		WHERE moved."agent_id" = ? AND moved."node_id" = ?
		AND NOT EXISTS (SELECT 1 FROM "agent_record_link" AS kept
			WHERE kept."agent_id" = moved."agent_id" AND kept."node_id" = ?
			AND kept."record_kind" = moved."record_kind" AND kept."record_id" = moved."record_id"
			AND kept."record_relation" = moved."record_relation"
			AND kept."passage_start_ms" = moved."passage_start_ms" AND kept."passage_end_ms" = moved."passage_end_ms")`,
		intoNodeId, agentId, fromNodeId, intoNodeId).Error; err != nil {
		return err
	}
	// What is left on the merged page went with its twin on the survivor.
	return self.tx.Exec(`DELETE FROM "agent_record_link" WHERE "agent_id" = ? AND "node_id" = ?`, agentId, fromNodeId).Error
}

// ListAgentRecordLinkStatuses is every record link of the agent as its key
// (models.RecordLinkKey) and status, for a pass that must not write what is
// already there and must not turn a statement into a proposal.
func (self *transaction) ListAgentRecordLinkStatuses(agentId string) (map[string]models.AgentEdgeStatus, error) {
	var rows []agentRecordLinkModel
	if err := self.tx.Select(`"node_id", "record_kind", "record_id", "record_relation", "passage_start_ms", "passage_end_ms", "link_status"`).
		Where(`"agent_id" = ?`, agentId).Find(&rows).Error; err != nil {
		return nil, err
	}
	statuses := make(map[string]models.AgentEdgeStatus, len(rows))
	for _, row := range rows {
		statuses[models.RecordLinkKey(row.NodeID, models.AgentRecordKind(row.RecordKind), row.RecordID,
			models.AgentRecordRelation(row.RecordRelation), row.PassageStartMS, row.PassageEndMS)] = models.AgentEdgeStatus(row.LinkStatus)
	}
	return statuses, nil
}
