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

// Albums: named sets of the library's photos. The same operations as the
// dashboard's Albums page and the agent's photo tool.

func newPhotoAlbumCommand() *cli.Command {
	return &cli.Command{
		Name:  "album",
		Usage: "named sets of your photos",
		Commands: []*cli.Command{
			{Name: "list", Usage: "your albums, the most recently changed first", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumList},
			{
				Name: "show", Usage: "an album and its photos", ArgsUsage: "<album id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.IntFlag{Name: "limit", Value: defaultPhotoListSize, Usage: "photos to list, 1 to 500"},
					&cli.StringFlag{Name: "continue-after", Usage: "continue after this photo, the last one of the previous page"},
				},
				Action: runPhotoAlbumShow,
			},
			{Name: "create", Usage: "make an album, empty or with photos in it", ArgsUsage: "<title> [photo id]...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumCreate},
			{Name: "rename", Usage: "give an album another title", ArgsUsage: "<album id> <title>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumRename},
			{Name: "delete", Usage: "delete an album; its photos stay in your library", ArgsUsage: "<album id>", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoAlbumDelete},
			{Name: "add", Usage: "put photos in an album", ArgsUsage: "<album id> <photo id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumAdd},
			{Name: "remove", Usage: "take photos out of an album; they stay in your library", ArgsUsage: "<album id> <photo id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumRemove},
			{Name: "cover", Usage: "choose which photo of an album stands for it", ArgsUsage: "<album id> <photo id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoAlbumCover},
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
		rows = append(rows, []string{album.ID, album.AlbumTitle, fmt.Sprint(album.PhotoCount), photoAlbumDatesText(album)})
	}
	return printTable([]string{"id", "title", "photos", "dates"}, rows)
}

func runPhotoAlbumShow(ctx context.Context, command *cli.Command) error {
	photoAlbumId := command.Args().First()
	if photoAlbumId == "" {
		return usage("usage: teanode photo album show <album id>")
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
	if err := printFields([][2]string{{"id", album.ID}, {"title", album.AlbumTitle}, {"photos", fmt.Sprint(album.PhotoCount)}, {"dates", photoAlbumDatesText(album)}}); err != nil {
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
	album, err := client.CreatePhotoAlbum(ctx, connection, albumTitle, photoIds)
	if err != nil {
		return describeError(command, err)
	}
	return printPhotoAlbum(command, album, "created")
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
