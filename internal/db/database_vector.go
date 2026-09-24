package db

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Ranking by meaning, in the database where it can be indexed and in the
// server where it cannot.
//
// Every vector is stored the same way wherever it lives: a real[] column
// beside the name of the model that wrote it. That choice was made so a
// deployment running the stock PostgreSQL image loses nothing
// (docs/decisions/20260910-embeddings-without-pgvector.md), and it is kept
// here: the pgvector index is an *expression* index over the same column,
// so the rows one path writes are the rows the other path reads.
//
// Which path a deployment gets is settled once, at open, by asking whether
// the extension is there. Nothing above this file knows the difference.

// VectorTable says where vectors of one kind live and how the server-side
// path should pick its candidates when there is no index to ask.
type VectorTable struct {
	// Table is the table, IDColumn the column naming what the vector is
	// about, and ScopeColumn the column a search narrows by -- the agent
	// whose memory it is, the mailbox the message is in.
	Table       string
	IDColumn    string
	ScopeColumn string

	// OrderColumn is what "the newest" means for this table, which is how
	// the server-side path chooses the bounded set it ranks. Empty means
	// the rows come in whatever order the table gives them, which is only
	// right for a table small enough to read whole.
	OrderColumn string

	// Candidates is how many rows the server-side path ranks. Ignored
	// where the database ranks for us.
	Candidates int
}

// The tables that hold vectors. Each carries its own scope column so a
// search is one indexed read rather than a join.
var (
	MailEmbeddingTable = VectorTable{Table: "mail_embedding", IDColumn: "mail_id", ScopeColumn: "mailbox_id", OrderColumn: "created_at", Candidates: 3000}
	AgentMemoryTable   = VectorTable{Table: "agent_memory", IDColumn: "id", ScopeColumn: "agent_id", OrderColumn: "modified_at", Candidates: 1000}
	AgentNodeTable     = VectorTable{Table: "agent_node_vector", IDColumn: "node_id", ScopeColumn: "agent_id", OrderColumn: "created_at", Candidates: 2000}
	AgentFactTable     = VectorTable{Table: "agent_fact_vector", IDColumn: "fact_id", ScopeColumn: "agent_id", OrderColumn: "created_at", Candidates: 3000}
	AgentChunkTable    = VectorTable{Table: "agent_chunk_vector", IDColumn: "chunk_id", ScopeColumn: "agent_id", OrderColumn: "created_at", Candidates: 4000}
)

// Scored is one row and how near it was, best first. The score is cosine
// similarity: one is the same direction, zero unrelated.
type Scored struct {
	ID    string
	Score float64
}

// VectorQuery narrows a search beyond the scope: the source a chunk must
// belong to, the node a fact must be on. Each clause is SQL with a `?`
// per argument, joined with AND, and is written here rather than passed in
// from a caller outside this package.
type VectorQuery struct {
	Where     []string
	Arguments []any

	// Floor is the least similarity worth answering with. Below it the
	// honest answer is that nothing here is about this.
	Floor float64
}

// VectorOperation ranks stored vectors against a query vector.
type VectorOperation interface {
	// Nearest is the rows of a table nearest the query, best first, above
	// the floor. Where the extension is present this is one indexed query;
	// where it is not, it is a bounded read and cosine in the server.
	Nearest(table VectorTable, scope, model string, query []float32, limit int, narrow VectorQuery) ([]Scored, error)

	// NoteVectorModel records the width a model writes, the first time one
	// is seen. The server reads these at start to build one index per
	// model, since the dimension is part of the index and differs.
	NoteVectorModel(model string, dimension int) error
	ListVectorModels() (map[string]int, error)
}

// VectorIndexing says whether this database ranks vectors itself. False
// means every search reads a bounded candidate set and ranks in the
// server, which is right for a personal mailbox and wrong for a corpus.
func (self *database) VectorIndexing() bool {
	self.vectorOnce.Do(func() {
		var present []int
		if err := self.db.Raw(`SELECT 1 FROM pg_extension WHERE extname = 'vector'`).Scan(&present).Error; err != nil {
			log.Warningf("cannot ask whether vector indexing is available: %s", err)
			return
		}
		if len(present) > 0 {
			self.vectorIndexing = true
			log.Noticef("vector indexing: on")
			return
		}
		// Not there yet. Creating it needs a privilege an operator's own
		// database may not give us, and a database that refuses is not a
		// database in trouble: it is one that ranks in the server.
		if err := self.db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
			log.Noticef("vector indexing: off, ranking in the server (%s)", err)
			return
		}
		self.vectorIndexing = true
		log.Noticef("vector indexing: on, extension created")
	})
	return self.vectorIndexing
}

// EnsureVectorIndex builds the index one table needs for one model, and is
// idempotent. Nothing happens without the extension.
//
// The index is an expression index over the real[] column with the model
// as its predicate, which is what lets the column stay an ordinary array
// that the server-side path can still read. It is built *before* the rows
// arrive wherever possible: maintaining it on insert costs a tenth of a
// millisecond a row, where building it afterwards over a corpus wants more
// memory than a small server has.
func (self *database) EnsureVectorIndex(table VectorTable, model string, dimension int) error {
	if !self.VectorIndexing() {
		return nil
	}
	if dimension <= 0 || dimension > 16000 {
		return fmt.Errorf("db: %q writes vectors of %d, which is not a width an index can be built at", model, dimension)
	}
	// An index that is this one under another name serves as well. The
	// name changed once, and every server that already had its indexes
	// went on to build each a second time at start: over a large corpus
	// that is gigabytes of index nobody needed, with writes to the table
	// held for as long as it takes, or a build that fails for want of
	// shared memory at every start.
	isBuilt, err := self.hasVectorIndex(table, model, dimension)
	if err != nil {
		return err
	}
	if isBuilt {
		return nil
	}
	name := vectorIndexName(table, model, dimension)
	statement := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw ((%s::vector(%d)) vector_cosine_ops) WHERE %s = %s`,
		pq.QuoteIdentifier(name), pq.QuoteIdentifier(table.Table),
		pq.QuoteIdentifier("vector"), dimension,
		pq.QuoteIdentifier("model"), pq.QuoteLiteral(model))
	started := time.Now()
	if err := self.db.Exec(statement).Error; err != nil {
		return fmt.Errorf("db: cannot build the vector index for %s on %s: %w", model, table.Table, err)
	}
	if taken := time.Since(started); taken > time.Second {
		log.Noticef("built the vector index for %s on %s in %s", model, table.Table, taken.Round(time.Millisecond))
	}
	return nil
}

// hasVectorIndex says whether a valid HNSW index for this table, model and
// width exists under any name. Compared by what the index is -- its
// width and the model its predicate names, as PostgreSQL writes the
// definition back -- never by its name, which is what let two model names
// that differ only in punctuation share one index before.
func (self *database) hasVectorIndex(table VectorTable, model string, dimension int) (bool, error) {
	var definitions []string
	if err := self.db.Raw(`SELECT pg_get_indexdef(index.indexrelid) FROM pg_index index
		JOIN pg_class indexed ON indexed.oid = index.indrelid
		WHERE indexed.relname = ? AND index.indisvalid AND pg_get_indexdef(index.indexrelid) LIKE '%USING hnsw%'`, table.Table).
		Scan(&definitions).Error; err != nil {
		return false, fmt.Errorf("db: cannot list the vector indexes on %s: %w", table.Table, err)
	}
	width := fmt.Sprintf("::vector(%d))", dimension)
	predicate := fmt.Sprintf("WHERE ((model)::text = %s::text)", pq.QuoteLiteral(model))
	for _, definition := range definitions {
		if strings.Contains(definition, width) && strings.HasSuffix(definition, predicate) {
			return true, nil
		}
	}
	return false, nil
}

// vectorIndexName includes the full identity in its digest because readable
// names can collide after punctuation replacement or PostgreSQL's truncation.
// Existing indexes are retained until an operator retires the older binary.
func vectorIndexName(table VectorTable, model string, dimension int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", table.Table, model, dimension)))
	prefix := table.Table + "_v_"
	if len(prefix) > 30 {
		prefix = prefix[:30]
	}
	return fmt.Sprintf("%s%x", prefix, digest[:16])
}

// Nearest ranks a table's vectors against the query.
func (self *transaction) Nearest(table VectorTable, scope, model string, query []float32, limit int, narrow VectorQuery) ([]Scored, error) {
	if scope == "" || model == "" || len(query) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if self.database != nil && self.database.VectorIndexing() {
		return self.nearestIndexed(table, scope, model, query, limit, narrow)
	}
	return self.nearestInServer(table, scope, model, query, limit, narrow)
}

// nearestIndexed asks the database, which walks the HNSW graph.
//
// Two details matter. The ORDER BY expression has to be written exactly as
// the index was declared -- the cast included -- or the planner will not
// use it. And a narrow search (one source out of two hundred) can walk the
// graph and come back with fewer rows than were asked for, because the
// filter is applied after; `hnsw.iterative_scan` makes the walk continue
// until the limit is met, which is what "relaxed_order" buys.
func (self *transaction) nearestIndexed(table VectorTable, scope, model string, query []float32, limit int, narrow VectorQuery) ([]Scored, error) {
	if err := self.tx.Exec(`SET LOCAL hnsw.iterative_scan = relaxed_order`).Error; err != nil {
		// An older pgvector has no such setting. The search still works;
		// a very narrow one may answer with less than it could.
		log.Debugf("no iterative vector scan on this database: %s", err)
	}
	literal := vectorLiteral(query)
	cast := fmt.Sprintf("%s::vector(%d)", pq.QuoteIdentifier("vector"), len(query))
	where := []string{
		pq.QuoteIdentifier(table.ScopeColumn) + " = ?",
		pq.QuoteIdentifier("model") + " = ?",
	}
	arguments := []any{scope, model}
	where = append(where, narrow.Where...)
	arguments = append(arguments, narrow.Arguments...)

	statement := fmt.Sprintf(
		`SELECT %s AS id, 1 - (%s <=> ?::vector(%d)) AS score FROM %s WHERE %s ORDER BY %s <=> ?::vector(%d) LIMIT ?`,
		pq.QuoteIdentifier(table.IDColumn), cast, len(query),
		pq.QuoteIdentifier(table.Table), strings.Join(where, " AND "),
		cast, len(query))

	// The query vector appears twice, once in the score and once in the
	// ordering; both must be the same literal or the planner sees two
	// expressions.
	all := append([]any{literal}, arguments...)
	all = append(all, literal, limit)

	var rows []Scored
	if err := self.tx.Raw(statement, all...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("db: ranking %s by meaning: %w", table.Table, err)
	}
	kept := rows[:0]
	for _, row := range rows {
		if row.Score >= narrow.Floor {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

// nearestInServer reads a bounded set and ranks it here. It is what a
// database without the extension does, and what every deployment did
// before there was one.
func (self *transaction) nearestInServer(table VectorTable, scope, model string, query []float32, limit int, narrow VectorQuery) ([]Scored, error) {
	where := []string{
		pq.QuoteIdentifier(table.ScopeColumn) + " = ?",
		pq.QuoteIdentifier("model") + " = ?",
		pq.QuoteIdentifier("vector") + " IS NOT NULL",
	}
	arguments := []any{scope, model}
	where = append(where, narrow.Where...)
	arguments = append(arguments, narrow.Arguments...)

	candidates := table.Candidates
	if candidates <= 0 {
		candidates = 2000
	}
	order := ""
	if table.OrderColumn != "" {
		order = " ORDER BY " + pq.QuoteIdentifier(table.OrderColumn) + " DESC"
	}
	statement := fmt.Sprintf(`SELECT %s AS id, %s AS vector FROM %s WHERE %s%s LIMIT ?`,
		pq.QuoteIdentifier(table.IDColumn), pq.QuoteIdentifier("vector"),
		pq.QuoteIdentifier(table.Table), strings.Join(where, " AND "), order)
	arguments = append(arguments, candidates)

	var rows []scannedVector
	if err := self.tx.Raw(statement, arguments...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("db: reading %s to rank by meaning: %w", table.Table, err)
	}
	queryNorm := VectorNorm(query)
	if queryNorm == 0 {
		return nil, nil
	}
	scored := make([]Scored, 0, len(rows))
	for _, row := range rows {
		vector := []float32(row.Vector)
		if len(vector) != len(query) {
			continue
		}
		score := CosineSimilarity(query, vector)
		if score >= narrow.Floor {
			scored = append(scored, Scored{ID: row.ID, Score: score})
		}
	}
	sort.SliceStable(scored, func(left, right int) bool { return scored[left].Score > scored[right].Score })
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

// scannedVector is one row the server-side path reads. A named type with
// its columns spelled out, because the mapper fills an array field from a
// named struct and leaves an anonymous one empty without saying so.
type scannedVector struct {
	ID     string          `gorm:"column:id"`
	Vector pq.Float32Array `gorm:"column:vector;type:real[]"`
}

// vectorLiteral is a vector as pgvector reads it: "[1,2,3]". Written by
// hand rather than through a driver type, so that this package keeps its
// one dependency on the extension in this file.
func vectorLiteral(vector []float32) string {
	var builder strings.Builder
	builder.Grow(len(vector) * 8)
	builder.WriteByte('[')
	for index, value := range vector {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String()
}

// CosineSimilarity is the cosine between two vectors, and zero where they
// cannot be compared. One cosine, used by both paths, so a database with
// the extension and one without answer the same question the same way.
func CosineSimilarity(left, right []float32) float64 {
	if len(left) != len(right) {
		return 0
	}
	leftNorm, rightNorm := VectorNorm(left), VectorNorm(right)
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	var dot float64
	for index := range left {
		dot += float64(left[index]) * float64(right[index])
	}
	return dot / (leftNorm * rightNorm)
}

// VectorNorm is a vector's length.
func VectorNorm(vector []float32) float64 {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	return math.Sqrt(sum)
}

type vectorModelModel struct {
	Model     string    `gorm:"column:model;primaryKey"`
	Dimension int       `gorm:"column:dimension"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (vectorModelModel) TableName() string { return "vector_model" }

// NoteVectorModel records what width a model writes. The first write wins:
// a model that changed its width changed its name too, because the width
// is part of the name wherever it was asked for.
func (self *transaction) NoteVectorModel(model string, dimension int) error {
	if model == "" || dimension <= 0 {
		return fmt.Errorf("db: a vector model needs a name and a width")
	}
	return self.tx.Exec(
		`INSERT INTO "vector_model" ("model", "dimension", "created_at") VALUES (?, ?, ?) ON CONFLICT ("model") DO NOTHING`,
		model, dimension, time.Now()).Error
}

// ListVectorModels is every model that has written a vector, and its
// width: what the server needs to build one index per model at start.
func (self *transaction) ListVectorModels() (map[string]int, error) {
	var rows []vectorModelModel
	if err := self.tx.Find(&rows).Error; err != nil {
		return nil, err
	}
	widths := make(map[string]int, len(rows))
	for _, row := range rows {
		widths[row.Model] = row.Dimension
	}
	return widths, nil
}
