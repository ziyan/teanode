package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// maximumTaxDocumentBytes is the largest file read-tax-document sends:
// more than any paystub or W-2.
const maximumTaxDocumentBytes = 20 << 20

// financeTaxCommands are the subcommands for paystubs, tax forms and the
// tax year.
func financeTaxCommands() []*cli.Command {
	taxYearFlag := func(usage string) cli.Flag {
		return &cli.IntFlag{Name: "tax-year", Usage: usage}
	}
	return []*cli.Command{
		{
			Name: "read-tax-document", Usage: "have your agent read paystubs, W-2s or 1099s from PDFs or photos, or from a message, and record them",
			ArgsUsage: "<file>... | --mailbox-item <id>",
			Description: "Each file goes up as a tax document, whose text the server never keeps, and the reading\n" +
				"job reads it in the background: whether it is a paystub, a W-2 or 1099s, and every line or box. Only the\n" +
				"last four digits of your taxpayer number are kept. --mailbox-item reads each PDF and picture a\n" +
				"message carries instead. teanode finance paystubs and teanode finance tax-forms show them once\n" +
				"read, and teanode finance tax-year puts the year together.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "mailbox-item", Usage: "read the PDFs and pictures this message carries instead of files, by its item id"},
			},
			Action: runFinanceReadTaxDocument,
		},
		{
			Name: "paystubs", Usage: "your paystubs, the latest pay date first, each with its check",
			Flags: append(rangeFlagsWithoutDefault(), JSONFlag(), taxYearFlag("only those paid in this year; not with a range"),
				&cli.IntFlag{Name: "limit", Usage: "how many, at most 200", Value: 60},
				&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"},
			),
			Action: runFinancePaystubs,
		},
		{Name: "paystub", Usage: "one paystub, line by line", ArgsUsage: "<paystub-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinancePaystub},
		{
			Name: "record-paystub", Usage: "record a paystub from a JSON file of its lines, checked so it adds up",
			ArgsUsage: "<file.json | ->",
			Description: "The file is one JSON object with the RecordPaystub arguments: agentAttachmentId (the uploaded\n" +
				"file it was read from), employerName, employerTaxId, employeeName, employeeTaxIdMask (the last four\n" +
				"digits only), payDate, periodStartOn, periodEndOn, currencyCode, grossAmount, yearToDateGrossAmount,\n" +
				"totalTaxAmount, totalDeductionAmount, netAmount and paystubLines ([{paystubLineKind, description,\n" +
				"hours, rateAmount, currentAmount, yearToDateAmount, paystubTaxKind, taxJurisdictionCode,\n" +
				"currentWagesAmount, yearToDateWagesAmount, paystubDeductionKind, employerCurrentAmount,\n" +
				"employerYearToDateAmount}]). A paystub that does not add up is refused unless\n" +
				"--is-unbalanced-accepted. Recording the same file again replaces what was read from it.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.BoolFlag{Name: "is-unbalanced-accepted", Usage: "record it even when its lines do not add up, marked unbalanced"},
			},
			Action: runFinanceRecordPaystub,
		},
		{
			Name: "delete-paystub", Usage: "delete a paystub, its lines and the file it was read from",
			ArgsUsage: "<paystub-id>", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runFinanceDeletePaystub,
		},
		{
			Name: "tax-forms", Usage: "your W-2s and 1099s, the latest tax year first, each with what it says about itself",
			Flags: []cli.Flag{JSONFlag(), taxYearFlag("only this year's"),
				&cli.IntFlag{Name: "limit", Usage: "how many, at most 200", Value: 60},
				&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"},
			},
			Action: runFinanceTaxForms,
		},
		{Name: "tax-form", Usage: "one W-2 or 1099, box by box", ArgsUsage: "<tax-form-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceTaxForm},
		{
			Name: "record-tax-form", Usage: "record a W-2 or a 1099 from a JSON file of its boxes",
			ArgsUsage: "<file.json | ->",
			Description: "The file is one JSON object with the RecordTaxForm arguments: agentAttachmentId (the uploaded\n" +
				"file it was read from), taxFormKind (w2, 1099_int, 1099_div or 1099_b), taxYear, isCorrected, payerName, payerTaxId,\n" +
				"recipientName, recipientTaxIdMask (the last four digits only) and taxFormEntries ([{boxCode,\n" +
				"boxQualifier, amount, textValue, isChecked}]): box 12's letter, a state's code in boxes 15 to 17\n" +
				"and box 14's label are the qualifier. Recording the same file again replaces what was read from it.",
			Flags:  []cli.Flag{JSONFlag()},
			Action: runFinanceRecordTaxForm,
		},
		{
			Name: "delete-tax-form", Usage: "delete a W-2, its boxes and the file it was read from",
			ArgsUsage: "<tax-form-id>", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runFinanceDeleteTaxForm,
		},
		{
			Name: "tax-year", Usage: "a tax year from your paystubs and W-2s: wages and withholding, projected to December 31, each W-2 box beside your paystubs, the tax owed beside what was withheld, and what is missing",
			ArgsUsage: "[year]",
			Description: "The estimate counts wages only: the tax on them less the standard deduction, Social Security\n" +
				"and Medicare as owed, state and local income tax as withheld, for the filing status you keep\n" +
				"(teanode finance set-tax-filing-status) or --filing-status; with neither, married filing jointly\n" +
				"when the documents name two people and single otherwise.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "filing-status", Usage: "estimate for this filing status instead of the one you keep: single, married_filing_jointly, married_filing_separately or head_of_household"},
			},
			Action: runFinanceTaxYear,
		},
		{
			Name: "tax-document-address", Usage: "the address to mail paystubs and W-2s to: each PDF or picture mailed there is read and recorded",
			Flags: []cli.Flag{JSONFlag()}, Action: runFinanceTaxDocumentAddress,
		},
		{
			Name: "regenerate-tax-document-address", Usage: "give the tax document address a new token; the old address stops taking mail",
			Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runFinanceRegenerateTaxDocumentAddress,
		},
		{
			Name: "set-tax-filing-status", Usage: "keep how you file your tax return, which teanode finance tax-year estimates the tax for",
			ArgsUsage: "single|married_filing_jointly|married_filing_separately|head_of_household",
			Flags:     []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "clear", Usage: "go back to assuming one from the number of people on your paystubs and W-2s"}},
			Action:    runFinanceSetTaxFilingStatus,
		},
	}
}

func runFinanceReadTaxDocument(ctx context.Context, command *cli.Command) error {
	itemId := strings.TrimSpace(command.String("mailbox-item"))
	paths := command.Args().Slice()
	if (itemId == "") == (len(paths) == 0) {
		return usage("give the files of the paystubs, W-2s or 1099s, or --mailbox-item with the message they are in, one of the two")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	readings := []*client.TaxDocumentReading{}
	read := func(variables map[string]any) error {
		var reading *client.TaxDocumentReading
		if err := describeError(command, client.RunFinance(ctx, connection, operationOf(command), variables, &reading)); err != nil {
			return err
		}
		readings = append(readings, reading)
		return nil
	}
	if itemId != "" {
		if err := read(map[string]any{"mailboxItemId": itemId}); err != nil {
			return err
		}
	}
	for _, path := range paths {
		var content []byte
		fileName := filepath.Base(path)
		if path == "-" {
			fileName = "document.pdf"
			content, err = io.ReadAll(io.LimitReader(os.Stdin, maximumTaxDocumentBytes+1))
		} else {
			content, err = readLimitedFile(path, maximumTaxDocumentBytes+1)
		}
		if err != nil {
			return err
		}
		if len(content) > maximumTaxDocumentBytes {
			return usage(fmt.Sprintf("%s is larger than %d MB, more than any paystub or W-2", fileName, maximumTaxDocumentBytes>>20))
		}
		attachment, err := client.UploadTaxDocument(ctx, connection, fileName, content)
		if err != nil {
			return describeError(command, err)
		}
		if err := read(map[string]any{"agentAttachmentId": attachment.ID}); err != nil {
			return err
		}
	}
	jobCount := 0
	for _, reading := range readings {
		jobCount += len(reading.AgentJobIDs)
	}
	return printDone(command, readings, fmt.Sprintf("queued: %d %s, read in the background; teanode finance paystubs and teanode finance tax-forms show them once read",
		jobCount, map[bool]string{true: "file", false: "files"}[jobCount == 1]))
}

func runFinancePaystubs(ctx context.Context, command *cli.Command) error {
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	variables := map[string]any{"limit": int(command.Int("limit")), "offset": int(command.Int("offset"))}
	if command.IsSet("tax-year") {
		if from != "" || to != "" {
			return usage("give --tax-year or a range of pay dates, not both")
		}
		variables["taxYear"] = int(command.Int("tax-year"))
	} else {
		variables["from"], variables["to"] = from, to
	}
	var page *client.PaystubPage
	if err := financeCall(ctx, command, operationOf(command), variables, &page); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FinancePaystubs) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no paystubs match")
		return nil
	}
	rows := make([][]string, 0, len(page.FinancePaystubs))
	for _, paystub := range page.FinancePaystubs {
		taxes, deductions := paystubLineTotals(paystub)
		rows = append(rows, []string{paystub.PayDate, paystub.EmployerName, money(paystub.GrossAmount, paystub.CurrencyCode),
			money(taxes, paystub.CurrencyCode), money(deductions, paystub.CurrencyCode), money(paystub.NetAmount, paystub.CurrencyCode),
			paystubCheckWords(paystub), paystub.ID})
	}
	if err := printTable([]string{"pay date", "employer", "gross", "taxes", "deductions", "take-home", "check", "id"}, rows); err != nil {
		return err
	}
	if page.HasMore {
		fmt.Fprintf(os.Stderr, "%d of %d shown; --offset %d for the next page\n", len(page.FinancePaystubs), page.TotalCount, int(command.Int("offset"))+len(page.FinancePaystubs))
	}
	return nil
}

// paystubLineTotals are what a paystub's tax lines and deduction lines
// come to this period.
func paystubLineTotals(paystub *models.FinancePaystub) (string, string) {
	taxes, deductions := new(big.Rat), new(big.Rat)
	for _, line := range paystub.PaystubLines {
		amount, err := finance.ParseAmount(line.CurrentAmount)
		if err != nil {
			continue
		}
		switch line.PaystubLineKind {
		case models.PaystubLineKindTax:
			taxes.Add(taxes, amount)
		case models.PaystubLineKindDeduction:
			deductions.Add(deductions, amount)
		}
	}
	return finance.FormatAmount(taxes), finance.FormatAmount(deductions)
}

// paystubCheckWords says whether a paystub adds up.
func paystubCheckWords(paystub *models.FinancePaystub) string {
	if paystub.PaystubCheckState == models.PaystubCheckStateBalanced {
		return "adds up"
	}
	return "off by " + finance.FormatReceiptAmount(paystub.CheckDifferenceAmount, paystub.CurrencyCode)
}

func runFinancePaystub(ctx context.Context, command *cli.Command) error {
	paystubId, err := financeArgument(command, 0, "the paystub's id; teanode finance paystubs lists them")
	if err != nil {
		return err
	}
	var paystub *models.FinancePaystub
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"paystubId": paystubId}, &paystub); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(paystub)
	}
	heading := fmt.Sprintf("%s, paid %s", paystub.EmployerName, paystub.PayDate)
	if paystub.PeriodStartOn != "" && paystub.PeriodEndOn != "" {
		heading += fmt.Sprintf(" for %s to %s", paystub.PeriodStartOn, paystub.PeriodEndOn)
	}
	_, _ = fmt.Fprintln(command.Writer, forTerminal(heading))
	rows := make([][]string, 0, len(paystub.PaystubLines))
	for _, line := range paystub.PaystubLines {
		kind := string(line.PaystubLineKind)
		switch {
		case line.PaystubTaxKind != "":
			kind = string(line.PaystubTaxKind)
			if line.TaxJurisdictionCode != "" {
				kind += " " + line.TaxJurisdictionCode
			}
		case line.PaystubDeductionKind != "":
			kind = string(line.PaystubDeductionKind)
		}
		rows = append(rows, []string{fmt.Sprint(line.LineNumber), kind, line.Description,
			finance.FormatReceiptAmount(line.CurrentAmount, paystub.CurrencyCode), finance.FormatReceiptAmount(line.YearToDateAmount, paystub.CurrencyCode),
			finance.FormatReceiptAmount(line.CurrentWagesAmount, paystub.CurrencyCode), finance.FormatReceiptAmount(line.EmployerCurrentAmount, paystub.CurrencyCode)})
	}
	if err := printTable([]string{"line", "kind", "description", "this period", "year to date", "wages", "employer"}, rows); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(command.Writer, "gross "+money(paystub.GrossAmount, paystub.CurrencyCode)+", take-home "+money(paystub.NetAmount, paystub.CurrencyCode)+", "+paystubCheckWords(paystub))
	return nil
}

// readTaxDocumentFile reads the JSON file record-paystub and
// record-tax-form take into the operation's variables.
func readTaxDocumentFile(path string) (map[string]any, error) {
	var content []byte
	var err error
	if path == "-" {
		content, err = io.ReadAll(io.LimitReader(os.Stdin, maximumReceiptFileBytes+1))
	} else {
		content, err = readLimitedFile(path, maximumReceiptFileBytes+1)
	}
	if err != nil {
		return nil, err
	}
	if len(content) > maximumReceiptFileBytes {
		return nil, usage(fmt.Sprintf("the file is larger than %d kB, more than any paystub or W-2", maximumReceiptFileBytes/1024))
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var variables map[string]any
	if err := decoder.Decode(&variables); err != nil {
		return nil, usage("the file is not one JSON object: " + err.Error())
	}
	return variables, nil
}

func runFinanceRecordPaystub(ctx context.Context, command *cli.Command) error {
	path, err := financeArgument(command, 0, "the JSON file of the paystub: teanode finance record-paystub paystub.json")
	if err != nil {
		return err
	}
	variables, err := readTaxDocumentFile(path)
	if err != nil {
		return err
	}
	if command.Bool("is-unbalanced-accepted") {
		variables["isUnbalancedAccepted"] = true
	}
	var paystub *models.FinancePaystub
	if err := financeCall(ctx, command, operationOf(command), variables, &paystub); err != nil {
		return err
	}
	return printDone(command, paystub, fmt.Sprintf("%s: %s paid %s, %s take-home, %s", paystub.ID, paystub.EmployerName, paystub.PayDate,
		money(paystub.NetAmount, paystub.CurrencyCode), paystubCheckWords(paystub)))
}

func runFinanceDeletePaystub(ctx context.Context, command *cli.Command) error {
	paystubId, err := financeArgument(command, 0, "the paystub's id; teanode finance paystubs lists them")
	if err != nil {
		return err
	}
	if err := confirm(command, "Delete paystub "+paystubId+", its lines and the file it was read from?"); err != nil {
		return err
	}
	var isDeleted bool
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"paystubId": paystubId}, &isDeleted); err != nil {
		return err
	}
	return printDone(command, map[string]any{"paystubId": paystubId, "isDeleted": isDeleted}, paystubId+": deleted")
}

// taxFormCheckWords says whether a tax form agrees with itself.
func taxFormCheckWords(findings []finance.TaxFormFinding) string {
	disagreeing := 0
	for _, finding := range findings {
		if !finding.IsAgreeing {
			disagreeing++
		}
	}
	switch {
	case len(findings) == 0:
		return ""
	case disagreeing == 0:
		return "agrees with itself"
	}
	return fmt.Sprintf("%d of %d checks disagree", disagreeing, len(findings))
}

func runFinanceTaxForms(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{"limit": int(command.Int("limit")), "offset": int(command.Int("offset"))}
	if command.IsSet("tax-year") {
		variables["taxYear"] = int(command.Int("tax-year"))
	}
	var page *client.TaxFormPage
	if err := financeCall(ctx, command, operationOf(command), variables, &page); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.TaxForms) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no tax forms match")
		return nil
	}
	rows := make([][]string, 0, len(page.TaxForms))
	for _, view := range page.TaxForms {
		taxForm := view.FinanceTaxForm
		kind := finance.TaxFormKindName(taxForm.TaxFormKind)
		if taxForm.IsCorrected {
			kind += " (corrected)"
		}
		rows = append(rows, []string{fmt.Sprint(taxForm.TaxYear), kind, taxForm.PayerName, taxForm.RecipientName, taxFormCheckWords(view.TaxFormFindings), taxForm.ID})
	}
	return printTable([]string{"year", "form", "payer", "recipient", "check", "id"}, rows)
}

func runFinanceTaxForm(ctx context.Context, command *cli.Command) error {
	taxFormId, err := financeArgument(command, 0, "the tax form's id; teanode finance tax-forms lists them")
	if err != nil {
		return err
	}
	var view *client.TaxFormView
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"taxFormId": taxFormId}, &view); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	taxForm := view.FinanceTaxForm
	_, _ = fmt.Fprintln(command.Writer, forTerminal(fmt.Sprintf("%d %s from %s for %s", taxForm.TaxYear, finance.TaxFormKindName(taxForm.TaxFormKind), taxForm.PayerName, taxForm.RecipientName)))
	rows := make([][]string, 0, len(taxForm.TaxFormEntries))
	for _, entry := range taxForm.TaxFormEntries {
		value := finance.FormatReceiptAmount(entry.Amount, "USD")
		switch {
		case entry.IsChecked:
			value = "checked"
		case value == "":
			value = entry.TextValue
		}
		rows = append(rows, []string{entry.BoxCode, entry.BoxQualifier, value})
	}
	if err := printTable([]string{"box", "of", "value"}, rows); err != nil {
		return err
	}
	for _, finding := range view.TaxFormFindings {
		_, _ = fmt.Fprintln(command.Writer, finding.FindingText)
	}
	return nil
}

func runFinanceRecordTaxForm(ctx context.Context, command *cli.Command) error {
	path, err := financeArgument(command, 0, "the JSON file of the form: teanode finance record-tax-form w2.json")
	if err != nil {
		return err
	}
	variables, err := readTaxDocumentFile(path)
	if err != nil {
		return err
	}
	var view *client.TaxFormView
	if err := financeCall(ctx, command, operationOf(command), variables, &view); err != nil {
		return err
	}
	return printDone(command, view, fmt.Sprintf("%s: %d W-2 from %s, %s", view.FinanceTaxForm.ID, view.FinanceTaxForm.TaxYear,
		view.FinanceTaxForm.PayerName, taxFormCheckWords(view.TaxFormFindings)))
}

func runFinanceDeleteTaxForm(ctx context.Context, command *cli.Command) error {
	taxFormId, err := financeArgument(command, 0, "the tax form's id; teanode finance tax-forms lists them")
	if err != nil {
		return err
	}
	if err := confirm(command, "Delete tax form "+taxFormId+", its boxes and the file it was read from?"); err != nil {
		return err
	}
	var isDeleted bool
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"taxFormId": taxFormId}, &isDeleted); err != nil {
		return err
	}
	return printDone(command, map[string]any{"taxFormId": taxFormId, "isDeleted": isDeleted}, taxFormId+": deleted")
}

func runFinanceTaxDocumentAddress(ctx context.Context, command *cli.Command) error {
	var address *client.TaxDocumentAddress
	if err := financeCall(ctx, command, operationOf(command), nil, &address); err != nil {
		return err
	}
	return printTaxDocumentAddress(command, address)
}

func runFinanceRegenerateTaxDocumentAddress(ctx context.Context, command *cli.Command) error {
	if err := confirm(command, "Give the tax document address a new token? Mail to the old address will be refused."); err != nil {
		return err
	}
	var address *client.TaxDocumentAddress
	if err := financeCall(ctx, command, operationOf(command), nil, &address); err != nil {
		return err
	}
	return printTaxDocumentAddress(command, address)
}

func printTaxDocumentAddress(command *cli.Command, address *client.TaxDocumentAddress) error {
	if command.Bool("json") {
		return PrintJSON(address)
	}
	if address.TaxDocumentAddress == "" {
		_, _ = fmt.Fprintln(command.Writer, "tax document address: none, since you have no mailbox address to receive mail; teanode finance read-tax-document takes files")
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "tax document address: %s\n", address.TaxDocumentAddress)
	_, _ = fmt.Fprintln(command.Writer, "each PDF or picture mailed there is read as a paystub, a W-2 or 1099s and shows in teanode finance tax-year")
	if !address.CanReadPDFs {
		_, _ = fmt.Fprintln(command.Writer, "this server cannot read PDFs: mail photos or screenshots")
	}
	return nil
}

func runFinanceSetTaxFilingStatus(ctx context.Context, command *cli.Command) error {
	filingStatus := strings.TrimSpace(command.Args().First())
	if command.Bool("clear") {
		filingStatus = ""
	} else if filingStatus == "" {
		return usage("give a filing status: single, married_filing_jointly, married_filing_separately or head_of_household; or --clear")
	}
	var saved string
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"taxFilingStatus": filingStatus}, &saved); err != nil {
		return err
	}
	if saved == "" {
		return printDone(command, map[string]string{"taxFilingStatus": saved}, "no filing status kept: the estimate assumes one from the number of people")
	}
	return printDone(command, map[string]string{"taxFilingStatus": saved}, "the tax year is estimated filing "+strings.ReplaceAll(saved, "_", " "))
}

func runFinanceTaxYear(ctx context.Context, command *cli.Command) error {
	taxYear := time.Now().Year()
	if text := strings.TrimSpace(command.Args().Get(0)); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil {
			return usage("the tax year is a year, as 2026")
		}
		taxYear = parsed
	}
	variables := map[string]any{"taxYear": taxYear}
	if filingStatus := strings.TrimSpace(command.String("filing-status")); filingStatus != "" {
		variables["taxFilingStatus"] = filingStatus
	}
	var summary *finance.TaxYearSummary
	if err := financeCall(ctx, command, operationOf(command), variables, &summary); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(summary)
	}
	if summary.PaystubCount == 0 && summary.TaxFormCount == 0 {
		_, _ = fmt.Fprintf(command.Writer, "no paystubs, W-2s or 1099s for %d; teanode finance read-tax-document adds them\n", taxYear)
		return nil
	}
	for _, employment := range summary.TaxYearEmployments {
		heading := fmt.Sprintf("%s at %s: ", employment.EmployeeName, employment.EmployerName)
		if employment.TaxYearFigureSource == finance.TaxYearFigureSourceW2 {
			heading += "from the W-2"
		} else {
			heading += fmt.Sprintf("from %d %s through %s", employment.PaystubCount, map[bool]string{true: "paystub", false: "paystubs"}[employment.PaystubCount == 1], employment.LatestPayDate)
		}
		_, _ = fmt.Fprintln(command.Writer, forTerminal(heading))
		if err := printTaxYearFigures(employment.Figures, employment.ProjectedFigures); err != nil {
			return err
		}
		if len(employment.TaxYearComparisons) > 0 {
			rows := make([][]string, 0, len(employment.TaxYearComparisons))
			for _, comparison := range employment.TaxYearComparisons {
				matching := "matches"
				if !comparison.IsMatching {
					matching = "differs by " + comparison.DifferenceAmount
				}
				rows = append(rows, []string{strings.TrimSpace(comparison.BoxCode + " " + comparison.BoxQualifier), comparison.BoxLabel, comparison.W2Amount, comparison.PaystubAmount, matching})
			}
			paystubColumn := "paystubs through " + employment.LatestPayDate
			if err := printTable([]string{"box", "", "W-2", paystubColumn, ""}, rows); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintln(command.Writer)
	}
	if len(summary.TaxYearEmployments) > 1 {
		_, _ = fmt.Fprintln(command.Writer, "household")
		if err := printTaxYearFigures(summary.Figures, summary.ProjectedFigures); err != nil {
			return err
		}
	}
	if investment := summary.TaxYearInvestment; investment != nil {
		_, _ = fmt.Fprintln(command.Writer, "investment income from 1099s")
		rows := [][]string{
			{"interest", investment.InterestAmount},
			{"ordinary dividends", investment.OrdinaryDividendsAmount},
			{"qualified dividends (part of ordinary)", investment.QualifiedDividendsAmount},
			{"capital gain distributions", investment.CapitalGainDistributionsAmount},
			{"short-term gain or loss", investment.ShortTermGainAmount},
			{"long-term gain or loss", investment.LongTermGainAmount},
		}
		for _, optional := range [][]string{
			{"tax-exempt interest", investment.TaxExemptInterestAmount},
			{"federal income tax withheld", investment.FederalIncomeTaxWithheldAmount},
			{"foreign tax paid", investment.ForeignTaxPaidAmount},
		} {
			if optional[1] != "0.00" {
				rows = append(rows, optional)
			}
		}
		if err := printTable([]string{"", ""}, rows); err != nil {
			return err
		}
	}
	if estimate := summary.TaxYearEstimate; estimate != nil {
		heading := "tax owed beside withheld, filing " + strings.ReplaceAll(string(estimate.TaxFilingStatus), "_", " ")
		if estimate.IsTaxFilingStatusAssumed {
			heading += " (assumed; teanode finance set-tax-filing-status keeps yours)"
		}
		if estimate.IsProjected {
			heading += ", by December 31"
		}
		_, _ = fmt.Fprintln(command.Writer, heading)
		rows := [][]string{{"pay (Medicare wages)", estimate.PayAmount}}
		if estimate.InvestmentIncomeAmount != "0.00" {
			rows = append(rows, []string{"investment income", estimate.InvestmentIncomeAmount}, []string{"adjusted gross income", estimate.AdjustedGrossIncomeAmount})
		}
		if estimate.CapitalLossDeductionAmount != "0.00" {
			rows = append(rows, []string{"capital loss taken", estimate.CapitalLossDeductionAmount})
		}
		rows = append(rows,
			[]string{"standard deduction", estimate.StandardDeductionAmount},
			[]string{"taxable income", estimate.TaxableIncomeAmount},
		)
		if estimate.PreferentialIncomeAmount != "0.00" {
			rows = append(rows, []string{"of it, qualified dividends and long-term gain", estimate.PreferentialIncomeAmount},
				[]string{"their tax at 0%, 15% or 20%", estimate.PreferentialIncomeTaxAmount})
		}
		if estimate.ForeignTaxCreditAmount != "0.00" {
			rows = append(rows, []string{"foreign tax credit", estimate.ForeignTaxCreditAmount})
		}
		rows = append(rows,
			[]string{"federal income tax", estimate.FederalIncomeTaxAmount},
			[]string{"Social Security tax", estimate.SocialSecurityTaxAmount},
			[]string{"Medicare tax", estimate.MedicareTaxAmount},
		)
		if estimate.NetInvestmentIncomeTaxAmount != "0.00" {
			rows = append(rows, []string{"net investment income tax", estimate.NetInvestmentIncomeTaxAmount})
		}
		rows = append(rows, []string{"state income tax, as withheld", estimate.StateIncomeTaxAmount})
		if estimate.LocalIncomeTaxAmount != "0.00" {
			rows = append(rows, []string{"local income tax, as withheld", estimate.LocalIncomeTaxAmount})
		}
		rows = append(rows, []string{"tax for the year", estimate.TotalTaxAmount}, []string{"paid so far", estimate.PaidTaxAmount})
		if estimate.IsProjected {
			rows = append(rows, []string{"still to be withheld", estimate.UpcomingWithheldTaxAmount})
		}
		if estimate.RefundAmount != "0.00" {
			rows = append(rows, []string{"refund at filing", estimate.RefundAmount})
		} else {
			rows = append(rows, []string{"owing at filing", estimate.OwingTaxAmount})
		}
		rows = append(rows, []string{"remaining income", estimate.RemainingIncomeAmount})
		if err := printTable([]string{"", ""}, rows); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(command.Writer, "wages and 1099s only: other income, itemized deductions and credits are not counted, nor a state's tax on investment income")
	}
	for _, finding := range summary.TaxYearFindings {
		_, _ = fmt.Fprintln(command.Writer, "- "+forTerminal(finding.FindingText))
	}
	return nil
}

// printTaxYearFigures writes a year's figures, with a column to December
// 31 when they are projected.
func printTaxYearFigures(figures, projected *finance.TaxYearFigures) error {
	type figureRow struct {
		label         string
		figure        func(*finance.TaxYearFigures) string
		isAlwaysShown bool
	}
	rows := []figureRow{
		{"wages (box 1)", func(figures *finance.TaxYearFigures) string { return figures.WagesAmount }, true},
		{"federal income tax withheld", func(figures *finance.TaxYearFigures) string { return figures.FederalIncomeTaxAmount }, true},
		{"Social Security wages", func(figures *finance.TaxYearFigures) string { return figures.SocialSecurityWagesAmount }, false},
		{"Social Security tax", func(figures *finance.TaxYearFigures) string { return figures.SocialSecurityTaxAmount }, false},
		{"Medicare wages", func(figures *finance.TaxYearFigures) string { return figures.MedicareWagesAmount }, false},
		{"Medicare tax", func(figures *finance.TaxYearFigures) string { return figures.MedicareTaxAmount }, false},
		{"traditional retirement", func(figures *finance.TaxYearFigures) string { return figures.RetirementTraditionalAmount }, false},
		{"Roth retirement", func(figures *finance.TaxYearFigures) string { return figures.RetirementRothAmount }, false},
		{"HSA", func(figures *finance.TaxYearFigures) string { return figures.HSAAmount }, false},
		{"dependent care", func(figures *finance.TaxYearFigures) string { return figures.DependentCareAmount }, false},
		{"local income tax", func(figures *finance.TaxYearFigures) string { return figures.LocalIncomeTaxAmount }, false},
	}
	headers := []string{"", "so far"}
	if projected != nil {
		headers = append(headers, "by December 31")
	}
	var table [][]string
	for _, row := range rows {
		value := row.figure(figures)
		if !row.isAlwaysShown && (value == "" || value == "0.00") {
			continue
		}
		line := []string{row.label, value}
		if projected != nil {
			line = append(line, row.figure(projected))
		}
		table = append(table, line)
	}
	for index, state := range figures.TaxYearStateFigures {
		wages, tax := []string{state.StateCode + " wages", state.StateWagesAmount}, []string{state.StateCode + " income tax", state.StateIncomeTaxAmount}
		if projected != nil && index < len(projected.TaxYearStateFigures) {
			wages = append(wages, projected.TaxYearStateFigures[index].StateWagesAmount)
			tax = append(tax, projected.TaxYearStateFigures[index].StateIncomeTaxAmount)
		}
		table = append(table, wages, tax)
	}
	return printTable(headers, table)
}
