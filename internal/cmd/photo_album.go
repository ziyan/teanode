package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Albums: named sets of photos, the person's own, their circles' and the ones
// shared with them. The same operations as the dashboard's Albums page and the
// agent's photo tool.

func newPhotoAlbumCommand() *cli.Command {
	return &cli.Command{
		Name:  "album",
		Usage: "named sets of your photos",
		Commands: []*cli.Command{
			{Name: "list", Usage: "the albums you reach, the most recently changed first: yours, your circles' and those shared with you", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumList},
			{
				Name: "read", Usage: "an album and its photos", ArgsUsage: "<album id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.IntFlag{Name: "limit", Value: defaultPhotoListSize, Usage: "photos to list, 1 to 500"},
					&cli.StringFlag{Name: "continue-after", Usage: "continue after this photo, the last one of the previous page"},
				},
				Action: runPhotoAlbumRead,
			},
			{
				Name: "create", Usage: "make an album, empty or with photos in it", ArgsUsage: "<title> [photo id]...",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "circle", Usage: "make it the album of a circle you are in, which every member adds photos to (a circle's id or name, from teanode circle list)"},
				},
				Action: runPhotoAlbumCreate,
			},
			{Name: "rename", Usage: "give an album another title", ArgsUsage: "<album id> <title>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumRename},
			{Name: "delete", Usage: "delete an album; its photos stay in your library", ArgsUsage: "<album id>", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoAlbumDelete},
			{Name: "add", Usage: "put photos in an album", ArgsUsage: "<album id> <photo id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumAdd},
			{Name: "remove", Usage: "take photos out of an album; they stay in your library", ArgsUsage: "<album id> <photo id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumRemove},
			{Name: "cover", Usage: "choose which photo of an album stands for it", ArgsUsage: "<album id> <photo id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumCover},
			{
				Name: "share", Usage: "share an album with a person or a circle you share, to view it or to add photos to it; sharing again changes what they have", ArgsUsage: "<album id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "person", Usage: "the username of somebody who shares a circle with you (teanode circle people lists them)"},
					&cli.StringFlag{Name: "circle", Usage: "a circle you are in, by id or name; everyone in it"},
					&cli.BoolFlag{Name: "contribute", Usage: "let them add photos of their own and take out the ones they added (otherwise they view and save)"},
					&cli.BoolFlag{Name: "with-location", Usage: "let them see where the photos were taken and download the original files (otherwise they get a picture that carries no place)"},
				},
				Action: runPhotoAlbumShare,
			},
			{
				Name: "share-update", Usage: "change one share of an album: what its people may do, and whether they see where the photos were taken; only what you name changes, the rest stays as the share has it", ArgsUsage: "<album id> <share id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "level", Usage: "view (they view and save) or contribute (they also add photos of their own and take out the ones they added)"},
					&cli.BoolFlag{Name: "with-location", Usage: "let them see where the photos were taken and download the original files"},
					&cli.BoolFlag{Name: "without-location", Usage: "show them a picture that carries no place, and no original files"},
				},
				Action: runPhotoAlbumShareUpdate,
			},
			{Name: "unshare", Usage: "take back one share of an album, with the photos its people put in it", ArgsUsage: "<share id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumUnshare},
			{Name: "shares", Usage: "who an album is shared with, and at what level", ArgsUsage: "<album id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumShares},
			{
				Name: "suggest", Usage: "trips and events among the photos that are in no album, newest first",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "after", Usage: "only photos taken on or after this date (2025-07-12 or an RFC 3339 time)"},
					&cli.StringFlag{Name: "before", Usage: "only photos taken before this date"},
				},
				Action: runPhotoAlbumSuggest,
			},
		},
	}
}

// photoDayText is a moment as the day it falls on in the shell's zone, which
// is how this command line reads a date.
func photoDayText(moment time.Time) string {
	return moment.Local().Format("2006-01-02")
}

// photoAlbumDatesText is the days an album's photos span: one day, or two.
func photoAlbumDatesText(album *models.PhotoAlbum) string {
	if album.FirstTakenAt == nil || album.LastTakenAt == nil {
		return ""
	}
	first, last := photoDayText(*album.FirstTakenAt), photoDayText(*album.LastTakenAt)
	if first == last {
		return first
	}
	return first + " to " + last
}

func runPhotoAlbumList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	albums, err := client.ListPhotoAlbums(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(albums)
	}
	if len(albums) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no albums yet")
		return nil
	}
	rows := make([][]string, 0, len(albums))
	for _, album := range albums {
		rows = append(rows, []string{album.ID, album.AlbumTitle, fmt.Sprint(album.PhotoCount), photoAlbumDatesText(album), photoAlbumOwnerText(album), photoAlbumLevelText(album)})
	}
	return printTable([]string{"id", "title", "photos", "dates", "owner", "level"}, rows)
}

// photoAlbumOwnerText is whose an album is, in words: "you", the person who
// shared it, or the circle it belongs to.
func photoAlbumOwnerText(album *models.PhotoAlbum) string {
	switch {
	case album.IsOwnedByCaller:
		return "you"
	case album.CircleID != nil:
		return forTerminal("circle " + album.CircleName)
	case album.OwnerName != "":
		return forTerminal(album.OwnerName)
	}
	return forTerminal(album.OwnerUsername)
}

// photoAlbumLevelText is what the caller may do with an album, in words.
func photoAlbumLevelText(album *models.PhotoAlbum) string {
	switch {
	case album.IsOwnedByCaller:
		return "owner"
	case album.ShareLevel != "":
		return string(album.ShareLevel)
	}
	return "view"
}

func runPhotoAlbumRead(ctx context.Context, command *cli.Command) error {
	photoAlbumId := command.Args().First()
	if photoAlbumId == "" {
		return usage("usage: teanode photo album read <album id>")
	}
	filter := &client.PhotoFilter{AlbumID: photoAlbumId, PageSize: int(command.Int("limit")), BeforePhotoID: command.String("continue-after")}
	if filter.PageSize < 1 || filter.PageSize > 500 {
		return usage("--limit must be from 1 to 500")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	album, err := client.ReadPhotoAlbum(ctx, connection, photoAlbumId)
	if err != nil {
		return describeError(command, err)
	}
	photos, err := client.ListPhotos(ctx, connection, filter)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"album": album, "photos": photos})
	}
	if err := printFields([][2]string{{"id", album.ID}, {"title", album.AlbumTitle}, {"photos", fmt.Sprint(album.PhotoCount)}, {"dates", photoAlbumDatesText(album)}, {"owner", photoAlbumOwnerText(album)}, {"level", photoAlbumLevelText(album)}}); err != nil {
		return err
	}
	if len(photos) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "\nno photos in it")
		return nil
	}
	_, _ = fmt.Fprintln(command.Writer)
	if err := printPhotoTable(photos); err != nil {
		return err
	}
	if len(photos) == filter.PageSize && album.PhotoCount > len(photos) {
		fmt.Fprintf(os.Stderr, "note: %d photos listed of %d in the album; add --continue-after %s for the next page\n", len(photos), album.PhotoCount, photos[len(photos)-1].ID)
	}
	return nil
}

// photoIdsOfArguments is the photos named after the first arguments, at most
// the server takes in one call.
func photoIdsOfArguments(command *cli.Command, skip int) ([]string, error) {
	photoIds := command.Args().Slice()[skip:]
	if len(photoIds) > client.MaximumPhotoIDs {
		return nil, usage(fmt.Sprintf("name at most %d photos at once", client.MaximumPhotoIDs))
	}
	return photoIds, nil
}

func printPhotoAlbum(command *cli.Command, album *models.PhotoAlbum, verb string) error {
	if command.Bool("json") {
		return PrintJSON(album)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %s (%s), %d photos\n", verb, forTerminal(album.AlbumTitle), album.ID, album.PhotoCount)
	return nil
}

func runPhotoAlbumCreate(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() == 0 {
		return usage("usage: teanode photo album create <title> [photo id]...")
	}
	albumTitle := command.Args().First()
	photoIds, err := photoIdsOfArguments(command, 1)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circleId := ""
	if circleNamed := strings.TrimSpace(command.String("circle")); circleNamed != "" {
		if circleId, _, err = photoCircleNamed(ctx, connection, circleNamed); err != nil {
			return describeError(command, err)
		}
	}
	album, err := client.CreateCirclePhotoAlbum(ctx, connection, circleId, albumTitle, photoIds)
	if err != nil {
		return describeError(command, err)
	}
	return printPhotoAlbum(command, album, "created")
}

// photoCircleNamed is the circle the person typed: by identifier or by name,
// among the circles they are in.
func photoCircleNamed(ctx context.Context, connection *client.Client, typed string) (string, string, error) {
	circles, err := client.ListCircles(ctx, connection)
	if err != nil {
		return "", "", err
	}
	for _, circle := range circles {
		if circle.ID == typed || strings.EqualFold(circle.Name, typed) {
			return circle.ID, circle.Name, nil
		}
	}
	return "", "", fmt.Errorf("%q is not a circle you are in; 'teanode circle list' lists them", typed)
}

// photoPersonNamed is the person the person typed: the username of somebody
// who shares a circle with them.
func photoPersonNamed(ctx context.Context, connection *client.Client, typed string) (string, string, error) {
	people, err := client.ListCirclePeople(ctx, connection)
	if err != nil {
		return "", "", err
	}
	for _, person := range people {
		if strings.EqualFold(person.Username, typed) {
			return person.UserID, person.Username, nil
		}
	}
	return "", "", fmt.Errorf("%q is not somebody you share a circle with; 'teanode circle people' lists them", typed)
}

func runPhotoAlbumShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode photo album share <album id> (--person <username> | --circle <circle>) [--contribute] [--with-location]")
	}
	personTyped, circleTyped := strings.TrimSpace(command.String("person")), strings.TrimSpace(command.String("circle"))
	if (personTyped == "") == (circleTyped == "") {
		return usage("share with one person (--person) or one circle (--circle)")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var granteeUserId, granteeCircleId, described string
	if personTyped != "" {
		granteeUserId, described, err = photoPersonNamed(ctx, connection, personTyped)
	} else {
		var circleName string
		granteeCircleId, circleName, err = photoCircleNamed(ctx, connection, circleTyped)
		described = "everyone in " + circleName
	}
	if err != nil {
		return describeError(command, err)
	}
	shareLevel := string(models.PhotoAlbumShareLevelView)
	if command.Bool("contribute") {
		shareLevel = string(models.PhotoAlbumShareLevelContribute)
	}
	share, err := client.SharePhotoAlbum(ctx, connection, command.Args().First(), granteeUserId, granteeCircleId, shareLevel, command.Bool("with-location"))
	if err != nil {
		return describeNotFound(command, err, "that album or "+described)
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	_, _ = fmt.Fprintf(command.Writer, "shared with %s at %s%s (share %s)\n", forTerminal(described), share.ShareLevel, photoShareLocationText(share), share.ID)
	return nil
}

// photoShareLocationText says what a share does about where photos were
// taken.
func photoShareLocationText(share *client.PhotoAlbumShare) string {
	if share.IsLocationIncluded {
		return ", with where the photos were taken"
	}
	return ", without where the photos were taken"
}

// runPhotoAlbumShareUpdate changes one share of an album, and only what the
// flags name. The server writes the level and the places together, so the share
// is read first and a setting the flags leave out is sent as the share has it:
// stopping somebody adding photos must not take the places away. A change that
// would leave the share as it is sends nothing.
func runPhotoAlbumShareUpdate(ctx context.Context, command *cli.Command) error {
	const usageText = "usage: teanode photo album share-update <album id> <share id> [--level view|contribute] [--with-location | --without-location]; 'teanode photo album shares' lists the ids"
	if command.Args().Len() != 2 {
		return usage(usageText)
	}
	shareLevel := strings.ToLower(strings.TrimSpace(command.String("level")))
	if shareLevel != "" && !models.PhotoAlbumShareLevel(shareLevel).IsValid() {
		return usage("--level is view or contribute, not " + command.String("level"))
	}
	if command.Bool("with-location") && command.Bool("without-location") {
		return usage("--with-location and --without-location say opposite things; give one")
	}
	if shareLevel == "" && !command.Bool("with-location") && !command.Bool("without-location") {
		return usage("name what to change: --level, --with-location or --without-location. " + usageText)
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	albumId, shareId := command.Args().Get(0), command.Args().Get(1)
	shares, err := client.ListPhotoAlbumShares(ctx, connection, albumId)
	if err != nil {
		return describeNotFound(command, err, "the album "+albumId)
	}
	var share *client.PhotoAlbumShare
	for _, listed := range shares {
		if listed.ID == shareId {
			share = listed
		}
	}
	if share == nil {
		return fmt.Errorf("the album has no share %s; 'teanode photo album shares %s' lists its shares", shareId, albumId)
	}
	isChanged := false
	if shareLevel != "" && shareLevel != string(share.ShareLevel) {
		isChanged = true
	} else {
		shareLevel = string(share.ShareLevel)
	}
	isLocationIncluded := share.IsLocationIncluded
	if command.Bool("with-location") || command.Bool("without-location") {
		if command.Bool("with-location") != isLocationIncluded {
			isChanged = true
		}
		isLocationIncluded = command.Bool("with-location")
	}
	if isChanged {
		if share, err = client.UpdatePhotoAlbumShare(ctx, connection, shareId, shareLevel, isLocationIncluded); err != nil {
			return describeNotFound(command, err, "the share "+shareId)
		}
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	with := share.GranteeUsername
	if share.GranteeCircleID != nil {
		with = "everyone in " + share.GranteeCircleName
	}
	if !isChanged {
		_, _ = fmt.Fprintf(command.Writer, "nothing changed: shared with %s at %s%s (share %s)\n", forTerminal(with), share.ShareLevel, photoShareLocationText(share), share.ID)
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "shared with %s at %s%s (share %s)\n", forTerminal(with), share.ShareLevel, photoShareLocationText(share), share.ID)
	return nil
}

func runPhotoAlbumShares(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode photo album shares <album id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shares, err := client.ListPhotoAlbumShares(ctx, connection, command.Args().First())
	if err != nil {
		return describeNotFound(command, err, "that album")
	}
	if command.Bool("json") {
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "the album is not shared with anybody")
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		with := share.GranteeUsername
		if share.GranteeCircleID != nil {
			with = "everyone in " + share.GranteeCircleName
		}
		location := "no"
		if share.IsLocationIncluded {
			location = "yes"
		}
		rows = append(rows, []string{forTerminal(with), share.ShareLevel, location, share.ID})
	}
	return printTable([]string{"with", "level", "places", "id"}, rows)
}

func runPhotoAlbumUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode photo album unshare <share id>; 'teanode photo album shares' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RemovePhotoAlbumShare(ctx, connection, command.Args().First()); err != nil {
		return describeNotFound(command, err, "the share "+command.Args().First())
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"photoAlbumShareId": command.Args().First(), "isRemoved": true})
	}
	_, _ = fmt.Fprintln(command.Writer, "no longer shared")
	return nil
}

func runPhotoAlbumRename(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode photo album rename <album id> <title>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	album, err := client.RenamePhotoAlbum(ctx, connection, command.Args().Get(0), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	return printPhotoAlbum(command, album, "renamed to")
}

func runPhotoAlbumDelete(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode photo album delete <album id>")
	}
	if err := confirm(command, "This deletes the album. Its photos stay in your library."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	photoAlbumId := command.Args().First()
	if err := client.DeletePhotoAlbum(ctx, connection, photoAlbumId); err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"photoAlbumId": photoAlbumId, "isDeleted": true})
	}
	_, _ = fmt.Fprintf(command.Writer, "deleted album %s\n", photoAlbumId)
	return nil
}

// runPhotoAlbumMove is add and remove: an album and a list of photos in, a
// count out.
func runPhotoAlbumMove(ctx context.Context, command *cli.Command, move func(context.Context, client.Executor, string, []string) (int, error), countName, verb string) error {
	if command.Args().Len() < 2 {
		return usage(fmt.Sprintf("usage: teanode photo album %s <album id> <photo id>...", command.Name))
	}
	photoIds, err := photoIdsOfArguments(command, 1)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	count, err := move(ctx, connection, command.Args().First(), photoIds)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]int{countName: count})
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %d of %d photos\n", verb, count, len(photoIds))
	return nil
}

func runPhotoAlbumAdd(ctx context.Context, command *cli.Command) error {
	return runPhotoAlbumMove(ctx, command, client.AddPhotosToAlbum, "addedCount", "added")
}

func runPhotoAlbumRemove(ctx context.Context, command *cli.Command) error {
	return runPhotoAlbumMove(ctx, command, client.RemovePhotosFromAlbum, "removedCount", "removed")
}

func runPhotoAlbumCover(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode photo album cover <album id> <photo id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	album, err := client.SetPhotoAlbumCover(ctx, connection, command.Args().Get(0), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(album)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s (%s) is shown by %s\n", forTerminal(album.AlbumTitle), album.ID, command.Args().Get(1))
	return nil
}

func runPhotoAlbumSuggest(ctx context.Context, command *cli.Command) error {
	takenAfter, err := parsePhotoDate("after", command.String("after"))
	if err != nil {
		return err
	}
	takenBefore, err := parsePhotoDate("before", command.String("before"))
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	suggestions, err := client.SuggestPhotoAlbums(ctx, connection, takenAfter, takenBefore)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(suggestions)
	}
	if len(suggestions) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no trips or events to suggest: they are runs of at least 12 photos that are in no album")
		return nil
	}
	rows := make([][]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		// The run ends a second before the time it is bounded by.
		lastDay := photoDayText(suggestion.TakenBefore.Add(-time.Second))
		dates := photoDayText(suggestion.TakenAfter)
		if lastDay != dates {
			dates += " to " + lastDay
		}
		rows = append(rows, []string{dates, strings.Join(suggestion.PlaceNames, "; "), suggestion.AlbumTitle, fmt.Sprint(suggestion.PhotoCount)})
	}
	return printTable([]string{"dates", "places", "title", "photos"}, rows)
}
