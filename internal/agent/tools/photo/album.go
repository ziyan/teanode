package photo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Albums are named sets of the library's photos. The operations call the ones
// the dashboard's Albums page and teanode photo album call.

// albumDayText is a moment as the day it falls on in the person's zone.
func albumDayText(moment *time.Time, location *time.Location) string {
	if moment == nil {
		return ""
	}
	return moment.In(location).Format("2006-01-02")
}

// albumRow is an album as the agent is told of it. The title is the person's
// own words, so an answer that carries it is untrusted.
func albumRow(album *models.PhotoAlbum, location *time.Location) map[string]any {
	return map[string]any{
		"album_id": album.ID, "album_title": album.AlbumTitle, "photo_count": album.PhotoCount,
		"first_taken_on": albumDayText(album.FirstTakenAt, location), "last_taken_on": albumDayText(album.LastTakenAt, location),
	}
}

func albumIdOf(asked *arguments, operation string) (string, error) {
	albumId := strings.TrimSpace(asked.AlbumID)
	if albumId == "" {
		return "", fmt.Errorf("%s needs album_id; album-list gives them", operation)
	}
	return albumId, nil
}

func albumList(ctx context.Context, current tools.Run) (*tools.Result, error) {
	albums, err := client.ListPhotoAlbums(ctx, current.Operations())
	if err != nil {
		return nil, err
	}
	location := tools.Location(current.Owner())
	rows := make([]map[string]any, 0, len(albums))
	for _, album := range albums {
		rows = append(rows, albumRow(album, location))
	}
	result, err := tools.JSONResult(map[string]any{"albums": rows, "note": "list with album_id gives an album's photos"})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

func albumRead(ctx context.Context, current tools.Run, asked *arguments) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, "album-read")
	if err != nil {
		return nil, err
	}
	album, err := client.ReadPhotoAlbum(ctx, current.Operations(), albumId)
	if err != nil {
		return nil, err
	}
	details := albumRow(album, tools.Location(current.Owner()))
	details["created_at"] = album.CreatedAt.In(tools.Location(current.Owner())).Format("2006-01-02 15:04")
	details["note"] = "list with album_id gives the album's photos; photos in the Trash are not counted until they are restored"
	result, err := tools.JSONResult(details)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

func albumCreate(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	albumTitle := strings.TrimSpace(asked.AlbumTitle)
	if albumTitle == "" {
		return nil, errors.New("album-create needs album_title")
	}
	var photoIds []string
	if len(asked.PhotoIDs) > 0 || strings.TrimSpace(asked.PhotoID) != "" {
		var err error
		if photoIds, err = photoIdsOf(asked, "album-create", maximumAlbumPhotoCount); err != nil {
			return nil, err
		}
	}
	album, err := client.CreatePhotoAlbum(ctx, executor, albumTitle, photoIds)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{"album_id": album.ID, "album_title": album.AlbumTitle, "photo_count": album.PhotoCount})
	if err != nil {
		return nil, err
	}
	result.Note = "made the album " + album.AlbumTitle
	return result, nil
}

func albumRename(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, "album-rename")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(asked.AlbumTitle) == "" {
		return nil, errors.New("album-rename needs album_title")
	}
	album, err := client.RenamePhotoAlbum(ctx, executor, albumId, asked.AlbumTitle)
	if err != nil {
		return nil, err
	}
	return tools.TextResult("%s is now called %s", album.ID, album.AlbumTitle), nil
}

func albumDelete(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, "album-delete")
	if err != nil {
		return nil, err
	}
	if err := client.DeletePhotoAlbum(ctx, executor, albumId); err != nil {
		return nil, err
	}
	return tools.TextResult("deleted the album %s; its photos are still in the library", albumId), nil
}

// albumMove is album-add and album-remove: an album and a list of photos in, a
// count out.
func albumMove(ctx context.Context, executor client.Executor, asked *arguments, change func(context.Context, client.Executor, string, []string) (int, error), done string) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, asked.Operation)
	if err != nil {
		return nil, err
	}
	photoIds, err := photoIdsOf(asked, asked.Operation, maximumAlbumPhotoCount)
	if err != nil {
		return nil, err
	}
	count, err := change(ctx, executor, albumId, photoIds)
	if err != nil {
		return nil, err
	}
	return tools.TextResult("%d of %d photos %s the album", count, len(photoIds), done), nil
}

// albumSuggest offers the trips and events among photos in no album. The
// dates it gives are exact: listing between them gives the run's photos.
func albumSuggest(ctx context.Context, current tools.Run, asked *arguments) (*tools.Result, error) {
	takenAfter, takenBefore, err := takenRangeOf(asked, tools.Location(current.Owner()))
	if err != nil {
		return nil, err
	}
	suggestions, err := client.SuggestPhotoAlbums(ctx, current.Operations(), takenAfter, takenBefore)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(suggestions))
	for _, suggestion := range suggestions {
		rows = append(rows, map[string]any{
			"album_title": suggestion.AlbumTitle, "photo_count": suggestion.PhotoCount, "place_names": suggestion.PlaceNames,
			"taken_after": suggestion.TakenAfter.Format(time.RFC3339), "taken_before": suggestion.TakenBefore.Format(time.RFC3339),
			"sample_photo_ids": suggestion.SamplePhotoIDs,
		})
	}
	answer := map[string]any{"suggestions": rows}
	if len(rows) == 0 {
		answer["note"] = "nothing to suggest: a trip or an event is a run of at least 12 photos, taken without a gap of more than 6 hours, that are in no album"
	} else {
		answer["note"] = "nothing is created yet. If the person agrees, list the photos with taken_after and taken_before as given, then album-create with the title they choose and those photo_ids"
	}
	return tools.JSONResult(answer)
}

// albumDeletePreview names the album on the card that asks before it is
// deleted, which is the one card here the person must read: an identifier
// would mean nothing to them. Any other operation is described by previewOf.
func albumDeletePreview(ctx context.Context, arguments json.RawMessage) string {
	var asked struct {
		Operation string `json:"operation"`
		AlbumID   string `json:"album_id"`
	}
	if err := json.Unmarshal(arguments, &asked); err != nil || strings.ToLower(strings.TrimSpace(asked.Operation)) != "album-delete" {
		return ""
	}
	named := "an album"
	if run, err := tools.RunFrom(ctx); err == nil && strings.TrimSpace(asked.AlbumID) != "" {
		if album, err := client.ReadPhotoAlbum(ctx, run.Operations(), strings.TrimSpace(asked.AlbumID)); err == nil {
			named = tools.Named(album.AlbumTitle, "an album")
		}
	}
	return "Delete " + named + "; its photos stay in the library"
}
