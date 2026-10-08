package mcpserve

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/mcp"
)

// ResultCharacters is how much of a tool's text one answer carries, in
// bytes. Past it the rest is held here and read with result_more.
//
// A client cuts a long answer on its own, somewhere past twelve thousand
// characters and without saying where, and the model on the other end then
// tells its person the list was truncated with no way to read on. Paging
// below that, and saying how to read on, is what keeps the whole answer
// reachable.
const ResultCharacters = 8000

// The bounds of what is held for result_more. A result is held for a
// caller, not for a session: the HTTP transport keeps none, and the same
// caller's next request is what reads on.
const (
	// resultsPerHolder is how many results one caller has held at once;
	// the oldest goes when another comes.
	resultsPerHolder = 8
	// resultHeldFor is how long a result is held after it was made.
	resultHeldFor = 30 * time.Minute
	// heldBytes bounds everything held, for every caller together.
	heldBytes = 64 << 20
	// moreLineReserve is the room a page keeps for the line after it that
	// says how to read on.
	moreLineReserve = 200
)

// resultMoreName is the tool that reads on.
const resultMoreName = "result_more"

// ResultStore holds the long results of tool calls so that the rest of one
// can be read a page at a time. One is shared by every request a process
// answers; it is memory only, and a restart forgets what it held.
type ResultStore struct {
	mutex      sync.Mutex
	held       map[string]*heldResult
	totalBytes int
	now        func() time.Time
}

// heldResult is one tool's whole text, and whose it is.
type heldResult struct {
	holder   string
	text     string
	storedAt time.Time
}

// NewResultStore is an empty store.
func NewResultStore() *ResultStore {
	return &ResultStore{held: map[string]*heldResult{}, now: time.Now}
}

// keep holds text for holder and returns the id it is read back by, or ""
// when it is larger than everything the store may hold.
func (self *ResultStore) keep(holder, text string) string {
	if len(text) > heldBytes {
		return ""
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.forgetExpired()
	for self.countHeldBy(holder) >= resultsPerHolder {
		self.forgetOldest(holder)
	}
	for self.totalBytes+len(text) > heldBytes && len(self.held) > 0 {
		self.forgetOldest("")
	}
	id := newResultId()
	self.held[id] = &heldResult{holder: holder, text: text, storedAt: self.now()}
	self.totalBytes += len(text)
	return id
}

// find is the text held under id for holder. Another caller's id is not
// found, the same as one that never existed.
func (self *ResultStore) find(holder, id string) (string, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.forgetExpired()
	result, ok := self.held[id]
	if !ok || result.holder != holder {
		return "", false
	}
	return result.text, true
}

func (self *ResultStore) forgetExpired() {
	cutoff := self.now().Add(-resultHeldFor)
	for id, result := range self.held {
		if result.storedAt.Before(cutoff) {
			self.forget(id)
		}
	}
}

func (self *ResultStore) countHeldBy(holder string) int {
	heldCount := 0
	for _, result := range self.held {
		if result.holder == holder {
			heldCount++
		}
	}
	return heldCount
}

// forgetOldest forgets holder's oldest result, or the oldest of anybody's
// when holder is empty.
func (self *ResultStore) forgetOldest(holder string) {
	oldestId := ""
	var oldest *heldResult
	for id, result := range self.held {
		if holder != "" && result.holder != holder {
			continue
		}
		if oldest == nil || result.storedAt.Before(oldest.storedAt) {
			oldestId, oldest = id, result
		}
	}
	if oldest != nil {
		self.forget(oldestId)
	}
}

func (self *ResultStore) forget(id string) {
	if result, ok := self.held[id]; ok {
		self.totalBytes -= len(result.text)
		delete(self.held, id)
	}
}

// newResultId is an id nobody can guess, so that one caller cannot read
// on in another's result even by trying ids.
func newResultId() string {
	random := make([]byte, 12)
	_, _ = rand.Read(random)
	return hex.EncodeToString(random)
}

// pageEnd is where a page of text that starts at offset ends: within
// budget bytes, never inside a character, and after a line break when one
// falls in the second half of the page.
func pageEnd(text string, offset, budget int) int {
	end := offset + budget
	if end >= len(text) {
		return len(text)
	}
	for end > offset && !utf8.RuneStart(text[end]) {
		end--
	}
	if halfway := offset + budget/2; halfway < end {
		if lineBreak := strings.LastIndexByte(text[halfway:end], '\n'); lineBreak >= 0 {
			return halfway + lineBreak + 1
		}
	}
	if end == offset {
		// A budget smaller than one character still moves on by one.
		_, size := utf8.DecodeRuneInString(text[offset:])
		return offset + size
	}
	return end
}

// page is the part of a result from offset that fits, and the line after
// it saying how to read on when there is more.
func (self *Server) page(id, text string, offset int) string {
	end := pageEnd(text, offset, self.resultCharacters-moreLineReserve)
	if end >= len(text) {
		return text[offset:]
	}
	left := len(text) - end
	if id == "" {
		return fmt.Sprintf("%s\n[%d more characters, too many for this server to hold for later: narrow the call to read them]", text[offset:end], left)
	}
	return fmt.Sprintf("%s\n[%d more characters: call %s with result_id %q and offset %d]", text[offset:end], left, resultMoreName, id, end)
}

// paged is a tool's text as one answer carries it: whole when it fits,
// otherwise its first page, with the rest held for result_more.
func (self *Server) paged(text string) string {
	if len(text) <= self.resultCharacters {
		return text
	}
	return self.page(self.results.keep(self.holder, text), text, 0)
}

// resultMoreTool is how result_more is listed.
func resultMoreTool() mcp.Tool {
	return mcp.Tool{
		Name: resultMoreName,
		Description: "Read on in a tool's result that was too long for one answer. " +
			"A long result ends with a line saying how many more characters there are, " +
			"and the result_id and offset to call this with; each call returns the next part the same way. " +
			"Results are held for 30 minutes.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"result_id": map[string]any{"type": "string", "description": "the id the long result named"},
				"offset":    map[string]any{"type": "integer", "description": "where to read from, as the long result named it"},
			},
			"required": []string{"result_id", "offset"},
		},
		Annotations: &mcp.ToolAnnotations{Title: "Read on in a long result", ReadOnlyHint: true, IdempotentHint: true},
	}
}

// resultMore reads on in a held result.
func (self *Server) resultMore(arguments []byte) (string, error) {
	var parameters struct {
		ResultID string `json:"result_id"`
		Offset   int    `json:"offset"`
	}
	if err := json.Unmarshal(arguments, &parameters); err != nil {
		return "", fmt.Errorf("%s takes result_id and offset", resultMoreName)
	}
	resultId := strings.TrimSpace(parameters.ResultID)
	text, ok := self.results.find(self.holder, resultId)
	if !ok {
		return "", fmt.Errorf("no result %q is held: results are held for 30 minutes, a few at a time, "+
			"and not across a restart; make the original call again", resultId)
	}
	if parameters.Offset < 0 || parameters.Offset > len(text) {
		return "", fmt.Errorf("offset %d is outside the result, which is %d characters long", parameters.Offset, len(text))
	}
	if parameters.Offset == len(text) {
		return fmt.Sprintf("nothing more: the result ends at offset %d", len(text)), nil
	}
	offset := parameters.Offset
	for offset > 0 && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return self.page(resultId, text, offset), nil
}
