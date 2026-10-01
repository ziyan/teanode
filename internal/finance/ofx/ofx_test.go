package ofx_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/finance/ofx"
)

// cardStatementSGML is an invented OFX 1.x card statement shaped like a
// phone wallet's export: a header block, unclosed leaves, LF line endings,
// zones written without a sign, and transactions with only a type, a day,
// an amount, a FITID and a name.
const cardStatementSGML = `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII
CHARSET:1252
COMPRESSION:NONE
OLDFILEUID:NONE
NEWFILEUID:NONE

<OFX>
<SIGNONMSGSRSV1>
<SONRS>
<STATUS>
<CODE>0
<SEVERITY>INFO
</STATUS>
<DTSERVER>20260202093000[0:GMT]
<LANGUAGE>ENG
<FI>
<ORG>Invented Card Issuer
<FID>99999
</FI>
</SONRS>
</SIGNONMSGSRSV1>
<CREDITCARDMSGSRSV1>
<CCSTMTTRNRS>
<TRNUID>0
<STATUS>
<CODE>0
<SEVERITY>INFO
</STATUS>
<CCSTMTRS>
<CURDEF>USD
<CCACCTFROM>
<ACCTID>11111a11-1aa1-1111-a11
</CCACCTFROM>
<BANKTRANLIST>
<DTSTART>20260101000000[0:GMT]
<DTEND>20260131235959[0:GMT]
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20260105120000[0:GMT]
<TRNAMT>-23.40
<FITID>aaaa0001-0000-0000-0000-000000000001
<NAME>INVENTED COFFEE ROASTERS
</STMTTRN>
<STMTTRN>
<TRNTYPE>PAYMENT
<DTPOSTED>20260115120000[0:GMT]
<TRNAMT>150.00
<FITID>aaaa0001-0000-0000-0000-000000000002
<NAME>PAYMENT RECEIVED - THANK YOU
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20260120120000[0:GMT]
<TRNAMT>12.00
<FITID>aaaa0001-0000-0000-0000-000000000003
<NAME>INVENTED BOOKSHOP &amp; CAFE
</STMTTRN>
</BANKTRANLIST>
<LEDGERBAL>
<BALAMT>-311.25
<DTASOF>20260131235959[0:GMT]
</LEDGERBAL>
<AVAILBAL>
<BALAMT>4688.75
<DTASOF>20260131235959[0:GMT]
</AVAILBAL>
</CCSTMTRS>
</CCSTMTTRNRS>
</CREDITCARDMSGSRSV1>
</OFX>
`

// bankStatementXML is an invented OFX 2.x bank statement: XML, every
// element closed, a zone with a signed offset and milliseconds, a comma as
// the decimal separator, and a memo and a payee.
const bankStatementXML = `<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<?OFX OFXHEADER="200" VERSION="211" SECURITY="NONE" OLDFILEUID="NONE" NEWFILEUID="NONE"?>
<OFX>
  <SIGNONMSGSRSV1>
    <SONRS>
      <STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS>
      <DTSERVER>20260301080000.000[-5:EST]</DTSERVER>
      <LANGUAGE>ENG</LANGUAGE>
      <FI><ORG>Invented Savings Bank</ORG><FID>12345</FID></FI>
    </SONRS>
  </SIGNONMSGSRSV1>
  <BANKMSGSRSV1>
    <STMTTRNRS>
      <TRNUID>1</TRNUID>
      <STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS>
      <STMTRS>
        <CURDEF>EUR</CURDEF>
        <BANKACCTFROM>
          <BANKID>000000000</BANKID>
          <ACCTID>000123456789</ACCTID>
          <ACCTTYPE>CHECKING</ACCTTYPE>
        </BANKACCTFROM>
        <BANKTRANLIST>
          <DTSTART>20260201</DTSTART>
          <DTEND>20260228</DTEND>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20260210230000.000[-5:EST]</DTPOSTED>
            <TRNAMT>-1.234,56</TRNAMT>
            <FITID>B-0001</FITID>
            <NAME>INVENTED LANDLORD</NAME>
            <MEMO>February rent</MEMO>
          </STMTTRN>
          <STMTTRN>
            <TRNTYPE>DIRECTDEP</TRNTYPE>
            <DTPOSTED>20260215</DTPOSTED>
            <TRNAMT>2500,00</TRNAMT>
            <FITID>B-0002</FITID>
            <PAYEE><NAME>Invented Employer</NAME></PAYEE>
            <MEMO>Salary</MEMO>
          </STMTTRN>
        </BANKTRANLIST>
        <LEDGERBAL><BALAMT>5432,10</BALAMT><DTASOF>20260228235959.000[-5:EST]</DTASOF></LEDGERBAL>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
</OFX>
`

func TestParseCardStatementSGML(test *testing.T) {
	test.Parallel()
	document, err := ofx.Parse(test.Context(), []byte(cardStatementSGML))
	if err != nil {
		test.Fatal(err)
	}
	if document.InstitutionOrganization != "Invented Card Issuer" || document.InstitutionID != "99999" {
		test.Errorf("institution %q %q", document.InstitutionOrganization, document.InstitutionID)
	}
	if len(document.Statements) != 1 {
		test.Fatalf("%d statements", len(document.Statements))
	}
	statement := document.Statements[0]
	if statement.StatementKind != ofx.StatementKindCreditCard || statement.CurrencyCode != "USD" || statement.AccountID != "11111a11-1aa1-1111-a11" {
		test.Errorf("statement %+v", statement)
	}
	if statement.StartedOn != "2026-01-01" || statement.EndedOn != "2026-01-31" {
		test.Errorf("covers %s to %s", statement.StartedOn, statement.EndedOn)
	}
	if statement.LedgerBalance == nil || statement.LedgerBalance.Amount != "-311.25" || statement.LedgerBalance.AsOfDay != "2026-01-31" {
		test.Errorf("ledger balance %+v", statement.LedgerBalance)
	}
	if statement.AvailableBalance == nil || statement.AvailableBalance.Amount != "4688.75" {
		test.Errorf("available balance %+v", statement.AvailableBalance)
	}
	if len(statement.Transactions) != 3 {
		test.Fatalf("%d transactions", len(statement.Transactions))
	}
	wanted := []struct{ transactionType, amount, fitId, name, postedOn string }{
		{"DEBIT", "-23.40", "aaaa0001-0000-0000-0000-000000000001", "INVENTED COFFEE ROASTERS", "2026-01-05"},
		{"PAYMENT", "150.00", "aaaa0001-0000-0000-0000-000000000002", "PAYMENT RECEIVED - THANK YOU", "2026-01-15"},
		{"CREDIT", "12.00", "aaaa0001-0000-0000-0000-000000000003", "INVENTED BOOKSHOP & CAFE", "2026-01-20"},
	}
	for index, transaction := range statement.Transactions {
		expected := wanted[index]
		if transaction.TransactionType != expected.transactionType || transaction.Amount != expected.amount ||
			transaction.FITID != expected.fitId || transaction.Name != expected.name || transaction.PostedOn != expected.postedOn || !transaction.HasPostedTime {
			test.Errorf("transaction %d: %+v", index, transaction)
		}
	}
}

func TestParseBankStatementXML(test *testing.T) {
	test.Parallel()
	document, err := ofx.Parse(test.Context(), []byte(bankStatementXML))
	if err != nil {
		test.Fatal(err)
	}
	statement := document.Statements[0]
	if statement.StatementKind != ofx.StatementKindBank || statement.CurrencyCode != "EUR" || statement.AccountType != "CHECKING" ||
		statement.BankID != "000000000" || statement.AccountID != "000123456789" {
		test.Errorf("statement %+v", statement)
	}
	if statement.StartedOn != "2026-02-01" || statement.EndedOn != "2026-02-28" {
		test.Errorf("covers %s to %s", statement.StartedOn, statement.EndedOn)
	}
	rent, salary := statement.Transactions[0], statement.Transactions[1]
	// Eleven at night in New York is the next day in UTC; the day is the
	// one the institution wrote.
	if rent.Amount != "-1234.56" || rent.PostedOn != "2026-02-10" || rent.Memo != "February rent" || rent.Name != "INVENTED LANDLORD" {
		test.Errorf("rent %+v", rent)
	}
	if !rent.PostedAt.Equal(time.Date(2026, time.February, 11, 4, 0, 0, 0, time.UTC)) {
		test.Errorf("rent posted at %s", rent.PostedAt.UTC())
	}
	if salary.Amount != "2500.00" || salary.PayeeName != "Invented Employer" || salary.HasPostedTime || salary.PostedOn != "2026-02-15" {
		test.Errorf("salary %+v", salary)
	}
	if statement.LedgerBalance == nil || statement.LedgerBalance.Amount != "5432.10" || statement.LedgerBalance.AsOfDay != "2026-02-28" {
		test.Errorf("ledger balance %+v", statement.LedgerBalance)
	}
	if statement.AvailableBalance != nil {
		test.Errorf("an available balance from nowhere: %+v", statement.AvailableBalance)
	}
}

// Windows line endings, lower case tags, a file that is not UTF-8, and an
// empty leaf left unclosed all read.
func TestParseToleratesHowFilesAreWritten(test *testing.T) {
	test.Parallel()
	content := strings.ReplaceAll(cardStatementSGML, "\n", "\r\n")
	content = strings.Replace(content, "<NAME>INVENTED COFFEE ROASTERS", "<name>INVENTED CAF\xc9\r\n<MEMO>", 1)
	document, err := ofx.Parse(test.Context(), []byte(content))
	if err != nil {
		test.Fatal(err)
	}
	first := document.Statements[0].Transactions[0]
	if first.Name != "INVENTED CAFÉ" || first.Memo != "" || first.FITID == "" {
		test.Errorf("%+v", first)
	}
	if len(document.Statements[0].Transactions) != 3 {
		test.Errorf("%d transactions", len(document.Statements[0].Transactions))
	}
}

func TestParseDate(test *testing.T) {
	test.Parallel()
	for _, example := range []struct {
		text    string
		moment  time.Time
		day     string
		hasTime bool
	}{
		{"20260131", time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), "2026-01-31", false},
		{"20260131120000", time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), "2026-01-31", true},
		{"20260131120000.000[-5:EST]", time.Date(2026, 1, 31, 17, 0, 0, 0, time.UTC), "2026-01-31", true},
		{"20260131120000[0:GMT]", time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), "2026-01-31", true},
		{"20260131020000[+9:JST]", time.Date(2026, 1, 30, 17, 0, 0, 0, time.UTC), "2026-01-31", true},
		{"20260131020000[5.5:IST]", time.Date(2026, 1, 30, 20, 30, 0, 0, time.UTC), "2026-01-31", true},
		{"202601311200", time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), "2026-01-31", true},
		{"20260131120000[-8]", time.Date(2026, 1, 31, 20, 0, 0, 0, time.UTC), "2026-01-31", true},
	} {
		moment, day, hasTime, err := ofx.ParseDate(example.text)
		if err != nil || !moment.Equal(example.moment) || day != example.day || hasTime != example.hasTime {
			test.Errorf("%s: %s %s %v %v", example.text, moment.UTC(), day, hasTime, err)
		}
	}
	for _, refused := range []string{"", "2026-01-31", "202601", "20261341", "20260131120000[-5:EST", "20260131[x:Y]"} {
		if _, _, _, err := ofx.ParseDate(refused); err == nil {
			test.Errorf("%q was read as a date", refused)
		}
	}
}

func TestParseAmount(test *testing.T) {
	test.Parallel()
	for text, wanted := range map[string]string{
		"-23.4": "-23.40", "150": "150.00", "+12.00": "12.00", "-12,34": "-12.34", "1.234,56": "1234.56",
		"1,234.56": "1234.56", ".5": "0.50", "0.125": "0.125", " -7.00 ": "-7.00",
	} {
		have, err := ofx.ParseAmount(text)
		if err != nil || have != wanted {
			test.Errorf("%q: %q %v, not %q", text, have, err, wanted)
		}
	}
	for _, refused := range []string{"", "-", "1,2,3", "12.3.4x", "--5", "1e5", "twelve"} {
		if have, err := ofx.ParseAmount(refused); err == nil {
			test.Errorf("%q was read as %q", refused, have)
		}
	}
}

func TestIsOFX(test *testing.T) {
	test.Parallel()
	for _, content := range []string{cardStatementSGML, bankStatementXML, "\xef\xbb\xbfOFXHEADER:100\n<OFX></OFX>", "<OFX>\n<SIGNONMSGSRSV1>"} {
		if !ofx.IsOFX([]byte(content)) {
			test.Errorf("not taken for OFX: %.40q", content)
		}
	}
	for _, content := range []string{"", "%PDF-1.7", "Date,Description,Amount\n2026-01-05,COFFEE,-3.00", "<html><body>OFX</body></html>"} {
		if ofx.IsOFX([]byte(content)) {
			test.Errorf("taken for OFX: %.40q", content)
		}
	}
}

func TestParseRefusals(test *testing.T) {
	test.Parallel()
	if _, err := ofx.Parse(test.Context(), []byte("Date,Amount\n")); !errors.Is(err, ofx.ErrNotOFX) {
		test.Errorf("a spreadsheet: %v", err)
	}
	if _, err := ofx.Parse(test.Context(), make([]byte, ofx.MaximumFileBytes+1)); !errors.Is(err, ofx.ErrTooLarge) {
		test.Errorf("a file too large: %v", err)
	}
	refusal := `OFXHEADER:100
<OFX><SIGNONMSGSRSV1><SONRS><STATUS><CODE>15500<SEVERITY>ERROR<MESSAGE>Invented sign-on failure</STATUS></SONRS></SIGNONMSGSRSV1></OFX>`
	if _, err := ofx.Parse(test.Context(), []byte(refusal)); !errors.Is(err, ofx.ErrNoStatement) || !strings.Contains(err.Error(), "Invented sign-on failure") {
		test.Errorf("a refusal: %v", err)
	}
	noAccount := strings.Replace(cardStatementSGML, "<ACCTID>11111a11-1aa1-1111-a11\n", "", 1)
	if _, err := ofx.Parse(test.Context(), []byte(noAccount)); err == nil {
		test.Error("a statement with no account was read")
	}
	badAmount := strings.Replace(cardStatementSGML, "<TRNAMT>-23.40", "<TRNAMT>about twenty", 1)
	if _, err := ofx.Parse(test.Context(), []byte(badAmount)); err == nil {
		test.Error("an amount that is not one was read")
	}
	deep := "OFXHEADER:100\n<OFX>" + strings.Repeat("<A>", 100) + "</OFX>"
	if _, err := ofx.Parse(test.Context(), []byte(deep)); err == nil {
		test.Error("a file nested a hundred deep was read")
	}
}

// parseWithin parses content and fails the test when parsing takes longer
// than the limit, answering the error otherwise.
func parseWithin(test *testing.T, ctx context.Context, content string, limit time.Duration) error {
	test.Helper()
	finished := make(chan error, 1)
	go func() {
		_, err := ofx.Parse(ctx, []byte(content))
		finished <- err
	}()
	select {
	case err := <-finished:
		return err
	case <-time.After(limit):
		test.Fatalf("parsing %d bytes took longer than %s", len(content), limit)
		return nil
	}
}

// A leaf broken up by tags the reader skips, comments, processing
// instructions and empty tags, used to have each run of text joined onto
// all of it before, so a couple of megabytes took minutes. Every skipped
// tag counts toward the limit on tags too, so a file of nothing else is
// refused.
func TestParseCostIsLinearInSkippedTags(test *testing.T) {
	test.Parallel()
	prefix := "OFXHEADER:100\n<OFX><SIGNONMSGSRSV1><SONRS><FI><ORG>x"
	for _, skipped := range []string{"a<>", "a<!---->", "a<?>"} {
		content := prefix + strings.Repeat(skipped, 2*1024*1024/len(skipped))
		if err := parseWithin(test, test.Context(), content, 2*time.Second); err == nil {
			test.Errorf("%q repeated: a file without a statement was read", skipped)
		}
	}
	tooMany := prefix + strings.Repeat("a<>", (ofx.MaximumFileBytes-len(prefix))/3)
	if err := parseWithin(test, test.Context(), tooMany, 2*time.Second); err == nil || !strings.Contains(err.Error(), "too many tags") {
		test.Errorf("a file of nothing but empty tags: %v", err)
	}
}

// A parse whose context is done stops with the context's error.
func TestParseStopsWhenCancelled(test *testing.T) {
	test.Parallel()
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	content := strings.Replace(cardStatementSGML, "<OFX>", "<OFX>"+strings.Repeat("<!---->", 10000), 1)
	if err := parseWithin(test, ctx, content, 2*time.Second); !errors.Is(err, context.Canceled) {
		test.Errorf("a cancelled parse: %v", err)
	}
}

// CURRENCY says the amount is in that currency; ORIGCURRENCY says the
// amount is in CURDEF already and only names the currency the purchase was
// made in.
func TestParseTransactionCurrencies(test *testing.T) {
	test.Parallel()
	content := strings.Replace(cardStatementSGML, "<NAME>INVENTED COFFEE ROASTERS", "<NAME>INVENTED COFFEE ROASTERS\n<CURRENCY>\n<CURRATE>1.0800\n<CURSYM>eur\n</CURRENCY>", 1)
	content = strings.Replace(content, "<NAME>INVENTED BOOKSHOP &amp; CAFE", "<NAME>INVENTED BOOKSHOP &amp; CAFE\n<ORIGCURRENCY>\n<CURRATE>1.2700\n<CURSYM>GBP\n</ORIGCURRENCY>", 1)
	document, err := ofx.Parse(test.Context(), []byte(content))
	if err != nil {
		test.Fatal(err)
	}
	transactions := document.Statements[0].Transactions
	if inEuros := transactions[0]; inEuros.CurrencyCode != "EUR" || inEuros.CurrencyRate != "1.0800" || inEuros.OriginalCurrencyCode != "" {
		test.Errorf("CURRENCY: %+v", inEuros)
	}
	if inDollars := transactions[2]; inDollars.CurrencyCode != "" || inDollars.OriginalCurrencyCode != "GBP" || inDollars.OriginalCurrencyRate != "1.2700" || inDollars.Amount != "12.00" {
		test.Errorf("ORIGCURRENCY: %+v", inDollars)
	}
	if plain := transactions[1]; plain.CurrencyCode != "" || plain.OriginalCurrencyCode != "" {
		test.Errorf("no currency: %+v", plain)
	}
}
