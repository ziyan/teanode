package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/models"
)

// A card's pictures and videos. A photo or video of the person's is checked
// as the card is posted and served from the photo routes. A web page's picture
// is fetched after the card is posted, by the server, scaled down and kept in
// the file store, so that a slow page never holds the post up and the person's
// device never asks the page's site for anything. Until it is fetched the
// entry is not shown, and once it is, the card is announced as replaced so an
// open feed draws it.
const (
	// feedCaptionCharacters is the longest caption.
	feedCaptionCharacters = 200

	// feedPictureWidthPixels is how wide a page's picture is kept: a card
	// shows it across its whole width, three pixels a point on a phone.
	feedPictureWidthPixels = 1200

	// feedPictureFetchTimeout bounds the fetching of all of a card's
	// pictures, and feedPictureParallelFetches how many pages are read at
	// once.
	feedPictureFetchTimeout    = 15 * time.Second
	feedPictureParallelFetches = 5
)

// feedPictureFetcher is what fetches pages and their pictures; a test hands in
// one that asks a server of its own.
var feedPictureFetcher = linkpreview.NewFetcher()

// normalizeFeedMedia bounds a card's pictures and videos and names each one.
// When a post gives none, a text card shows the picture of its first source
// link, and a list card's rows with a link show theirs.
func normalizeFeedMedia(item *models.AgentFeedItem) error {
	rowLinks := feedListRowLinks(item)
	if len(item.FeedMedia) == 0 {
		item.FeedMedia = defaultFeedMedia(item, rowLinks)
	}
	cardCount, rowCount := 0, 0
	seenKeys := map[string]bool{}
	seenRows := map[int]bool{}
	media := make([]models.FeedMedia, 0, len(item.FeedMedia))
	for _, entry := range item.FeedMedia {
		entry.PhotoID, entry.PageURL = strings.TrimSpace(entry.PhotoID), strings.TrimSpace(entry.PageURL)
		entry.MediaCaption = cutMarked(strings.TrimSpace(entry.MediaCaption), feedCaptionCharacters)
		switch {
		case entry.PhotoID != "" && entry.PageURL != "":
			return feedRefusal("a picture is either one of their photos (photo_id) or a page's picture (page_url), not both")
		case entry.PhotoID != "":
			if entry.FeedMediaKind != models.FeedMediaVideo {
				entry.FeedMediaKind = models.FeedMediaPhoto
			}
			entry.ImageFileID, entry.IsPictureMissing, entry.SiteName = "", false, ""
		case entry.PageURL != "":
			if !isWebAddress(entry.PageURL) {
				return feedRefusal("%q is not a page; page_url is an http or https address", entry.PageURL)
			}
			entry.FeedMediaKind = models.FeedMediaPicture
		default:
			return feedRefusal("a picture needs photo_id, one of their photos, or page_url, a page whose picture to show")
		}
		if entry.ListRowIndex != nil {
			if item.FeedCardLayout != models.FeedLayoutList {
				return feedRefusal("list_row_index is for a list card; leave it out to show the picture on the card")
			}
			if *entry.ListRowIndex < 0 || *entry.ListRowIndex >= len(rowLinks) {
				return feedRefusal("list_row_index %d is not a row of this list, which has %d", *entry.ListRowIndex, len(rowLinks))
			}
			if seenRows[*entry.ListRowIndex] {
				return feedRefusal("row %d has two pictures; a row has one", *entry.ListRowIndex)
			}
			seenRows[*entry.ListRowIndex] = true
			rowCount++
		} else {
			cardCount++
		}
		if entry.MediaKey == "" || seenKeys[entry.MediaKey] {
			entry.MediaKey = newFeedMediaKey()
		}
		seenKeys[entry.MediaKey] = true
		media = append(media, entry)
	}
	if cardCount > models.FeedMediaMost {
		return feedRefusal("a card shows at most %d pictures and videos, and this one has %d", models.FeedMediaMost, cardCount)
	}
	if rowCount > models.FeedRowMediaMost {
		return feedRefusal("a list's rows have at most %d pictures", models.FeedRowMediaMost)
	}
	item.FeedMedia = media
	return nil
}

// defaultFeedMedia is what a card shows when its post gives no pictures: the
// picture of a text card's first source link, and of each linked row of a
// list card.
func defaultFeedMedia(item *models.AgentFeedItem, rowLinks []string) []models.FeedMedia {
	media := []models.FeedMedia{}
	switch item.FeedCardLayout {
	case models.FeedLayoutText, "":
		if len(item.SourceLinks) > 0 && isWebAddress(item.SourceLinks[0].SourceURL) {
			media = append(media, models.FeedMedia{PageURL: item.SourceLinks[0].SourceURL})
		}
	case models.FeedLayoutList:
		for index, link := range rowLinks {
			if link != "" && len(media) < models.FeedRowMediaMost {
				media = append(media, models.FeedMedia{PageURL: link, ListRowIndex: &index})
			}
		}
	}
	return media
}

// feedListRowLinks is the link of each row of a list card, empty for a row
// without one; nil for another layout.
func feedListRowLinks(item *models.AgentFeedItem) []string {
	if item.FeedCardLayout != models.FeedLayoutList || item.LayoutContent == "" {
		return nil
	}
	var content feedListLayout
	if err := decodeLayoutContent(item.LayoutContent, &content); err != nil {
		return nil
	}
	links := make([]string, len(content.ListRows))
	for index, row := range content.ListRows {
		if isWebAddress(row.RowURL) {
			links[index] = row.RowURL
		}
	}
	return links
}

func newFeedMediaKey() string {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return "m" + hex.EncodeToString(buffer)
}

// resolveFeedPhotos checks that each photo or video on the card is one the
// person may see, and fills in its kind and size.
func resolveFeedPhotos(tx db.Transaction, userId string, item *models.AgentFeedItem) error {
	for index := range item.FeedMedia {
		entry := &item.FeedMedia[index]
		if entry.PhotoID == "" {
			continue
		}
		access, err := tx.GetPhotoAccess(userId, entry.PhotoID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && access == nil) {
			return feedRefusal("there is no photo %s of theirs; use an id the photo tool gave you", entry.PhotoID)
		}
		if err != nil {
			return err
		}
		photo, err := tx.GetPhoto(access.OwnerUserID, entry.PhotoID)
		if err != nil {
			return err
		}
		if photo == nil {
			return feedRefusal("there is no photo %s of theirs; use an id the photo tool gave you", entry.PhotoID)
		}
		entry.FeedMediaKind = models.FeedMediaPhoto
		if photo.PhotoKind == models.PhotoKindVideo {
			entry.FeedMediaKind = models.FeedMediaVideo
		}
		entry.WidthPixels, entry.HeightPixels = photo.WidthPixels, photo.HeightPixels
	}
	return nil
}

// carryFeedPictures keeps the pictures a card already had for the pages it
// still shows, so a card that is replaced or changed does not fetch them
// again, and is the files of the pictures it no longer shows, to delete once
// the change is committed.
func carryFeedPictures(previous []models.FeedMedia, item *models.AgentFeedItem) []string {
	kept := map[string]models.FeedMedia{}
	for _, entry := range previous {
		if entry.FeedMediaKind == models.FeedMediaPicture && (entry.ImageFileID != "" || entry.IsPictureMissing) {
			kept[entry.PageURL] = entry
		}
	}
	isUsed := map[string]bool{}
	for index := range item.FeedMedia {
		entry := &item.FeedMedia[index]
		earlier, isFound := kept[entry.PageURL]
		if entry.IsPictureWaiting() && isFound {
			entry.ImageFileID, entry.IsPictureMissing, entry.SiteName = earlier.ImageFileID, earlier.IsPictureMissing, earlier.SiteName
			entry.WidthPixels, entry.HeightPixels = earlier.WidthPixels, earlier.HeightPixels
		}
		if entry.FeedMediaKind == models.FeedMediaPicture && entry.ImageFileID != "" {
			isUsed[entry.ImageFileID] = true
		}
	}
	var removed []string
	for _, entry := range previous {
		if entry.ImageFileID != "" && !isUsed[entry.ImageFileID] {
			removed = append(removed, entry.ImageFileID)
		}
	}
	return removed
}

// deleteFeedPictures removes the files of pictures no card shows any more.
func (self *Agent) deleteFeedPictures(fileIds []string) {
	for _, fileId := range fileIds {
		if err := self.settings.Storage.DeleteFile(context.Background(), fileId); err != nil {
			log.Warningf("cannot remove the picture %s of a card: %s", fileId, err)
		}
	}
}

// startFeedPictures fetches, in the background, the pictures of the card's
// pages that are not fetched yet. postingConversationId is the conversation
// of the turn that posted the card, empty for the person's own post: only a
// page that a tool answered with there is fetched.
func (self *Agent) startFeedPictures(item *models.AgentFeedItem, postingConversationId string) {
	hasWaiting := false
	for index := range item.FeedMedia {
		if item.FeedMedia[index].IsPictureWaiting() {
			hasWaiting = true
		}
	}
	if !hasWaiting {
		return
	}
	self.feedPictures.Add(1)
	go func() {
		defer self.feedPictures.Done()
		if err := self.fetchFeedPictures(item.ID, postingConversationId); err != nil {
			log.Warningf("cannot fetch the pictures of card %s: %s", item.ID, err)
		}
	}()
}

// WaitFeedPictures waits until the pictures being fetched are kept.
func (self *Agent) WaitFeedPictures() {
	self.feedPictures.Wait()
}

// feedPictureFetched is what fetching one page's picture came to.
type feedPictureFetched struct {
	mediaKey    string
	pageUrl     string
	imageFileId string
	siteName    string
	image       *linkpreview.Image
}

func (self *Agent) fetchFeedPictures(feedItemId, postingConversationId string) error {
	ctx, cancel := context.WithTimeout(context.Background(), feedPictureFetchTimeout)
	defer cancel()
	var waiting []models.FeedMedia
	isAllowed := map[string]bool{}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		item, err := tx.GetAgentFeedItem(feedItemId)
		if err != nil || item == nil {
			return err
		}
		for _, entry := range item.FeedMedia {
			if !entry.IsPictureWaiting() {
				continue
			}
			waiting = append(waiting, entry)
			// A page the model wrote the address of, rather than one a tool
			// answered with, could carry what it read in its query, and
			// fetching it would hand that to the page's site.
			isAllowed[entry.PageURL] = postingConversationId == ""
			if postingConversationId != "" {
				if isAllowed[entry.PageURL], err = tx.HasLinkInToolAnswer(postingConversationId, entry.PageURL); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if len(waiting) == 0 {
		return nil
	}

	results := make([]*feedPictureFetched, len(waiting))
	var group sync.WaitGroup
	turns := make(chan struct{}, feedPictureParallelFetches)
	for index, entry := range waiting {
		if !isAllowed[entry.PageURL] {
			results[index] = &feedPictureFetched{mediaKey: entry.MediaKey, pageUrl: entry.PageURL}
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			turns <- struct{}{}
			defer func() { <-turns }()
			results[index] = self.fetchFeedPicture(ctx, entry)
		}()
	}
	group.Wait()

	// The card may have changed meanwhile: a picture is kept only for an
	// entry that still waits for that page, and the rest are thrown away.
	isKept := map[string]bool{}
	if err := self.settings.Database.TransactionContext(context.Background(), func(tx db.Transaction) error {
		existing, err := tx.GetAgentFeedItem(feedItemId)
		if err != nil {
			return err
		}
		if existing == nil {
			return nil
		}
		updated, err := tx.UpdateAgentFeedItem(feedItemId, func(item *models.AgentFeedItem) error {
			for _, result := range results {
				for index := range item.FeedMedia {
					entry := &item.FeedMedia[index]
					if entry.MediaKey != result.mediaKey || entry.PageURL != result.pageUrl || !entry.IsPictureWaiting() {
						continue
					}
					if result.image == nil {
						entry.IsPictureMissing = true
						continue
					}
					entry.ImageFileID, entry.SiteName = result.imageFileId, result.siteName
					entry.WidthPixels, entry.HeightPixels = result.image.WidthPixels, result.image.HeightPixels
					isKept[result.imageFileId] = true
				}
			}
			return nil
		}, time.Now())
		if err != nil {
			return err
		}
		return self.announceFeedChange(tx, updated, models.FeedChangeReplaced)
	}); err != nil {
		isKept = map[string]bool{}
		defer self.deleteFeedPictures(unusedFeedPictures(results, isKept))
		return err
	}
	self.deleteFeedPictures(unusedFeedPictures(results, isKept))
	return nil
}

// unusedFeedPictures are the files fetched that no card shows.
func unusedFeedPictures(results []*feedPictureFetched, isKept map[string]bool) []string {
	var unused []string
	for _, result := range results {
		if result.imageFileId != "" && !isKept[result.imageFileId] {
			unused = append(unused, result.imageFileId)
		}
	}
	return unused
}

// fetchFeedPicture fetches the picture of one page, or the address itself
// when it is a picture rather than a page, and keeps it in the file store. A
// page without a picture comes back with no image.
func (self *Agent) fetchFeedPicture(ctx context.Context, entry models.FeedMedia) *feedPictureFetched {
	result := &feedPictureFetched{mediaKey: entry.MediaKey, pageUrl: entry.PageURL}
	imageUrl := entry.PageURL
	page, err := feedPictureFetcher.FetchPage(ctx, entry.PageURL)
	switch {
	case errors.Is(err, linkpreview.ErrNotHTML):
	case err != nil:
		log.Debugf("cannot read the page of a card's picture: %s", err)
		return result
	default:
		result.siteName = page.SiteName
		imageUrl = page.ImageURL
	}
	if imageUrl == "" {
		return result
	}
	image, err := feedPictureFetcher.FetchImageScaled(ctx, imageUrl, feedPictureWidthPixels)
	if err != nil {
		log.Debugf("cannot keep the picture of a card's page: %s", err)
		return result
	}
	imageFileId := fmt.Sprintf("feed-picture-%d-%s", time.Now().UnixNano(), entry.MediaKey)
	if err := self.settings.Storage.PutFile(ctx, imageFileId, image.Content); err != nil {
		log.Warningf("cannot keep the picture of a card: %s", err)
		return result
	}
	result.imageFileId, result.image = imageFileId, image
	return result
}
