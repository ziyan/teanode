package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// NewCircleCommand builds "teanode circle", the named sets of people on this
// server who know each other, and the invitations to join one.
//
// A circle is named by its identifier, which "circle list" prints. People are
// named by username, the way "teanode user" names accounts.
func NewCircleCommand() *cli.Command {
	return &cli.Command{
		Name:  "circle",
		Usage: "the circles you are in: family, friends or colleagues on this server",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list your circles",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runCircleList,
			},
			{
				Name:      "show",
				Usage:     "show a circle: its members and, for an admin, the invitations still waiting",
				ArgsUsage: "<circle-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runCircleShow,
			},
			{
				Name:      "create",
				Usage:     "make a circle, with you as its admin",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "kind", Usage: "what it is for: family, friends or colleagues", Required: true},
					JSONFlag(),
				},
				Action: runCircleCreate,
			},
			{
				Name:      "rename",
				Usage:     "change a circle's name; needs an admin",
				ArgsUsage: "<circle-id> <name>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runCircleRename,
			},
			{
				Name:      "delete",
				Usage:     "delete a circle with its members and invitations; needs an admin",
				ArgsUsage: "<circle-id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runCircleDelete,
			},
			{
				Name:      "invite",
				Usage:     "invite somebody by username; needs an admin",
				ArgsUsage: "<circle-id> <username>",
				Description: "The answer is the same whether or not the username is an account, so this is\n" +
					"not a way to list who is on the server. The person is told, and joins when\n" +
					"they accept.",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "admin", Usage: "invite them as an admin rather than a member"},
				},
				Action: runCircleInvite,
			},
			{
				Name:   "invitations",
				Usage:  "list the invitations to you that you have not answered",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runCircleInvitations,
			},
			{
				Name:      "accept",
				Usage:     "join a circle you were invited to",
				ArgsUsage: "<invitation-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runCircleAccept,
			},
			{
				Name:      "decline",
				Usage:     "turn down an invitation",
				ArgsUsage: "<invitation-id>",
				Action:    runCircleDecline,
			},
			{
				Name:      "withdraw",
				Usage:     "take back an invitation nobody has answered; needs an admin",
				ArgsUsage: "<invitation-id>",
				Action:    runCircleWithdraw,
			},
			{
				Name:      "role",
				Usage:     "make a member an admin, or an admin a member; needs an admin",
				ArgsUsage: "<circle-id> <username> admin|member",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runCircleRole,
			},
			{
				Name:      "remove",
				Usage:     "take a member out of a circle; needs an admin",
				ArgsUsage: "<circle-id> <username>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runCircleRemove,
			},
			{
				Name:      "leave",
				Usage:     "leave a circle",
				ArgsUsage: "<circle-id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runCircleLeave,
			},
			{
				Name:   "people",
				Usage:  "list everybody you share a circle with, once each",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runCirclePeople,
			},
			{
				Name:      "add",
				Usage:     "put an account in a circle without an invitation; needs user:manage",
				ArgsUsage: "<circle-id> <username>",
				Description: "For an account made for somebody who will not set it up themselves. The\n" +
					"circle does not have to be one you are in.",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "admin", Usage: "add them as an admin rather than a member"},
					JSONFlag(),
				},
				Action: runCircleAdd,
			},
		},
	}
}

// circleMemberRole is the role --admin asks for.
func circleMemberRole(command *cli.Command) string {
	if command.Bool("admin") {
		return "admin"
	}
	return "member"
}

// circleDescription is what a circle is called in a sentence: its name and
// what it is for.
func circleDescription(circle *client.Circle) string {
	return fmt.Sprintf("%s (%s)", forTerminal(circle.Name), circle.CircleKind)
}

func runCircleList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circles, err := client.ListCircles(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(circles)
	}
	if len(circles) == 0 {
		fmt.Println("you are not in any circle; make one with: teanode circle create --kind family <name>")
		return nil
	}
	rows := make([][]string, 0, len(circles))
	for _, circle := range circles {
		rows = append(rows, []string{circle.ID, forTerminal(circle.Name), circle.CircleKind, circle.MemberRole, fmt.Sprint(len(circle.Members))})
	}
	return printTable([]string{"ID", "NAME", "KIND", "YOU ARE", "MEMBERS"}, rows)
}

func runCircleShow(ctx context.Context, command *cli.Command) error {
	circleId := command.Args().First()
	if circleId == "" {
		return usage("which circle? usage: teanode circle show <circle-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circle, err := client.ReadCircle(ctx, connection, circleId)
	if err != nil {
		return describeNotFound(command, err, "circle "+circleId)
	}
	if command.Bool("json") {
		return PrintJSON(circle)
	}
	fmt.Printf("%s, created %s; you are %s\n\n", circleDescription(circle), circle.CreatedAt.Format("2006-01-02"), circle.MemberRole)
	rows := make([][]string, 0, len(circle.Members))
	for _, member := range circle.Members {
		rows = append(rows, []string{forTerminal(member.Username), forTerminal(member.Name), member.MemberRole, member.JoinedAt.Format("2006-01-02")})
	}
	if err := printTable([]string{"USERNAME", "NAME", "ROLE", "JOINED"}, rows); err != nil {
		return err
	}
	if len(circle.PendingInvitations) > 0 {
		fmt.Println("\nWaiting for an answer:")
		rows = rows[:0]
		for _, invitation := range circle.PendingInvitations {
			rows = append(rows, []string{invitation.ID, forTerminal(invitation.InvitedUsername), invitation.MemberRole, invitation.ExpiresAt.Format("2006-01-02")})
		}
		return printTable([]string{"INVITATION", "USERNAME", "AS", "EXPIRES"}, rows)
	}
	return nil
}

func runCircleCreate(ctx context.Context, command *cli.Command) error {
	name := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if name == "" {
		return usage("what is it called? usage: teanode circle create --kind <kind> <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circle, err := client.CreateCircle(ctx, connection, name, command.String("kind"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(circle)
	}
	fmt.Printf("made %s: %s\n", circleDescription(circle), circle.ID)
	return nil
}

func runCircleRename(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	if len(arguments) < 2 || strings.TrimSpace(strings.Join(arguments[1:], " ")) == "" {
		return usage("usage: teanode circle rename <circle-id> <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circle, err := client.RenameCircle(ctx, connection, arguments[0], strings.TrimSpace(strings.Join(arguments[1:], " ")))
	if err != nil {
		return describeNotFound(command, err, "circle "+arguments[0])
	}
	if command.Bool("json") {
		return PrintJSON(circle)
	}
	fmt.Printf("renamed to %s\n", circleDescription(circle))
	return nil
}

func runCircleDelete(ctx context.Context, command *cli.Command) error {
	circleId := command.Args().First()
	if circleId == "" {
		return usage("which circle? usage: teanode circle delete <circle-id>")
	}
	if err := confirm(command, "This deletes the circle for everybody in it, with its invitations."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteCircle(ctx, connection, circleId); err != nil {
		return describeNotFound(command, err, "circle "+circleId)
	}
	fmt.Println("deleted the circle")
	return nil
}

func runCircleInvite(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	if len(arguments) != 2 {
		return usage("usage: teanode circle invite <circle-id> <username> [--admin]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.InviteToCircle(ctx, connection, arguments[0], arguments[1], circleMemberRole(command)); err != nil {
		return describeNotFound(command, err, "circle "+arguments[0])
	}
	// The same words whoever the username is, as the server's answer is.
	fmt.Printf("invitation sent to %s, if that account exists; they join when they accept\n", forTerminal(arguments[1]))
	return nil
}

func runCircleInvitations(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	invitations, err := client.ListCircleInvitations(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(invitations)
	}
	if len(invitations) == 0 {
		fmt.Println("no invitations waiting")
		return nil
	}
	rows := make([][]string, 0, len(invitations))
	for _, invitation := range invitations {
		rows = append(rows, []string{invitation.ID, forTerminal(invitation.CircleName), invitation.CircleKind, forTerminal(invitation.InvitedByName), invitation.MemberRole, invitation.ExpiresAt.Format("2006-01-02")})
	}
	return printTable([]string{"INVITATION", "CIRCLE", "KIND", "FROM", "AS", "EXPIRES"}, rows)
}

func runCircleAccept(ctx context.Context, command *cli.Command) error {
	invitationId := command.Args().First()
	if invitationId == "" {
		return usage("which invitation? usage: teanode circle accept <invitation-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circle, err := client.AnswerCircleInvitation(ctx, connection, invitationId, true)
	if err != nil {
		return describeNotFound(command, err, "invitation "+invitationId)
	}
	if command.Bool("json") {
		return PrintJSON(circle)
	}
	fmt.Printf("joined %s: %s\n", circleDescription(circle), circle.ID)
	if hint := circleSharingHint(circle.CircleKind); hint != "" {
		fmt.Println(hint)
	}
	return nil
}

// circleSharingHint says what can be shared with a circle just joined, and
// the commands that do it: a family shares the calendar and where they are,
// colleagues the calendar. The CLI does not know what the account may use, so
// it says "if your account may", where the dashboard and the app leave out
// what is not allowed. Empty for any other circle.
func circleSharingHint(circleKind string) string {
	switch models.CircleKind(circleKind) {
	case models.CircleKindFamily:
		return "you can share your calendar and where you are with it, if your account may use them: teanode calendar share, teanode location share"
	case models.CircleKindColleagues:
		return "you can share your calendar with it, if your account may use it: teanode calendar share"
	}
	return ""
}

func runCircleDecline(ctx context.Context, command *cli.Command) error {
	invitationId := command.Args().First()
	if invitationId == "" {
		return usage("which invitation? usage: teanode circle decline <invitation-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if _, err := client.AnswerCircleInvitation(ctx, connection, invitationId, false); err != nil {
		return describeNotFound(command, err, "invitation "+invitationId)
	}
	fmt.Println("declined")
	return nil
}

func runCircleWithdraw(ctx context.Context, command *cli.Command) error {
	invitationId := command.Args().First()
	if invitationId == "" {
		return usage("which invitation? usage: teanode circle withdraw <invitation-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.WithdrawCircleInvitation(ctx, connection, invitationId); err != nil {
		return describeNotFound(command, err, "invitation "+invitationId)
	}
	fmt.Println("withdrew the invitation")
	return nil
}

func runCircleRole(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	if len(arguments) != 3 || (arguments[2] != "admin" && arguments[2] != "member") {
		return usage("usage: teanode circle role <circle-id> <username> admin|member")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circle, err := client.SetCircleMemberRole(ctx, connection, arguments[0], arguments[1], arguments[2])
	if err != nil {
		return describeNotFound(command, err, "circle "+arguments[0])
	}
	if command.Bool("json") {
		return PrintJSON(circle)
	}
	fmt.Printf("%s is now %s of %s\n", forTerminal(arguments[1]), arguments[2], circleDescription(circle))
	return nil
}

func runCircleRemove(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	if len(arguments) != 2 {
		return usage("usage: teanode circle remove <circle-id> <username>")
	}
	if err := confirm(command, fmt.Sprintf("This takes %s out of the circle.", forTerminal(arguments[1]))); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if _, err := client.RemoveCircleMember(ctx, connection, arguments[0], arguments[1]); err != nil {
		return describeNotFound(command, err, "circle "+arguments[0])
	}
	fmt.Printf("removed %s\n", forTerminal(arguments[1]))
	return nil
}

func runCircleLeave(ctx context.Context, command *cli.Command) error {
	circleId := command.Args().First()
	if circleId == "" {
		return usage("which circle? usage: teanode circle leave <circle-id>")
	}
	if err := confirm(command, "This takes you out of the circle, and the last person to leave deletes it."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.LeaveCircle(ctx, connection, circleId); err != nil {
		return describeNotFound(command, err, "circle "+circleId)
	}
	fmt.Println("left the circle")
	return nil
}

func runCirclePeople(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	people, err := client.ListCirclePeople(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(people)
	}
	if len(people) == 0 {
		fmt.Println("you share a circle with nobody yet")
		return nil
	}
	rows := make([][]string, 0, len(people))
	for _, person := range people {
		// Somebody with no display name is shown by their username, as the
		// dashboard shows them.
		name := person.Name
		if name == "" {
			name = person.Username
		}
		rows = append(rows, []string{forTerminal(person.Username), forTerminal(name)})
	}
	return printTable([]string{"USERNAME", "NAME"}, rows)
}

func runCircleAdd(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	if len(arguments) != 2 {
		return usage("usage: teanode circle add <circle-id> <username> [--admin]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circle, err := client.AddCircleMember(ctx, connection, arguments[0], arguments[1], circleMemberRole(command))
	if err != nil {
		return describeNotFound(command, err, "circle "+arguments[0]+" or account "+arguments[1])
	}
	if command.Bool("json") {
		return PrintJSON(circle)
	}
	fmt.Printf("put %s in %s\n", forTerminal(arguments[1]), circleDescription(circle))
	return nil
}
