package apigraph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/linkclean"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/util/safefetch"
)

// The card under a message that holds a link: the page's picture, its site,
// its title and a line about it. The dashboard and the iPhone app ask for the
// previews of the links in the messages on screen, and so old messages get one
// too.
//
// The page is fetched here and the answer kept, shared by everyone who sees
// the link, so the page's site learns that this server looked and nothing
// about who was reading. Its picture is fetched here too, scaled down, kept in
// the file store and served from linkPreviewImageView, so that a reader's
// device never loads anything from the linked site.

const (
	// linkPreviewMostURLs is how many links one question may be about.
	linkPreviewMostURLs = 10

	// linkPreviewLongestURL is the longest link read, the longest safefetch
	// will fetch.
	linkPreviewLongestURL = 2048

	// linkPreviewTimeToLive is how long a page that was read is trusted, and
	// linkPreviewFailureTimeToLive how long the news that one could not be.
	linkPreviewTimeToLive        = 24 * time.Hour
	linkPreviewFailureTimeToLive = time.Hour

	// linkPreviewKeptFor is how long a row is kept at all. Past it the row
	// and its picture are removed, whether or not the link is still shown.
	linkPreviewKeptFor = 30 * 24 * time.Hour

	// linkPreviewPruneBatch and linkPreviewPruneInterval bound the removal of
	// old rows, which is done along with a fetch rather than by a job of its
	// own.
	linkPreviewPruneBatch    = 50
	linkPreviewPruneInterval = time.Hour

	// linkPreviewParallelFetches is how many pages one question fetches at a
	// time.
	linkPreviewParallelFetches = 5

	// linkPreviewFetchesPerMinute is how many pages one account may have
	// fetched a minute. A question that is answered from what is kept costs
	// nothing.
	linkPreviewFetchesPerMinute = 60

	// linkPreviewFetchTimeout bounds one page and its picture together.
	linkPreviewFetchTimeout = 20 * time.Second
)

// The words a row's fetchFailure is written in.
const (
	linkPreviewFailureUnreachable = "unreachable"
	linkPreviewFailureNotWebPage  = "notWebPage"
	linkPreviewFailureNoWords     = "noWords"
	linkPreviewFailureUnreadable  = "unreadable"
)

// LinkPreviewQuery reads what the pages behind links say about themselves.
type LinkPreviewQuery interface {
	// Reads the previews of up to 10 links in the words of a conversation, in
	// the order given: for each, the link with its tracking parameters taken
	// off, the short text to show for it, and, when the page could be read, its
	// title, a line about it, the name of its site and the address of its
	// picture. A link gets a preview only if its exact text, as given or
	// cleaned, is in a message of the conversation that its agent did not
	// write: a person's, or what a tool answered. Any other link comes back
	// with isAvailable false and is never fetched, since a reply can be made to
	// carry what the agent read in a link's query. The page is fetched by the
	// server and what it said is kept for a day, or an hour when it could not
	// be read, so asking again is cheap. A link that is not a web address, or
	// whose page could not be read, comes back with isAvailable false and
	// nothing else filled in. The picture is served from the server, never from
	// the linked site. Needs a signed-in caller who may use the conversation.
	ReadLinkPreviews(ctx context.Context, arguments ReadLinkPreviewsArguments) ([]*LinkPreviewView, error)
}

// ReadLinkPreviewsArguments names the links.
type ReadLinkPreviewsArguments struct {
	// ConversationID is the conversation whose messages the links are in. It
	// is one the caller may read, as for ReadAgentConversation.
	ConversationID string `json:"conversationId"`

	// URLs are the links, at most 10, each at most 2048 characters.
	URLs []string `json:"urls"`
}

// LinkPreviewView is what one link's page says about itself.
type LinkPreviewView struct {
	// URL is the link as it was asked for.
	URL string `json:"url"`

	// CleanedURL is the link without its tracking parameters. It is what a
	// click should open.
	CleanedURL string `json:"cleanedUrl"`

	// DisplayText is the short text for a link that has no words of its own.
	DisplayText string `json:"displayText"`

	// IsAvailable says the page was read and had a title or a line about it.
	// When false, the fields below are empty.
	IsAvailable bool `json:"isAvailable"`

	Title       string `json:"title"`
	Description string `json:"description"`

	// SiteName is what the page calls its site, or its host when it says
	// nothing.
	SiteName string `json:"siteName"`

	// ImageURL is the page's picture as this server kept it, null when the
	// page has none. The address works for a signed-in caller only.
	ImageURL          *string `json:"imageUrl"`
	ImageWidthPixels  *int    `json:"imageWidthPixels"`
	ImageHeightPixels *int    `json:"imageHeightPixels"`
}

var _ LinkPreviewQuery = &graph{}

// linkPreviewState is what a graph needs to answer: the fetcher, which a test
// replaces, and the limits.
type linkPreviewState struct {
	fetcher *linkpreview.Fetcher

	limitsOnce  sync.Once
	limitsValue *ratelimit.Registry

	pruneMutex sync.Mutex
	prunedAt   time.Time
}

func (self *graph) linkPreviewFetcher() *linkpreview.Fetcher {
	if self.linkPreviews.fetcher == nil {
		return linkpreview.NewFetcher()
	}
	return self.linkPreviews.fetcher
}

// linkPreviewLimits are the limits on the pages one account has fetched,
// made the first time they are wanted.
func (self *graph) linkPreviewLimits() *ratelimit.Registry {
	self.linkPreviews.limitsOnce.Do(func() {
		self.linkPreviews.limitsValue = ratelimit.NewRegistry(float64(linkPreviewFetchesPerMinute)/60, linkPreviewFetchesPerMinute, 10000, 5*time.Minute)
	})
	return self.linkPreviews.limitsValue
}

// linkPreviewAsked is one link of a question.
type linkPreviewAsked struct {
	written string
	cleaned string

	// key is what the row is kept under: the cleaned link without its
	// fragment, which no page serves differently. Empty when the link is not
	// one a page can be fetched from.
	key string
}

func (self *graph) ReadLinkPreviews(ctx context.Context, arguments ReadLinkPreviewsArguments) ([]*LinkPreviewView, error) {
	if len(arguments.URLs) == 0 || len(arguments.URLs) > linkPreviewMostURLs {
		return nil, fmt.Errorf("%w: give between 1 and %d links", api.ErrInvalidArguments, linkPreviewMostURLs)
	}
	asked := make([]*linkPreviewAsked, len(arguments.URLs))
	var keys []string
	for index, written := range arguments.URLs {
		written = strings.TrimSpace(written)
		if written == "" || len(written) > linkPreviewLongestURL {
			return nil, fmt.Errorf("%w: a link is between 1 and %d characters", api.ErrInvalidArguments, linkPreviewLongestURL)
		}
		asked[index] = newLinkPreviewAsked(written)
		if asked[index].key != "" {
			keys = append(keys, asked[index].key)
		}
	}

	var limitKey string
	cached := map[string]*db.LinkPreview{}
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		// The conversation is resolved as ReadAgentConversation resolves it.
		principal, found, err := self.requireConversationPerson(phaseContext)
		if err != nil {
			return err
		}
		tx := self.transaction(phaseContext)
		if strings.TrimSpace(arguments.ConversationID) == "" {
			if err := requireConversationUse(principal, found, &models.AgentConversation{Kind: models.AgentConversationMain}); err != nil {
				return err
			}
		}
		conversation, other, _, err := self.conversationFor(tx, principal, found, arguments.ConversationID, true)
		if err != nil {
			return err
		}
		// A chat is between its people: an operator with agent:act reads none
		// of them but their own.
		if conversation.IsChat() && other != nil {
			return api.ErrNotFound
		}
		if err := requireConversationUse(principal, found, conversation); err != nil {
			return err
		}
		limitKey = principal.User.ID
		// Only a link somebody other than the agent wrote is looked up or
		// fetched; the rest are answered as having no preview. A run's
		// messages are all the agent's or the code's, its opening message
		// too, so a run has none.
		keys = keys[:0]
		for _, each := range asked {
			if each.key == "" {
				continue
			}
			if conversation.Kind == models.AgentConversationRun {
				each.key = ""
				continue
			}
			isAllowed, err := tx.HasLinkNotWrittenByAgent(conversation, each.written, each.cleaned)
			if err != nil {
				return err
			}
			if !isAllowed {
				each.key = ""
				continue
			}
			keys = append(keys, each.key)
		}
		cached, err = tx.ListLinkPreviews(keys)
		return err
	}); err != nil {
		return nil, err
	}

	// A page is fetched in no transaction: it can take seconds, and a held
	// transaction is a held connection.
	now := time.Now()
	var group sync.WaitGroup
	var mutex sync.Mutex
	turns := make(chan struct{}, linkPreviewParallelFetches)
	isFetching := map[string]bool{}
	for _, found := range asked {
		if found.key == "" || isFetching[found.key] || isLinkPreviewFresh(cached[found.key], now) {
			continue
		}
		isFetching[found.key] = true
		if !self.linkPreviewLimits().Allow(limitKey) {
			// Asked again in a moment, the page is fetched then. What is
			// kept, stale as it is, is better than nothing meanwhile.
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			turns <- struct{}{}
			defer func() { <-turns }()
			fetched, err := self.fetchLinkPreview(ctx, found.key)
			if err != nil {
				log.Warningf("cannot keep the preview of a link: %s", err)
				return
			}
			mutex.Lock()
			cached[found.key] = fetched
			mutex.Unlock()
		}()
	}
	group.Wait()
	if len(isFetching) > 0 {
		self.pruneLinkPreviews(context.WithoutCancel(ctx), now)
	}

	views := make([]*LinkPreviewView, len(asked))
	for index, found := range asked {
		views[index] = newLinkPreviewView(found, cached[found.key])
	}
	return views, nil
}

func newLinkPreviewAsked(written string) *linkPreviewAsked {
	found := &linkPreviewAsked{written: written, cleaned: linkclean.Clean(written)}
	key, _, _ := strings.Cut(found.cleaned, "#")
	if _, err := safefetch.ParseTarget(key); err == nil {
		found.key = key
	}
	return found
}

// isLinkPreviewFresh says a kept row is still trusted.
func isLinkPreviewFresh(kept *db.LinkPreview, now time.Time) bool {
	if kept == nil {
		return false
	}
	timeToLive := linkPreviewTimeToLive
	if kept.FetchFailure != "" {
		timeToLive = linkPreviewFailureTimeToLive
	}
	return now.Sub(kept.FetchedAt) < timeToLive
}

func newLinkPreviewView(found *linkPreviewAsked, kept *db.LinkPreview) *LinkPreviewView {
	view := &LinkPreviewView{URL: found.written, CleanedURL: found.cleaned, DisplayText: linkclean.DisplayText(found.written)}
	if kept == nil || kept.FetchFailure != "" || (kept.Title == "" && kept.Description == "") {
		return view
	}
	view.IsAvailable = true
	view.Title = kept.Title
	view.Description = kept.Description
	view.SiteName = kept.SiteName
	if view.SiteName == "" {
		if parsed, err := url.Parse(kept.URL); err == nil {
			view.SiteName = strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
		}
	}
	if kept.ImageFileID != "" {
		address := api.LinkPreviewImagePath(kept.ImageFileID)
		view.ImageURL = &address
		if kept.ImageWidthPixels > 0 && kept.ImageHeightPixels > 0 {
			view.ImageWidthPixels, view.ImageHeightPixels = &kept.ImageWidthPixels, &kept.ImageHeightPixels
		}
	}
	return view
}

// fetchLinkPreview reads a page and its picture, and keeps what it found, or
// that it found nothing. The fetch is not tied to the question that caused it:
// a reader who scrolls on leaves the next one the page.
func (self *graph) fetchLinkPreview(ctx context.Context, key string) (*db.LinkPreview, error) {
	fetchContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), linkPreviewFetchTimeout)
	defer cancel()

	preview := &db.LinkPreview{URL: key, FetchedAt: time.Now()}
	page, err := self.linkPreviewFetcher().FetchPage(fetchContext, key)
	switch {
	case errors.Is(err, linkpreview.ErrNotHTML):
		preview.FetchFailure = linkPreviewFailureNotWebPage
	case err != nil && !errors.Is(err, linkpreview.ErrNotFetched):
		preview.FetchFailure = linkPreviewFailureUnreadable
	case err != nil:
		preview.FetchFailure = linkPreviewFailureUnreachable
	case !page.HasWords():
		preview.FetchFailure = linkPreviewFailureNoWords
	}
	if err != nil {
		log.Debugf("cannot read the page of a link: %s", err)
	}

	imageFileId := ""
	if preview.FetchFailure == "" {
		preview.Title, preview.Description, preview.SiteName = page.Title, page.Description, page.SiteName
		if page.ImageURL != "" {
			image, err := self.linkPreviewFetcher().FetchImage(fetchContext, page.ImageURL)
			if err != nil {
				// The card is shown without a picture.
				log.Debugf("cannot keep the picture of a page: %s", err)
			} else {
				imageFileId = fmt.Sprintf("link-preview-%d", time.Now().UnixNano())
				if err := self.storage.PutFile(fetchContext, imageFileId, image.Content); err != nil {
					return nil, err
				}
				preview.ImageFileID = imageFileId
				preview.ImageSizeBytes = len(image.Content)
				preview.ImageWidthPixels, preview.ImageHeightPixels = image.WidthPixels, image.HeightPixels
			}
		}
	}

	if err := self.database.TransactionContext(fetchContext, func(transaction db.Transaction) error {
		replacedImageFileId, err := transaction.SaveLinkPreview(preview)
		if err != nil {
			return err
		}
		// The old picture goes once the row naming the new one is committed.
		transaction.AfterCommit(func() { self.deleteLinkPreviewImage(context.WithoutCancel(ctx), replacedImageFileId) })
		return nil
	}); err != nil {
		self.deleteLinkPreviewImage(context.WithoutCancel(ctx), imageFileId)
		return nil, err
	}
	return preview, nil
}

func (self *graph) deleteLinkPreviewImage(ctx context.Context, imageFileId string) {
	if imageFileId == "" {
		return
	}
	if err := self.storage.DeleteFile(ctx, imageFileId); err != nil {
		log.Warningf("cannot remove the file %s of a link preview: %s", imageFileId, err)
	}
}

// pruneLinkPreviews removes the oldest rows past linkPreviewKeptFor, and their
// pictures, at most once an interval and a batch at a time.
func (self *graph) pruneLinkPreviews(ctx context.Context, now time.Time) {
	self.linkPreviews.pruneMutex.Lock()
	if now.Sub(self.linkPreviews.prunedAt) < linkPreviewPruneInterval {
		self.linkPreviews.pruneMutex.Unlock()
		return
	}
	self.linkPreviews.prunedAt = now
	self.linkPreviews.pruneMutex.Unlock()

	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		old, err := transaction.ListLinkPreviewsFetchedBefore(now.Add(-linkPreviewKeptFor), linkPreviewPruneBatch)
		if err != nil || len(old) == 0 {
			return err
		}
		keys := make([]string, len(old))
		imageFileIds := make([]string, len(old))
		for index, preview := range old {
			keys[index], imageFileIds[index] = preview.URL, preview.ImageFileID
		}
		if err := transaction.DeleteLinkPreviews(keys); err != nil {
			return err
		}
		transaction.AfterCommit(func() {
			for _, imageFileId := range imageFileIds {
				self.deleteLinkPreviewImage(ctx, imageFileId)
			}
		})
		return nil
	}); err != nil {
		log.Warningf("cannot remove old link previews: %s", err)
	}
}

// linkPreviewImageView serves the picture of a link preview to a signed-in
// reader. It is public web content, fetched without anything of anyone's, so
// who may see it is anybody who may sign in; the name is not a guess a stranger
// can make, and the row must name it.
func (self *graph) linkPreviewImageView(response http.ResponseWriter, request *http.Request) {
	username := api.UsernameFromRequest(request)
	if username == "" {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	if username != config.LocalUsername {
		var user *models.User
		if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) (err error) {
			user, err = transaction.GetUserByUsername(username)
			return err
		}); err != nil {
			http.Error(response, "cannot read that just now", http.StatusInternalServerError)
			return
		}
		if user == nil || user.Disabled() {
			http.Error(response, "not signed in", http.StatusUnauthorized)
			return
		}
	}

	imageFileId := mux.Vars(request)["imageFileId"]
	var preview *db.LinkPreview
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) (err error) {
		preview, err = transaction.GetLinkPreviewByImageFile(imageFileId)
		return err
	}); err != nil {
		log.Errorf("cannot read a link preview's picture: %s", err)
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	if preview == nil {
		http.Error(response, "no such picture", http.StatusNotFound)
		return
	}

	tag := `"` + imageFileId + `"`
	response.Header().Set("ETag", tag)
	// Private because a signed-in reader is the only one let in, and a day
	// because a changed picture has a new name.
	response.Header().Set("Cache-Control", "private, max-age=86400")
	if matchesTag(request.Header.Get("If-None-Match"), tag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	content, err := self.storage.GetFile(request.Context(), imageFileId)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			log.Errorf("cannot read the file %s of a link preview: %s", imageFileId, err)
			http.Error(response, "cannot read that just now", http.StatusInternalServerError)
			return
		}
		http.Error(response, "no such picture", http.StatusNotFound)
		return
	}
	response.Header().Set("Content-Type", "image/jpeg")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	// It is a picture and nothing else, whatever the file turns out to hold.
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	response.Header().Set("Content-Disposition", "inline")
	if request.Method == http.MethodHead {
		return
	}
	if _, err := response.Write(content); err != nil {
		log.Debugf("a link preview's picture could not be written: %s", err)
	}
}
