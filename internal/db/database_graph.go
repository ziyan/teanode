package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/version"
)

// The graph's store: pages addressed by path, facts on them, edges
// between them.
//
// Everything here is scoped to one agent. A path is unique per agent, and
// every read takes the agent as well as the path, so a caller that forgets
// whose graph it is asking about gets nothing rather than somebody else's.

// GraphOperation is the graph as the rest of the server reaches it.
type GraphOperation interface {
	// AsActor says who the writes in this transaction are by, so the
	// history can say. Unset means the agent itself.
	//
	// On the transaction rather than on every call because it is true of
	// the whole run: a nightly pass is the nightly run from beginning to
	// end, and threading it through fifteen signatures would be fifteen
	// chances to pass the wrong one.
	AsActor(actor models.RevisionActor)

	// EnsureAgentRoots makes the pages every graph starts with, and is
	// idempotent. Called the first time an agent's graph is touched.
	EnsureAgentRoots(agentId string) error

	// GetAgentNode is a page by its path, or nil. GetAgentNodeByID is the
	// same by identifier, which is what the API and the vector search use.
	GetAgentNode(agentId, path string) (*models.AgentNode, error)
	GetAgentNodeByID(agentId, nodeId string) (*models.AgentNode, error)
	GetAgentNodes(agentId string, nodeIds []string) ([]*models.AgentNode, error)

	// PutAgentNode creates the page at its path or updates the one there,
	// filling in the parent from the path and making any missing page
	// above it as a folder. The node it answers with carries the
	// identifier and the times.
	PutAgentNode(node *models.AgentNode) (*models.AgentNode, error)

	// MoveAgentNode puts a page under another, rewriting the paths of
	// everything beneath it.
	MoveAgentNode(agentId, path, newParentPath string) (*models.AgentNode, error)

	// DeleteAgentNode removes a page and everything under it. Only a
	// person does this; the nightly run marks dormant instead.
	DeleteAgentNode(agentId, path string) (int64, error)

	// ListAgentNodeChildren is the pages directly under one.
	ListAgentNodeChildren(agentId, nodeId string) ([]*models.AgentNode, error)

	// FirstAgentFactLines is one line per page -- the first fact on each
	// -- for a list that has to tell one row from the next where the page
	// has no opening. One query for the whole page of rows.
	FirstAgentFactLines(agentId string, nodeIds []string) (map[string]string, error)

	// ListAgentNodeChildrenPage is a page of them, by name,
	// with how many there are altogether: a folder of two thousand
	// projects is browsed fifty at a time and the heading says two
	// thousand.
	ListAgentNodeChildrenPage(agentId, nodeId string, limit, offset int) ([]*models.AgentNode, int64, error)

	// ListAgentIndex is the top of the graph, pinned first then by
	// importance: what a prompt carries. Dormant pages are never in it.
	ListAgentIndex(agentId string, limit int) ([]*models.AgentNode, error)

	// ListAgentNodesUnder is every page at or below a path, oldest first,
	// for a tree view and for the move above.
	ListAgentNodesUnder(agentId, path string, limit int) ([]*models.AgentNode, error)

	// AddAgentFact puts a sentence on a page, taking the next number.
	AddAgentFact(fact *models.AgentFact) (*models.AgentFact, error)

	// CountAgentNodeChildren is how many pages are filed under each of
	// the given ones, in one query.
	CountAgentNodeChildren(agentId string, nodeIds []string) (map[string]int, error)

	// MergeAgentNodes folds one page into another: its facts move over,
	// its links are re-pointed, its children go under the other, its
	// aliases and name join the other's aliases, and it is deleted. Two
	// pages for one thing is the graph's commonest wrong shape.
	MergeAgentNodes(agentId, fromPath, intoPath string) (*models.AgentNode, error)

	// MoveAgentFact puts a fact on another page, keeping its identifier
	// and taking the next number there; both pages' histories say so.
	MoveAgentFact(agentId, factId, toNodeId string) (*models.AgentFact, error)

	// UpdateAgentFact changes one, by identifier.
	UpdateAgentFact(agentId, factId string, modify func(*models.AgentFact) error) (*models.AgentFact, error)

	// FoldAgentFact marks a fact as standing behind another one: dormant,
	// pointing at the row the page now states, kept. StrikeAgentFact
	// marks one dormant with nothing in its place. Both are what the
	// agent does on its own, and neither deletes -- only the person's own
	// forgetting reaches DeleteAgentFact. The reason is what the page's
	// history shows, since the same two columns move for several quite
	// different decisions.
	FoldAgentFact(agentId, factId, intoFactId, reason string) (*models.AgentFact, error)
	StrikeAgentFact(agentId, factId, reason string) (*models.AgentFact, error)

	// GetAgentFact is one fact by node and number -- how a person and a
	// model name one -- and GetAgentFacts several by identifier.
	GetAgentFact(agentId, nodeId string, number int) (*models.AgentFact, error)
	GetAgentFacts(agentId string, factIds []string) ([]*models.AgentFact, error)

	// ListAgentFacts is a page's facts, live ones first by use.
	ListAgentFacts(agentId, nodeId string, includeDormant bool, limit int) ([]*models.AgentFact, error)
	// ListAgentFactsLively is the same, most recently wanted or changed
	// first, for a reader that has room for only some of them.
	ListAgentFactsLively(agentId, nodeId string, limit int) ([]*models.AgentFact, error)

	// ListAgentFactsForAudience is what an unattended run of a kind
	// reads, newest and most used first.
	ListAgentFactsForAudience(agentId string, audience models.AgentAudience, limit int) ([]*models.AgentFact, error)

	// ListAgentFactsBetween is every fact that happened in a stretch of
	// time, oldest first: what a period page is written from.
	ListAgentFactsBetween(agentId string, from, until time.Time, limit int) ([]*models.AgentFact, error)

	// ListAgentFactsLearnedSince is what has been filed lately, newest
	// first: what the agent page shows under "Learned today", and what a
	// person struck from it corrects.
	ListAgentFactsLearnedSince(agentId string, since time.Time, limit int) ([]*models.AgentFact, error)

	// DeleteAgentFact forgets one.
	DeleteAgentFact(agentId, factId string) error

	// SearchAgentGraph finds pages and facts by words, best match first.
	SearchAgentGraph(agentId, query string, limit int) ([]*models.AgentNode, []*models.AgentFact, error)

	// PutAgentEdge joins two pages, or changes the join.
	PutAgentEdge(edge *models.AgentEdge) error
	DeleteAgentEdge(agentId, fromId, toId string, relation models.AgentEdgeRelation) error

	// ListAgentEdges is everything joined to a page, in either direction,
	// with the other end's path filled in.
	ListAgentEdges(agentId, nodeId string) ([]*models.AgentEdge, error)

	// TouchAgentNodes and TouchAgentFacts mark what a prompt carried or a
	// search found as used. Used time feeds the nightly importance; it
	// does not order the index, because a prompt whose order moves every
	// turn cannot be cached.
	TouchAgentNodes(nodeIds []string, at time.Time) error
	TouchAgentFacts(factIds []string, at time.Time) error

	// The vectors. Put writes one; ListWithout says what is still waiting
	// for one, so a change of model catches up a few at a time.
	PutAgentNodeVector(agentId, nodeId, model string, vector []float32) error
	PutAgentFactVector(agentId, factId, model string, vector []float32) error
	ListAgentNodesWithoutVector(agentId, model string, limit int) ([]*models.AgentNode, error)
	ListAgentFactsWithoutVector(agentId, model string, limit int) ([]*models.AgentFact, error)

	// CountAgentGraph is how much there is, for the dashboard and for the
	// nightly run's own report.
	CountAgentGraph(agentId string) (nodes, facts int64, err error)

	// SetUserContact names the address book entry that is the account
	// itself, and GetUserContact reads it back.
	SetUserContact(userId, contactId string) error
}

// --- rows -------------------------------------------------------------

type agentNodeModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	AgentID    string     `gorm:"column:agent_id"`
	Path       string     `gorm:"column:path"`
	ParentID   *string    `gorm:"column:parent_id"`
	Kind       string     `gorm:"column:kind"`
	Name       string     `gorm:"column:name"`
	Aliases    []byte     `gorm:"column:aliases;type:jsonb"`
	Summary    string     `gorm:"column:summary"`
	ContactID  *string    `gorm:"column:contact_id"`
	Pinned     bool       `gorm:"column:pinned"`
	Importance float32    `gorm:"column:importance"`
	Dormant    bool       `gorm:"column:dormant"`
	UsedAt     *time.Time `gorm:"column:used_at"`
	Version    string     `gorm:"column:version"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	ModifiedAt time.Time  `gorm:"column:modified_at"`

	// NextFactNumber is the high-water mark described in the migration. It
	// is not on the model: nothing above this package has any business
	// knowing how a number is chosen.
	NextFactNumber int `gorm:"column:next_fact_number"`
}

func (agentNodeModel) TableName() string { return "agent_node" }

type agentFactModel struct {
	ID           string     `gorm:"column:id;primaryKey"`
	AgentID      string     `gorm:"column:agent_id"`
	NodeID       string     `gorm:"column:node_id"`
	Number       int        `gorm:"column:number"`
	Kind         string     `gorm:"column:kind"`
	Text         string     `gorm:"column:text"`
	HappenedAt   *time.Time `gorm:"column:happened_at"`
	Confidence   float32    `gorm:"column:confidence"`
	Inferred     bool       `gorm:"column:inferred"`
	Evidence     []byte     `gorm:"column:evidence;type:jsonb"`
	Audiences    []byte     `gorm:"column:audiences;type:jsonb"`
	SupersededBy *string    `gorm:"column:superseded_by"`
	Dormant      bool       `gorm:"column:dormant"`
	UsedAt       *time.Time `gorm:"column:used_at"`
	Version      string     `gorm:"column:version"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	ModifiedAt   time.Time  `gorm:"column:modified_at"`
}

func (agentFactModel) TableName() string { return "agent_fact" }

type agentEdgeModel struct {
	AgentID    string     `gorm:"column:agent_id"`
	FromID     string     `gorm:"column:from_id;primaryKey"`
	ToID       string     `gorm:"column:to_id;primaryKey"`
	Relation   string     `gorm:"column:relation;primaryKey"`
	Weight     float32    `gorm:"column:weight"`
	Evidence   []byte     `gorm:"column:evidence;type:jsonb"`
	Note       string     `gorm:"column:note"`
	HappenedAt *time.Time `gorm:"column:happened_at"`
	UsedAt     *time.Time `gorm:"column:used_at"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
}

func (agentEdgeModel) TableName() string { return "agent_edge" }

// --- conversion -------------------------------------------------------

func nodeToModel(node *models.AgentNode) (*agentNodeModel, error) {
	aliases := node.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	encoded, err := json.Marshal(aliases)
	if err != nil {
		return nil, err
	}
	row := &agentNodeModel{
		ID: node.ID, AgentID: node.AgentID, Path: node.Path, Kind: string(node.Kind),
		Name: node.Name, Aliases: encoded, Summary: node.Summary, Pinned: node.Pinned,
		Importance: node.Importance, Dormant: node.Dormant, UsedAt: node.UsedAt,
		// The build doing the writing, always this one: a row's version
		// answers "what wrote what is there now", not "what wrote it
		// first". See migration 0073.
		Version:   version.Version(),
		CreatedAt: node.CreatedAt, ModifiedAt: node.ModifiedAt,

		NextFactNumber: 1,
	}
	if node.ParentID != "" {
		parent := node.ParentID
		row.ParentID = &parent
	}
	if node.ContactID != "" {
		contact := node.ContactID
		row.ContactID = &contact
	}
	return row, nil
}

func (self *agentNodeModel) toModel() (*models.AgentNode, error) {
	node := &models.AgentNode{
		ID: self.ID, AgentID: self.AgentID, Path: self.Path, Kind: models.AgentNodeKind(self.Kind),
		Name: self.Name, Summary: self.Summary, Pinned: self.Pinned, Importance: self.Importance,
		Dormant: self.Dormant, UsedAt: self.UsedAt, Version: self.Version,
		CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt,
		Aliases: []string{},
	}
	if self.ParentID != nil {
		node.ParentID = *self.ParentID
	}
	if self.ContactID != nil {
		node.ContactID = *self.ContactID
	}
	if len(self.Aliases) > 0 {
		if err := json.Unmarshal(self.Aliases, &node.Aliases); err != nil {
			return nil, err
		}
	}
	return node, nil
}

func factToModel(fact *models.AgentFact) (*agentFactModel, error) {
	evidence := fact.Evidence
	if evidence == nil {
		evidence = []models.Evidence{}
	}
	audiences := fact.Audiences
	if audiences == nil {
		audiences = []models.AgentAudience{}
	}
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	encodedAudiences, err := json.Marshal(audiences)
	if err != nil {
		return nil, err
	}
	row := &agentFactModel{
		ID: fact.ID, AgentID: fact.AgentID, NodeID: fact.NodeID, Number: fact.Number,
		Kind: string(fact.Kind), Text: fact.Text, HappenedAt: fact.HappenedAt,
		Confidence: fact.Confidence, Inferred: fact.Inferred,
		Evidence: encodedEvidence, Audiences: encodedAudiences,
		Dormant: fact.Dormant, UsedAt: fact.UsedAt, Version: version.Version(),
		CreatedAt: fact.CreatedAt, ModifiedAt: fact.ModifiedAt,
	}
	if fact.SupersededBy != "" {
		superseded := fact.SupersededBy
		row.SupersededBy = &superseded
	}
	return row, nil
}

func (self *agentFactModel) toModel() (*models.AgentFact, error) {
	fact := &models.AgentFact{
		ID: self.ID, AgentID: self.AgentID, NodeID: self.NodeID, Number: self.Number,
		Kind: models.AgentFactKind(self.Kind), Text: self.Text, HappenedAt: self.HappenedAt,
		Confidence: self.Confidence, Inferred: self.Inferred, Dormant: self.Dormant,
		UsedAt: self.UsedAt, Version: self.Version,
		CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt,
		Evidence: []models.Evidence{}, Audiences: []models.AgentAudience{},
	}
	if self.SupersededBy != nil {
		fact.SupersededBy = *self.SupersededBy
	}
	if len(self.Evidence) > 0 {
		if err := json.Unmarshal(self.Evidence, &fact.Evidence); err != nil {
			return nil, err
		}
	}
	if len(self.Audiences) > 0 {
		if err := json.Unmarshal(self.Audiences, &fact.Audiences); err != nil {
			return nil, err
		}
	}
	return fact, nil
}

func (self *transaction) nodesFrom(query *gorm.DB) ([]*models.AgentNode, error) {
	var rows []agentNodeModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	nodes := make([]*models.AgentNode, 0, len(rows))
	for index := range rows {
		node, err := rows[index].toModel()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (self *transaction) factsFrom(query *gorm.DB) ([]*models.AgentFact, error) {
	var rows []agentFactModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	facts := make([]*models.AgentFact, 0, len(rows))
	for index := range rows {
		fact, err := rows[index].toModel()
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

// --- pages ------------------------------------------------------------

// EnsureAgentRoots makes the pages every graph starts with.
// AsActor records who this transaction's writes are by.
func (self *transaction) AsActor(actor models.RevisionActor) {
	self.actor = actor
}

// actorOf is who a write is by, defaulting to the agent.
func (self *transaction) actorOf() models.RevisionActor {
	if self.actor == "" {
		return models.ActorAgent
	}
	return self.actor
}

// note files one change against a page. A failure to record history is
// logged and not returned: a page that changed and a history that did not
// is worth less than a page that did not change at all, but refusing the
// change would be worse than either.
func (self *transaction) note(agentId, nodeId string, kind models.RevisionKind, before, after map[string]any, reason string) {
	if nodeId == "" {
		return
	}
	if _, err := self.RecordAgentRevision(&models.AgentRevision{
		AgentID: agentId, NodeID: nodeId, Kind: kind, Actor: self.actorOf(),
		Before: before, After: after, Reason: reason,
	}); err != nil {
		log.Warningf("cannot record what changed on page %q: %s", nodeId, err)
	}
}

func (self *transaction) EnsureAgentRoots(agentId string) error {
	if agentId == "" {
		return fmt.Errorf("db: roots need an agent")
	}
	for _, root := range models.AgentRoots {
		existing, err := self.GetAgentNode(agentId, root.Path)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if _, err := self.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: root.Path, Kind: root.Kind,
			Name: root.Name, Summary: root.Summary,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) GetAgentNode(agentId, path string) (*models.AgentNode, error) {
	if agentId == "" || path == "" {
		return nil, nil
	}
	nodes, err := self.nodesFrom(self.tx.Where(`"agent_id" = ? AND "path" = ?`, agentId, path).Limit(1))
	if err != nil || len(nodes) == 0 {
		return nil, err
	}
	return nodes[0], nil
}

func (self *transaction) GetAgentNodeByID(agentId, nodeId string) (*models.AgentNode, error) {
	if agentId == "" || nodeId == "" {
		return nil, nil
	}
	nodes, err := self.nodesFrom(self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, nodeId).Limit(1))
	if err != nil || len(nodes) == 0 {
		return nil, err
	}
	return nodes[0], nil
}

func (self *transaction) GetAgentNodes(agentId string, nodeIds []string) ([]*models.AgentNode, error) {
	if agentId == "" || len(nodeIds) == 0 {
		return nil, nil
	}
	return self.nodesFrom(self.tx.Where(`"agent_id" = ? AND "id" IN ?`, agentId, nodeIds))
}

// PutAgentNode writes a page, making whatever is missing above it.
//
// The parent is worked out from the path rather than given, because the
// path is what the model writes and a parent identifier is not something
// it could know. A page written at "work/mujin/dev/portal" when none of
// those exist gets three folders and a project, which is what somebody
// filing a thing three levels down means.
func (self *transaction) PutAgentNode(node *models.AgentNode) (*models.AgentNode, error) {
	if node.AgentID == "" {
		return nil, fmt.Errorf("db: a page needs an agent")
	}
	node.Path = models.NormalizePath(node.Path)
	if node.Kind == "" {
		node.Kind = models.NodeFolder
	}
	if node.Name == "" {
		node.Name = models.LastSegment(node.Path)
	}
	if err := node.Validate(); err != nil {
		return nil, err
	}

	// Everything above it, as folders, oldest first.
	if parentPath := models.ParentPath(node.Path); parentPath != "" {
		parent, err := self.GetAgentNode(node.AgentID, parentPath)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			parent, err = self.PutAgentNode(&models.AgentNode{
				AgentID: node.AgentID, Path: parentPath, Kind: models.NodeFolder,
			})
			if err != nil {
				return nil, err
			}
		}
		node.ParentID = parent.ID
	} else {
		node.ParentID = ""
	}

	existing, err := self.GetAgentNode(node.AgentID, node.Path)
	if err != nil {
		return nil, err
	}
	// PostgreSQL keeps microseconds. Truncating here means the page a
	// caller is handed back carries the time that was actually stored, so
	// writing twice and reading once agree.
	now := time.Now().Truncate(time.Microsecond)
	written := *node
	if existing != nil {
		written.ID = existing.ID
		written.CreatedAt = existing.CreatedAt
		if written.UsedAt == nil {
			written.UsedAt = existing.UsedAt
		}
	} else {
		written.ID = newID()
		written.CreatedAt = now
	}
	written.ModifiedAt = now

	row, err := nodeToModel(&written)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Save writes every column, and the fact counter is this
		// package's, not the caller's: read it back rather than resetting
		// it to one.
		var counters []int
		if err := self.tx.Raw(`SELECT "next_fact_number" FROM "agent_node" WHERE "id" = ?`, written.ID).Scan(&counters).Error; err != nil {
			return nil, err
		}
		if len(counters) > 0 && counters[0] > 0 {
			row.NextFactNumber = counters[0]
		}
		if err := self.tx.Save(row).Error; err != nil {
			return nil, err
		}
	} else {
		// An upsert rather than an insert, because two transactions can
		// reach here for the same path at once and both find nothing:
		// a turn building its prompt makes the roots while a filing run
		// is making them too, and one of them then hit the unique index.
		// Making it a conflict the database settles is the only fix that
		// does not depend on who got there first.
		if err := self.tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "agent_id"}, {Name: "path"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"modified_at", "kind", "name", "aliases", "summary", "parent_id",
				// The build that wrote what is there now, so that a later
				// one can find what an earlier one left.
				"version",
			}),
		}).Create(row).Error; err != nil {
			return nil, err
		}
		// The row that is there may be the other transaction's, with its
		// own identifier, so the answer is read back rather than assumed.
		settled, err := self.GetAgentNode(written.AgentID, written.Path)
		if err != nil {
			return nil, err
		}
		if settled != nil {
			written = *settled
		}
	}
	if err := self.indexNode(&written); err != nil {
		return nil, err
	}
	// A page whose words changed no longer means what its vector says it
	// means; dropping it puts the page back in the queue for a new one.
	if existing != nil && (existing.Summary != written.Summary || existing.Name != written.Name) {
		if err := self.tx.Exec(`DELETE FROM "agent_node_vector" WHERE "node_id" = ?`, written.ID).Error; err != nil {
			return nil, err
		}
	}
	switch {
	case existing == nil:
		self.note(written.AgentID, written.ID, models.RevisionCreated, nil,
			map[string]any{"path": written.Path, "kind": string(written.Kind), "name": written.Name}, "")
	case existing.Summary != written.Summary:
		self.note(written.AgentID, written.ID, models.RevisionPage,
			map[string]any{"text": existing.Summary},
			map[string]any{"text": written.Summary}, "")
	case existing.Name != written.Name:
		self.note(written.AgentID, written.ID, models.RevisionRenamed,
			map[string]any{"text": existing.Name},
			map[string]any{"text": written.Name}, "")
	}
	return &written, nil
}

// indexNode writes the page's full-text column. Written on the way in
// rather than by a trigger, which is how mail does it.
func (self *transaction) indexNode(node *models.AgentNode) error {
	text := node.Path + " " + node.Name + " " + strings.Join(node.Aliases, " ") + " " + node.Summary
	return self.tx.Exec(`UPDATE "agent_node" SET "search" = to_tsvector('simple', ?) WHERE "id" = ?`, text, node.ID).Error
}

func (self *transaction) MoveAgentNode(agentId, path, newParentPath string) (*models.AgentNode, error) {
	node, err := self.GetAgentNode(agentId, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, ErrNotFound
	}
	newParentPath = models.NormalizePath(newParentPath)
	target := models.JoinPath(newParentPath, models.LastSegment(path))
	if target == path {
		return node, nil
	}
	// A page cannot be moved inside itself; the paths below it would
	// chase their own tails.
	if newParentPath == path || strings.HasPrefix(newParentPath, path+"/") {
		return nil, fmt.Errorf("db: %q cannot be moved inside itself", path)
	}
	if existing, err := self.GetAgentNode(agentId, target); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("db: %q is already a page", target)
	}

	var parentId *string
	if newParentPath != "" {
		parent, err := self.GetAgentNode(agentId, newParentPath)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			parent, err = self.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: newParentPath, Kind: models.NodeFolder})
			if err != nil {
				return nil, err
			}
		}
		parentId = &parent.ID
	}

	// The subtree's paths, rewritten in one statement: everything at or
	// below the old path gets the new prefix.
	if err := self.tx.Exec(
		`UPDATE "agent_node" SET "path" = ? || substring("path" from ?::int), "modified_at" = ?
		 WHERE "agent_id" = ? AND ("path" = ? OR "path" LIKE ?)`,
		target, len(path)+1, time.Now(), agentId, path, likeEscaped(path)+"/%").Error; err != nil {
		return nil, err
	}
	if err := self.tx.Model(&agentNodeModel{}).Where(`"id" = ?`, node.ID).
		Update("parent_id", parentId).Error; err != nil {
		return nil, err
	}
	moved, err := self.GetAgentNode(agentId, target)
	if err != nil || moved == nil {
		return nil, err
	}
	if err := self.indexNode(moved); err != nil {
		return nil, err
	}
	self.note(agentId, moved.ID, models.RevisionMoved,
		map[string]any{"path": path}, map[string]any{"path": target}, "")
	return moved, nil
}

func (self *transaction) DeleteAgentNode(agentId, path string) (int64, error) {
	node, err := self.GetAgentNode(agentId, path)
	if err != nil || node == nil {
		return 0, err
	}
	result := self.tx.Where(`"agent_id" = ? AND ("path" = ? OR "path" LIKE ?)`, agentId, path, likeEscaped(path)+"/%").
		Delete(&agentNodeModel{})
	return result.RowsAffected, result.Error
}

func (self *transaction) ListAgentNodeChildrenPage(agentId, nodeId string, limit, offset int) ([]*models.AgentNode, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	// An empty page is the top: the roots, which have no parent. Not a
	// fixed list -- a source filed under "work" makes a root called work,
	// and a list that did not know about it showed its pages under Notes.
	where := self.tx.Where(`"agent_id" = ? AND "parent_id" = ? AND NOT "dormant"`, agentId, nodeId)
	if nodeId == "" {
		where = self.tx.Where(`"agent_id" = ? AND "parent_id" IS NULL AND NOT "dormant"`, agentId)
	}
	var total int64
	if err := where.Model(&agentNodeModel{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// By name, not by importance: importance orders the prompt's index,
	// where the model reads the top; a person scanning a folder of a
	// hundred projects wants them where they expect them. The person's
	// own page leads the roots.
	nodes, err := self.nodesFrom(where.
		Order(`"pinned" DESC, ("kind" = 'self') DESC, lower("name") ASC, "path" ASC`).Limit(limit).Offset(offset))
	return nodes, total, err
}

func (self *transaction) FirstAgentFactLines(agentId string, nodeIds []string) (map[string]string, error) {
	lines := map[string]string{}
	if len(nodeIds) == 0 {
		return lines, nil
	}
	var rows []struct {
		NodeID string `gorm:"column:node_id"`
		Text   string `gorm:"column:text"`
	}
	if err := self.tx.Raw(`
		SELECT DISTINCT ON ("node_id") "node_id", "text" FROM "agent_fact"
		WHERE "agent_id" = ? AND "node_id" = ANY(?) AND NOT "dormant" AND "superseded_by" IS NULL
		ORDER BY "node_id", "number" ASC`, agentId, pq.Array(nodeIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		lines[row.NodeID] = row.Text
	}
	return lines, nil
}

func (self *transaction) ListAgentNodeChildren(agentId, nodeId string) ([]*models.AgentNode, error) {
	return self.nodesFrom(self.tx.Where(`"agent_id" = ? AND "parent_id" = ?`, agentId, nodeId).
		Order(`"pinned" DESC, "importance" DESC, "path" ASC`))
}

func (self *transaction) ListAgentIndex(agentId string, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 60
	}
	return self.nodesFrom(self.tx.Where(`"agent_id" = ? AND NOT "dormant"`, agentId).
		Order(`"pinned" DESC, "importance" DESC, "modified_at" DESC`).Limit(limit))
}

func (self *transaction) ListAgentNodesUnder(agentId, path string, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 500
	}
	query := self.tx.Where(`"agent_id" = ?`, agentId)
	if path != "" {
		query = query.Where(`("path" = ? OR "path" LIKE ?)`, path, likeEscaped(path)+"/%")
	}
	return self.nodesFrom(query.Order(`"path" ASC`).Limit(limit))
}

// --- facts ------------------------------------------------------------

// AddAgentFact puts a sentence on a page, taking the next number.
//
// The number comes from the highest one the page has ever had rather than
// the count of what it has now, so that a number never names two different
// sentences even after one is forgotten.
func (self *transaction) AddAgentFact(fact *models.AgentFact) (*models.AgentFact, error) {
	if fact.AgentID == "" || fact.NodeID == "" {
		return nil, fmt.Errorf("db: a fact needs an agent and a page")
	}
	if fact.Kind == "" {
		fact.Kind = models.FactPlain
	}
	if fact.Confidence == 0 {
		fact.Confidence = 1
	}
	if err := fact.Validate(); err != nil {
		return nil, err
	}
	// Take the page's next number and move the mark on, in one statement
	// so that two facts written at once cannot be given the same one.
	var numbers []int
	if err := self.tx.Raw(
		`UPDATE "agent_node" SET "next_fact_number" = "next_fact_number" + 1
		 WHERE "id" = ? AND "agent_id" = ? RETURNING "next_fact_number" - 1`,
		fact.NodeID, fact.AgentID).Scan(&numbers).Error; err != nil {
		return nil, err
	}
	if len(numbers) == 0 {
		return nil, fmt.Errorf("db: no page %q to put a fact on", fact.NodeID)
	}
	created := *fact
	created.ID = newID()
	created.Number = numbers[0]
	created.CreatedAt = time.Now().Truncate(time.Microsecond)
	created.ModifiedAt = created.CreatedAt
	row, err := factToModel(&created)
	if err != nil {
		return nil, err
	}
	if err := self.tx.Create(row).Error; err != nil {
		return nil, err
	}
	if err := self.indexFact(&created); err != nil {
		return nil, err
	}
	self.note(created.AgentID, created.NodeID, models.RevisionFactAdded, nil,
		map[string]any{"number": created.Number, "text": created.Text, "kind": string(created.Kind)}, "")
	return &created, nil
}

func (self *transaction) indexFact(fact *models.AgentFact) error {
	return self.tx.Exec(`UPDATE "agent_fact" SET "search" = to_tsvector('simple', ?) WHERE "id" = ?`, fact.Text, fact.ID).Error
}

// factJournal is what a write to a fact leaves in the page's history
// when it takes the row out of what the page states.
//
// Two ways out and they are not the same change: superseded points at
// the row that stands in its place, dormant points at nothing. A kind
// left empty files nothing, which is what an ordinary edit that happens
// to set dormant wants -- the nightly pass that retires facts nobody has
// wanted in half a year would otherwise write a line per fact per night.
type factJournal struct {
	superseded models.RevisionKind
	dormant    models.RevisionKind
	reason     string
}

func (self *transaction) UpdateAgentFact(agentId, factId string, modify func(*models.AgentFact) error) (*models.AgentFact, error) {
	return self.writeAgentFact(agentId, factId, modify, factJournal{
		superseded: models.RevisionFactMerged,
		reason:     "it said what another fact already said",
	})
}

// FoldAgentFact marks a fact as standing behind another one on the same
// page: dormant, pointing at the row the page now states, still readable.
//
// Nothing is deleted. A fold is a judgement -- two sentences a cosine
// called near enough, or a later statement of the same thing -- and a
// judgement the person disagrees with has to be visible before it can be
// undone. The history entry carries both identifiers so the pair can be
// found again from the journal alone.
func (self *transaction) FoldAgentFact(agentId, factId, intoFactId, reason string) (*models.AgentFact, error) {
	if intoFactId == "" || intoFactId == factId {
		return nil, fmt.Errorf("db: folding a fact needs another fact to fold it into")
	}
	return self.writeAgentFact(agentId, factId, func(fact *models.AgentFact) error {
		fact.SupersededBy = intoFactId
		fact.Dormant = true
		return nil
	}, factJournal{superseded: models.RevisionFactFolded, reason: reason})
}

// StrikeAgentFact marks a fact dormant with nothing standing in its
// place: a line that says nothing, found by the rules as they are now.
//
// Dormant rather than gone for the same reason as a fold, and because
// the rule that struck it may itself be wrong: what was struck can be
// read back and put right.
func (self *transaction) StrikeAgentFact(agentId, factId, reason string) (*models.AgentFact, error) {
	return self.writeAgentFact(agentId, factId, func(fact *models.AgentFact) error {
		fact.Dormant = true
		return nil
	}, factJournal{dormant: models.RevisionFactStruck, reason: reason})
}

// writeAgentFact is the one writer behind all three: it locks the row,
// applies the change, reindexes when the words moved, and files one
// history entry.
//
// The journal comes from the caller because the columns that move are
// the same for a merge, a write-time fold and a striking, and only the
// caller knows which of them it just did. A page's history that calls
// all three "merged two facts" is a history nobody can act on.
func (self *transaction) writeAgentFact(agentId, factId string, modify func(*models.AgentFact) error, journal factJournal) (*models.AgentFact, error) {
	var rows []agentFactModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(`"id" = ? AND "agent_id" = ?`, factId, agentId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	fact, err := rows[0].toModel()
	if err != nil {
		return nil, err
	}
	wasSaying := fact.Text
	wasSuperseded := fact.SupersededBy
	wasDormant := fact.Dormant
	if err := modify(fact); err != nil {
		return nil, err
	}
	if err := fact.Validate(); err != nil {
		return nil, err
	}
	fact.ID = factId
	fact.ModifiedAt = time.Now().Truncate(time.Microsecond)
	row, err := factToModel(fact)
	if err != nil {
		return nil, err
	}
	if err := self.tx.Save(row).Error; err != nil {
		return nil, err
	}
	switch {
	case fact.Text != wasSaying:
		if err := self.indexFact(fact); err != nil {
			return nil, err
		}
		if err := self.tx.Exec(`DELETE FROM "agent_fact_vector" WHERE "fact_id" = ?`, factId).Error; err != nil {
			return nil, err
		}
		self.note(fact.AgentID, fact.NodeID, models.RevisionFactEdited,
			map[string]any{"number": fact.Number, "text": wasSaying},
			map[string]any{"number": fact.Number, "text": fact.Text}, "")
	case fact.SupersededBy != "" && wasSuperseded == "" && journal.superseded != "":
		self.note(fact.AgentID, fact.NodeID, journal.superseded,
			map[string]any{"number": fact.Number, "text": fact.Text, "id": fact.ID},
			map[string]any{"supersededBy": fact.SupersededBy}, journal.reason)
	case fact.Dormant && !wasDormant && journal.dormant != "":
		self.note(fact.AgentID, fact.NodeID, journal.dormant,
			map[string]any{"number": fact.Number, "text": fact.Text, "id": fact.ID},
			map[string]any{"dormant": true}, journal.reason)
	}
	return fact, nil
}

func (self *transaction) GetAgentFact(agentId, nodeId string, number int) (*models.AgentFact, error) {
	facts, err := self.factsFrom(self.tx.Where(`"agent_id" = ? AND "node_id" = ? AND "number" = ?`, agentId, nodeId, number).Limit(1))
	if err != nil || len(facts) == 0 {
		return nil, err
	}
	return facts[0], nil
}

func (self *transaction) GetAgentFacts(agentId string, factIds []string) ([]*models.AgentFact, error) {
	if agentId == "" || len(factIds) == 0 {
		return nil, nil
	}
	return self.factsFrom(self.tx.Where(`"agent_id" = ? AND "id" IN ?`, agentId, factIds))
}

func (self *transaction) ListAgentFacts(agentId, nodeId string, includeDormant bool, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 200
	}
	query := self.tx.Where(`"agent_id" = ? AND "node_id" = ?`, agentId, nodeId)
	if !includeDormant {
		query = query.Where(`NOT "dormant" AND "superseded_by" IS NULL`)
	}
	return self.factsFrom(query.Order(`"number" ASC`).Limit(limit))
}

// ListAgentFactsLively is a page's facts with the ones most recently
// wanted or changed first: what a reader with room for twenty of a
// hundred should see. By number, the twenty were the oldest, and the
// fact filed last week never reached a prompt on a page of eighty.
func (self *transaction) ListAgentFactsLively(agentId, nodeId string, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 30
	}
	return self.factsFrom(self.tx.Where(`"agent_id" = ? AND "node_id" = ? AND NOT "dormant" AND "superseded_by" IS NULL`, agentId, nodeId).
		Order(`"used_at" DESC NULLS LAST, "modified_at" DESC`).Limit(limit))
}

func (self *transaction) ListAgentFactsForAudience(agentId string, audience models.AgentAudience, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 30
	}
	encoded, _ := json.Marshal([]models.AgentAudience{audience})
	return self.factsFrom(self.tx.
		Where(`"agent_id" = ? AND NOT "dormant" AND "superseded_by" IS NULL AND "audiences" @> ?::jsonb`, agentId, string(encoded)).
		Order(`"used_at" DESC NULLS LAST, "modified_at" DESC`).Limit(limit))
}

func (self *transaction) ListAgentFactsBetween(agentId string, from, until time.Time, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 2000
	}
	return self.factsFrom(self.tx.
		Where(`"agent_id" = ? AND NOT "dormant" AND "happened_at" >= ? AND "happened_at" < ?`, agentId, from, until).
		Order(`"happened_at" ASC`).Limit(limit))
}

func (self *transaction) ListAgentFactsLearnedSince(agentId string, since time.Time, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 100
	}
	return self.factsFrom(self.tx.
		Where(`"agent_id" = ? AND "created_at" >= ?`, agentId, since).
		Order(`"created_at" DESC`).Limit(limit))
}

func (self *transaction) DeleteAgentFact(agentId, factId string) error {
	// Read it first, so the history says what was taken off rather than
	// only that something was.
	facts, err := self.GetAgentFacts(agentId, []string{factId})
	if err != nil {
		return err
	}
	if err := self.tx.Where(`"id" = ? AND "agent_id" = ?`, factId, agentId).Delete(&agentFactModel{}).Error; err != nil {
		return err
	}
	if len(facts) > 0 {
		self.note(agentId, facts[0].NodeID, models.RevisionFactGone, wholeFact(facts[0]), nil, "")
	}
	return nil
}

// wholeFact is a fact as a history entry has to carry it when the row
// itself is going away.
//
// Everything else that leaves a page leaves the row behind, so the entry
// only has to say what changed. A deletion is the person's own forgetting
// and the row goes, so the number and the words are not enough to put it
// back: what it was, how sure of it the agent was, when it was true, who
// reads it and what it was read from all have to be in the journal or
// they are gone with it.
func wholeFact(fact *models.AgentFact) map[string]any {
	whole := map[string]any{
		"number":     fact.Number,
		"text":       fact.Text,
		"kind":       string(fact.Kind),
		"confidence": fact.Confidence,
		"inferred":   fact.Inferred,
		"evidence":   fact.Evidence,
		"audiences":  fact.Audiences,
	}
	if fact.HappenedAt != nil {
		whole["happenedAt"] = fact.HappenedAt.Format(time.RFC3339)
	}
	return whole
}

// --- search -----------------------------------------------------------

// AnyWord is the tsquery for "any of these words, best match first".
//
// `plainto_tsquery` joins the words it finds with AND, and with the
// `simple` dictionary it drops nothing -- so "what is the portal?" asks
// for a row holding *what*, *is*, *the* and *portal*, and the page
// called Portal does not have the first three. Every question phrased as
// a sentence found nothing.
//
// Joining with OR instead and letting `ts_rank` sort it out is what a
// search should do: a row matching three words of four comes above one
// matching one. The replacement is on what `plainto_tsquery` produced,
// so whatever the person typed has already been made safe.
//
// `simple` cannot segment Chinese or Japanese, so a graph whose words are
// in those is found by meaning and not here; the tool's answer says so.
const AnyWord = `replace(plainto_tsquery('simple', ?)::text, ' & ', ' | ')::tsquery`

// SearchAgentGraph finds pages and facts by words.
func (self *transaction) SearchAgentGraph(agentId, query string, limit int) ([]*models.AgentNode, []*models.AgentFact, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	query = SearchText(query)
	var nodeIds []string
	if err := self.tx.Raw(
		`SELECT "id" FROM "agent_node"
		 WHERE "agent_id" = ? AND "search" @@ `+AnyWord+`
		 ORDER BY ts_rank("search", `+AnyWord+`) DESC, "importance" DESC LIMIT ?`,
		agentId, query, query, limit).Scan(&nodeIds).Error; err != nil {
		return nil, nil, err
	}
	var factIds []string
	if err := self.tx.Raw(
		`SELECT "id" FROM "agent_fact"
		 WHERE "agent_id" = ? AND NOT "dormant" AND "superseded_by" IS NULL AND "search" @@ `+AnyWord+`
		 ORDER BY ts_rank("search", `+AnyWord+`) DESC, "modified_at" DESC LIMIT ?`,
		agentId, query, query, limit).Scan(&factIds).Error; err != nil {
		return nil, nil, err
	}
	nodes, err := self.GetAgentNodes(agentId, nodeIds)
	if err != nil {
		return nil, nil, err
	}
	facts, err := self.GetAgentFacts(agentId, factIds)
	if err != nil {
		return nil, nil, err
	}
	// The identifier lists are in rank order; the reads are not.
	return orderNodesBy(nodes, nodeIds), orderFactsBy(facts, factIds), nil
}

func orderNodesBy(nodes []*models.AgentNode, ids []string) []*models.AgentNode {
	byId := make(map[string]*models.AgentNode, len(nodes))
	for _, node := range nodes {
		byId[node.ID] = node
	}
	ordered := make([]*models.AgentNode, 0, len(ids))
	for _, id := range ids {
		if node := byId[id]; node != nil {
			ordered = append(ordered, node)
		}
	}
	return ordered
}

func orderFactsBy(facts []*models.AgentFact, ids []string) []*models.AgentFact {
	byId := make(map[string]*models.AgentFact, len(facts))
	for _, fact := range facts {
		byId[fact.ID] = fact
	}
	ordered := make([]*models.AgentFact, 0, len(ids))
	for _, id := range ids {
		if fact := byId[id]; fact != nil {
			ordered = append(ordered, fact)
		}
	}
	return ordered
}

// --- edges ------------------------------------------------------------

func (self *transaction) PutAgentEdge(edge *models.AgentEdge) error {
	if edge.AgentID == "" || edge.FromID == "" || edge.ToID == "" || edge.Relation == "" {
		return fmt.Errorf("db: an edge needs an agent, two pages and a relation")
	}
	if edge.FromID == edge.ToID {
		return fmt.Errorf("db: a page cannot be joined to itself")
	}
	if !models.IsAgentEdgeRelation(edge.Relation) {
		return fmt.Errorf("db: %q is not a relation", edge.Relation)
	}
	evidence := edge.Evidence
	if evidence == nil {
		evidence = []models.Evidence{}
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	weight := edge.Weight
	if weight == 0 {
		weight = 1
	}
	// What was there before, so the history records a link being made or
	// its meaning changing and nothing else. Without this, the nightly
	// reweighting -- which writes every edge every night -- would fill a
	// page's history with changes nobody made.
	var existing []agentEdgeModel
	if err := self.tx.Where(`"from_id" = ? AND "to_id" = ? AND "relation" = ?`,
		edge.FromID, edge.ToID, string(edge.Relation)).Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	note := truncateRunes(edge.Note, 400)

	row := &agentEdgeModel{
		AgentID: edge.AgentID, FromID: edge.FromID, ToID: edge.ToID,
		Relation: string(edge.Relation), Weight: weight, Evidence: encoded,
		Note: note, HappenedAt: edge.HappenedAt,
		UsedAt: edge.UsedAt, CreatedAt: time.Now(),
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "from_id"}, {Name: "to_id"}, {Name: "relation"}},
		DoUpdates: clause.AssignmentColumns([]string{"weight", "evidence", "note", "happened_at"}),
	}).Create(row).Error; err != nil {
		return err
	}
	if len(existing) > 0 && existing[0].Note == note {
		return nil // the same link, said again
	}
	var before map[string]any
	if len(existing) > 0 {
		before = map[string]any{"relation": string(edge.Relation), "text": existing[0].Note}
	}
	// Against both ends, and each end is told which other page it is now
	// joined to: "linked to Portal" is a history somebody can read, and
	// "linked to something" is not.
	paths := self.pathsOf(edge.AgentID, edge.FromID, edge.ToID)
	after := map[string]any{"relation": string(edge.Relation), "text": edge.Note}
	self.note(edge.AgentID, edge.FromID, models.RevisionLinked,
		withOther(before, paths[edge.ToID]), withOther(after, paths[edge.ToID]), "")
	self.note(edge.AgentID, edge.ToID, models.RevisionLinked,
		withOther(before, paths[edge.FromID]), withOther(after, paths[edge.FromID]), "")
	return nil
}

// pathsOf is the path of each of a handful of pages, for a history that
// names the other end of a link rather than its identifier.
func (self *transaction) pathsOf(agentId string, nodeIds ...string) map[string]string {
	paths := map[string]string{}
	nodes, err := self.GetAgentNodes(agentId, nodeIds)
	if err != nil {
		log.Debugf("cannot read the pages a link joins: %s", err)
		return paths
	}
	for _, node := range nodes {
		paths[node.ID] = node.Path
	}
	return paths
}

// withOther is a change with the page at the other end of the link named
// in it. Nil in stays nil out: a link being made has no "before".
func withOther(values map[string]any, path string) map[string]any {
	if values == nil {
		return nil
	}
	copied := make(map[string]any, len(values)+1)
	for key, value := range values {
		copied[key] = value
	}
	copied["path"] = path
	return copied
}

func (self *transaction) DeleteAgentEdge(agentId, fromId, toId string, relation models.AgentEdgeRelation) error {
	if err := self.tx.Where(`"agent_id" = ? AND "from_id" = ? AND "to_id" = ? AND "relation" = ?`,
		agentId, fromId, toId, string(relation)).Delete(&agentEdgeModel{}).Error; err != nil {
		return err
	}
	paths := self.pathsOf(agentId, fromId, toId)
	before := map[string]any{"relation": string(relation)}
	self.note(agentId, fromId, models.RevisionUnlinked, withOther(before, paths[toId]), nil, "")
	self.note(agentId, toId, models.RevisionUnlinked, withOther(before, paths[fromId]), nil, "")
	return nil
}

func (self *transaction) ListAgentEdges(agentId, nodeId string) ([]*models.AgentEdge, error) {
	var rows []agentEdgeModel
	if err := self.tx.Where(`"agent_id" = ? AND ("from_id" = ? OR "to_id" = ?)`, agentId, nodeId, nodeId).
		Order(`"relation" ASC`).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	wanted := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		wanted = append(wanted, row.FromID, row.ToID)
	}
	nodes, err := self.GetAgentNodes(agentId, wanted)
	if err != nil {
		return nil, err
	}
	byId := make(map[string]*models.AgentNode, len(nodes))
	for _, node := range nodes {
		byId[node.ID] = node
	}
	edges := make([]*models.AgentEdge, 0, len(rows))
	for _, row := range rows {
		edge := &models.AgentEdge{
			AgentID: row.AgentID, FromID: row.FromID, ToID: row.ToID,
			Relation: models.AgentEdgeRelation(row.Relation), Weight: row.Weight,
			Note: row.Note, HappenedAt: row.HappenedAt, UsedAt: row.UsedAt,
			CreatedAt: row.CreatedAt, Evidence: []models.Evidence{},
		}
		// Both ends by name as well as by path: an edge is read as a
		// sentence about two things, and a path is not a name.
		if from := byId[row.FromID]; from != nil {
			edge.FromPath, edge.FromName, edge.FromKind = from.Path, from.Name, from.Kind
		}
		if to := byId[row.ToID]; to != nil {
			edge.ToPath, edge.ToName, edge.ToKind = to.Path, to.Name, to.Kind
		}
		if len(row.Evidence) > 0 {
			if err := json.Unmarshal(row.Evidence, &edge.Evidence); err != nil {
				return nil, err
			}
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// --- use, vectors, counts ---------------------------------------------

func (self *transaction) TouchAgentNodes(nodeIds []string, at time.Time) error {
	if len(nodeIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentNodeModel{}).Where(`"id" IN ?`, nodeIds).Update("used_at", at).Error
}

func (self *transaction) TouchAgentFacts(factIds []string, at time.Time) error {
	if len(factIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentFactModel{}).Where(`"id" IN ?`, factIds).Update("used_at", at).Error
}

func (self *transaction) PutAgentNodeVector(agentId, nodeId, model string, vector []float32) error {
	return self.putGraphVector("agent_node_vector", "node_id", agentId, nodeId, model, vector)
}

func (self *transaction) PutAgentFactVector(agentId, factId, model string, vector []float32) error {
	return self.putGraphVector("agent_fact_vector", "fact_id", agentId, factId, model, vector)
}

func (self *transaction) putGraphVector(table, idColumn, agentId, id, model string, vector []float32) error {
	if agentId == "" || id == "" || model == "" || len(vector) == 0 {
		return fmt.Errorf("db: a vector needs the agent, the row, the model and the vector")
	}
	if err := self.NoteVectorModel(model, len(vector)); err != nil {
		return err
	}
	statement := fmt.Sprintf(
		`INSERT INTO %s (%s, "agent_id", "model", "vector", "created_at") VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (%s, "model") DO UPDATE SET "vector" = EXCLUDED."vector", "created_at" = EXCLUDED."created_at"`,
		pq.QuoteIdentifier(table), pq.QuoteIdentifier(idColumn), pq.QuoteIdentifier(idColumn))
	return self.tx.Exec(statement, id, agentId, model, pq.Float32Array(vector), time.Now()).Error
}

func (self *transaction) ListAgentNodesWithoutVector(agentId, model string, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 20
	}
	var ids []string
	if err := self.tx.Raw(
		`SELECT n."id" FROM "agent_node" n
		 LEFT JOIN "agent_node_vector" v ON v."node_id" = n."id" AND v."model" = ?
		 WHERE n."agent_id" = ? AND v."node_id" IS NULL AND (n."summary" <> '' OR n."name" <> '')
		 ORDER BY n."pinned" DESC, n."modified_at" DESC LIMIT ?`, model, agentId, limit).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return self.GetAgentNodes(agentId, ids)
}

func (self *transaction) ListAgentFactsWithoutVector(agentId, model string, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 20
	}
	var ids []string
	if err := self.tx.Raw(
		`SELECT f."id" FROM "agent_fact" f
		 LEFT JOIN "agent_fact_vector" v ON v."fact_id" = f."id" AND v."model" = ?
		 WHERE f."agent_id" = ? AND v."fact_id" IS NULL AND NOT f."dormant"
		 ORDER BY f."modified_at" DESC LIMIT ?`, model, agentId, limit).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return self.GetAgentFacts(agentId, ids)
}

func (self *transaction) CountAgentGraph(agentId string) (int64, int64, error) {
	var nodes, facts int64
	if err := self.tx.Model(&agentNodeModel{}).Where(`"agent_id" = ?`, agentId).Count(&nodes).Error; err != nil {
		return 0, 0, err
	}
	if err := self.tx.Model(&agentFactModel{}).Where(`"agent_id" = ? AND NOT "dormant"`, agentId).Count(&facts).Error; err != nil {
		return 0, 0, err
	}
	return nodes, facts, nil
}

// SetUserContact names the address book entry that is the account itself.
// Empty clears it.
func (self *transaction) SetUserContact(userId, contactId string) error {
	if userId == "" {
		return fmt.Errorf("db: naming the contact that is somebody needs the account")
	}
	var value any
	if contactId != "" {
		value = contactId
	}
	return self.tx.Exec(`UPDATE "user" SET "contact_id" = ? WHERE "id" = ?`, value, userId).Error
}

// ErrNoSuchFact is a fact that is not there any more: struck, or merged
// away, between a caller reading it and acting on it.
var ErrNoSuchFact = errors.New("db: no such fact")

func (self *transaction) MoveAgentFact(agentId, factId, toNodeId string) (*models.AgentFact, error) {
	facts, err := self.factsFrom(self.tx.Where(`"id" = ? AND "agent_id" = ?`, factId, agentId).Limit(1))
	if err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchFact, factId)
	}
	fact := facts[0]
	if fact.NodeID == toNodeId {
		return fact, nil
	}
	var numbers []int
	if err := self.tx.Raw(
		`UPDATE "agent_node" SET "next_fact_number" = "next_fact_number" + 1
		 WHERE "id" = ? AND "agent_id" = ? RETURNING "next_fact_number" - 1`,
		toNodeId, agentId).Scan(&numbers).Error; err != nil {
		return nil, err
	}
	if len(numbers) == 0 {
		return nil, fmt.Errorf("db: no page %q to move a fact to", toNodeId)
	}
	from := fact.NodeID
	before := map[string]any{"number": fact.Number, "text": fact.Text}
	fact.NodeID, fact.Number, fact.ModifiedAt = toNodeId, numbers[0], time.Now().Truncate(time.Microsecond)
	if err := self.tx.Model(&agentFactModel{}).Where(`"id" = ?`, fact.ID).Updates(map[string]any{
		"node_id": fact.NodeID, "number": fact.Number, "modified_at": fact.ModifiedAt,
	}).Error; err != nil {
		return nil, err
	}
	paths := self.pathsOf(agentId, from, toNodeId)
	self.note(agentId, from, models.RevisionFactGone, withOther(before, paths[toNodeId]), nil, "moved")
	self.note(agentId, toNodeId, models.RevisionFactAdded, nil,
		withOther(map[string]any{"number": fact.Number, "text": fact.Text, "kind": string(fact.Kind)}, paths[from]), "moved")
	return fact, nil
}

func (self *transaction) MergeAgentNodes(agentId, fromPath, intoPath string) (*models.AgentNode, error) {
	from, err := self.GetAgentNode(agentId, fromPath)
	if err != nil {
		return nil, err
	}
	if from == nil {
		return nil, fmt.Errorf("there is no page at %s", fromPath)
	}
	into, err := self.GetAgentNode(agentId, intoPath)
	if err != nil {
		return nil, err
	}
	if into == nil {
		return nil, fmt.Errorf("there is no page at %s", intoPath)
	}
	if from.ID == into.ID {
		return into, nil
	}
	facts, err := self.ListAgentFacts(agentId, from.ID, true, 10000)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		if _, err := self.MoveAgentFact(agentId, fact.ID, into.ID); err != nil {
			return nil, err
		}
	}
	edges, err := self.ListAgentEdges(agentId, from.ID)
	if err != nil {
		return nil, err
	}
	for _, edge := range edges {
		moved := *edge
		if moved.FromID == from.ID {
			moved.FromID = into.ID
		}
		if moved.ToID == from.ID {
			moved.ToID = into.ID
		}
		if err := self.DeleteAgentEdge(agentId, edge.FromID, edge.ToID, edge.Relation); err != nil {
			return nil, err
		}
		// A link from the page to itself, after the merge, is no link.
		if moved.FromID == moved.ToID {
			continue
		}
		if err := self.PutAgentEdge(&moved); err != nil {
			return nil, err
		}
	}
	children, err := self.ListAgentNodeChildren(agentId, from.ID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if _, err := self.MoveAgentNode(agentId, child.Path, into.Path); err != nil {
			return nil, err
		}
	}
	// What the merged page was called is another name for the survivor.
	aliases := append([]string{}, into.Aliases...)
	known := map[string]bool{strings.ToLower(into.Name): true}
	for _, alias := range aliases {
		known[strings.ToLower(alias)] = true
	}
	for _, name := range append([]string{from.Name, models.LastSegment(from.Path)}, from.Aliases...) {
		if name = strings.TrimSpace(name); name != "" && !known[strings.ToLower(name)] {
			aliases = append(aliases, name)
			known[strings.ToLower(name)] = true
		}
	}
	into.Aliases = aliases
	if strings.TrimSpace(into.Summary) == "" {
		into.Summary = from.Summary
	}
	if into, err = self.PutAgentNode(into); err != nil {
		return nil, err
	}
	self.note(agentId, into.ID, models.RevisionMoved, map[string]any{"merged": from.Path}, nil, "merged into this page")
	if _, err := self.DeleteAgentNode(agentId, from.Path); err != nil {
		return nil, err
	}
	return into, nil
}

func (self *transaction) CountAgentNodeChildren(agentId string, nodeIds []string) (map[string]int, error) {
	counts := map[string]int{}
	if len(nodeIds) == 0 {
		return counts, nil
	}
	var rows []struct {
		ParentID string `gorm:"column:parent_id"`
		Count    int    `gorm:"column:count"`
	}
	if err := self.tx.Raw(`
		SELECT "parent_id", count(*) AS "count" FROM "agent_node"
		WHERE "agent_id" = ? AND "parent_id" = ANY(?) AND NOT "dormant"
		GROUP BY "parent_id"`, agentId, pq.Array(nodeIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.ParentID] = row.Count
	}
	return counts, nil
}
