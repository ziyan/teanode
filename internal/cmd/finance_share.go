package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Finance accounts shared with other people: who an account is shared with and
// at what level, and reading what others shared with --person on accounts,
// transactions and spending-summary. The agent's finance tool has no part in
// any of it. docs/subsystems/finance.md ("Sharing") says what each level shows.

// financePersonFlag names whose shared finance a read shows.
func financePersonFlag() cli.Flag {
	return &cli.StringFlag{Name: "person", Usage: "the username of somebody who shares a finance account with you; yours by default"}
}

// financeShareCommands are the subcommands of teanode finance that share an
// account and list the shares. They are named for what the person does, not by
// the rule: share, shares and unshare stand for the operations of
// FinanceShareQuery and FinanceShareMutation.
func financeShareCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "share",
			Usage:     "share one of your finance accounts with a person or a circle",
			ArgsUsage: "<finance-account> <username|circle>",
			Description: "Share a finance account, by its id or its name, with a person you share a circle with, or\n" +
				"with a whole circle, at a level. Each level shows what the one before shows and more:\n\n" +
				"  balance       the account's name, kind and balance, and for a credit card what it owes\n" +
				"                against its limit\n" +
				"  spending      and your money out and in by month and spending category\n" +
				"  transactions  and each transaction's day, amount, merchant and spending category\n\n" +
				"Receipts, notes, the provider's data, paystubs, tax forms, budgets and rules are never\n" +
				"shared, and neither is anything with your agent: it cannot read what is shared with you.\n" +
				"Sharing again changes the level. Sharing an account with somebody new, or at a higher\n" +
				"level, is done on the Settings tab of the Finance page on the dashboard or in the iPhone\n" +
				"app, not from a command line; a lower level, and ending a share, work here:\n\n" +
				"  teanode finance share \"Everyday Checking\" family --level balance",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "level", Usage: "how much to show: balance, spending or transactions"},
			},
			Action: runFinanceShare,
		},
		{
			Name:   "shares",
			Usage:  "who your finance accounts are shared with, and what is shared with you",
			Flags:  []cli.Flag{JSONFlag()},
			Action: runFinanceShares,
		},
		{
			Name:      "unshare",
			Usage:     "stop sharing a finance account with a person or a circle",
			ArgsUsage: "<share-id>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runFinanceUnshare,
		},
	}
}

// ownFinanceAccountNamed is the caller's finance account named by what was
// typed: its id, or its name, with or without its mask, ignoring case. A name
// that more than one account has is refused with their ids.
func ownFinanceAccountNamed(ctx context.Context, command *cli.Command, typed string) (*client.FinanceAccount, error) {
	var accounts []*client.FinanceAccount
	if err := financeCall(ctx, command, "FinanceAccounts", map[string]any{}, &accounts); err != nil {
		return nil, err
	}
	var named []*client.FinanceAccount
	for _, account := range accounts {
		if account.ID == typed {
			return account, nil
		}
		if strings.EqualFold(account.AccountName, typed) || strings.EqualFold(account.AccountName+" ••"+account.AccountMask, typed) {
			named = append(named, account)
		}
	}
	switch len(named) {
	case 0:
		return nil, fmt.Errorf("%q is neither the id nor the name of one of your finance accounts; 'teanode finance accounts' lists them", typed)
	case 1:
		return named[0], nil
	}
	ids := make([]string, 0, len(named))
	for _, account := range named {
		ids = append(ids, account.ID)
	}
	return nil, fmt.Errorf("%q is the name of more than one of your finance accounts: %s; give the id of the one you mean", typed, strings.Join(ids, ", "))
}

func runFinanceShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode finance share <finance-account> <username|circle> --level balance|spending|transactions")
	}
	financeShareLevel := strings.ToLower(strings.TrimSpace(command.String("level")))
	if !models.FinanceShareLevel(financeShareLevel).IsGrantable() {
		return usage("say how much to show with --level balance, spending or transactions")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	account, err := ownFinanceAccountNamed(ctx, command, strings.TrimSpace(command.Args().First()))
	if err != nil {
		return describeError(command, err)
	}
	typed := command.Args().Get(1)
	granteeUserId, granteeCircleId, described, err := shareTarget(ctx, connection, typed)
	if err != nil {
		return describeError(command, err)
	}
	share, err := client.ShareFinanceAccount(ctx, connection, account.ID, granteeUserId, granteeCircleId, financeShareLevel)
	if client.IsTapRequired(err) {
		return &describedError{errors.New("sharing a finance account with somebody new, or at a higher level, is done on the Settings tab of the Finance page " +
			"on the dashboard, or in Finance in the iPhone app's Settings, not from a command line; a lower level, and ending a share, work here")}
	}
	if err != nil {
		return describeNotFound(command, err, "that finance account or "+typed)
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	fmt.Printf("shared %s with %s at %s\n", forTerminal(financeAccountWords(account.AccountName, account.AccountMask)), forTerminal(described), share.FinanceShareLevel)
	return nil
}

// financeAccountWords is an account as one cell: its name and, when it has
// one, its mask.
func financeAccountWords(accountName, accountMask string) string {
	if accountMask == "" {
		return accountName
	}
	return accountName + " ••" + accountMask
}

func runFinanceShares(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	made, err := client.ListFinanceAccountShares(ctx, connection, "")
	if err != nil {
		return describeError(command, err)
	}
	reaching, err := client.ListSharedFinanceAccounts(ctx, connection, "")
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if made == nil {
			made = []*client.FinanceShare{}
		}
		if reaching == nil {
			reaching = []*client.SharedFinanceAccount{}
		}
		return PrintJSON(map[string]any{"sharedByYou": made, "sharedWithYou": reaching})
	}
	if len(made) == 0 && len(reaching) == 0 {
		fmt.Println("no finance account is shared with you or by you")
		return nil
	}
	if len(made) > 0 {
		rows := make([][]string, 0, len(made))
		for _, share := range made {
			with := share.GranteeUsername
			if share.GranteeCircleID != nil {
				with = models.DescribeCircleByName(share.GranteeCircleName)
			}
			rows = append(rows, []string{financeAccountWords(share.AccountName, share.AccountMask), with, share.FinanceShareLevel, share.ID})
		}
		fmt.Println("shared by you")
		if err := printTable([]string{"account", "with", "level", "id"}, rows); err != nil {
			return err
		}
	}
	if len(reaching) > 0 {
		if len(made) > 0 {
			fmt.Println()
		}
		rows := make([][]string, 0, len(reaching))
		for _, account := range reaching {
			rows = append(rows, []string{account.OwnerUsername, financeAccountWords(account.AccountName, account.AccountMask), account.FinanceShareLevel, account.FinanceAccountID})
		}
		fmt.Println("shared with you")
		return printTable([]string{"from", "account", "level", "id"}, rows)
	}
	return nil
}

func runFinanceUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode finance unshare <share-id>; 'teanode finance shares' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UnshareFinanceAccount(ctx, connection, command.Args().First()); err != nil {
		return describeNotFound(command, err, "the share "+command.Args().First())
	}
	return printDone(command, map[string]any{"shareId": command.Args().First(), "isUnshared": true}, "no longer shared")
}

// sharedFinanceAccountsOfPerson are the finance accounts the person named
// with --person shares with the caller. The server answers not found for what
// they did not share, so a person who shares nothing is said to share nothing.
func sharedFinanceAccountsOfPerson(ctx context.Context, command *cli.Command, connection *client.Client) ([]*client.SharedFinanceAccount, error) {
	username := strings.TrimSpace(command.String("person"))
	shared, err := client.ListSharedFinanceAccounts(ctx, connection, "")
	if err != nil {
		return nil, describeError(command, err)
	}
	var ofPerson []*client.SharedFinanceAccount
	for _, account := range shared {
		if strings.EqualFold(account.OwnerUsername, username) {
			ofPerson = append(ofPerson, account)
		}
	}
	if len(ofPerson) == 0 {
		return nil, fmt.Errorf("%s does not share a finance account with you; 'teanode finance shares' lists who does", username)
	}
	return ofPerson, nil
}

// sharedFinanceAccountsAtLevel are the accounts a person shares at a level or
// better, narrowed to one by --finance-account. Saying which one is needed
// when several are left, since a page of transactions is of one account.
func sharedFinanceAccountsAtLevel(command *cli.Command, accounts []*client.SharedFinanceAccount, level models.FinanceShareLevel, isOneNeeded bool) ([]*client.SharedFinanceAccount, error) {
	wanted := strings.TrimSpace(command.String("finance-account"))
	var kept []*client.SharedFinanceAccount
	for _, account := range accounts {
		if models.FinanceShareLevel(account.FinanceShareLevel).AtLeast(level) && (wanted == "" || account.FinanceAccountID == wanted) {
			kept = append(kept, account)
		}
	}
	username := strings.TrimSpace(command.String("person"))
	if len(kept) == 0 {
		return nil, fmt.Errorf("%s shares no finance account with you at %s or more; 'teanode finance accounts --person %s' shows what is shared and at what level",
			username, level, username)
	}
	if isOneNeeded && len(kept) > 1 {
		ids := make([]string, 0, len(kept))
		for _, account := range kept {
			ids = append(ids, account.FinanceAccountID+" ("+financeAccountWords(account.AccountName, account.AccountMask)+")")
		}
		return nil, usage("say which account with --finance-account: " + strings.Join(ids, ", "))
	}
	return kept, nil
}

// runFinanceSharedAccounts is teanode finance accounts --person: what the
// person shares, at the level each was shared at.
func runFinanceSharedAccounts(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	accounts, err := sharedFinanceAccountsOfPerson(ctx, command, connection)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(accounts)
	}
	rows := make([][]string, 0, len(accounts))
	for _, account := range accounts {
		used := ""
		if account.CreditUsage != nil {
			used = usageShareWords(account.CreditUsage.UsageShare)
		}
		rows = append(rows, []string{
			account.FinanceAccountID, financeAccountWords(account.AccountName, account.AccountMask), account.AccountKind,
			money(account.CurrentBalance, account.CurrencyCode), used, dayOf(account.BalanceAt), account.FinanceShareLevel,
		})
	}
	return printTable([]string{"id", "account", "kind", "balance", "used", "as of", "shared at"}, rows)
}

// runFinanceSharedTransactions is teanode finance transactions --person: a
// page of one shared account's transactions.
func runFinanceSharedTransactions(ctx context.Context, command *cli.Command) error {
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	accounts, err := sharedFinanceAccountsOfPerson(ctx, command, connection)
	if err != nil {
		return err
	}
	chosen, err := sharedFinanceAccountsAtLevel(command, accounts, models.FinanceShareLevelTransactions, true)
	if err != nil {
		return err
	}
	page, err := client.ListSharedFinanceTransactions(ctx, connection, chosen[0].FinanceAccountID, from, to, int(command.Int("limit")), strings.TrimSpace(command.String("after")))
	if err != nil {
		return describeNotFound(command, err, "that finance account")
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FinanceTransactions) == 0 {
		fmt.Println("no finance transactions match")
		return nil
	}
	rows := make([][]string, 0, len(page.FinanceTransactions))
	for _, transaction := range page.FinanceTransactions {
		rows = append(rows, []string{
			transaction.PostedOn, money(transaction.Amount, transaction.CurrencyCode), forTerminal(transaction.MerchantName), forTerminal(transaction.SpendingCategoryName),
		})
	}
	if err := printTable([]string{"posted", "amount", "merchant", "spending category"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		fmt.Printf("note: add --after %s for the next page\n", page.NextCursor)
	}
	return nil
}

// runFinanceSharedSpending is teanode finance spending-summary --person:
// money out and in by month and spending category, for each shared account at
// the spending level or more.
func runFinanceSharedSpending(ctx context.Context, command *cli.Command) error {
	if command.IsSet("group-by") || command.IsSet("currency") {
		return usage("--group-by and --currency do not apply with --person: what is shared is by month and spending category, in each account's own currency")
	}
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	accounts, err := sharedFinanceAccountsOfPerson(ctx, command, connection)
	if err != nil {
		return err
	}
	chosen, err := sharedFinanceAccountsAtLevel(command, accounts, models.FinanceShareLevelSpending, false)
	if err != nil {
		return err
	}
	type spendingOfAccount struct {
		FinanceAccountID string                             `json:"financeAccountId"`
		AccountName      string                             `json:"accountName"`
		SpendingRows     []*client.SharedFinanceSpendingRow `json:"spendingRows"`
	}
	var all []*spendingOfAccount
	rows := [][]string{}
	for _, account := range chosen {
		spending, err := client.ListSharedFinanceSpending(ctx, connection, account.FinanceAccountID, from, to)
		if err != nil {
			return describeNotFound(command, err, "that finance account")
		}
		all = append(all, &spendingOfAccount{FinanceAccountID: account.FinanceAccountID, AccountName: account.AccountName, SpendingRows: spending})
		for _, row := range spending {
			category := forTerminal(row.SpendingCategoryName)
			if category == "" {
				category = "(none)"
			}
			rows = append(rows, []string{
				financeAccountWords(account.AccountName, account.AccountMask), row.SpendingMonth, category,
				money(row.MoneyOut, row.CurrencyCode), money(row.MoneyIn, row.CurrencyCode), fmt.Sprint(row.FinanceTransactionCount),
			})
		}
	}
	if command.Bool("json") {
		return PrintJSON(all)
	}
	if len(rows) == 0 {
		fmt.Println("nothing spent in that time")
		return nil
	}
	return printTable([]string{"account", "month", "spending category", "money out", "money in", "transactions"}, rows)
}
