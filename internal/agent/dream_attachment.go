package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// What the night does about the pictures and files a record came with.
//
// A scan keeps an attachment's bytes and gives it no text, so nothing can
// read it and the reading is never offered it -- see the eligible clause
// in database_dream.go, which is there so that a night handed an empty
// document cannot learn nothing from it and mark it read. This is the
// phase that gives such a document its text, in two steps.
//
// The first step is a judgement made for nothing. On the archive this was
// written for there are fifty-three thousand of these files, forty-seven
// thousand of them pictures, and opening every one costs real money. So
// the model is shown what is free to know -- the name, the size, the kind
// of file, the channel and thread, and the words of the message the file
// arrived with -- and asked which are worth opening. A screenshot in a
// thread about something going wrong is; an avatar, a logo, a signature
// image or a meme in a social channel is not.
//
// The second step opens the ones it chose: the bytes come out of the
// store, go to the scan model as a picture, and what comes back becomes
// the document's text through the ordinary path. From there chunking, the
// vectors, the reading that turns documents into facts and the evidence a
// fact quotes all work unchanged, which is the whole reason the
// description is filed as text rather than kept somewhere of its own.
//
// Nothing here deletes anything, and nothing is marked read that was not.
// A file the night decided against keeps its bytes and carries the reason
// on its row, so a person who disagrees can read why and put it back.

// The bounds.
const (
	// attachmentBatch is how many files one call decides about. Larger
	// than a batch of documents because each is one line rather than
	// twelve hundred runes of an opening, and the decision is worth
	// making over a spread wide enough to compare within.
	attachmentBatch = 40

	// attachmentsANight is how many files one night decides about and
	// picturesANight how many of those it opens. Pacing, like every other
	// bound in a night: what is not decided tonight is decided tomorrow,
	// and the allowance and the clock stop it well before these do on any
	// night that has other work.
	attachmentsANight = 400
	picturesANight    = 100

	// pictureLargest is the largest picture the night sends to a model.
	// The same number the memory tool's `look` holds the person's own
	// question to, so that what the night would not open is not opened
	// for them either; tools.PictureLargest says why.
	pictureLargest = tools.PictureLargest
)

// attachmentsMost is how many of a batch of this size the night will
// open. A quarter, because the archive holds far more pictures than
// there is money to look at them, and a cap makes the decision a
// comparative one: the model has to rank what it was shown and name only
// the best of it rather than everything it could argue for.
//
// One function so that the number the prompt is rendered with and the
// number the answer is measured against cannot drift apart. They must
// agree, because a list that came back full means something different
// from one that did not -- see declinedWhenFull.
func attachmentsMost(count int) int { return max(1, count/4) }

// declinedByDefault is what is recorded against a file the model was
// shown, had room to choose, and did not choose. It is written in the
// person's words rather than the model's because the model said nothing
// about this one; what it said was which others it wanted.
const declinedByDefault = "the agent looked at its name, size, kind and the words it came with, and judged it not worth opening"

// declinedWhenFull is what is recorded instead against a file left over
// from a batch whose list came back full. The model named as many files
// as it was allowed to name, so this one was never weighed against the
// ones it named, and declinedByDefault would tell a person that a
// judgement was made about it that was not. All this row can honestly
// say is how many the night could take and that this was not among them.
func declinedWhenFull(count int) string {
	return fmt.Sprintf("the agent could choose at most %d of the %d files it was shown at once, "+
		"the %d it chose were others, and this one was passed over rather than judged",
		attachmentsMost(count), count, attachmentsMost(count))
}

// dreamAttachments decides which of the files a record came with are
// worth opening, and opens them.
//
// It counts nothing onto the night's row. What it opens is not read yet
// -- it has passages now, and the reading comes for it like any other
// document -- so counting it here would say twice that one thing was
// read. What it did is on its own runs, which the person can open.
func (self *Agent) dreamAttachments(ctx context.Context, run *Run, budget *dreamBudget) {
	if ctx.Err() != nil || !budget.left() {
		return
	}
	most, pictures := attachmentsANight, picturesANight
	if run.Agent.DreamBootstrap {
		most, pictures = most*2, pictures*2
	}
	var waiting []*models.AgentDocument
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		waiting, err = tx.ListAgentAttachmentsToDecide(run.Agent.ID, most)
		return err
	}); err != nil {
		log.Warningf("cannot list the files waiting for a decision: %s", err)
		return
	}
	if len(waiting) == 0 {
		return
	}

	// What no model here could look at is settled without asking one. A
	// video, an archive or a spreadsheet is not a picture, and buying a
	// judgement about whether it is worth opening something that cannot
	// be opened is the one kind of call that can never pay for itself.
	// The reason is recorded exactly as the model's own declines are, so
	// the row says why either way and a later build that can read such a
	// file can go back over them.
	var considered []*models.AgentDocument
	refusals := map[string][]string{}
	for _, document := range waiting {
		if reason := unopenable(document); reason != "" {
			refusals[reason] = append(refusals[reason], document.ID)
			continue
		}
		considered = append(considered, document)
	}
	for reason, documentIds := range refusals {
		declineAttachments(ctx, run, documentIds, reason)
	}

	opened := 0
	for start := 0; start < len(considered); {
		if ctx.Err() != nil || !budget.left() {
			return
		}
		end := min(start+attachmentBatch, len(considered))
		batch := considered[start:end]
		start = end

		chosen, answered := self.chooseAttachments(ctx, run, batch, budget)
		if !answered {
			// A model that did not answer has decided nothing, so nothing
			// is declined and nothing is opened: the same files are put
			// to a night that does answer. The phase stops at the first
			// silence rather than going on through the batches, because
			// every one of them would cost the same and fail the same
			// way.
			return
		}
		var declined []string
		for _, document := range batch {
			why, wanted := chosen[document.ID]
			if !wanted {
				declined = append(declined, document.ID)
				continue
			}
			// Chosen, with nothing left to open it with: left exactly as
			// it was, neither declined nor read. Tomorrow decides about
			// it again, which costs the decision twice and is the right
			// way round -- the alternative marks a file the night meant
			// to open as one it passed over.
			if opened >= pictures || ctx.Err() != nil || !budget.left() {
				continue
			}
			if self.readPicture(ctx, run, budget, document, why) {
				opened++
			}
		}
		// A list that came back full is not the same answer as a list
		// with room left in it. When the model named as many as it was
		// allowed to, the rest of the batch never got a judgement, so the
		// row says that instead of claiming one. They are declined either
		// way, deliberately: ListAgentAttachmentsToDecide orders by
		// happened_at and passes over what is already declined, so a file
		// left undecided would be put to every night forever and the ones
		// behind it would never be reached at all.
		reason := declinedByDefault
		if len(chosen) >= attachmentsMost(len(batch)) {
			reason = declinedWhenFull(len(batch))
		}
		declineAttachments(ctx, run, declined, reason)
	}
}

// emptyFileBytes is below the size of any file with something in it: a
// blank line, a byte order mark.
const emptyFileBytes = 8

// unopenable is why nothing here can open this file, and "" for a picture
// the night could put to a model.
//
// Decided from the content type the scan recorded rather than from the
// bytes, which is the point: this runs before anything is fetched, and a
// file that is not a picture is never carried out of the store at all.
func unopenable(document *models.AgentDocument) string {
	contentType := document.ContentType()
	if contentType == "" {
		return "nothing said what kind of file it is, and only a picture can be read here"
	}
	if !IsImageAttachment(contentType) {
		// A text file is read as text where it arrives; one that reached
		// the night had nothing to read in it, which is the reason to
		// give, not that it is not a picture.
		if document.Bytes < emptyFileBytes {
			return "it is empty, so there is nothing in it to read"
		}
		if strings.HasPrefix(contentType, "text/") {
			return fmt.Sprintf("a %s file, and no words could be read out of it", contentType)
		}
		return fmt.Sprintf("a %s is not a picture, and only a picture can be read here", contentType)
	}
	if document.Bytes > pictureLargest {
		return fmt.Sprintf("%s, larger than the %s a picture may be to be worth sending to a model",
			formatBytes(document.Bytes), formatBytes(pictureLargest))
	}
	return ""
}

// chooseAttachments asks which of a batch are worth opening, and answers
// with the reason given for each, and whether the model answered at all.
func (self *Agent) chooseAttachments(ctx context.Context, run *Run, documents []*models.AgentDocument, budget *dreamBudget) (map[string]string, bool) {
	// A decision model first, where one is configured: this is a question
	// with two answers about a line of text, which is the whole of what
	// such a model does, and it answers in well under a second without
	// spending a model's attention or writing a word. It says so when it
	// cannot, and then a model is asked exactly as before.
	if chosen, answered := self.decideAttachments(ctx, documents); answered {
		return chosen, true
	}

	var builder strings.Builder
	for _, document := range documents {
		builder.WriteString("[" + document.ID + "] " + attachmentLine(document) + "\n")
	}
	prompt, err := render("attachments.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Count":      len(documents),
		"Most":       attachmentsMost(len(documents)),
		"Items":      builder.String(),
	})
	if err != nil {
		return nil, false
	}
	thinking, err := self.dreamThought(ctx, run,
		budget, fmt.Sprintf("Looked at %d files", len(documents)), prompt, false)
	if err != nil {
		log.Warningf("a dream could not ask which files are worth opening: %s", err)
		return nil, false
	}
	// Answered in words rather than with the object, as the reading is:
	// the words hold the judgement, so they go to one more call with no
	// tools that has only to write it down.
	extracted, err := llm.ExtractJSON(thinking.Text)
	if err != nil && len(thinking.Text) > 200 && !textualToolCall(thinking.Text) {
		extracted, err = self.attachmentObjectFromWords(ctx, run, budget, thinking.Text)
	}
	if err != nil {
		self.retitle(ctx, run, thinking.Conversation,
			fmt.Sprintf("Looked at %d files, and answered with no object", len(documents)))
		return nil, false
	}
	// A pointer, so that a list which is there and empty can be told from
	// one that is not there at all.
	//
	// They are opposite answers and they read the same to a decoder. An
	// object of {"open":[]} is the night having weighed the batch and
	// wanted none of it; {} or {"error":"..."} is the night having said
	// nothing about it. Both unmarshal without complaint into an empty
	// slice, so both used to come back as a decision -- and the caller
	// declines what was not chosen, and a declined file is passed over by
	// every night afterwards. A model that answered with an error object
	// could therefore retire a whole batch from the reading queue for
	// good, which is the one outcome nothing here is allowed to do by
	// accident.
	var answer struct {
		Open *[]struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"open"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil || answer.Open == nil {
		self.retitle(ctx, run, thinking.Conversation,
			fmt.Sprintf("Looked at %d files, and answered with no object", len(documents)))
		return nil, false
	}
	// Only identifiers that were in the batch. A model that answers with
	// one it invented, or with one from the batch before, would otherwise
	// have a file opened that nobody was shown.
	shown := make(map[string]bool, len(documents))
	for _, document := range documents {
		shown[document.ID] = true
	}
	// And no more than it was allowed to ask for. The bound was in the
	// prompt and in the sentence written against the files left over, and
	// nowhere in the code: a model naming the whole batch had the whole
	// batch opened, at a vision call each.
	most := attachmentsMost(len(documents))
	chosen := map[string]string{}
	for _, wanted := range *answer.Open {
		id := strings.TrimSpace(wanted.ID)
		if !shown[id] || len(chosen) >= most {
			continue
		}
		chosen[id] = strings.TrimSpace(wanted.Reason)
	}
	self.retitle(ctx, run, thinking.Conversation,
		fmt.Sprintf("Looked at %d files and chose %d to open", len(documents), len(chosen)))
	return chosen, true
}

// attachmentObjectFromWords asks once more, with no tools, for the object
// a judgement given in words should have ended with.
func (self *Agent) attachmentObjectFromWords(ctx context.Context, run *Run, budget *dreamBudget, words string) (string, error) {
	prompt, err := render("attachments_object.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Reading":    cutRunes(words, 12000),
	})
	if err != nil {
		return "", err
	}
	thinking, err := self.dreamThought(ctx, run, budget,
		"Wrote the object for a choice given in words", prompt, false)
	if err != nil {
		return "", err
	}
	return llm.ExtractJSON(thinking.Text)
}

// readPicture sends one picture to the model and makes what it says the
// document's text. It answers whether the document ended with passages.
//
// Every way of failing leaves the document exactly as it was: no text, no
// decline, nothing marked read. The bytes may be there tomorrow, the
// model may answer tomorrow, and a file the night meant to open is worth
// coming back to. Marking it read on a failure is the one outcome that
// cannot be undone by trying again, because nothing ever comes back for a
// document that says it was read.
func (self *Agent) readPicture(ctx context.Context, run *Run, budget *dreamBudget,
	document *models.AgentDocument, why string) bool {
	content, err := DocumentBytes(ctx, run.Storage(), document)
	if err != nil {
		log.Warningf("cannot read the bytes of %s: %s", document.Cite(), err)
		return false
	}
	prompt, err := render("picture.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Name":       document.Cite(),
		"Where":      attachmentWhere(document),
		"Said":       metadataText(document, "said"),
		"Why":        why,
	})
	if err != nil {
		return false
	}
	thinking, err := self.dreamThoughtAbout(ctx, run, budget, "Looked at "+document.Cite(), prompt,
		[]llm.ContentPart{{Type: "image", MediaType: document.ContentType(), Data: content}}, false)
	if err != nil {
		log.Warningf("a dream could not look at %s: %s", document.Cite(), err)
		return false
	}
	// A model that answered with nothing has described nothing. Filing an
	// empty text would give the document no passages and leave it in the
	// same place, but it would also look, on the row, like a picture that
	// had been read and found to hold nothing.
	text := strings.TrimSpace(thinking.Text)
	if text == "" {
		return false
	}
	// Without the cancellation, as the reading's own marking is: a night
	// out of time has still paid for this description and losing it would
	// mean paying again.
	if err := keepDreamPicture(ctx, run, document, text); err != nil {
		log.Warningf("cannot keep what %s turned out to show: %s", document.Cite(), err)
		return false
	}
	self.retitle(ctx, run, thinking.Conversation, "Read the picture "+document.Cite())
	return true
}

// declineAttachments records that the night decided against opening these
// and why, so that no later night pays to decide again.
func declineAttachments(ctx context.Context, run *Run, documentIds []string, reason string) {
	if len(documentIds) == 0 {
		return
	}
	if err := dreamBookkeeping(ctx, run, func(tx db.Transaction) error {
		return tx.MarkAgentDocumentsDeclined(documentIds, reason, time.Now())
	}); err != nil {
		log.Warningf("cannot record what a dream decided against opening: %s", err)
	}
}

// attachmentLine is one file as the deciding call is shown it: everything
// that is free to know about it and nothing that costs anything.
//
// One line each, with what was said folded onto a second and its own
// newlines collapsed, because a message of six lines would otherwise read
// as six files.
func attachmentLine(document *models.AgentDocument) string {
	parts := []string{document.Cite()}
	if document.Bytes > 0 {
		parts = append(parts, formatBytes(document.Bytes))
	}
	if contentType := document.ContentType(); contentType != "" {
		parts = append(parts, contentType)
	}
	if where := attachmentWhere(document); where != "" {
		parts = append(parts, where)
	}
	if document.HappenedAt != nil {
		parts = append(parts, document.HappenedAt.Format("2 Jan 2006"))
	}
	line := strings.Join(parts, " — ")
	said := strings.Join(strings.Fields(metadataText(document, "said")), " ")
	if said == "" {
		return line
	}
	who := document.Author()
	if who != "" {
		who += ": "
	}
	return line + "\n    " + who + said
}

// attachmentWhere is where a file came from, in the words a person would
// use: the channel and the thread the record it arrived with was in.
func attachmentWhere(document *models.AgentDocument) string {
	var parts []string
	if channel := document.Channel(); channel != "" {
		parts = append(parts, "in "+channel)
	}
	if thread := document.Thread(); thread != "" {
		parts = append(parts, "thread "+thread)
	}
	return strings.Join(parts, ", ")
}

// metadataText is one string a scan recorded about a document.
func metadataText(document *models.AgentDocument, key string) string {
	if document == nil || document.Metadata == nil {
		return ""
	}
	if value, ok := document.Metadata[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
