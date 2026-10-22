package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// NewSharingCommand builds "teanode sharing", everything you share and
// everything shared with you, across calendar, location, Health, memory pages,
// photo albums, recordings, finance accounts, skills and passwords, in one
// place. Each kind is still shared
// and stopped with its own command; what is only here is declining a share
// that was made to you by name.
func NewSharingCommand() *cli.Command {
	return &cli.Command{
		Name:  "sharing",
		Usage: "everything you share, and everything shared with you",
		Description: "Prints two lists: what you share, with whom and since when, and what is shared\n" +
			"with you, by whom, through which circle or by name. A share of passwords is\n" +
			"counted and never named, since an item's title is sealed. The kinds are " + sharingKindWords() + ".\n\n" +
			"  teanode sharing\n" +
			"  teanode sharing decline calendar <share-id>",
		Flags:  []cli.Flag{JSONFlag()},
		Action: runSharing,
		Commands: []*cli.Command{
			{
				Name:      "decline",
				Usage:     "stop receiving a share that was made to you by name",
				ArgsUsage: "<kind> <share-id>",
				Description: "The kind and the share's id are the ones 'teanode sharing' prints. A share made to a\n" +
					"circle cannot be declined: it ends when you leave the circle. Passwords shared\n" +
					"with you are removed by whoever shared them. The owner is not told, and has to\n" +
					"share again for you to receive it.",
				Flags:  []cli.Flag{ForceFlag()},
				Action: runSharingDecline,
			},
		},
	}
}

// sharingKindWords are the kinds, as the commands and the agent name them.
func sharingKindWords() string {
	words := make([]string, 0, len(models.ShareKinds))
	for _, shareKind := range models.ShareKinds {
		words = append(words, string(shareKind))
	}
	return strings.Join(words, ", ")
}

// sharingKindName is a kind as it is said to a person: memory page, not
// memory_page.
func sharingKindName(shareKind string) string {
	return strings.ReplaceAll(shareKind, "_", " ")
}

// sharingWhat is what a share is of: the calendar, page, album, recording,
// finance account or skill by name (an account with the last digits of its
// number), how many password items, and nothing for location and Health, which
// are shared whole.
func sharingWhat(shareKind, subjectName, subjectMask string, passwordItemCount int) string {
	if shareKind == string(models.ShareKindPasswords) {
		if passwordItemCount == 1 {
			return "1 item"
		}
		return fmt.Sprintf("%d items", passwordItemCount)
	}
	if shareKind == string(models.ShareKindFinance) {
		return forTerminal(financeAccountWords(subjectName, subjectMask))
	}
	return forTerminal(subjectName)
}

// sharingLevel is the level of a share and what else it gives, in one cell.
func sharingLevel(shareLevel string, shareScopes []string) string {
	parts := []string{}
	if shareLevel != "" {
		parts = append(parts, shareLevel)
	}
	parts = append(parts, shareScopes...)
	return forTerminal(strings.Join(parts, ", "))
}

// sharingPerson is a person as the lists name them: the username, with the
// name where there is one that differs.
func sharingPerson(username, name string) string {
	if name == "" || name == username {
		return forTerminal(username)
	}
	return forTerminal(username + " (" + name + ")")
}

func runSharing(ctx context.Context, command *cli.Command) error {
	if command.Args().Present() {
		return usage(fmt.Sprintf("teanode sharing takes no arguments, and has one subcommand, decline; got %q", command.Args().First()))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	sharing, err := client.ListSharing(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(sharing)
	}

	fmt.Println("Shared by you")
	if len(sharing.SharesMade) == 0 {
		fmt.Println("nothing yet")
	} else {
		rows := make([][]string, 0, len(sharing.SharesMade))
		for _, share := range sharing.SharesMade {
			var with string
			if share.GranteeCircleID != nil {
				with = "circle " + forTerminal(share.GranteeCircleName)
			} else {
				with = sharingPerson(share.GranteeUsername, share.GranteeName)
			}
			rows = append(rows, []string{
				sharingKindName(share.ShareKind), sharingWhat(share.ShareKind, share.SubjectName, share.SubjectMask, share.PasswordItemCount), with,
				sharingLevel(share.ShareLevel, share.ShareScopes), share.CreatedAt.Format("2006-01-02"), forTerminal(share.ShareID),
			})
		}
		if err := printTable([]string{"KIND", "WHAT", "WITH", "LEVEL", "SINCE", "ID"}, rows); err != nil {
			return err
		}
	}

	fmt.Println()
	fmt.Println("Shared with you")
	if len(sharing.SharesReceived) == 0 {
		fmt.Println("nothing yet")
		return nil
	}
	rows := make([][]string, 0, len(sharing.SharesReceived))
	for _, share := range sharing.SharesReceived {
		through := "by name"
		if !share.IsDirect {
			through = "circle " + forTerminal(share.GranteeCircleName)
		}
		rows = append(rows, []string{
			sharingKindName(share.ShareKind), sharingWhat(share.ShareKind, share.SubjectName, share.SubjectMask, share.PasswordItemCount),
			sharingPerson(share.OwnerUsername, share.OwnerName), through, sharingLevel(share.ShareLevel, share.ShareScopes),
			share.CreatedAt.Format("2006-01-02"), forTerminal(share.ShareID),
		})
	}
	return printTable([]string{"KIND", "WHAT", "FROM", "THROUGH", "LEVEL", "SINCE", "ID"}, rows)
}

func runSharingDecline(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("which share? usage: teanode sharing decline <kind> <share-id>; the kinds are " + sharingKindWords())
	}
	shareKind := strings.ReplaceAll(strings.ToLower(command.Args().Get(0)), "-", "_")
	shareId := command.Args().Get(1)
	if err := confirm(command, "This stops you receiving that share, and whoever shared it has to share again for you to have it back."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeclineShare(ctx, connection, shareKind, shareId); err != nil {
		return describeNotFound(command, err, "a "+sharingKindName(shareKind)+" share "+shareId+" made to you by name")
	}
	fmt.Println("declined the share")
	return nil
}
