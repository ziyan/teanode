package apigraph

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/search"
)

// Search across everything: the person's words looked for in their chats,
// mail, contacts, photos, memory, documents, recordings, places and finance
// at once. The dashboard's search box, the command line's teanode search and
// the agent's search tool all call this one operation.
//
// It adds no index and asks no model. Each area is the search that area
// already has, run through the same helper that decides whether the person
// may use the area at all (requireConversationPerson, requireMailbox,
// requireOwnAddressBook, requirePhotosPerson, requireAgentPerson,
// requirePermission for recordings, requireLocationPerson), so that there is
// one place deciding access to an area and this operation cannot read what
// that area's own page would refuse. internal/search runs them side by side.

// SearchQuery searches everything of the person's at once.
type SearchQuery interface {
	// Looks for the words in the caller's chats, mail, contacts, photos,
	// memory, documents, recordings, places and finance at once, a few results
	// from each. Each area is searched by its own search, by the words alone
	// (nothing is asked of a model), and only if the caller may use it: an area
	// they may not use is left out, and an area that failed or took longer than
	// three seconds is named in failedAreas without failing the rest. areas
	// narrows the search to some of chats, mail, contacts, photos, memory,
	// documents, recordings, places and finance; left out, every area is
	// searched. perAreaLimit is how many results to keep from each area, 5 by
	// default and 20 at most. Needs a signed-in caller.
	SearchEverything(ctx context.Context, arguments SearchEverythingArguments) (*SearchEverythingView, error)
}

// SearchEverythingArguments is what to look for, and where.
type SearchEverythingArguments struct {
	// Words are what to look for.
	Words string `json:"words"`

	// PerAreaLimit is how many results to keep from each area, 5 by default
	// and 20 at most.
	PerAreaLimit int `json:"perAreaLimit" graphapi:"nullable"`

	// Areas narrows the search to these areas: chats, mail, contacts, photos,
	// memory, documents, recordings, places or finance. Left out, every area
	// the caller may use is searched.
	Areas []string `json:"areas" graphapi:"nullable"`
}

// SearchEverythingView is what a search across everything found.
type SearchEverythingView struct {
	// Words are what was looked for, without the spaces around them.
	Words string `json:"words"`

	// Areas are only the areas with results, in a fixed order: chats, mail,
	// contacts, photos, memory, documents, recordings, places, finance.
	Areas []*SearchAreaView `json:"areas"`

	// FailedAreas are the areas that failed or did not answer in time. An
	// area the caller may not use is not among them.
	FailedAreas []string `json:"failedAreas"`
}

// SearchAreaView is one area's results.
type SearchAreaView struct {
	// SearchArea is the area: chats, mail, contacts, photos, memory,
	// documents, recordings, places or finance.
	SearchArea string `json:"searchArea"`

	Results []*SearchResultView `json:"results"`

	// HasMore says the area holds more than these, which its own page finds.
	HasMore bool `json:"hasMore"`
}

// SearchResultView is one thing found.
type SearchResultView struct {
	SearchArea string `json:"searchArea"`

	// TargetID is what opens it in its area: the conversation, mail item,
	// contact, photo, memory page path, document, recording, place or
	// finance transaction.
	TargetID string `json:"targetId"`

	TitleText string `json:"titleText"`

	// DetailText is a snippet, a sender, an account.
	DetailText string `json:"detailText"`

	// HappenedAt is when it was said, received, taken or posted.
	HappenedAt *time.Time `json:"happenedAt" graphapi:"nullable"`
}

// snippetLength is how many characters of a longer text a result shows.
const snippetLength = 200

// SearchEverything is the operation. It is one of isModelBackedQuery, though it
// asks no model: it holds no transaction of its own across its work, because
// every area reads in a transaction of its own, side by side, and a
// transaction shared between them could only be used by one at a time.
func (self *graph) SearchEverything(ctx context.Context, arguments SearchEverythingArguments) (*SearchEverythingView, error) {
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		_, err := self.requireSignedIn(phaseContext)
		return err
	}); err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, name := range arguments.Areas {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if !search.IsArea(name) {
			return nil, fmt.Errorf("%w: %q is not an area to search; the areas are %s", api.ErrInvalidArguments, name, strings.Join(search.Areas, ", "))
		}
		wanted[name] = true
	}

	finders := map[string]search.Func{
		search.AreaChats:      self.searchChats,
		search.AreaMail:       self.searchMail,
		search.AreaContacts:   self.searchContacts,
		search.AreaPhotos:     self.searchPhotos,
		search.AreaMemory:     self.searchMemory,
		search.AreaDocuments:  self.searchDocuments,
		search.AreaRecordings: self.searchRecordings,
		search.AreaPlaces:     self.searchPlaces,
		search.AreaFinance:    self.searchFinance,
	}
	searchers := make([]*search.Searcher, 0, len(finders))
	for _, area := range search.Areas {
		if len(wanted) > 0 && !wanted[area] {
			continue
		}
		searchers = append(searchers, &search.Searcher{SearchArea: area, Search: self.searchInPhase(finders[area])})
	}
	found, err := search.Everything(ctx, searchers, arguments.Words, search.Options{PerAreaLimit: arguments.PerAreaLimit})
	if err != nil {
		return nil, err
	}

	view := &SearchEverythingView{Words: found.Words, Areas: make([]*SearchAreaView, 0, len(found.Areas)), FailedAreas: found.FailedAreas}
	for _, area := range found.Areas {
		areaView := &SearchAreaView{SearchArea: area.SearchArea, HasMore: area.HasMore, Results: make([]*SearchResultView, 0, len(area.Results))}
		for _, result := range area.Results {
			areaView.Results = append(areaView.Results, &SearchResultView{
				SearchArea: result.SearchArea, TargetID: result.TargetID, TitleText: result.TitleText,
				DetailText: result.DetailText, HappenedAt: result.HappenedAt,
			})
		}
		view.Areas = append(view.Areas, areaView)
	}
	return view, nil
}

// searchInPhase runs one area's search in a transaction of its own, with the
// caller's permissions read afresh in it, so that areas can be searched side
// by side and a permission taken away is seen by the next search. The
// transaction is bound to the area's context, so that an area out of time has
// its queries cancelled.
func (self *graph) searchInPhase(find search.Func) search.Func {
	return func(ctx context.Context, words string, limit int) ([]*search.Result, error) {
		var results []*search.Result
		err := self.readInPhase(ctx, func(phaseContext context.Context) (err error) {
			results, err = find(phaseContext, words, limit)
			return err
		})
		return results, err
	}
}

// refusedAsUnavailable turns what the helpers that decide access answer to a
// caller who may not use an area (not found, not signed in, the service off,
// no agent that is on) into search.ErrNotAvailable, which leaves the area out.
// Any other error is returned as it is, and is a failure of the area.
func refusedAsUnavailable(err error) error {
	var unavailableService *models.ServiceUnavailableError
	if errors.Is(err, api.ErrNotFound) || errors.Is(err, api.ErrNotLoggedIn) || errors.Is(err, agent.ErrUnavailable) || errors.As(err, &unavailableService) {
		return fmt.Errorf("%w: %s", search.ErrNotAvailable, err)
	}
	return err
}

// markdownMarks are the marks at the start of a line of Markdown that say
// what the line is (a heading's #, a list's - or 1., a quote's >), which a
// one-line snippet shows as they are.
var markdownMarks = regexp.MustCompile(`(?m)^[ \t]*(#{1,6}[ \t]+|[-*+][ \t]+|\d+\.[ \t]+|>[ \t]?)`)

// snippetOf is the start of a longer text on one line, for a result to show.
func snippetOf(text string) string {
	text = strings.Join(strings.Fields(markdownMarks.ReplaceAllString(text, "")), " ")
	if utf8.RuneCountInString(text) <= snippetLength {
		return text
	}
	return string([]rune(text)[:snippetLength]) + "…"
}

// firstNonEmpty is the first of the texts that says something.
func firstNonEmpty(texts ...string) string {
	for _, text := range texts {
		if strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// searchChats is the caller's conversations with their agent, found by their
// title, their summary and what was said in them. The chats between people
// are not searched here: listing them makes the rows they are listed from
// (SyncChatsOf), and a search writes nothing.
func (self *graph) searchChats(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	principal, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	if !canUseAgentConversations(principal, found) {
		return nil, search.ErrNotAvailable
	}
	conversations, err := self.transaction(ctx).SearchAgentConversations(found.ID, words, limit)
	if err != nil {
		return nil, err
	}
	results := make([]*search.Result, 0, len(conversations))
	for _, conversation := range conversations {
		lastAt := conversation.LastAt
		results = append(results, &search.Result{
			TargetID: conversation.ID, TitleText: strings.TrimSpace(conversation.Title),
			DetailText: snippetOf(conversation.Summary), HappenedAt: &lastAt,
		})
	}
	return results, nil
}

// searchMail is the whole of each of the caller's mailboxes, as the mail page
// searches one: every folder of it, and not the notes a phone keeps there.
func (self *graph) searchMail(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	if err := self.requireService(models.ServiceMailboxes); err != nil {
		return nil, refusedAsUnavailable(err)
	}
	principal, err := self.requirePermission(ctx, models.PermissionMailRead)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	if principal.User == nil {
		return nil, search.ErrNotAvailable
	}
	mailboxes, err := self.transaction(ctx).ListMailboxes(principal.User.ID)
	if err != nil {
		return nil, err
	}
	type found struct {
		result     *search.Result
		receivedAt time.Time
	}
	var threads []found
	// One message delivered to several of the person's addresses is several
	// items, in one mailbox or across them; it is found once. Twice as many
	// are asked for, so that the copies left out do not leave the page short.
	seenMessageIds := map[string]bool{}
	seenCopies := map[string]time.Time{}
	asked := limit * 2
	for _, mailbox := range mailboxes {
		page, err := self.ListMailboxThreads(ctx, ListMailboxThreadsArguments{MailboxID: &mailbox.ID, Search: &words, First: &asked})
		if err != nil {
			return nil, err
		}
		for _, thread := range page.Threads {
			if thread.Item == nil || thread.Item.Mail == nil {
				continue
			}
			mail := thread.Item.Mail
			if messageId := strings.TrimSpace(mail.MessageID); messageId != "" {
				if seenMessageIds[messageId] {
					continue
				}
				seenMessageIds[messageId] = true
			}
			// A sender that writes to each address separately gives each
			// copy its own Message-ID: the same sender and subject within a
			// few minutes is the same mail to the person.
			copyKey := strings.ToLower(strings.TrimSpace(mail.From)) + "\x00" + strings.TrimSpace(mail.Subject)
			if lastAt, isSeen := seenCopies[copyKey]; isSeen && absoluteDuration(lastAt.Sub(mail.ReceivedAt)) <= mailCopyWindow {
				continue
			}
			seenCopies[copyKey] = mail.ReceivedAt
			receivedAt := mail.ReceivedAt
			threads = append(threads, found{receivedAt: receivedAt, result: &search.Result{
				TargetID: thread.Item.ID, TitleText: strings.TrimSpace(mail.Subject),
				DetailText: firstNonEmpty(mail.FromName, mail.From), HappenedAt: &receivedAt,
			}})
		}
	}
	// Newest first across the mailboxes, as the threads are within each.
	sort.SliceStable(threads, func(left, right int) bool { return threads[left].receivedAt.After(threads[right].receivedAt) })
	results := make([]*search.Result, 0, min(len(threads), limit))
	for _, thread := range threads[:min(len(threads), limit)] {
		results = append(results, thread.result)
	}
	return results, nil
}

// searchContacts is every address book of the caller's, by name, organization,
// address or number.
func (self *graph) searchContacts(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	books, err := self.transaction(ctx).ListAddressBooks(principal.User.ID)
	if err != nil {
		return nil, err
	}
	var contacts []*models.Contact
	for _, listed := range books {
		book, err := self.requireOwnAddressBook(ctx, listed.ID)
		if err != nil {
			return nil, refusedAsUnavailable(err)
		}
		found, err := self.transaction(ctx).ListContacts(book.ID, words, limit)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, found...)
	}
	sort.SliceStable(contacts, func(left, right int) bool {
		return strings.ToLower(contacts[left].Name) < strings.ToLower(contacts[right].Name)
	})
	results := make([]*search.Result, 0, min(len(contacts), limit))
	for _, contact := range contacts[:min(len(contacts), limit)] {
		detail := []string{contact.Organization}
		if len(contact.Emails) > 0 {
			detail = append(detail, contact.Emails[0])
		}
		results = append(results, &search.Result{
			TargetID: contact.ID, TitleText: firstNonEmpty(contact.Name, contact.Organization, strings.Join(contact.Emails, ", "), strings.Join(contact.Phones, ", ")),
			DetailText: strings.Join(nonEmptyOf(detail...), ", "),
		})
	}
	return results, nil
}

// nonEmptyOf is the texts that say something.
func nonEmptyOf(texts ...string) []string {
	kept := make([]string, 0, len(texts))
	for _, text := range texts {
		if strings.TrimSpace(text) != "" {
			kept = append(kept, strings.TrimSpace(text))
		}
	}
	return kept
}

// searchPhotos is the caller's own library by the words alone: a place, a file
// name, a camera, an album, a description or the words read in the picture.
// ListPhotos would also read the words' meaning from an embedding model, and a
// search across everything costs no model call.
func (self *graph) searchPhotos(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	photos, err := self.transaction(ctx).ListPhotos(principal.User.ID, &db.PhotoFilter{SearchText: words, PageSize: limit})
	if err != nil {
		return nil, err
	}
	results := make([]*search.Result, 0, len(photos))
	for _, photo := range photos {
		happenedAt := photo.CreatedAt
		if photo.TakenAt != nil {
			happenedAt = *photo.TakenAt
		}
		// A photo that has been described but has no file name or place is
		// known by its description.
		title := firstNonEmpty(photo.OriginalFilename, photo.PlaceName)
		detail := firstNonEmpty(snippetOf(photo.PhotoDescription), photo.PlaceName)
		if title == "" {
			title, detail = detail, ""
		} else if detail == title {
			detail = ""
		}
		results = append(results, &search.Result{TargetID: photo.ID, TitleText: title, DetailText: detail, HappenedAt: &happenedAt})
	}
	return results, nil
}

// searchMemory is the pages and facts of the caller's agent's memory, by the
// words. A fact is shown as the page it is on, once.
func (self *graph) searchMemory(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	tx := self.transaction(ctx)
	nodes, facts, err := tx.SearchAgentGraphFrom(found.ID, words, 0, limit)
	if err != nil {
		return nil, err
	}
	learned, err := self.factsWithPaths(tx, found.ID, facts)
	if err != nil {
		return nil, err
	}
	results := make([]*search.Result, 0, len(nodes)+len(learned))
	seen := map[string]bool{}
	for _, node := range nodes {
		modifiedAt := node.ModifiedAt
		seen[node.Path] = true
		results = append(results, &search.Result{
			TargetID: node.Path, TitleText: firstNonEmpty(node.Name, node.Path),
			DetailText: snippetOf(node.Summary), HappenedAt: &modifiedAt,
		})
	}
	for _, each := range learned {
		if each.Path == "" || seen[each.Path] {
			continue
		}
		seen[each.Path] = true
		happenedAt := each.Fact.HappenedAt
		if happenedAt == nil {
			modifiedAt := each.Fact.ModifiedAt
			happenedAt = &modifiedAt
		}
		results = append(results, &search.Result{
			TargetID: each.Path, TitleText: firstNonEmpty(each.Name, each.Path),
			DetailText: snippetOf(each.Fact.Text), HappenedAt: happenedAt,
		})
	}
	return results[:min(len(results), limit)], nil
}

// searchDocuments is what the caller's agent has indexed: files, coding
// transcripts and chats it has read, found as passages and shown as the
// document each is in, once. By the words alone, as the knowledge search is on
// a server with no embedding model.
func (self *graph) searchDocuments(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	// Several passages of one document rank near each other, so more are
	// asked for than will be shown.
	passages, err := indexed.Search(ctx, self.transaction(ctx), nil, found.ID, indexed.Query{Words: words, Limit: min(limit*3, indexed.SearchMost)})
	if err != nil {
		return nil, err
	}
	results := make([]*search.Result, 0, min(len(passages.Passages), limit))
	seen := map[string]bool{}
	for _, passage := range passages.Passages {
		if seen[passage.DocumentID] {
			continue
		}
		seen[passage.DocumentID] = true
		results = append(results, &search.Result{
			TargetID: passage.DocumentID, TitleText: firstNonEmpty(passage.Title, passage.ExternalID),
			DetailText: snippetOf(passage.Text), HappenedAt: passage.HappenedAt,
		})
	}
	return results[:min(len(results), limit)], nil
}

// searchRecordings is the caller's recordings by title and summary.
func (self *graph) searchRecordings(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	if principal.User == nil {
		return nil, search.ErrNotAvailable
	}
	recordings, err := self.transaction(ctx).ListRecordings(principal.User.ID, "", words, limit)
	if err != nil {
		return nil, err
	}
	results := make([]*search.Result, 0, len(recordings))
	for _, recording := range recordings {
		happenedAt := recording.CreatedAt
		if recording.RecordedAt != nil {
			happenedAt = *recording.RecordedAt
		}
		results = append(results, &search.Result{
			TargetID: recording.ID, TitleText: strings.TrimSpace(recording.RecordingTitle),
			DetailText: snippetOf(recording.RecordingSummary), HappenedAt: &happenedAt,
		})
	}
	return results, nil
}

// searchPlaces is the caller's own places by name and address. Another
// person's places, which ListPlaces reaches by a share, are not searched.
func (self *graph) searchPlaces(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	reach, err := self.reachLocation(ctx, principal, "", models.LocationShareLevelHistory)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	places, _, err := self.transaction(ctx).ListPlaces(reach.User.ID, db.PlaceFilter{Query: words, LimitCount: limit})
	if err != nil {
		return nil, err
	}
	results := make([]*search.Result, 0, len(places))
	for _, place := range places {
		results = append(results, &search.Result{
			TargetID: place.ID, TitleText: firstNonEmpty(place.PlaceName, place.OSMName, place.AddressText),
			DetailText: firstNonEmpty(place.AddressText, string(place.PlaceLabel)),
		})
	}
	return results, nil
}

// searchFinance is the caller's agent's finance transactions, by description
// and merchant, with the copies of a transaction left out as the list leaves
// them. Left out altogether where the server offers no finance provider.
func (self *graph) searchFinance(ctx context.Context, words string, limit int) ([]*search.Result, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, refusedAsUnavailable(err)
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, search.ErrNotAvailable
	}
	page, err := self.transaction(ctx).ListFinanceTransactions(found.ID, &db.FinanceTransactionFilter{Text: words, IsDuplicateExcluded: true, Limit: limit})
	if err != nil {
		return nil, financeError(err)
	}
	results := make([]*search.Result, 0, len(page.FinanceTransactions))
	for _, transaction := range page.FinanceTransactions {
		happenedAt := transaction.TransactedAt
		if happenedAt == nil {
			if posted, err := time.Parse("2006-01-02", transaction.PostedOn); err == nil {
				happenedAt = &posted
			}
		}
		results = append(results, &search.Result{
			TargetID: transaction.ID, TitleText: firstNonEmpty(transaction.MerchantName, transaction.Description),
			DetailText: strings.TrimSpace(transaction.Amount + " " + transaction.CurrencyCode), HappenedAt: happenedAt,
		})
	}
	return results, nil
}

// mailCopyWindow is how close in time two mails from one sender with one
// subject arrive to be taken as copies of one mail.
const mailCopyWindow = 10 * time.Minute

func absoluteDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return -duration
	}
	return duration
}
