package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// The store for knowledge: sources, the documents read from them, the
// chunks those are cut into, and the symbols a code file declares.
//
// Everything is scoped to one agent, and a chunk carries its source as
// well as its document so that a search narrowed to one source is one
// indexed read rather than two joins.

// KnowledgeOperation is knowledge as the rest of the server reaches it.
type KnowledgeOperation interface {
	// Sources.
	// PutAgentSource advances the generation. Existing rows must be read under
	// LockAgentSource so concurrent ingestion progress is preserved.
	PutAgentSource(source *models.AgentKnowledgeSource) (*models.AgentKnowledgeSource, error)
	SetAgentSourceUnknownAuthors(sourceId string, addresses []string) error
	GetAgentSource(agentId, sourceId string) (*models.AgentKnowledgeSource, error)
	LockAgentSource(agentId, sourceId string) (*models.AgentKnowledgeSource, error)
	GetAgentSourceByName(agentId, name string) (*models.AgentKnowledgeSource, error)

	// ListAgentSourceCheckouts is the checkouts a source's commits were
	// read from, as directories relative to the source: where each of its
	// files belongs, since a file does not say.
	ListAgentSourceCheckouts(sourceId string) ([]string, error)
	ListAgentSources(agentId string) ([]*models.AgentKnowledgeSource, error)
	DeleteAgentSource(agentId, sourceId string) error

	// ListDueAgentSources is every enabled source whose time has come, or
	// which said last time that it had more to do.
	ListDueAgentSources(now time.Time, limit int) ([]*models.AgentKnowledgeSource, error)

	// MarkAgentSourceRun records what a pass did and when the next is due.
	MarkAgentSourceRun(sourceId string, cursor map[string]any, counts SourceCounts, more bool, lastError string, nextRun *time.Time) error

	// Documents.
	PutAgentDocument(document *models.AgentDocument) (*models.AgentDocument, error)
	GetAgentDocument(agentId, documentId string) (*models.AgentDocument, error)
	GetAgentDocuments(agentId string, documentIds []string) ([]*models.AgentDocument, error)
	GetAgentDocumentByExternal(sourceId, externalId string) (*models.AgentDocument, error)
	DeleteAgentDocument(agentId, documentId string) error

	// ListAgentDocumentHashes is what a source already holds, so a pass
	// can tell an unchanged file from one to read again without reading
	// either.
	ListAgentDocumentHashes(sourceId string) (map[string]string, error)

	// AgentDocumentStorageKey is the key some document of this agent
	// keeps the bytes of this hash under, and "" where none does.
	//
	// An attachment is identified by the hash of its bytes, so the same
	// picture pasted into four threads -- or found again by another
	// source -- is bytes this server already holds. Asked before the
	// computer is asked for them, it is what keeps a second pass from
	// carrying twenty-four gigabytes across the socket again.
	AgentDocumentStorageKey(agentId, hash string) (string, error)

	// MarkAgentDocumentsSeen says the source still has these, named the
	// way the source names them. One statement for a whole page of a
	// scan, because a page of an archive is two thousand names.
	MarkAgentDocumentsSeen(sourceId string, externalIds []string, at time.Time) error

	// RetitleAgentDocuments writes the title and metadata a source now
	// gives documents whose text it still has unchanged, where the title
	// is not the one stored, and says how many it changed.
	RetitleAgentDocuments(sourceId string, documents []*models.AgentDocument) (int64, error)

	// CountAgentSourceDocuments is how many documents and passages a
	// source holds, counted rather than kept: a pass adds what it files,
	// and a document filed again under the same name was added twice.
	CountAgentSourceDocuments(sourceId string) (documents, chunks int, err error)

	// DeleteAgentDocumentsUnseen removes what a source no longer has:
	// the documents it did not name in a pass that began at the given
	// time and reached the end of the tree, with their passages and
	// their symbols. It answers how many went.
	DeleteAgentDocumentsUnseen(sourceId string, before time.Time) (int, error)

	// ListAgentDocumentsBetween is what happened in a stretch of time:
	// what a period page is written from.
	ListAgentDocumentsBetween(agentId string, kinds []models.AgentDocumentKind, from, until time.Time, limit int) ([]*models.AgentDocument, error)

	// Chunks.
	ReplaceAgentChunks(document *models.AgentDocument, chunks []*models.AgentChunk) error
	GetAgentChunks(agentId string, chunkIds []string) ([]*models.AgentChunk, error)
	ListAgentChunks(agentId, documentId string) ([]*models.AgentChunk, error)
	SearchAgentChunks(agentId string, sourceIds []string, query string, limit int) ([]*models.AgentChunk, error)

	// ListAgentChunksWithoutVector is what is still waiting to be
	// embedded, oldest first so a backlog drains in the order it arrived.
	ListAgentChunksWithoutVector(agentId, model string, limit int) ([]*models.AgentChunk, error)
	PutAgentChunkVector(agentId, sourceId, chunkId, model string, vector []float32) error

	// PutAgentChunkVectors writes a batch in a fixed order, so that two
	// runs writing overlapping batches wait for each other rather than
	// deadlocking.
	PutAgentChunkVectors(agentId string, vectors []AgentChunkVector) error

	// CountAgentChunksWithoutVector is how much is left, for the page
	// that says how far along the first pass is.
	CountAgentChunksWithoutVector(agentId, model string) (int64, error)

	// Symbols.
	ReplaceAgentSymbols(agentId, documentId string, symbols []*models.AgentSymbol) error
	LookupAgentSymbols(agentId string, names []string, limit int) ([]*models.AgentSymbol, error)

	// CountAgentKnowledge is how much there is, per source.
	CountAgentKnowledge(agentId, sourceId string) (documents, chunks int64, err error)

	// CountAgentAttachmentsBySource is what became of the pictures and
	// files each source carried, keyed by the source's identifier: how
	// many wait for the night to decide about them, how many it decided
	// against opening, and how many it opened and read.
	//
	// The same three numbers reading.Progress reports for the whole
	// agent, cut by source, because a person looking at a card wants to
	// know which of their sources the fifty thousand screenshots are in.
	CountAgentAttachmentsBySource(agentId string) (map[string]AgentAttachmentCounts, error)

	// ListAgentAttachmentsDeclined is the files the night decided against
	// opening, newest first, for one source or for every source when no
	// source is named. Each carries the reason on its metadata, which is
	// the whole point of keeping the row rather than deleting the file.
	ListAgentAttachmentsDeclined(agentId, sourceId string, limit int) ([]*models.AgentDocument, error)
}

// AgentAttachmentCounts is what became of one source's files.
type AgentAttachmentCounts struct {
	// Undecided has not been looked at even from the outside; Declined
	// was looked at and passed over; Described was opened and made text
	// of.
	Undecided int64
	Declined  int64
	Described int64
}

// SourceCounts is what one pass of a source did.
type SourceCounts struct {
	Documents int
	Chunks    int
	Refused   int

	// Seen is how many things the source named, changed or not. Unlike
	// the three above it is not kept on the source's row: it is only
	// what tells the end of a pass that it really read the tree, rather
	// than having been handed an empty answer by a folder nothing had
	// mounted.
	Seen int

	// CheckoutsKeptToProfile is how many checkouts the pass kept to what
	// git says about them, their files left unread, and
	// FilesKeptToProfile how many files that was.
	//
	// Both are counted over the whole tree by whoever walked it, so a
	// page carries the same numbers as the page before it: they are
	// written down as they arrive rather than added up.
	CheckoutsKeptToProfile int
	FilesKeptToProfile     int
}

// --- rows -------------------------------------------------------------

type agentSourceModel struct {
	ID             string     `gorm:"column:id;primaryKey"`
	AgentID        string     `gorm:"column:agent_id"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	ModifiedAt     time.Time  `gorm:"column:modified_at"`
	Kind           string     `gorm:"column:kind"`
	Name           string     `gorm:"column:name"`
	Specification  []byte     `gorm:"column:specification;type:jsonb"`
	RootPath       string     `gorm:"column:root_path"`
	Enabled        bool       `gorm:"column:enabled"`
	Cron           string     `gorm:"column:cron"`
	Generation     int64      `gorm:"column:generation"`
	Cursor         []byte     `gorm:"column:cursor;type:jsonb"`
	Instance       string     `gorm:"column:instance"`
	LastRunAt      *time.Time `gorm:"column:last_run_at"`
	NextRunAt      *time.Time `gorm:"column:next_run_at"`
	LastError      string     `gorm:"column:last_error"`
	DocumentCount  int        `gorm:"column:document_count"`
	ChunkCount     int        `gorm:"column:chunk_count"`
	RefusedCount   int        `gorm:"column:refused_count"`
	More           bool       `gorm:"column:more"`
	UnknownAuthors []byte     `gorm:"column:unknown_authors;type:jsonb"`

	CheckoutsKeptToProfile int `gorm:"column:checkouts_kept_to_profile"`
	FilesKeptToProfile     int `gorm:"column:files_kept_to_profile"`
}

func (agentSourceModel) TableName() string { return "agent_source" }

type agentDocumentModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	AgentID    string     `gorm:"column:agent_id"`
	SourceID   string     `gorm:"column:source_id"`
	ExternalID string     `gorm:"column:external_id"`
	Kind       string     `gorm:"column:kind"`
	Title      string     `gorm:"column:title"`
	URL        string     `gorm:"column:url"`
	HappenedAt *time.Time `gorm:"column:happened_at"`
	ModifiedAt *time.Time `gorm:"column:modified_at"`
	Hash       string     `gorm:"column:hash"`
	Bytes      int64      `gorm:"column:bytes"`
	StorageKey string     `gorm:"column:storage_key"`
	Metadata   []byte     `gorm:"column:metadata;type:jsonb"`
	Private    bool       `gorm:"column:private"`
	SeenAt     *time.Time `gorm:"column:seen_at"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
}

func (agentDocumentModel) TableName() string { return "agent_document" }

type agentChunkModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	AgentID    string    `gorm:"column:agent_id"`
	DocumentID string    `gorm:"column:document_id"`
	SourceID   string    `gorm:"column:source_id"`
	Number     int       `gorm:"column:number"`
	Text       string    `gorm:"column:text"`
	Segmented  bool      `gorm:"column:segmented"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (agentChunkModel) TableName() string { return "agent_chunk" }

type agentSymbolModel struct {
	AgentID    string `gorm:"column:agent_id"`
	DocumentID string `gorm:"column:document_id;primaryKey"`
	Symbol     string `gorm:"column:symbol;primaryKey"`
	Lowered    string `gorm:"column:lowered"`
	Kind       string `gorm:"column:kind"`
	Line       int    `gorm:"column:line;primaryKey"`
}

func (agentSymbolModel) TableName() string { return "agent_symbol" }

// --- sources ----------------------------------------------------------

func (self *transaction) PutAgentSource(source *models.AgentKnowledgeSource) (*models.AgentKnowledgeSource, error) {
	if source.AgentID == "" {
		return nil, fmt.Errorf("db: a source needs an agent")
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	specification, err := json.Marshal(source.Specification)
	if err != nil {
		return nil, err
	}
	cursor, err := json.Marshal(orEmptyMap(source.Cursor))
	if err != nil {
		return nil, err
	}
	unknownAuthors, err := json.Marshal(orEmptyStrings(source.UnknownAuthors))
	if err != nil {
		return nil, err
	}
	now := time.Now().Truncate(time.Microsecond)
	written := *source
	written.ModifiedAt = now
	written.Generation++
	create := written.ID == ""
	if create {
		written.ID = newID()
		written.CreatedAt = now
	}
	row := &agentSourceModel{
		ID: written.ID, AgentID: written.AgentID, CreatedAt: written.CreatedAt, ModifiedAt: now,
		Kind: string(written.Kind), Name: written.Name, Specification: specification,
		RootPath: written.RootPath, Enabled: written.Enabled, Cron: written.Cron,
		Cursor: cursor, Instance: written.Instance, Generation: written.Generation,
		LastRunAt: written.LastRunAt, NextRunAt: written.NextRunAt, LastError: written.LastError,
		DocumentCount: written.DocumentCount, ChunkCount: written.ChunkCount,
		RefusedCount: written.RefusedCount, More: written.More,
		CheckoutsKeptToProfile: written.CheckoutsKeptToProfile,
		FilesKeptToProfile:     written.FilesKeptToProfile,
		UnknownAuthors:         unknownAuthors,
	}
	if create {
		if err := self.tx.Create(row).Error; err != nil {
			return nil, err
		}
	} else {
		updated := self.tx.Model(&agentSourceModel{}).
			Where("id = ? AND agent_id = ? AND generation = ?", source.ID, source.AgentID, source.Generation).
			Select("*").Updates(row)
		if updated.Error != nil {
			return nil, updated.Error
		}
		if updated.RowsAffected != 1 {
			return nil, fmt.Errorf("db: source changed or was removed; reload it before saving")
		}
	}
	return &written, nil
}

func orEmptyMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func orEmptyStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}

func (self *agentSourceModel) toModel() (*models.AgentKnowledgeSource, error) {
	source := &models.AgentKnowledgeSource{
		ID: self.ID, AgentID: self.AgentID, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt,
		Kind: models.AgentKnowledgeKind(self.Kind), Name: self.Name, RootPath: self.RootPath,
		Enabled: self.Enabled, Cron: self.Cron, Instance: self.Instance, Generation: self.Generation,
		LastRunAt: self.LastRunAt, NextRunAt: self.NextRunAt, LastError: self.LastError,
		DocumentCount: self.DocumentCount, ChunkCount: self.ChunkCount,
		RefusedCount: self.RefusedCount, More: self.More,
		CheckoutsKeptToProfile: self.CheckoutsKeptToProfile,
		FilesKeptToProfile:     self.FilesKeptToProfile,
		Cursor:                 map[string]any{}, UnknownAuthors: []string{},
	}
	if len(self.Specification) > 0 {
		if err := json.Unmarshal(self.Specification, &source.Specification); err != nil {
			return nil, err
		}
	}
	for _, pair := range []struct {
		raw   []byte
		value any
	}{
		{self.Cursor, &source.Cursor},
		{self.UnknownAuthors, &source.UnknownAuthors},
	} {
		if len(pair.raw) > 0 {
			if err := json.Unmarshal(pair.raw, pair.value); err != nil {
				return nil, err
			}
		}
	}
	return source, nil
}

func (self *transaction) sourcesFrom(query *gorm.DB) ([]*models.AgentKnowledgeSource, error) {
	var rows []agentSourceModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	sources := make([]*models.AgentKnowledgeSource, 0, len(rows))
	for index := range rows {
		source, err := rows[index].toModel()
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func (self *transaction) GetAgentSource(agentId, sourceId string) (*models.AgentKnowledgeSource, error) {
	sources, err := self.sourcesFrom(self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, sourceId).Limit(1))
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	return sources[0], nil
}

func (self *transaction) ListAgentSourceCheckouts(sourceId string) ([]string, error) {
	var checkouts []string
	err := self.tx.Raw(`SELECT DISTINCT "metadata"->>'checkout' FROM "agent_document"
		WHERE "source_id" = ? AND "kind" = 'commit' AND COALESCE("metadata"->>'checkout', '') <> ''`, sourceId).
		Scan(&checkouts).Error
	return checkouts, err
}

func (self *transaction) GetAgentSourceByName(agentId, name string) (*models.AgentKnowledgeSource, error) {
	sources, err := self.sourcesFrom(self.tx.Where(`"agent_id" = ? AND LOWER("name") = ?`, agentId, strings.ToLower(name)).Limit(1))
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	return sources[0], nil
}

func (self *transaction) ListAgentSources(agentId string) ([]*models.AgentKnowledgeSource, error) {
	return self.sourcesFrom(self.tx.Where(`"agent_id" = ?`, agentId).Order(`"created_at" ASC`))
}

func (self *transaction) DeleteAgentSource(agentId, sourceId string) error {
	return self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, sourceId).Delete(&agentSourceModel{}).Error
}

// ListDueAgentSources is what wants reading now: a source whose next time
// has come, or one that said last time it had more to do.
//
// A source with more to do is due immediately rather than in ten minutes.
// A first pass over a checkout and a chat archive is hundreds of
// thousands of chunks, and at one bounded pass every ten minutes that is
// two days; run back to back it is a night.
func (self *transaction) ListDueAgentSources(now time.Time, limit int) ([]*models.AgentKnowledgeSource, error) {
	if limit <= 0 {
		limit = 20
	}
	return self.sourcesFrom(self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where(`"enabled" AND ("more" OR ("next_run_at" IS NOT NULL AND "next_run_at" <= ?))`, now).
		Order(`"more" DESC, "next_run_at" ASC NULLS LAST`).Limit(limit))
}

func (self *transaction) MarkAgentSourceRun(sourceId string, cursor map[string]any, counts SourceCounts, more bool, lastError string, nextRun *time.Time) error {
	encoded, err := json.Marshal(orEmptyMap(cursor))
	if err != nil {
		return err
	}
	now := time.Now()
	return self.tx.Model(&agentSourceModel{}).Where(`"id" = ?`, sourceId).Updates(map[string]any{
		"cursor": encoded, "last_run_at": now, "next_run_at": nextRun,
		"last_error": lastError, "more": more, "modified_at": now,
		"document_count": counts.Documents, "chunk_count": counts.Chunks,
		"refused_count":             counts.Refused,
		"checkouts_kept_to_profile": counts.CheckoutsKeptToProfile,
		"files_kept_to_profile":     counts.FilesKeptToProfile,
	}).Error
}

func (self *transaction) CountAgentSourceDocuments(sourceId string) (documents, chunks int, err error) {
	var documentCount, chunkCount int64
	if err := self.tx.Model(&agentDocumentModel{}).Where(`"source_id" = ?`, sourceId).Count(&documentCount).Error; err != nil {
		return 0, 0, err
	}
	if err := self.tx.Table(`"agent_chunk"`).
		Where(`"document_id" IN (SELECT "id" FROM "agent_document" WHERE "source_id" = ?)`, sourceId).
		Count(&chunkCount).Error; err != nil {
		return 0, 0, err
	}
	return int(documentCount), int(chunkCount), nil
}

// --- documents --------------------------------------------------------

func (self *transaction) PutAgentDocument(document *models.AgentDocument) (*models.AgentDocument, error) {
	if document.AgentID == "" || document.SourceID == "" || document.ExternalID == "" {
		return nil, fmt.Errorf("db: a document needs an agent, a source and a name where it came from")
	}
	metadata, err := json.Marshal(orEmptyMap(document.Metadata))
	if err != nil {
		return nil, err
	}
	existing, err := self.GetAgentDocumentByExternal(document.SourceID, document.ExternalID)
	if err != nil {
		return nil, err
	}
	written := *document
	now := time.Now().Truncate(time.Microsecond)
	if existing != nil {
		written.ID = existing.ID
		written.CreatedAt = existing.CreatedAt
	} else {
		written.ID = newID()
		written.CreatedAt = now
	}
	// Writing a document is the source saying it still has it, so the
	// pass that files one never has to say so a second time.
	seen := now
	written.SeenAt = &seen
	row := &agentDocumentModel{
		ID: written.ID, AgentID: written.AgentID, SourceID: written.SourceID,
		ExternalID: truncateRunes(written.ExternalID, 500), Kind: string(written.Kind),
		Title: truncateRunes(written.Title, 500), URL: written.URL,
		HappenedAt: written.HappenedAt, ModifiedAt: written.ModifiedAt,
		Hash: written.Hash, Bytes: written.Bytes, StorageKey: written.StorageKey,
		Metadata: metadata, Private: written.Private, SeenAt: &seen,
		CreatedAt: written.CreatedAt,
	}
	if existing != nil {
		if err := self.tx.Save(row).Error; err != nil {
			return nil, err
		}
	} else if err := self.tx.Create(row).Error; err != nil {
		return nil, err
	}
	return &written, nil
}

func (self *agentDocumentModel) toModel() (*models.AgentDocument, error) {
	document := &models.AgentDocument{
		ID: self.ID, AgentID: self.AgentID, SourceID: self.SourceID, ExternalID: self.ExternalID,
		Kind: models.AgentDocumentKind(self.Kind), Title: self.Title, URL: self.URL,
		HappenedAt: self.HappenedAt, ModifiedAt: self.ModifiedAt, Hash: self.Hash,
		Bytes: self.Bytes, StorageKey: self.StorageKey, Private: self.Private,
		SeenAt: self.SeenAt, CreatedAt: self.CreatedAt, Metadata: map[string]any{},
	}
	if len(self.Metadata) > 0 {
		if err := json.Unmarshal(self.Metadata, &document.Metadata); err != nil {
			return nil, err
		}
	}
	return document, nil
}

func (self *transaction) documentsFrom(query *gorm.DB) ([]*models.AgentDocument, error) {
	var rows []agentDocumentModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	documents := make([]*models.AgentDocument, 0, len(rows))
	for index := range rows {
		document, err := rows[index].toModel()
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func (self *transaction) GetAgentDocument(agentId, documentId string) (*models.AgentDocument, error) {
	documents, err := self.documentsFrom(self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, documentId).Limit(1))
	if err != nil || len(documents) == 0 {
		return nil, err
	}
	return documents[0], nil
}

func (self *transaction) GetAgentDocuments(agentId string, documentIds []string) ([]*models.AgentDocument, error) {
	if len(documentIds) == 0 {
		return nil, nil
	}
	return self.documentsFrom(self.tx.Where(`"agent_id" = ? AND "id" IN ?`, agentId, documentIds))
}

func (self *transaction) GetAgentDocumentByExternal(sourceId, externalId string) (*models.AgentDocument, error) {
	documents, err := self.documentsFrom(self.tx.Where(`"source_id" = ? AND "external_id" = ?`, sourceId, truncateRunes(externalId, 500)).Limit(1))
	if err != nil || len(documents) == 0 {
		return nil, err
	}
	return documents[0], nil
}

func (self *transaction) DeleteAgentDocument(agentId, documentId string) error {
	return self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, documentId).Delete(&agentDocumentModel{}).Error
}

func (self *transaction) AgentDocumentStorageKey(agentId, hash string) (string, error) {
	if agentId == "" || hash == "" {
		return "", nil
	}
	var keys []string
	if err := self.tx.Raw(`SELECT "storage_key" FROM "agent_document"
		WHERE "agent_id" = ? AND "hash" = ? AND "storage_key" <> '' LIMIT 1`,
		agentId, hash).Scan(&keys).Error; err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", nil
	}
	return keys[0], nil
}

func (self *transaction) ListAgentDocumentHashes(sourceId string) (map[string]string, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		Hash       string `gorm:"column:hash"`
	}
	if err := self.tx.Raw(`SELECT "external_id", "hash" FROM "agent_document" WHERE "source_id" = ?`, sourceId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	hashes := make(map[string]string, len(rows))
	for _, row := range rows {
		hashes[row.ExternalID] = row.Hash
	}
	return hashes, nil
}

// RetitleAgentDocuments writes a new title, and the metadata beside it,
// on documents a source still has with the same text: a conversation
// renamed after it was filed. Only where the title differs, in one
// statement for the page, since nearly every unchanged document is also
// unrenamed. The metadata is merged into what is stored rather than put
// in its place, so what the night wrote there (digested, declined) stays.
func (self *transaction) RetitleAgentDocuments(sourceId string, documents []*models.AgentDocument) (int64, error) {
	if sourceId == "" || len(documents) == 0 {
		return 0, nil
	}
	externalIds := make([]string, 0, len(documents))
	titles := make([]string, 0, len(documents))
	metadatas := make([]string, 0, len(documents))
	for _, document := range documents {
		if document.ExternalID == "" || document.Title == "" {
			continue
		}
		metadata, err := json.Marshal(orEmptyMap(document.Metadata))
		if err != nil {
			return 0, err
		}
		// Cut the same way they were written, or a long name would not
		// match the row it named.
		externalIds = append(externalIds, truncateRunes(document.ExternalID, 500))
		titles = append(titles, truncateRunes(document.Title, 500))
		metadatas = append(metadatas, string(metadata))
	}
	if len(externalIds) == 0 {
		return 0, nil
	}
	result := self.tx.Exec(
		`UPDATE "agent_document" AS "document"
		 SET "title" = "renamed"."title", "metadata" = "document"."metadata" || "renamed"."metadata"::jsonb
		 FROM unnest(?::text[], ?::text[], ?::text[]) AS "renamed"("external_id", "title", "metadata")
		 WHERE "document"."source_id" = ? AND "document"."external_id" = "renamed"."external_id"
		   AND "document"."title" IS DISTINCT FROM "renamed"."title"`,
		pq.Array(externalIds), pq.Array(titles), pq.Array(metadatas), sourceId)
	return result.RowsAffected, result.Error
}

// MarkAgentDocumentsSeen says the source still has these things.
//
// One statement for a whole page of a scan rather than one for each
// name: a page of an archive is two thousand of them, and almost all of
// them are unchanged, which is the case this has to be cheap in.
func (self *transaction) MarkAgentDocumentsSeen(sourceId string, externalIds []string, at time.Time) error {
	if sourceId == "" || len(externalIds) == 0 {
		return nil
	}
	names := make([]string, 0, len(externalIds))
	for _, externalId := range externalIds {
		if externalId == "" {
			continue
		}
		// Cut the same way they were written, or a long name would not
		// match the row it named.
		names = append(names, truncateRunes(externalId, 500))
	}
	if len(names) == 0 {
		return nil
	}
	return self.tx.Exec(
		`UPDATE "agent_document" SET "seen_at" = ? WHERE "source_id" = ? AND "external_id" = ANY(?)`,
		at, sourceId, pq.Array(names)).Error
}

// DeleteAgentDocumentsUnseen removes the documents a pass did not see.
//
// The passages, their vectors and the symbols go with each document:
// every one of those tables references it ON DELETE CASCADE, so this is
// one statement and not four.
//
// Only ever called with the time a pass over the whole tree began, so
// that a document the source no longer reports is the only kind of
// document it can take.
func (self *transaction) DeleteAgentDocumentsUnseen(sourceId string, before time.Time) (int, error) {
	if sourceId == "" || before.IsZero() {
		return 0, nil
	}
	result := self.tx.Exec(`DELETE FROM "agent_document" WHERE "source_id" = ? AND "seen_at" < ?`, sourceId, before)
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (self *transaction) ListAgentDocumentsBetween(agentId string, kinds []models.AgentDocumentKind, from, until time.Time, limit int) ([]*models.AgentDocument, error) {
	if limit <= 0 {
		limit = 2000
	}
	query := self.tx.Where(`"agent_id" = ? AND "happened_at" >= ? AND "happened_at" < ?`, agentId, from, until)
	if len(kinds) > 0 {
		names := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			names = append(names, string(kind))
		}
		query = query.Where(`"kind" IN ?`, names)
	}
	return self.documentsFrom(query.Order(`"happened_at" ASC`).Limit(limit))
}

// --- chunks -----------------------------------------------------------

// ReplaceAgentChunks writes a document's chunks, replacing whatever was
// there. Replacing rather than adding is what makes a second pass over a
// changed file correct: the old slices go, and their vectors with them.
func (self *transaction) ReplaceAgentChunks(document *models.AgentDocument, chunks []*models.AgentChunk) error {
	if document == nil || document.ID == "" {
		return fmt.Errorf("db: chunks need a document")
	}
	if err := self.tx.Where(`"document_id" = ?`, document.ID).Delete(&agentChunkModel{}).Error; err != nil {
		return err
	}
	now := time.Now().Truncate(time.Microsecond)
	for index, chunk := range chunks {
		row := &agentChunkModel{
			ID: newID(), AgentID: document.AgentID, DocumentID: document.ID,
			SourceID: document.SourceID, Number: index + 1,
			Text: chunk.Text, Segmented: chunk.Segmented, CreatedAt: now,
		}
		if err := self.tx.Create(row).Error; err != nil {
			return err
		}
		chunk.ID = row.ID
		chunk.Number = row.Number
		// The full-text column only for text PostgreSQL can segment. A
		// chunk of Chinese would otherwise get one enormous token that
		// matches nothing, at the cost of writing it.
		if chunk.Segmented {
			if err := self.tx.Exec(`UPDATE "agent_chunk" SET "search" = to_tsvector('simple', ?) WHERE "id" = ?`, chunk.Text, row.ID).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (self *transaction) chunksFrom(query *gorm.DB) ([]*models.AgentChunk, error) {
	var rows []agentChunkModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	chunks := make([]*models.AgentChunk, 0, len(rows))
	for index := range rows {
		row := &rows[index]
		chunks = append(chunks, &models.AgentChunk{
			ID: row.ID, AgentID: row.AgentID, DocumentID: row.DocumentID, SourceID: row.SourceID,
			Number: row.Number, Text: row.Text, Segmented: row.Segmented, CreatedAt: row.CreatedAt,
		})
	}
	return chunks, nil
}

func (self *transaction) GetAgentChunks(agentId string, chunkIds []string) ([]*models.AgentChunk, error) {
	if len(chunkIds) == 0 {
		return nil, nil
	}
	return self.chunksFrom(self.tx.Where(`"agent_id" = ? AND "id" IN ?`, agentId, chunkIds))
}

func (self *transaction) ListAgentChunks(agentId, documentId string) ([]*models.AgentChunk, error) {
	return self.chunksFrom(self.tx.Where(`"agent_id" = ? AND "document_id" = ?`, agentId, documentId).Order(`"number" ASC`))
}

// SearchAgentChunks finds chunks by words.
//
// This is also the candidate set the server-side ranking uses where the
// database has no vector index: the words find two thousand, and cosine
// puts them in order. That finds everything the words find, in a better
// order, and misses a paraphrase that shares no word with the question.
func (self *transaction) SearchAgentChunks(agentId string, sourceIds []string, query string, limit int) ([]*models.AgentChunk, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	// Any of the words that carry meaning, best match first: see AnyWord
	// and SearchText for why neither every word nor any word is the rule.
	query = SearchText(query)
	statement := self.tx.Raw(`
		SELECT * FROM "agent_chunk"
		WHERE "agent_id" = ? AND "search" @@ `+AnyWord+`
		  AND (? OR "source_id" = ANY(?))
		ORDER BY ts_rank("search", `+AnyWord+`) DESC LIMIT ?`,
		agentId, query, len(sourceIds) == 0, pq.Array(sourceIds), query, limit)
	return self.chunksFrom(statement)
}

func (self *transaction) ListAgentChunksWithoutVector(agentId, model string, limit int) ([]*models.AgentChunk, error) {
	if limit <= 0 {
		limit = 200
	}
	// By the marker on the passage, not by an anti-join against the
	// vectors: see migration 0074 for what that cost once the corpus was
	// real. A row whose marker is not the model configured now needs a
	// vector -- which covers both "never embedded" and "embedded by a
	// model the operator has since changed".
	// No ORDER BY, on purpose. Every passage gets a vector eventually and
	// nothing reads them in between, so the order they are done in is not
	// a property anybody has. Asking for oldest-first was what forced the
	// whole table to be sorted to return a hundred rows -- the index can
	// answer "which are not done" and stop, and cannot do that and sort
	// by a different column as well.
	return self.chunksFrom(self.tx.Raw(`
		SELECT * FROM "agent_chunk"
		WHERE "agent_id" = ? AND "vector_model" <> ? LIMIT ?`, agentId, model, limit))
}

func (self *transaction) CountAgentChunksWithoutVector(agentId, model string) (int64, error) {
	var counts []int64
	if err := self.tx.Raw(`
		SELECT count(*) FROM "agent_chunk"
		WHERE "agent_id" = ? AND "vector_model" <> ?`, agentId, model).Scan(&counts).Error; err != nil {
		return 0, err
	}
	if len(counts) == 0 {
		return 0, nil
	}
	return counts[0], nil
}

func (self *transaction) PutAgentChunkVector(agentId, sourceId, chunkId, model string, vector []float32) error {
	if agentId == "" || chunkId == "" || model == "" || len(vector) == 0 {
		return fmt.Errorf("db: a chunk's vector needs the agent, the chunk, the model and the vector")
	}
	if err := self.NoteVectorModel(model, len(vector)); err != nil {
		return err
	}
	if err := self.tx.Exec(`
		INSERT INTO "agent_chunk_vector" ("chunk_id", "agent_id", "source_id", "model", "vector", "created_at")
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT ("chunk_id", "model") DO UPDATE SET "vector" = EXCLUDED."vector", "created_at" = EXCLUDED."created_at"`,
		chunkId, agentId, sourceId, model, pq.Float32Array(vector), time.Now()).Error; err != nil {
		return err
	}
	// And the marker beside the text, which is what the "still to do"
	// query reads. Written in the same transaction as the vector, so the
	// two cannot disagree.
	return self.tx.Exec(`UPDATE "agent_chunk" SET "vector_model" = ? WHERE "id" = ?`, model, chunkId).Error
}

// PutAgentChunkVectors writes a batch of vectors in one pass, in a fixed
// order.
//
// The order is the point. Two ingest runs happen at once, each writes a
// hundred vectors in one transaction, and where their batches overlap --
// which they do, because both ask for "chunks with no vector" and get
// overlapping answers -- two transactions taking the same row locks in
// different orders deadlock. PostgreSQL picks one and kills it, the
// batch is lost, and the log says `deadlock detected` with no hint of
// what to do about it. Sorted, the second waits for the first instead.
func (self *transaction) PutAgentChunkVectors(agentId string, vectors []AgentChunkVector) error {
	sorted := append([]AgentChunkVector(nil), vectors...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].ChunkID < sorted[right].ChunkID })
	for _, row := range sorted {
		if err := self.PutAgentChunkVector(agentId, row.SourceID, row.ChunkID, row.Model, row.Vector); err != nil {
			return err
		}
	}
	return nil
}

// AgentChunkVector is one passage's vector, for a batch write.
type AgentChunkVector struct {
	ChunkID  string
	SourceID string
	Model    string
	Vector   []float32
}

// --- symbols ----------------------------------------------------------

func (self *transaction) ReplaceAgentSymbols(agentId, documentId string, symbols []*models.AgentSymbol) error {
	if err := self.tx.Where(`"document_id" = ?`, documentId).Delete(&agentSymbolModel{}).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, symbol := range symbols {
		name := truncateRunes(strings.TrimSpace(symbol.Symbol), 200)
		if name == "" {
			continue
		}
		key := name + ":" + fmt.Sprint(symbol.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := self.tx.Create(&agentSymbolModel{
			AgentID: agentId, DocumentID: documentId, Symbol: name,
			Lowered: strings.ToLower(name), Kind: symbol.Kind, Line: symbol.Line,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// LookupAgentSymbols is an exact match on a name, asked before any vector.
// A log line pasted into a chat names a function, and which file that is
// in is a question a cosine answers badly and a string match answers.
func (self *transaction) LookupAgentSymbols(agentId string, names []string, limit int) ([]*models.AgentSymbol, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	lowered := make([]string, 0, len(names))
	for _, name := range names {
		if trimmed := strings.ToLower(strings.TrimSpace(name)); trimmed != "" {
			lowered = append(lowered, trimmed)
		}
	}
	if len(lowered) == 0 {
		return nil, nil
	}
	var rows []agentSymbolModel
	if err := self.tx.Where(`"agent_id" = ? AND "lowered" IN ?`, agentId, lowered).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	symbols := make([]*models.AgentSymbol, 0, len(rows))
	for _, row := range rows {
		symbols = append(symbols, &models.AgentSymbol{
			AgentID: row.AgentID, DocumentID: row.DocumentID,
			Symbol: row.Symbol, Kind: row.Kind, Line: row.Line,
		})
	}
	return symbols, nil
}

func (self *transaction) CountAgentKnowledge(agentId, sourceId string) (int64, int64, error) {
	var documents, chunks int64
	documentQuery := self.tx.Model(&agentDocumentModel{}).Where(`"agent_id" = ?`, agentId)
	chunkQuery := self.tx.Model(&agentChunkModel{}).Where(`"agent_id" = ?`, agentId)
	if sourceId != "" {
		documentQuery = documentQuery.Where(`"source_id" = ?`, sourceId)
		chunkQuery = chunkQuery.Where(`"source_id" = ?`, sourceId)
	}
	if err := documentQuery.Count(&documents).Error; err != nil {
		return 0, 0, err
	}
	if err := chunkQuery.Count(&chunks).Error; err != nil {
		return 0, 0, err
	}
	return documents, chunks, nil
}

// CountAgentAttachmentsBySource counts what became of each source's files.
//
// The rule is the one CountAgentDocumentsReading uses, so that a card and
// the reading line never disagree: a file nothing has made text of is set
// aside, and which of the two kinds of aside it is depends on whether the
// night has decided about it.
func (self *transaction) CountAgentAttachmentsBySource(agentId string) (map[string]AgentAttachmentCounts, error) {
	var rows []struct {
		SourceID  string
		Undecided int64
		Declined  int64
		Described int64
	}
	if err := self.tx.Raw(`SELECT "source_id" AS source_id,
			count(*) FILTER (WHERE "aside" AND NOT "declined") AS undecided,
			count(*) FILTER (WHERE "aside" AND "declined") AS declined,
			count(*) FILTER (WHERE NOT "aside" AND NOT "declined") AS described
		FROM (
			SELECT d."source_id" AS "source_id",
				jsonb_exists(d."metadata", 'declined') AS "declined",
				NOT EXISTS (
					SELECT 1 FROM "agent_chunk" WHERE "document_id" = d."id") AS "aside"
			FROM "agent_document" d
			WHERE d."agent_id" = ? AND d."kind" = ?) AS "documents"
		GROUP BY "source_id"`,
		agentId, string(models.DocumentAttachment)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]AgentAttachmentCounts, len(rows))
	for _, row := range rows {
		counts[row.SourceID] = AgentAttachmentCounts{
			Undecided: row.Undecided, Declined: row.Declined, Described: row.Described,
		}
	}
	return counts, nil
}

func (self *transaction) ListAgentAttachmentsDeclined(agentId, sourceId string, limit int) ([]*models.AgentDocument, error) {
	if limit <= 0 {
		limit = 100
	}
	query := self.tx.
		Where(`"agent_id" = ? AND "kind" = ?`, agentId, string(models.DocumentAttachment)).
		Where(`jsonb_exists("metadata", 'declined')`)
	if sourceId != "" {
		query = query.Where(`"source_id" = ?`, sourceId)
	}
	return self.documentsFrom(query.Order(`"happened_at" DESC NULLS LAST`).Limit(limit))
}

// LockAgentSource serializes ingestion writes with source edits and removal.
func (self *transaction) LockAgentSource(agentId, sourceId string) (*models.AgentKnowledgeSource, error) {
	sources, err := self.sourcesFrom(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("agent_id = ? AND id = ?", agentId, sourceId).Limit(1))
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	return sources[0], nil
}

// SetAgentSourceUnknownAuthors records observations without changing the source generation.
// The caller holds the source lock and checks that its ingestion generation is current.
func (self *transaction) SetAgentSourceUnknownAuthors(sourceId string, addresses []string) error {
	encodedAddresses, err := json.Marshal(orEmptyStrings(addresses))
	if err != nil {
		return err
	}
	return self.tx.Model(&agentSourceModel{}).Where("id = ?", sourceId).
		Updates(map[string]any{"unknown_authors": encodedAddresses, "modified_at": time.Now()}).Error
}
