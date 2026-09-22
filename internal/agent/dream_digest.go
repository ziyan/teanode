package agent

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// dreamDigest works through what arrived and files what it taught.
//
// By priority rather than by order, because six hundred thousand threads
// will never all be read: what the person pinned, then what they started,
// then what they answered, then the rest newest first. And when the
// backlog is larger than anybody would wait for, a stretch at a time
// instead of an item at a time -- coarser, complete, and marked so it can
// be done again finely later.
func (self *Agent) dreamDigest(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	var waiting []*models.AgentDocument
	var backlog int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		limit := dreamDigest
		if run.Agent.DreamBootstrap {
			limit = dreamDigestBootstrap
		}
		waiting, backlog, err = tx.ListAgentDocumentsToDigest(run.Agent.ID, chatNamesOf(run.Owner), limit)
		return err
	}); err != nil {
		log.Warningf("cannot list what is waiting to be read: %s", err)
		return
	}
	record.Backlog = int(backlog)
	if len(waiting) == 0 {
		return
	}
	// Never coarsely. Reading titles instead of contents made pages and
	// no facts -- twenty-eight of them with nothing on them -- and a
	// backlog is pacing: what is not read tonight is read on a later
	// night, in full, in the order that matters.
	record.Coarse = false

	waiting = self.markTinyRead(ctx, run, waiting)

	// Batches go to the model a few at a time where it has the slots: a
	// service metered by the call gets one, a model of the person's own
	// as many as it can take. Each batch marks its own documents read as
	// it finishes, so a night that ends between batches loses nothing.
	concurrency := run.Configuration().Agent.Limits.ScanConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	// A batch is one source's documents. The night's order puts chat
	// before notes before code, and where that order crossed from one
	// source into another inside a batch the model carried the page it
	// had just filed to across the line: eleven facts about a checkout,
	// then a chat's news filed to the checkout's page. Sorted by source
	// behind the night's order, and cut where the source changes, a
	// batch holds things that belong together.
	sort.SliceStable(waiting, func(left, right int) bool {
		return waiting[left].SourceID < waiting[right].SourceID
	})
	var mutex sync.Mutex
	complete := dreamDigestCompletion(record, budget, &mutex)
	var group sync.WaitGroup
	slots := make(chan struct{}, concurrency)
	stopped := false
	// Batches the model has not answered, in a row. One is a slow answer
	// on a long batch -- a stream that ran past the request timeout
	// happened about once an hour on the local model -- and a night that
	// stopped reading at the first of those read sixty documents of the
	// two hundred and forty it had time for. Three in a row is a model
	// that is not answering tonight.
	silent := 0
	for start := 0; start < len(waiting); {
		mutex.Lock()
		halt := stopped
		mutex.Unlock()
		if halt || ctx.Err() != nil || !budget.left() || !budget.readingTimeLeft() {
			break
		}
		end := start + 1
		for end < len(waiting) && end < start+dreamBatch && waiting[end].SourceID == waiting[start].SourceID {
			end++
		}
		batch := waiting[start:end]
		start = end
		slots <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			_, answered := self.digestBatch(ctx, run, batch, budget, record.Coarse, complete)
			mutex.Lock()
			defer mutex.Unlock()
			if !answered {
				// What it did not read waits for a night when it
				// answers: the batch is not marked read. The reading
				// goes on past one silence and stops at the third.
				if !budget.left() {
					// Except that a batch the allowance could not pay
					// for is not a silence, and blaming the model for
					// the night running out of tokens reads as a fault
					// on the dream's row.
					//
					// It is not a night that caught up either, and
					// saying nothing made it look like one: a night
					// that read nothing and reported nothing wrong is
					// how bootstrapping decides the backlog is done,
					// so a provider that refused every call switched
					// catching up off with fifty thousand documents
					// still waiting. Say what stopped it instead.
					if record.LastError == "" {
						record.LastError = budget.whyItStopped()
					}
					stopped = true
					return
				}
				silent++
				if silent >= dreamSilences {
					record.LastError = readingStopReason(self.budgetNow(ctx, run))
					stopped = true
				}
				return
			}
			silent = 0
		}()
	}
	group.Wait()
}

// markTinyRead marks read, without a call, the documents that say too
// little to be worth a share of one, and answers with the rest.
//
// A channel-day that is one person joining, a file of twenty bytes: four
// hundred of them a night, forty to a call, filed three facts, and the
// calls are better spent on the ones with words in them.
//
// How much a document says is the larger of the size its source recorded
// and the text its passages hold, because neither measure on its own is
// one every document has. A commit is not a file and nothing ever stated
// a size for one, so a rule that read the size alone took every commit
// in an archive for empty -- one thousand eight hundred and seventy-four
// of them stamped read in a single minute, the only documents that carry
// an author and so the only ones an authorship map can be built from --
// and read is the one state a document must not reach without having
// been read, because nothing goes back for it. It happens the other way
// round as well: a file whose text nothing could extract has a size and
// no passages. Only a document that both measures call empty is set
// aside.
func (self *Agent) markTinyRead(ctx context.Context, run *Run, waiting []*models.AgentDocument) []*models.AgentDocument {
	documentIds := make([]string, 0, len(waiting))
	for _, document := range waiting {
		documentIds = append(documentIds, document.ID)
	}
	var characters map[string]int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		characters, err = tx.MeasureAgentDocuments(run.Agent.ID, documentIds)
		return err
	}); err != nil {
		// A measurement that did not happen is not evidence that
		// anything is empty. Reading them all costs calls; marking them
		// read on a query that failed costs the documents themselves.
		log.Warningf("cannot measure what is waiting to be read: %s", err)
		return waiting
	}

	var tiny []string
	kept := waiting[:0]
	for _, document := range waiting {
		if max(document.Bytes, characters[document.ID]) < digestSmallest {
			tiny = append(tiny, document.ID)
			continue
		}
		kept = append(kept, document)
	}
	if len(tiny) > 0 {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.MarkAgentDocumentsDigested(tiny, time.Now())
		}); err != nil {
			log.Warningf("cannot mark the small ones as read: %s", err)
		}
	}
	return kept
}

// digestBatch reads a handful of documents and files what they taught.
func (self *Agent) digestBatch(ctx context.Context, run *Run, documents []*models.AgentDocument, budget *dreamBudget, coarse bool, complete digestCompletion) (int, bool) {
	material := self.retrieveDigestMaterial(ctx, run, documents, coarse)
	request, err := buildDigestRequest(run.Owner, material, coarse)
	if err != nil {
		return 0, false
	}
	thinking, err := self.dreamThought(ctx, run, budget, fmt.Sprintf("Read %d documents", len(documents)), request.Prompt, true)
	if err != nil {
		// A batch the model's context cannot hold is cut in two and each
		// half read on its own. Twenty openings of twelve hundred runes
		// each are a few hundred tokens over a thirty-two thousand window
		// when the documents are written in Chinese, where a rune costs
		// far more tokens than an English one does, and ten halves of
		// such a batch all fit. Marking the whole batch read instead lost
		// two hundred and twenty documents in two nights, none of which
		// would ever have been read again.
		if llm.IsContextLengthError(err) && len(documents) > 1 {
			if thinking != nil {
				self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %d documents: too long for the model, split in two", len(documents)))
			}
			return self.digestHalves(ctx, run, documents, budget, coarse, complete)
		}
		// One document that on its own does not fit is not going to fit
		// next time either: it is marked read, and its run says why, so
		// the reading moves on rather than stopping at it every dream.
		if llm.IsContextLengthError(err) && thinking != nil {
			self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %d documents: too long for the model, skipped", len(documents)))
			return 0, completeDigestWithoutFacts(ctx, run, documents, complete)
		}
		// Not read: a batch the model never answered is not marked as
		// read. Four thousand documents were, in ten minutes, while the
		// model was unreachable, and would never have been read again.
		log.Warningf("a dream's digest could not ask the model: %s", err)
		return 0, false
	}
	answer, err := self.parseDigestResponse(ctx, run, budget, thinking.Text)
	if err != nil {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %d documents, and answered with no object", len(documents)))
		return 0, self.givingUpOn(ctx, run, documents) && completeDigestWithoutFacts(ctx, run, documents, complete)
	}
	// Evidence points at the document rather than at a conversation:
	// these facts came from something read, not something said.
	filed, err := self.fileWhatWasLearned(ctx, run, answer, nil, models.EvidenceDocument, request.Shown, func(transaction db.Transaction, filed whatWasFiled) error {
		if complete == nil {
			return nil
		}
		return complete(transaction, documents, filed.Filed)
	})
	if err != nil {
		// Leave the batch unread when fact filing fails. Page preparation
		// can have committed already, but the facts still need a retry.
		log.Warningf("cannot file what a dream's digest found: %s", err)
		return 0, false
	}
	// A reading that quoted words nobody wrote says so on its own row, so
	// the night's runs show how often the model invents rather than only
	// how much it filed.
	if checked := filed.Describe(); checked != "" {
		self.retitle(ctx, run, thinking.Conversation,
			fmt.Sprintf("Read %d documents, filed %d, %s", len(documents), filed.Filed, checked))
	}
	return filed.Filed, true
}

// markRead records that a batch's documents were read, so the reading
// does not come back to them.
// givingUpOn records that the night could not make sense of what came
// back about these documents, and says whether the batch should now count
// as read.
//
// It said yes always. A batch whose answer will not parse was marked read
// regardless, and the reasoning for that is sound as far as it goes: a
// batch that blocks blocks every night after it, and the documents behind
// it are never reached. What it left out is that the batch is then gone --
// read, by a night that read nothing of it, with nothing anywhere saying
// so and nothing to find it by.
//
// So the first time is remembered and the documents are left waiting,
// which costs them one more night and no more. The second time they are
// let go, because two is enough to say it is not the weather. Either way
// the count stays on the row, so what was given up on can be found:
//
//	metadata ? 'garbled'
func (self *Agent) givingUpOn(ctx context.Context, run *Run, documents []*models.AgentDocument) bool {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.ID)
	}
	given := map[string]int{}
	if err := dreamBookkeeping(ctx, run, func(tx db.Transaction) error {
		found, err := tx.AgentDocumentsGivenUpOn(ids)
		if err != nil {
			return err
		}
		given = found
		return tx.MarkAgentDocumentsGarbled(ids, time.Now())
	}); err != nil {
		log.Warningf("cannot write down what the night could not read: %s", err)
		// Unwritten, so the count cannot be trusted to grow, so this must
		// not be the try that gives up: the batch waits.
		return false
	}
	// Given up on only where every one of them has been tried before. A
	// batch is read as a batch, so one document new to it is a batch that
	// has not had its second night.
	for _, id := range ids {
		if given[id] < 1 {
			log.Noticef("a dream could not read the answer about %d documents; they wait for one more night", len(ids))
			return false
		}
	}
	log.Warningf("a dream could not read the answer about %d documents a second time; they are marked read and can be found by metadata ? 'garbled'", len(ids))
	return true
}

func markRead(ctx context.Context, run *Run, documents []*models.AgentDocument) {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.ID)
	}
	if err := dreamBookkeeping(ctx, run, func(tx db.Transaction) error {
		return tx.MarkAgentDocumentsDigested(ids, time.Now())
	}); err != nil {
		log.Warningf("cannot mark what was read: %s", err)
	}
}

// digestHalves reads a batch the model's context could not hold as two
// halves, and answers for the batch as a whole: what both halves filed,
// and answered only where both of them were.
//
// A half that was answered is marked read here, as it finishes, so that
// a half the model never answered leaves only itself for a night that
// answers: the batch as a whole is not marked read, and what was filed
// from the first half is not filed again.
func (self *Agent) digestHalves(ctx context.Context, run *Run, documents []*models.AgentDocument, budget *dreamBudget, coarse bool, complete digestCompletion) (int, bool) {
	middle := len(documents) / 2
	total := 0
	for _, half := range [][]*models.AgentDocument{documents[:middle], documents[middle:]} {
		// The night's deadline and its allowance are checked before each
		// half, as the reading checks them before each batch: a half the
		// night has no time or no tokens for is not read and not marked
		// read.
		if ctx.Err() != nil || !budget.left() {
			return total, false
		}
		filed, answered := self.digestBatch(ctx, run, half, budget, coarse, complete)
		total += filed
		if !answered {
			return total, false
		}
		if complete == nil {
			markRead(ctx, run, half)
		}
	}
	return total, true
}
