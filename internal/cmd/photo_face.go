package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// People: the faces found in the library, grouped by person, and named by
// choosing a contact. The same operations as the dashboard's People page and
// the agent's photo tool.

func newPhotoPeopleCommand() *cli.Command {
	return &cli.Command{
		Name:  "people",
		Usage: "the people found in your photos, and who they are",
		Commands: []*cli.Command{
			{
				Name: "list", Usage: "the people found in your photos: those named first, by name, then groups of at least three faces nobody has named",
				Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "hidden", Usage: "list the people you hid as well"}}, Action: runPhotoPeopleList,
			},
			{
				Name: "show", Usage: "a person and their photos", ArgsUsage: "<group id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.IntFlag{Name: "limit", Value: defaultPhotoListSize, Usage: "photos to list, 1 to 500"},
					&cli.StringFlag{Name: "continue-after", Usage: "continue after this photo, the last one of the previous page"},
				},
				Action: runPhotoPeopleShow,
			},
			{Name: "name", Usage: "say who a group of faces is, by a contact from your address book (teanode contact list gives their ids); a contact that names another group puts the two together, and a contact with no picture is given a face as one", ArgsUsage: "<group id> <contact id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoPeopleName},
			{Name: "unname", Usage: "take the contact off a group of faces", ArgsUsage: "<group id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoPeopleUnname},
			{Name: "merge", Usage: "put groups of faces into the first: they are one person", ArgsUsage: "<group id> <group id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoPeopleMerge},
			{Name: "remove", Usage: "take a face out of its group: it is not them", ArgsUsage: "<face id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoPeopleRemove},
			{Name: "hide", Usage: "leave a group out of the people list", ArgsUsage: "<group id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoPeopleHide},
			{Name: "unhide", Usage: "list a hidden group again", ArgsUsage: "<group id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoPeopleHide},
		},
	}
}

func newPhotoFacesCommand() *cli.Command {
	return &cli.Command{
		Name:  "faces",
		Usage: "whether faces are found in your photos",
		Commands: []*cli.Command{
			{Name: "on", Usage: "find faces in your photos, the ones already here as well", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoFaces},
			{Name: "off", Usage: "stop finding faces, and delete every face and group of people found so far", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoFaces},
		},
	}
}

// photoPersonText is how a group of faces is called: by its contact, with
// "(you)" after the name when the contact is your own card, or, for a group
// nobody has named, by nothing.
func photoPersonText(group *models.PhotoFaceGroup) string {
	if group.ContactName != "" {
		return group.ContactName + yourselfText(group.IsOwnContact)
	}
	return ""
}

// yourselfText follows a name that is the person's own contact.
func yourselfText(isOwnContact bool) string {
	if isOwnContact {
		return " (you)"
	}
	return ""
}

func printPhotoFaceGroup(command *cli.Command, group *models.PhotoFaceGroup, verb string) error {
	if command.Bool("json") {
		return PrintJSON(group)
	}
	name := photoPersonText(group)
	if name == "" {
		name = "a group of faces nobody has named"
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %s (%s), %d faces in %d photos\n", verb, forTerminal(name), group.ID, group.FaceCount, group.PhotoCount)
	return nil
}

func runPhotoPeopleList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	groups, err := client.ListPhotoFaceGroups(ctx, connection, command.Bool("hidden"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(groups)
	}
	if len(groups) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no people yet: faces are found as photos are processed, and a group needs three faces to be listed until you name it")
		return nil
	}
	rows := make([][]string, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, []string{group.ID, photoPersonText(group), fmt.Sprint(group.FaceCount), fmt.Sprint(group.PhotoCount), group.ContactID, hiddenText(group)})
	}
	return printTable([]string{"id", "name", "faces", "photos", "contact", "hidden"}, rows)
}

func hiddenText(group *models.PhotoFaceGroup) string {
	if group.IsHidden {
		return "hidden"
	}
	return ""
}

func runPhotoPeopleShow(ctx context.Context, command *cli.Command) error {
	photoFaceGroupId := command.Args().First()
	if photoFaceGroupId == "" {
		return usage("usage: teanode photo people show <group id>")
	}
	filter := &client.PhotoFilter{FaceGroupID: photoFaceGroupId, PageSize: int(command.Int("limit")), BeforePhotoID: command.String("continue-after")}
	if filter.PageSize < 1 || filter.PageSize > 500 {
		return usage("--limit must be from 1 to 500")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := client.ReadPhotoFaceGroup(ctx, connection, photoFaceGroupId)
	if err != nil {
		return describeError(command, err)
	}
	photos, err := client.ListPhotos(ctx, connection, filter)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"person": group, "photos": photos})
	}
	if err := printFields([][2]string{
		{"id", group.ID}, {"name", photoPersonText(group)}, {"contact", group.ContactID},
		{"faces", fmt.Sprint(group.FaceCount)}, {"photos", fmt.Sprint(group.PhotoCount)}, {"hidden", yesNo(group.IsHidden)},
	}); err != nil {
		return err
	}
	if len(photos) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "\nno photos of them")
		return nil
	}
	_, _ = fmt.Fprintln(command.Writer)
	if err := printPhotoTable(photos); err != nil {
		return err
	}
	if len(photos) == filter.PageSize && group.PhotoCount > len(photos) {
		fmt.Fprintf(os.Stderr, "note: %d photos listed of %d; add --continue-after %s for the next page\n", len(photos), group.PhotoCount, photos[len(photos)-1].ID)
	}
	return nil
}

func runPhotoPeopleName(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode photo people name <group id> <contact id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	named, err := client.NamePhotoFaceGroup(ctx, connection, command.Args().Get(0), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(named)
	}
	if err := printPhotoFaceGroup(command, named.PhotoFaceGroup, "named"); err != nil {
		return err
	}
	// A contact names one person: a contact that named another group already
	// puts the two together, and a contact without a picture is given a face.
	if named.MergedPhotoFaceGroupID != "" {
		_, _ = fmt.Fprintf(command.Writer, "merged with %s\n", forTerminal(named.PhotoFaceGroup.ContactName))
	}
	if named.IsContactPictureSet {
		_, _ = fmt.Fprintf(command.Writer, "used a face as their picture (teanode contact remove-picture %s takes it off)\n", named.PhotoFaceGroup.ContactID)
	}
	return nil
}

func runPhotoPeopleUnname(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode photo people unname <group id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := client.UnnamePhotoFaceGroup(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	return printPhotoFaceGroup(command, group, "unnamed")
}

func runPhotoPeopleMerge(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("usage: teanode photo people merge <group id> <group id>...")
	}
	if command.Args().Len() > client.MaximumMergedPhotoFaceGroups {
		return usage(fmt.Sprintf("name at most %d groups at once", client.MaximumMergedPhotoFaceGroups))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := client.MergePhotoFaceGroups(ctx, connection, command.Args().Slice())
	if err != nil {
		return describeError(command, err)
	}
	return printPhotoFaceGroup(command, group, "merged into")
}

func runPhotoPeopleRemove(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode photo people remove <face id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := client.RemovePhotoFaceFromGroup(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	return printPhotoFaceGroup(command, group, "the face is now alone in")
}

// runPhotoPeopleHide is hide and unhide: the command's own name says which.
func runPhotoPeopleHide(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage(fmt.Sprintf("usage: teanode photo people %s <group id>", command.Name))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isHidden := command.Name == "hide"
	group, err := client.HidePhotoFaceGroup(ctx, connection, command.Args().First(), isHidden)
	if err != nil {
		return describeError(command, err)
	}
	if isHidden {
		return printPhotoFaceGroup(command, group, "hid")
	}
	return printPhotoFaceGroup(command, group, "listing again")
}

// runPhotoFaces is faces on and faces off. Off deletes what was found, so it
// asks first.
func runPhotoFaces(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage(fmt.Sprintf("usage: teanode photo faces %s", command.Name))
	}
	isEnabled := command.Name == "on"
	if !isEnabled {
		if err := confirm(command, "This deletes every face found in your photos and every group of people. Turning faces on again finds them again."); err != nil {
			return err
		}
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isNowEnabled, err := client.SetPhotoFaceRecognition(ctx, connection, isEnabled)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]bool{"isPhotoFaceRecognitionEnabled": isNowEnabled})
	}
	if isNowEnabled {
		_, _ = fmt.Fprintln(command.Writer, "finding faces in your photos: they appear as the photos are read, a few at a time")
		return nil
	}
	_, _ = fmt.Fprintln(command.Writer, "no longer finding faces; the faces and groups found so far are deleted")
	return nil
}

// photoFaceText is a face in a photo as the details of the photo list it: the
// contact's name, or that nobody has named the group, with the group to name.
func photoFaceText(face *models.PhotoFace) string {
	switch {
	case face.ContactName != "":
		return fmt.Sprintf("%s%s (group %s, face %s)", face.ContactName, yourselfText(face.IsOwnContact), face.PhotoFaceGroupID, face.ID)
	case face.PhotoFaceGroupID != "":
		return fmt.Sprintf("not named (group %s, face %s)", face.PhotoFaceGroupID, face.ID)
	}
	return fmt.Sprintf("a face too small or too unsure to group (face %s)", face.ID)
}

// photoFaceRecognitionText is whether faces are being found, in the words of
// the dashboard's People page.
func photoFaceRecognitionText(library *client.PhotoLibrary) string {
	switch {
	case !library.IsFaceRecognitionAvailable:
		return "not available: " + library.FaceRecognitionUnavailableMessage
	case !library.IsPhotoFaceRecognitionEnabled:
		return "off (teanode photo faces on)"
	}
	return "on"
}
