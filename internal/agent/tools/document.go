package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// The file behind a document a knowledge source indexed, and the bound on
// showing one to a model.
//
// Both live here rather than in the agent package because two callers need
// them and one of them is a tool. The night reads a picture during a dream
// (internal/agent/dream_attachment.go); the memory tool's `look` reads the
// same picture again when the person asks about it. A tool package imports
// this package and nothing above it, so anything both ends share has to be
// at this level -- the same reason IsImage is here rather than beside the
// conversation's attachments.

// PictureLargest is the largest picture this program sends to a model.
//
// Smaller than the twenty-five megabytes a scan will carry off a person's
// machine, because carrying a file and paying to look at it are different
// questions: a four-thousand-pixel screenshot costs several times what a
// thousand-pixel one does and says the same thing. Anything above this is
// passed over with its reason rather than spent on.
//
// One number for the night and for the conversation on purpose. A look the
// person asked for costs what the night's own look costs, so a picture the
// night was willing to open is one the agent can open again when they ask
// about it, and a picture it refused is refused the same way to their face
// rather than silently costing more.
const PictureLargest = 8 << 20

// DocumentBytes is an attachment document's bytes, out of the store.
//
// The whole reason the bytes are kept rather than fetched when they are
// wanted: this answers with the laptop they came from shut and the person
// asleep, which is when the reading happens.
func DocumentBytes(ctx context.Context, files storage.Files, document *models.AgentDocument) ([]byte, error) {
	if document == nil {
		return nil, errors.New("there is no document to read")
	}
	if document.StorageKey == "" {
		// Not a failure of this call, and said so rather than answered
		// with nothing: a pass that could not reach the computer files
		// the document without a key and the next one fills it in.
		return nil, fmt.Errorf("%s has no stored bytes", document.Cite())
	}
	if files == nil {
		return nil, errors.New("this server has nowhere to keep a file")
	}
	return files.GetFile(ctx, document.StorageKey)
}

// AttachmentPageCharacters is how much of a file's text one view of it holds:
// what a turn shows of a file the person attached, and what one
// read_attachment gives. The rest is read on from next_offset, so a long file
// is read through, never cut.
const AttachmentPageCharacters = 60000

// PageOfText is the part of a text from offset, at most limit characters,
// and where the next part starts, or zero when this is the end. Counted in
// characters, so a part never ends inside one.
func PageOfText(text string, offset, limit int) (string, int) {
	if limit <= 0 {
		limit = AttachmentPageCharacters
	}
	characters := []rune(text)
	if offset < 0 {
		offset = 0
	}
	if offset >= len(characters) {
		return "", 0
	}
	end := offset + limit
	if end >= len(characters) {
		return string(characters[offset:]), 0
	}
	return string(characters[offset:end]), end
}
