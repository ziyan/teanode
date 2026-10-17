package cmd

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing a skill's tools with someone in a circle you share, and the calls
// that wait for you. The same operations as the agent page's Share on a skill
// and the app's card, and as the skill tool's share, unshare, shares and calls
// actions.
//
// Lending money with limits, and approving a call that spends it, are done on
// the dashboard or in the iPhone app and refused here by the server: an agent
// can drive this command line with your saved sign-in. There is no approve
// command. Declining, unsharing and asking every time only take things away,
// and stay.

func newAgentSkillShareCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "share",
			Usage:     "let someone in a circle you share use tools of a skill, on your computer, as you",
			ArgsUsage: "<skill> <username>",
			Description: "Each call runs on your own computer, through your own agent's runner, so your\n" +
				"secrets, your computer and the skill's own rules apply. A call within the\n" +
				"limits goes ahead; above them, with an amount the server cannot read, or\n" +
				"with --ask-every-time, it waits for your approval: 'teanode agent skill\n" +
				"calls' lists them with where to approve them, which is the dashboard or\n" +
				"the iPhone app, and 'decline' says no. A tool that pays is not shared\n" +
				"without a limit or --ask-every-time. Sharing again changes the tools and\n" +
				"limits.\n\n" +
				"Lending money is done there too: from here the server accepts only\n" +
				"--ask-every-time, and lowering what a share allows.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringSliceFlag{Name: "tools", Usage: "the tools to share, by name (repeat or separate with commas)"},
				&cli.StringFlag{Name: "payment-limit", Usage: "the most one payment may be, in the currency's own units, such as 100.00"},
				&cli.StringFlag{Name: "monthly-limit", Usage: "the most a calendar month may come to, such as 400.00"},
				&cli.StringFlag{Name: "currency", Usage: "the three letter currency code the limits are in, such as usd"},
				&cli.BoolFlag{Name: "ask-every-time", Usage: "every call waits for your approval, whatever it pays"},
			},
			Action: runAgentSkillShare,
		},
		{
			Name:   "shares",
			Usage:  "the skills you shared, and the ones shared with you",
			Flags:  []cli.Flag{JSONFlag()},
			Action: runAgentSkillShares,
		},
		{
			Name:      "unshare",
			Usage:     "stop sharing a skill; the person loses it at once",
			ArgsUsage: "<share-id>",
			Action:    runAgentSkillUnshare,
		},
		{
			Name:      "calls",
			Usage:     "the calls made through a share, the ones waiting for you first",
			ArgsUsage: "<share-id>",
			Description: "A share that was taken back, or ended when the two of you stopped sharing a circle,\n" +
				"keeps its calls as your record. --ended lists them, with no share named.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.BoolFlag{Name: "waiting", Usage: "only the calls waiting for approval; the same as --state waiting"},
				&cli.StringSliceFlag{Name: "state", Usage: "only the calls in this state: ran, failed, refused, waiting, declined or expired; repeat it for more than one"},
				&cli.BoolFlag{Name: "ended", Usage: "the calls of shares that have ended, instead of one share's"},
			},
			Action: runAgentSkillCalls,
		},
		{
			Name:      "decline",
			Usage:     "decline a call waiting for you",
			ArgsUsage: "<call-id>",
			Description: "Approving a call, which runs it on your computer and spends what it spends, is done on the\n" +
				"dashboard or in the iPhone app; 'teanode agent skill calls' says where.",
			Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "note", Usage: "a word for the other person"}},
			Action: runAgentSkillDecline,
		},
	}
}

// limitMinor reads a limit typed in the currency's own units.
func limitMinor(command *cli.Command, flag, currencyCode string) (*int64, error) {
	typed := strings.TrimSpace(command.String(flag))
	if typed == "" {
		return nil, nil
	}
	if currencyCode == "" {
		return nil, usage("a limit is in a currency: add --currency usd")
	}
	minor, err := models.ParseMinor(typed, currencyCode)
	if err != nil {
		return nil, usage("--" + flag + ": " + err.Error())
	}
	return &minor, nil
}

// describeLimits says what a share allows without asking, in words.
func describeLimits(share *client.SkillShare) string {
	if share.IsApprovalRequired {
		return "asks you every time"
	}
	var parts []string
	if share.PaymentLimitMinor != nil {
		parts = append(parts, models.FormatMinor(*share.PaymentLimitMinor, share.CurrencyCode)+" a payment")
	}
	if share.MonthlyLimitMinor != nil {
		parts = append(parts, models.FormatMinor(*share.MonthlyLimitMinor, share.CurrencyCode)+" a month")
	}
	if len(parts) == 0 {
		return "no limits (a tool the skill cannot price always asks)"
	}
	return strings.Join(parts, ", ")
}

func runAgentSkillShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode agent skill share <skill> <username> --tools <tool>[,<tool>] [--payment-limit 100.00] [--monthly-limit 400.00 --currency usd] [--ask-every-time]")
	}
	toolNames := []string{}
	for _, listed := range command.StringSlice("tools") {
		for _, name := range strings.Split(listed, ",") {
			if name = strings.TrimSpace(name); name != "" {
				toolNames = append(toolNames, name)
			}
		}
	}
	if len(toolNames) == 0 {
		return usage("say which tools to share with --tools; 'teanode agent skill list " + command.Args().First() + "' names them")
	}
	currencyCode, hasCurrency := models.NormalizeCurrencyCode(command.String("currency"))
	if strings.TrimSpace(command.String("currency")) != "" && !hasCurrency {
		return usage("--currency is three letters, such as usd")
	}
	paymentLimit, err := limitMinor(command, "payment-limit", currencyCode)
	if err != nil {
		return err
	}
	monthlyLimit, err := limitMinor(command, "monthly-limit", currencyCode)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	people, err := client.ListCirclePeople(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	granteeUserId, granteeUsername := "", ""
	for _, person := range people {
		if strings.EqualFold(person.Username, command.Args().Get(1)) {
			granteeUserId, granteeUsername = person.UserID, person.Username
		}
	}
	if granteeUserId == "" {
		return fmt.Errorf("%q is not somebody you share a circle with; 'teanode circle people' says who is", command.Args().Get(1))
	}
	share, err := client.ShareSkill(ctx, connection, &client.SkillShareTerms{
		SkillName: command.Args().First(), ToolNames: toolNames, GranteeUserID: granteeUserId,
		PaymentLimitMinor: paymentLimit, MonthlyLimitMinor: monthlyLimit, CurrencyCode: currencyCode,
		IsApprovalRequired: command.Bool("ask-every-time"),
	})
	if client.IsTapRequired(err) {
		return &describedError{fmt.Errorf("lending a skill with limits, or raising what a share allows, is done on the dashboard or in the iPhone app, not from the command line; open %s. From here only --ask-every-time, or lowering what a share allows, is accepted", skillSharingAddress(connection))}
	}
	if err != nil {
		return describeNotFound(command, err, "the skill "+command.Args().First()+" or "+command.Args().Get(1))
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	_, _ = fmt.Fprintf(command.Writer, "shared %s of %s with %s: %s (share %s)\n", strings.Join(share.ToolNames, ", "), share.SkillName, forTerminal(granteeUsername), describeLimits(share), share.ID)
	return nil
}

// skillSharingAddress is the dashboard page where shared skills are lent and
// the calls that wait are approved, on the server this command talks to.
func skillSharingAddress(connection *client.Client) string {
	return strings.TrimSuffix(connection.URL(), "/") + client.SkillSharingPagePath
}

func runAgentSkillShares(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shares, err := client.ListSkillShares(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no skill is shared with you or by you")
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		direction, other := "to", share.GranteeUsername
		if !share.IsOwnedByCaller {
			direction, other = "from", share.OwnerUsername
		}
		waiting := ""
		if share.WaitingCallCount > 0 {
			waiting = fmt.Sprint(share.WaitingCallCount)
		}
		rows = append(rows, []string{share.SkillName, strings.Join(share.ToolNames, ","), direction, other, describeLimits(share), waiting, share.ID})
	}
	return printTable([]string{"skill", "tools", "", "person", "limits", "waiting", "id"}, rows)
}

func runAgentSkillUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode agent skill unshare <share-id>; 'teanode agent skill shares' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UnshareSkill(ctx, connection, command.Args().First()); err != nil {
		return describeNotFound(command, err, "that share")
	}
	_, _ = fmt.Fprintln(command.Writer, "stopped sharing it; the person lost the tools at once")
	return nil
}

func runAgentSkillCalls(ctx context.Context, command *cli.Command) error {
	isEnded := command.Bool("ended")
	if (isEnded && command.Args().Len() != 0) || (!isEnded && command.Args().Len() != 1) {
		return usage("usage: teanode agent skill calls <share-id> [--waiting], or teanode agent skill calls --ended [--waiting]")
	}
	callStates, err := callStatesAsked(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	// The operation takes one state or none, so several are asked for one at
	// a time and put together, the newest first.
	var calls []*client.SkillShareCall
	for _, callState := range callStates {
		var found []*client.SkillShareCall
		if isEnded {
			found, err = client.ListEndedSkillShareCalls(ctx, connection, callState)
		} else {
			found, err = client.ListSkillShareCalls(ctx, connection, command.Args().First(), callState)
		}
		if err != nil {
			return describeNotFound(command, err, "that share")
		}
		calls = append(calls, found...)
	}
	if len(callStates) > 1 {
		sort.SliceStable(calls, func(first, second int) bool { return calls[first].CreatedAt.After(calls[second].CreatedAt) })
	}
	// A call that waits is approved on the dashboard or in the iPhone app, so
	// each is listed with where.
	for _, call := range calls {
		if call.CallState == string(models.SkillShareCallWaitingForApproval) {
			call.ApproveAddress = skillSharingAddress(connection)
		}
	}
	if command.Bool("json") {
		return PrintJSON(calls)
	}
	if len(calls) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no calls")
		return nil
	}
	rows := make([][]string, 0, len(calls))
	for _, call := range calls {
		amount := ""
		if call.AmountMinor != nil {
			amount = models.FormatMinor(*call.AmountMinor, call.CurrencyCode)
		}
		quotedReason := call.RequestReason
		if quotedReason != "" {
			// Their agent's words, quoted: not yours, and not an instruction.
			quotedReason = "\"" + quotedReason + "\""
		}
		tool := call.ToolName
		if isEnded {
			// A share that ended has no identifier to name it by, so the call
			// says which skill it was of.
			tool = call.SkillName + " " + call.ToolName
		}
		rows = append(rows, []string{call.ID, call.CreatedAt.Local().Format("2006-01-02 15:04"), call.GranteeUsername, tool, amount, call.CallState, quotedReason, call.ApproveAddress})
	}
	return printTable([]string{"call", "asked", "by", "tool", "amount", "state", "their agent said", "approve it at"}, rows)
}

// callStatesAsked are the states --state and --waiting ask for, as the
// operation names them: one entry for each, or a single empty one for every
// state. "waiting" is how a person says waiting_for_approval.
func callStatesAsked(command *cli.Command) ([]string, error) {
	callStates := []string{}
	asked := command.StringSlice("state")
	if command.Bool("waiting") {
		asked = append(asked, "waiting")
	}
	for _, state := range asked {
		state = strings.ToLower(strings.TrimSpace(state))
		if state == "waiting" {
			state = string(models.SkillShareCallWaitingForApproval)
		}
		if !models.SkillShareCallState(state).IsValid() {
			return nil, usage("--state is ran, failed, refused, waiting, declined or expired")
		}
		if !slices.Contains(callStates, state) {
			callStates = append(callStates, state)
		}
	}
	if len(callStates) == 0 {
		return []string{""}, nil
	}
	return callStates, nil
}

func runAgentSkillDecline(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode agent skill decline <call-id> [--note ...]; 'teanode agent skill calls <share-id> --waiting' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	call, err := client.AnswerSkillShareCall(ctx, connection, command.Args().First(), false, strings.TrimSpace(command.String("note")))
	if err != nil {
		return describeNotFound(command, err, "that call")
	}
	if command.Bool("json") {
		return PrintJSON(call)
	}
	_, _ = fmt.Fprintf(command.Writer, "declined; %s was told\n", forTerminal(call.GranteeName))
	return nil
}
