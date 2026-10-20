package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A preview is saved per address, and saving the same address again replaces
// the row and says which picture file the old row named, so that the caller
// removes only the file nobody names any more.
func TestSavingALinkPreviewReplacesTheRowAndNamesTheOldPicture(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		now := time.Now()
		replaced, err := tx.SaveLinkPreview(&db.LinkPreview{
			URL: "https://example.com/garden-chair", Title: "Garden Chair", Description: "A chair.", SiteName: "Example Shop",
			ImageFileID: "link-preview-first", ImageSizeBytes: 9000, ImageWidthPixels: 600, ImageHeightPixels: 400, FetchedAt: now,
		})
		if err != nil || replaced != "" {
			t.Fatalf("the first save replaced %q: %v", replaced, err)
		}

		replaced, err = tx.SaveLinkPreview(&db.LinkPreview{
			URL: "https://example.com/garden-chair", Title: "Garden Chair, blue", ImageFileID: "link-preview-second", FetchedAt: now,
		})
		if err != nil || replaced != "link-preview-first" {
			t.Fatalf("the second save replaced %q (%v), want the first picture", replaced, err)
		}

		replaced, err = tx.SaveLinkPreview(&db.LinkPreview{
			URL: "https://example.com/garden-chair", Title: "Garden Chair, blue", ImageFileID: "link-preview-second", FetchedAt: now,
		})
		if err != nil || replaced != "" {
			t.Fatalf("saving with the same picture replaced %q: %v", replaced, err)
		}

		previews, err := tx.ListLinkPreviews([]string{"https://example.com/garden-chair", "https://example.com/never-fetched"})
		if err != nil {
			t.Fatalf("ListLinkPreviews: %s", err)
		}
		if len(previews) != 1 {
			t.Fatalf("ListLinkPreviews returned %d rows, want 1", len(previews))
		}
		found := previews["https://example.com/garden-chair"]
		if found == nil || found.Title != "Garden Chair, blue" || found.Description != "" || found.ImageWidthPixels != 0 {
			t.Errorf("the row is %+v: the second save did not replace the first", found)
		}
	})
}

func TestALinkPreviewIsFoundByItsPictureFile(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, preview := range []*db.LinkPreview{
			{URL: "https://example.com/with-picture", ImageFileID: "link-preview-one", FetchedAt: time.Now()},
			{URL: "https://example.com/without-picture", FetchedAt: time.Now()},
			{URL: "https://example.com/also-without-picture", FetchedAt: time.Now()},
		} {
			if _, err := tx.SaveLinkPreview(preview); err != nil {
				t.Fatalf("SaveLinkPreview: %s", err)
			}
		}
		found, err := tx.GetLinkPreviewByImageFile("link-preview-one")
		if err != nil || found == nil || found.URL != "https://example.com/with-picture" {
			t.Errorf("GetLinkPreviewByImageFile = %+v, %v", found, err)
		}
		if found, err := tx.GetLinkPreviewByImageFile("link-preview-missing"); err != nil || found != nil {
			t.Errorf("GetLinkPreviewByImageFile of a name nobody has = %+v, %v", found, err)
		}
		// Rows without a picture do not collide on the empty name, and the
		// empty name finds none of them.
		if found, err := tx.GetLinkPreviewByImageFile(""); err != nil || found != nil {
			t.Errorf("GetLinkPreviewByImageFile of the empty name = %+v, %v", found, err)
		}
	})
}

func TestOldLinkPreviewsAreListedOldestFirstAndDeleted(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		now := time.Now()
		for address, age := range map[string]time.Duration{
			"https://example.com/old":   90 * 24 * time.Hour,
			"https://example.com/older": 120 * 24 * time.Hour,
			"https://example.com/fresh": time.Hour,
		} {
			if _, err := tx.SaveLinkPreview(&db.LinkPreview{URL: address, FetchedAt: now.Add(-age)}); err != nil {
				t.Fatalf("SaveLinkPreview: %s", err)
			}
		}
		old, err := tx.ListLinkPreviewsFetchedBefore(now.Add(-30*24*time.Hour), 10)
		if err != nil {
			t.Fatalf("ListLinkPreviewsFetchedBefore: %s", err)
		}
		if len(old) != 2 || old[0].URL != "https://example.com/older" || old[1].URL != "https://example.com/old" {
			t.Fatalf("ListLinkPreviewsFetchedBefore = %+v, want older then old", old)
		}
		limited, err := tx.ListLinkPreviewsFetchedBefore(now.Add(-30*24*time.Hour), 1)
		if err != nil || len(limited) != 1 {
			t.Errorf("a limit of 1 returned %d rows: %v", len(limited), err)
		}

		if err := tx.DeleteLinkPreviews([]string{old[0].URL, old[1].URL}); err != nil {
			t.Fatalf("DeleteLinkPreviews: %s", err)
		}
		left, err := tx.ListLinkPreviews([]string{"https://example.com/old", "https://example.com/older", "https://example.com/fresh"})
		if err != nil || len(left) != 1 || left["https://example.com/fresh"] == nil {
			t.Errorf("after the delete %d rows are left (%v), want only the fresh one", len(left), err)
		}
	})
}
