package agent

import (
	"context"
	"strings"
	"unicode"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// fileDocument writes one thing and its chunks.
//
// storageKey is where the thing's own bytes are kept, which is empty for
// everything that is only its text: an attachment is the one kind whose
// meaning is in the bytes, and it is the first thing ever to fill the
// column the original design reserved for them.
// takeTheNullsOut strips the one byte a text column will not hold.
//
// A NUL is valid UTF-8, so nothing upstream of the insert objects to it:
// the extraction is clean, the JSON is clean, and then Postgres refuses
// the row with "invalid byte sequence for encoding UTF8: 0x00" and the
// whole document is lost over a byte that says nothing. Two files out of
// a Drive did this, every pass, and would have gone on doing it. What a
// reader is meant to read has no NUL in it, so it goes and the rest of
// the document is kept.
func takeTheNullsOut(entry *computer.ScanEntry) {
	entry.Title = withoutNulls(entry.Title)
	entry.Text = withoutNulls(entry.Text)
	entry.URL = withoutNulls(entry.URL)
	// The metadata is stored as jsonb, which will not hold one either --
	// and it arrives from a script this end did not write.
	for key, value := range entry.Metadata {
		entry.Metadata[key] = withoutNullsIn(value)
	}
}

// withoutNulls is a string a text column will take.
func withoutNulls(text string) string {
	if !strings.Contains(text, "\x00") {
		return text
	}
	return strings.ReplaceAll(text, "\x00", "")
}

// withoutNullsIn is the same for whatever JSON decoded into.
func withoutNullsIn(value any) any {
	switch held := value.(type) {
	case string:
		return withoutNulls(held)
	case []any:
		for index, each := range held {
			held[index] = withoutNullsIn(each)
		}
		return held
	case map[string]any:
		for key, each := range held {
			held[key] = withoutNullsIn(each)
		}
		return held
	}
	return value
}

func (self *Agent) fileDocument(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, entry computer.ScanEntry, storageKey string) (int, error) {
	chunks := chunkText(entry.Text)
	written := 0
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := lockIngestSource(tx, source); err != nil {
			return err
		}
		metadata := entry.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["source"] = source.Name
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: source.AgentID, SourceID: source.ID, ExternalID: entry.ExternalID,
			Kind: documentKindOf(entry.Kind), Title: entry.Title, URL: entry.URL,
			HappenedAt: entry.HappenedAt, ModifiedAt: entry.ModifiedAt,
			Hash: entry.Hash, Bytes: entry.Size, StorageKey: storageKey,
			Metadata: metadata, Private: entry.Private,
		})
		if err != nil {
			return err
		}
		if err := tx.ReplaceAgentChunks(document, chunks); err != nil {
			return err
		}
		written = len(chunks)
		if len(entry.Symbols) > 0 {
			symbols := make([]*models.AgentSymbol, 0, len(entry.Symbols))
			for _, symbol := range entry.Symbols {
				symbols = append(symbols, &models.AgentSymbol{
					AgentID: source.AgentID, DocumentID: document.ID,
					Symbol: symbol.Symbol, Kind: symbol.Kind, Line: symbol.Line,
				})
			}
			if err := tx.ReplaceAgentSymbols(source.AgentID, document.ID, symbols); err != nil {
				return err
			}
		}
		return nil
	})
	return written, err
}

// documentKindOf maps what the device called a thing to what this end
// calls it.
//
// A records script writes whatever it found -- a mail message, a forum
// post -- and those used to fall through to `file`, which is the right
// answer for a thing with no better name and the wrong one for a message.
// The kinds that have a name of their own keep it; the default stands for
// everything else.
func documentKindOf(kind string) models.AgentDocumentKind {
	switch kind {
	case "commit":
		return models.DocumentCommit
	case "chat":
		return models.DocumentChat
	case "journal":
		return models.DocumentJournal
	case "page":
		return models.DocumentPage
	case "message":
		return models.DocumentMessage
	case "post":
		return models.DocumentPost
	case "file":
		return models.DocumentFile
	case computer.KindAttachment:
		return models.DocumentAttachment
	}
	return models.DocumentFile
}

// chunkText cuts a document into slices small enough to rank.
//
// Cut at a blank line where there is one within reach, otherwise at a
// line ending, otherwise at the bound: a slice that begins mid-sentence
// still matches, and one that begins mid-word does not. Each carries the
// tail of the one before it, so a sentence cut in half is whole somewhere.
func chunkText(text string) []*models.AgentChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var chunks []*models.AgentChunk
	runes := []rune(text)
	start := 0
	for start < len(runes) {
		end := start + models.ChunkCharacters
		if end >= len(runes) {
			end = len(runes)
		} else {
			end = cutAt(runes, start, end)
		}
		slice := strings.TrimSpace(string(runes[start:end]))
		if slice != "" {
			chunks = append(chunks, &models.AgentChunk{
				Text:      slice,
				Segmented: segmentable(slice),
			})
		}
		if end >= len(runes) {
			break
		}
		next := end - models.ChunkOverlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

// cutAt is where to end a slice: the last blank line, else the last line
// ending, else where it was going to end anyway.
func cutAt(runes []rune, start, end int) int {
	window := end - start
	floor := start + window/2
	for index := end - 1; index > floor; index-- {
		if runes[index] == '\n' && index > 0 && runes[index-1] == '\n' {
			return index
		}
	}
	for index := end - 1; index > floor; index-- {
		if runes[index] == '\n' {
			return index
		}
	}
	for index := end - 1; index > floor; index-- {
		if unicode.IsSpace(runes[index]) {
			return index
		}
	}
	return end
}

// segmentable says whether PostgreSQL's full-text search can make words
// of this text.
//
// It cannot segment Chinese or Japanese: there are no spaces to split on,
// and the stock server has no dictionary for them. A chunk that is mostly
// those characters would get one enormous token matching nothing, so it
// is left out of the word index and found by meaning alone -- and marked,
// so that an answer citing it can say which way it was found rather than
// leaving somebody to wonder why a search missed it.
func segmentable(text string) bool {
	unsegmented, total := 0, 0
	for _, character := range text {
		if !unicode.IsLetter(character) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, character) ||
			unicode.Is(unicode.Hiragana, character) ||
			unicode.Is(unicode.Katakana, character) {
			unsegmented++
		}
	}
	if total == 0 {
		return true
	}
	return float64(unsegmented)/float64(total) < 0.3
}
