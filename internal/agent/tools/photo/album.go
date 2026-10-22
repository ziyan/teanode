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
	row := map[string]any{
		"album_id": album.ID, "album_title": album.AlbumTitle, "photo_count": album.PhotoCount,
		"first_taken_on": albumDayText(album.FirstTakenAt, location), "last_taken_on": albumDayText(album.LastTakenAt, location),
	}
	// Whose it is and what the person may do with it: an album that is not
	// theirs is read where it is, and nothing in it is theirs to keep until
	// they ask.
	switch {
	case album.IsOwnedByCaller:
		row["level"] = "owner"
	case album.CircleID != nil:
		row["circle_name"], row["level"] = album.CircleName, string(album.ShareLevel)
	default:
		row["owner_name"], row["level"] = album.OwnerName, string(album.ShareLevel)
	}
	if !album.IsOwnedByCaller && album.IsLocationIncluded {
		row["shows_where_photos_were_taken"] = true
	}
	return row
}

func albumIdOf(asked *arguments, operation string) (string, error) {
	albumId := strings.TrimSpace(asked.AlbumID)
	if albumId == "" {
		return "", fmt.Errorf("%s needs album_id; album-list gives them", operation)
	}
	return albumId, nil
}

func albumList(ctx context.Context, current tools.Run, asked *arguments) (*tools.Result, error) {
	albums, err := client.ListPhotoAlbums(ctx, current.Operations())
	if err != nil {
		return nil, err
	}
	location := tools.Location(current.Owner())
	rows := make([]map[string]any, 0, len(albums))
	isInChat := tools.IsAnsweringInChat(current)
	for _, album := range albums {
		// In a chat only the person's own albums are listed: the others are
		// what people shared with the person, not with the room.
		if isInChat && !album.IsOwnedByCaller {
			continue
		}
		rows = append(rows, albumRow(album, location))
	}
	answer := map[string]any{"note": "album-read with album_id (or list with album_id) gives an album's photos, in an album shared with the person too; an album with an owner_name or circle_name is not the person's own"}
	page, err := pageOfRows(rows, asked, answer)
	if err != nil {
		return nil, err
	}
	answer["albums"] = page
	result, err := tools.JSONResult(answer)
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
	if tools.IsAnsweringInChat(current) && !album.IsOwnedByCaller {
		return nil, errOthersPhotosInChat
	}
	details := albumRow(album, tools.Location(current.Owner()))
	details["created_at"] = album.CreatedAt.In(tools.Location(current.Owner())).Format("2006-01-02 15:04")
	details["note"] = "the album's photos follow, a page at a time; photos in the Trash are not counted until they are restored"
	// The album's photos are the list the answer pages: the rows list gives for
	// album_id, from offset, and next_offset when there are more.
	listed := *asked
	listed.AlbumID = albumId
	filter, err := filterOf(&listed, tools.Location(current.Owner()))
	if err != nil {
		return nil, err
	}
	photos, err := client.ListPhotos(ctx, current.Operations(), filter)
	if err != nil {
		return nil, err
	}
	details["photos"] = listedRows(photos)
	pagePhotos(ctx, current.Operations(), filter, len(photos), details)
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
	circleId := ""
	if circleTyped := strings.TrimSpace(asked.Circle); circleTyped != "" {
		var err error
		if circleId, _, err = circleNamed(ctx, executor, circleTyped); err != nil {
			return nil, err
		}
	}
	album, err := client.CreateCirclePhotoAlbum(ctx, executor, circleId, albumTitle, photoIds)
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

// albumCover chooses which photo of an album stands for it; the server
// refuses a photo that is not in the album.
func albumCover(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, "album-cover")
	if err != nil {
		return nil, err
	}
	photoId := strings.TrimSpace(asked.PhotoID)
	if photoId == "" {
		return nil, errors.New("album-cover needs photo_id, a photo that is in the album; list with album_id gives them")
	}
	album, err := client.SetPhotoAlbumCover(ctx, executor, albumId, photoId)
	if err != nil {
		return nil, err
	}
	return tools.TextResult("%s now shows photo %s as its cover", album.ID, photoId), nil
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

// albumCardPreview names the album, and for a change of a share the person,
// on the cards the person must read before they answer them: deleting an
// album, sharing one and changing a share. An identifier would mean nothing to
// them. Any other operation is described by previewOf, and so is a change of a
// share whose album or share cannot be read here.
func albumCardPreview(ctx context.Context, callArguments json.RawMessage) string {
	var asked struct {
		Operation string `json:"operation"`
		AlbumID   string `json:"album_id"`
		ShareID   string `json:"share_id"`
		Person    string `json:"person"`
		Circle    string `json:"circle"`
		Level     string `json:"share_level"`
		Location  *bool  `json:"is_location_included"`
	}
	if err := json.Unmarshal(callArguments, &asked); err != nil {
		return ""
	}
	operation := strings.ToLower(strings.TrimSpace(asked.Operation))
	if operation != "album-delete" && operation != "album-share" && operation != "album-share-update" {
		return ""
	}
	named := "an album"
	var run tools.Run
	if current, err := tools.RunFrom(ctx); err == nil && strings.TrimSpace(asked.AlbumID) != "" {
		run = current
		if album, err := client.ReadPhotoAlbum(ctx, run.Operations(), strings.TrimSpace(asked.AlbumID)); err == nil {
			named = tools.Named(album.AlbumTitle, "an album")
		}
	}
	if operation == "album-delete" {
		return "Delete " + named + "; its photos stay in the library"
	}
	if operation == "album-share-update" {
		if run == nil || strings.TrimSpace(asked.ShareID) == "" {
			return ""
		}
		share, err := shareOf(ctx, run.Operations(), strings.TrimSpace(asked.AlbumID), strings.TrimSpace(asked.ShareID))
		if err != nil {
			return ""
		}
		after, err := settingsAfter(share, &arguments{ShareLevel: asked.Level, IsLocationIncluded: asked.Location})
		if err != nil {
			return ""
		}
		return shareUpdateCardText(named, share, after)
	}
	with := tools.Named(asked.Person, "")
	if with == "" {
		with = "everyone in " + tools.Named(asked.Circle, "one of their circles")
	}
	level := "view"
	if strings.ToLower(strings.TrimSpace(asked.Level)) == string(models.PhotoAlbumShareLevelContribute) {
		level = "view and add photos"
	}
	text := "Share " + named + " with " + with + ", to " + level
	if asked.Location != nil && *asked.Location {
		text += ", including where the photos were taken"
	}
	return text
}

// circleNamed is the circle the person's agent was told of: by identifier or
// by name, among the circles the person is in.
func circleNamed(ctx context.Context, executor client.Executor, typed string) (string, string, error) {
	circles, err := client.ListCircles(ctx, executor)
	if err != nil {
		return "", "", err
	}
	for _, circle := range circles {
		if circle.ID == typed || strings.EqualFold(circle.Name, typed) {
			return circle.ID, circle.Name, nil
		}
	}
	return "", "", fmt.Errorf("%q is not a circle the person is in; circle list gives them", typed)
}

// personNamed is the person the agent was told to share with: by username,
// among the people who share a circle with the person.
func personNamed(ctx context.Context, executor client.Executor, typed string) (string, string, error) {
	people, err := client.ListCirclePeople(ctx, executor)
	if err != nil {
		return "", "", err
	}
	for _, person := range people {
		if strings.EqualFold(person.Username, typed) {
			return person.UserID, person.Username, nil
		}
	}
	return "", "", fmt.Errorf("%q does not share a circle with the person; circle_people gives the people they can share with", typed)
}

// albumShare shares one of the person's albums. The card that asked first says
// whom with and how far, because this decides who may look at the person's
// photos.
func albumShare(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, "album-share")
	if err != nil {
		return nil, err
	}
	person, circle := strings.TrimSpace(asked.Person), strings.TrimSpace(asked.Circle)
	if (person == "") == (circle == "") {
		return nil, errors.New("album-share needs person (a username) or circle (a name or id), not both and not neither")
	}
	shareLevel := strings.ToLower(strings.TrimSpace(asked.ShareLevel))
	if shareLevel == "" {
		shareLevel = string(models.PhotoAlbumShareLevelView)
	}
	if !models.PhotoAlbumShareLevel(shareLevel).IsValid() {
		return nil, fmt.Errorf("share_level is view or contribute, not %q", asked.ShareLevel)
	}
	var granteeUserId, granteeCircleId, sharedWith string
	if person != "" {
		granteeUserId, sharedWith, err = personNamed(ctx, executor, person)
	} else {
		var circleName string
		granteeCircleId, circleName, err = circleNamed(ctx, executor, circle)
		sharedWith = "everyone in " + circleName
	}
	if err != nil {
		return nil, err
	}
	isLocationIncluded := asked.IsLocationIncluded != nil && *asked.IsLocationIncluded
	share, err := client.SharePhotoAlbum(ctx, executor, albumId, granteeUserId, granteeCircleId, shareLevel, isLocationIncluded)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{
		"share_id": share.ID, "shared_with": sharedWith, "share_level": share.ShareLevel, "is_location_included": share.IsLocationIncluded,
	})
	if err != nil {
		return nil, err
	}
	result.Note = "shared an album with " + sharedWith
	return result, nil
}

// shareSettings is what a share lets the people it names do.
type shareSettings struct {
	shareLevel         string
	isLocationIncluded bool
}

// settingsOf is the settings a share has now.
func settingsOf(share *client.PhotoAlbumShare) shareSettings {
	return shareSettings{shareLevel: string(share.ShareLevel), isLocationIncluded: share.IsLocationIncluded}
}

// settingsAfter is the settings a share has once a call is done. A setting the
// call does not name stays as the share has it: both are written together,
// so filling in the one that was left out is what keeps "stop them adding
// photos" from also taking the places away.
func settingsAfter(share *client.PhotoAlbumShare, asked *arguments) (shareSettings, error) {
	after := settingsOf(share)
	if shareLevel := strings.ToLower(strings.TrimSpace(asked.ShareLevel)); shareLevel != "" {
		if !models.PhotoAlbumShareLevel(shareLevel).IsValid() {
			return after, fmt.Errorf("share_level is view or contribute, not %q", asked.ShareLevel)
		}
		after.shareLevel = shareLevel
	}
	if asked.IsLocationIncluded != nil {
		after.isLocationIncluded = *asked.IsLocationIncluded
	}
	return after, nil
}

// shareOf is the share a call names, among the shares of its album.
func shareOf(ctx context.Context, executor client.Executor, albumId, shareId string) (*client.PhotoAlbumShare, error) {
	shares, err := client.ListPhotoAlbumShares(ctx, executor, albumId)
	if err != nil {
		return nil, err
	}
	for _, share := range shares {
		if share.ID == shareId {
			return share, nil
		}
	}
	return nil, fmt.Errorf("the album has no share %q; album-shares gives the shares of an album", shareId)
}

// granteeText is who a share is with, as the person knows them. Names are
// what other people called themselves.
func granteeText(share *client.PhotoAlbumShare) string {
	if share.GranteeCircleID != nil {
		return "everyone in " + tools.Named(share.GranteeCircleName, "a circle")
	}
	return tools.Named(share.GranteeName, tools.Named(share.GranteeUsername, "somebody"))
}

// shareSettingsText is " to ..." and the settings a change names, in the
// words the cards use. A setting that is not named is left out.
func shareSettingsText(isLevelNamed bool, shareLevel string, isLocationNamed, isLocationIncluded bool) string {
	var parts []string
	if isLevelNamed {
		if strings.EqualFold(strings.TrimSpace(shareLevel), string(models.PhotoAlbumShareLevelContribute)) {
			parts = append(parts, "view and add photos")
		} else {
			parts = append(parts, "view only")
		}
	}
	if isLocationNamed {
		if isLocationIncluded {
			parts = append(parts, "show where the photos were taken")
		} else {
			parts = append(parts, "hide where the photos were taken")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " to " + strings.Join(parts, ", and ")
}

// shareUpdateCardText is the card for album-share-update once the share and the
// album are known: whose share of which album, what it becomes, and what stays.
func shareUpdateCardText(albumText string, share *client.PhotoAlbumShare, after shareSettings) string {
	before := settingsOf(share)
	text := "Change the share of " + albumText + " with " + granteeText(share)
	if after == before {
		return text + ": it is already like that, so nothing changes"
	}
	isLevelChanged := after.shareLevel != before.shareLevel
	isLocationChanged := after.isLocationIncluded != before.isLocationIncluded
	text += shareSettingsText(isLevelChanged, after.shareLevel, isLocationChanged, after.isLocationIncluded)
	if isLevelChanged && !isLocationChanged && after.isLocationIncluded {
		text += ", keeping where the photos were taken"
	}
	return text
}

// albumShareUpdate changes one share, by its id and its album: only what the
// call names. UpdatePhotoAlbumShare writes the level and the places together,
// as the dashboard's sheet of shares does, so the share is read first and the
// setting left out is sent as it is. A call that would change nothing makes no
// request.
//
// The question the person is asked before a call comes from riskOf, which sees
// only the arguments, so it asks whenever the call names contribute or places
// on, even for a share that has them already. The card, made by
// albumCardPreview, reads the share and says what really changes.
func albumShareUpdate(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, "album-share-update")
	if err != nil {
		return nil, err
	}
	shareId := strings.TrimSpace(asked.ShareID)
	if shareId == "" {
		return nil, errors.New("album-share-update needs share_id; album-shares gives them")
	}
	if strings.TrimSpace(asked.ShareLevel) == "" && asked.IsLocationIncluded == nil {
		return nil, errors.New("album-share-update needs share_level (view or contribute), is_location_included, or both: it changes only what is named")
	}
	share, err := shareOf(ctx, executor, albumId, shareId)
	if err != nil {
		return nil, err
	}
	after, err := settingsAfter(share, asked)
	if err != nil {
		return nil, fmt.Errorf("album-share-update: %w", err)
	}
	if after == settingsOf(share) {
		result := tools.TextResult("nothing changed: the share with %s already has share_level %s and is_location_included %t", granteeText(share), after.shareLevel, after.isLocationIncluded)
		result.Untrusted = true
		return result, nil
	}
	share, err = client.UpdatePhotoAlbumShare(ctx, executor, shareId, after.shareLevel, after.isLocationIncluded)
	if err != nil {
		return nil, err
	}
	row := map[string]any{"share_id": share.ID, "share_level": share.ShareLevel, "is_location_included": share.IsLocationIncluded}
	if share.GranteeCircleID != nil {
		row["circle_name"] = share.GranteeCircleName
	} else {
		row["person"], row["person_name"] = share.GranteeUsername, share.GranteeName
	}
	result, err := tools.JSONResult(row)
	if err != nil {
		return nil, err
	}
	// Names are what other people called themselves.
	result.Untrusted = true
	result.Note = "changed a share of an album"
	return result, nil
}

func albumUnshare(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	shareId := strings.TrimSpace(asked.ShareID)
	if shareId == "" {
		return nil, errors.New("album-unshare needs share_id; album-shares gives them")
	}
	if err := client.RemovePhotoAlbumShare(ctx, executor, shareId); err != nil {
		return nil, err
	}
	return tools.TextResult("stopped sharing; the photos the other person put in the album left it with their access"), nil
}

func albumShares(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	albumId, err := albumIdOf(asked, "album-shares")
	if err != nil {
		return nil, err
	}
	shares, err := client.ListPhotoAlbumShares(ctx, executor, albumId)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(shares))
	for _, share := range shares {
		row := map[string]any{"share_id": share.ID, "share_level": share.ShareLevel, "is_location_included": share.IsLocationIncluded}
		if share.GranteeCircleID != nil {
			row["circle_name"] = share.GranteeCircleName
		} else {
			row["person"], row["person_name"] = share.GranteeUsername, share.GranteeName
		}
		rows = append(rows, row)
	}
	// Names are what other people called themselves.
	answer := map[string]any{}
	page, err := pageOfRows(rows, asked, answer)
	if err != nil {
		return nil, err
	}
	answer["shares"] = page
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

// saveShared keeps photos of a shared album in the person's own library, at
// the person's asking. What is kept is what the person could see: without the
// place unless the share included it.
func saveShared(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	photoIds, err := photoIdsOf(asked, "save-shared", maximumSaveSharedPhotoCount)
	if err != nil {
		return nil, err
	}
	rows := make([]string, 0, len(photoIds))
	savedCount := 0
	for _, photoId := range photoIds {
		saved, err := client.SavePhotoToLibrary(ctx, executor, photoId)
		if err != nil {
			rows = append(rows, photoId+": "+err.Error())
			continue
		}
		savedCount++
		rows = append(rows, photoId+": kept as "+saved.ID)
	}
	result := tools.TextResult("%s", strings.Join(rows, "\n"))
	result.Note = fmt.Sprintf("kept %d of %d photos in the library", savedCount, len(photoIds))
	return result, nil
}
