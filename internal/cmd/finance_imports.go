package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// maximumStatementDocumentBytes is the largest picture or PDF
// read-statement sends: more than any screenshot or statement.
const maximumStatementDocumentBytes = 20 << 20

// financeImportCommands are the subcommands for statements read from
// pictures and PDFs, and the list of what was read in the background.
func financeImportCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name: "read-statement", Usage: "have your agent read one account's transactions from screenshots, photos or PDF statements, and import them",
			ArgsUsage: "<file>...",
			Description: "Each file goes up to your agent, and the reading job reads it in the background: the account\n" +
				"it is of and every transaction it shows, imported into that account of imported statements, or into\n" +
				"a new one when the account was not imported before. What came of it is posted to your feed, and\n" +
				"teanode finance imports lists it. An OFX file needs no reading: teanode finance import-statement.",
			Flags:  []cli.Flag{JSONFlag()},
			Action: runFinanceReadStatement,
		},
		{
			Name: "imports", Usage: "the files read into finance in the background, newest first, and what came of each",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.IntFlag{Name: "limit", Usage: "how many, at most 200", Value: 50},
				&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"},
			},
			Action: runFinanceImports,
		},
	}
}

func runFinanceReadStatement(ctx context.Context, command *cli.Command) error {
	paths := command.Args().Slice()
	if len(paths) == 0 {
		return usage("give the screenshots, photos or PDFs of the statement: teanode finance read-statement screenshot.png")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	readings := []*client.StatementReading{}
	for _, path := range paths {
		var content []byte
		fileName := filepath.Base(path)
		if path == "-" {
			fileName = "statement.pdf"
			content, err = io.ReadAll(io.LimitReader(os.Stdin, maximumStatementDocumentBytes+1))
		} else {
			content, err = readLimitedFile(path, maximumStatementDocumentBytes+1)
		}
		if err != nil {
			return err
		}
		if len(content) > maximumStatementDocumentBytes {
			return usage(fmt.Sprintf("%s is larger than %d MB, more than any screenshot or statement", fileName, maximumStatementDocumentBytes>>20))
		}
		attachment, err := client.UploadAgentAttachment(ctx, connection, fileName, content)
		if err != nil {
			return describeError(command, err)
		}
		var reading *client.StatementReading
		if err := describeError(command, client.RunFinance(ctx, connection, operationOf(command), map[string]any{"agentAttachmentId": attachment.ID}, &reading)); err != nil {
			return err
		}
		readings = append(readings, reading)
	}
	return printDone(command, readings, fmt.Sprintf("queued: %d %s, read in the background; teanode finance imports shows what came of each",
		len(readings), map[bool]string{true: "file", false: "files"}[len(readings) == 1]))
}

func runFinanceImports(ctx context.Context, command *cli.Command) error {
	var page *client.FinanceImportPage
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"limit": int(command.Int("limit")), "offset": int(command.Int("offset"))}, &page); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FinanceImports) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing was read into finance yet")
		return nil
	}
	rows := make([][]string, 0, len(page.FinanceImports))
	for _, financeImport := range page.FinanceImports {
		queuedAt := financeImport.QueuedAt
		rows = append(rows, []string{formatTime(&queuedAt), financeImport.FinanceImportKind, financeImport.FinanceImportSource,
			financeImport.FileName, financeImport.AgentJobStatus, financeImport.OutcomeText})
	}
	if err := printTable([]string{"queued", "read as", "from", "file", "status", "outcome"}, rows); err != nil {
		return err
	}
	if page.HasMore {
		fmt.Fprintf(os.Stderr, "%d of %d shown; --offset %d for the next page\n", len(page.FinanceImports), page.TotalCount, int(command.Int("offset"))+len(page.FinanceImports))
	}
	return nil
}
