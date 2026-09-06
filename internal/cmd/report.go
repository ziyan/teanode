package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewReportCommand builds "teanode report": the DMARC aggregate reports other
// people send about mail claiming to be from these domains, which is how an
// operator finds out somebody is forging one.
func NewReportCommand() *cli.Command {
	return &cli.Command{
		Name:  "report",
		Usage: "DMARC aggregate reports received about your domains",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list reports, newest first",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "domain", Usage: "only this domain; every domain unless given"},
					&cli.IntFlag{Name: "first", Value: 50, Usage: "how many to show; 0 for every one"},
					JSONFlag(),
				},
				Action: runReportList,
			},
			{
				Name:      "get",
				Aliases:   []string{"show"},
				Usage:     "show a report; --json includes the feedback as it was parsed",
				ArgsUsage: "<report-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runReportGet,
			},
		},
	}
}

func runReportList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domainId := ""
	if name := command.String("domain"); name != "" {
		domain, err := requireDomain(ctx, command, connection, name)
		if err != nil {
			return err
		}
		domainId = domain.ID
	}
	reports, err := client.ListReports(ctx, connection, domainId, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	defer noteCapped(len(reports), int(command.Int("first")), "teanode report list")
	if command.Bool("json") {
		return PrintJSON(reports)
	}
	if len(reports) == 0 {
		fmt.Println("no reports; they arrive once a DMARC record with a rua address is published and somebody receives mail claiming to be from the domain")
		return nil
	}
	names := domainNames(ctx, connection)
	rows := make([][]string, 0, len(reports))
	for _, report := range reports {
		rows = append(rows, []string{
			report.ID, report.BeginAt.Local().Format("2006-01-02"), domainName(names, report.DomainID), report.FromDomain,
			report.SenderDomain, report.IP, fmt.Sprint(report.Count), alignment(report), report.Disposition,
		})
	}
	return printTable([]string{"ID", "PERIOD", "DOMAIN", "CLAIMED FROM", "SENT BY", "IP", "MESSAGES", "ALIGNED", "DISPOSITION"}, rows)
}

func alignment(report *client.Report) string {
	switch {
	case report.DKIMAligned && report.SPFAligned:
		return "dkim, spf"
	case report.DKIMAligned:
		return "dkim"
	case report.SPFAligned:
		return "spf"
	default:
		return "neither"
	}
}

func runReportGet(ctx context.Context, command *cli.Command) error {
	reportId := command.Args().First()
	if reportId == "" {
		return usage("which report? usage: teanode report get <report-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	report, err := client.GetReport(ctx, connection, reportId)
	if err != nil {
		return describeNotFound(command, err, "report "+reportId)
	}
	if command.Bool("json") {
		return PrintJSON(report)
	}
	return printFields([][2]string{
		{"id", report.ID},
		{"domain", domainName(domainNames(ctx, connection), report.DomainID)},
		{"period", report.BeginAt.Local().Format("2006-01-02 15:04") + " to " + report.EndAt.Local().Format("2006-01-02 15:04")},
		{"claimed from", report.FromDomain},
		{"sent by", strings.TrimSpace(report.SenderDomain + " " + report.IP)},
		{"reverse name", report.RDNS},
		{"messages", fmt.Sprint(report.Count)},
		{"aligned", alignment(report)},
		{"disposition", report.Disposition},
		{"received", formatTime(&report.CreatedAt)},
		{"mail", report.MailID},
	})
}
