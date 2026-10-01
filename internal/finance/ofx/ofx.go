// Package ofx reads the statements in an Open Financial Exchange file: what
// a bank's or a card issuer's "download transactions" button writes as .ofx,
// .qfx or .qbo. It reads; it never renders, runs or fetches anything a file
// names, and it holds no database handle, so everything in it is tested
// against invented files alone.
//
// Both versions are read. OFX 1.x is SGML: a block of NAME:VALUE header
// lines, then tags whose leaves are never closed (<TRNAMT>-12.50 and the
// next tag). OFX 2.x is XML, every element closed. One tolerant reader
// covers both: a leaf ends at the next tag whether or not it is closed, and
// a closing tag closes whatever is still open inside it.
package ofx

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// MaximumFileBytes is the largest file Parse reads. A card's export of
// three years is a few hundred kilobytes; ten megabytes is far past any
// real statement and small enough that a hostile file costs little.
const MaximumFileBytes = 10 * 1024 * 1024

// maximumElements bounds how many elements one file may hold, so a file of
// nothing but tags cannot make the tree it builds unbounded in count as
// well as in bytes.
const maximumElements = 2_000_000

// maximumDepth bounds how deeply elements nest. Real statements nest about
// eight deep.
const maximumDepth = 64

// ErrTooLarge is a file over MaximumFileBytes.
var ErrTooLarge = fmt.Errorf("ofx: the file is larger than %d MB", MaximumFileBytes/(1024*1024))

// ErrNotOFX is content that is not an OFX file at all.
var ErrNotOFX = errors.New("ofx: this is not an OFX file")

// ErrNoStatement is an OFX file that holds no bank or card statement.
var ErrNoStatement = errors.New("ofx: the file holds no bank or credit card statement")

// StatementKind says which kind of statement it is.
type StatementKind string

const (
	// StatementKindBank is a bank account's statement (STMTRS).
	StatementKindBank StatementKind = "bank"

	// StatementKindCreditCard is a credit card's statement (CCSTMTRS).
	StatementKindCreditCard StatementKind = "creditcard"
)

// Document is every statement one file holds.
type Document struct {
	// InstitutionOrganization and InstitutionID are the sign-on's FI
	// aggregate: ORG, the institution's name as it writes it, and FID.
	InstitutionOrganization string
	InstitutionID           string

	Statements []*Statement
}

// Statement is one account's statement.
type Statement struct {
	StatementKind StatementKind

	// CurrencyCode is CURDEF, the currency every amount is in unless a
	// transaction says otherwise.
	CurrencyCode string

	// AccountID is ACCTID, the institution's identifier for the account.
	// For a bank it is commonly the account number, so it is for telling
	// accounts apart and is never to be shown or stored as it is.
	AccountID string

	// BankID is BANKID, a bank's routing number; empty for a card.
	BankID string

	// AccountType is ACCTTYPE for a bank account (CHECKING, SAVINGS,
	// MONEYMRKT, CREDITLINE, CD); empty for a card.
	AccountType string

	// StartedOn and EndedOn are DTSTART and DTEND, the days the statement
	// covers, "2006-01-02"; empty when the file does not say.
	StartedOn string
	EndedOn   string

	LedgerBalance    *Balance
	AvailableBalance *Balance

	Transactions []*Transaction
}

// Balance is LEDGERBAL or AVAILBAL: an amount as of a moment.
type Balance struct {
	// Amount is a decimal with a point, as the file signs it: for a card,
	// what is owed is negative.
	Amount string

	// AsOf is DTASOF, and AsOfDay the day it names as written, in the
	// file's own time zone, "2006-01-02".
	AsOf    time.Time
	AsOfDay string
}

// Transaction is one STMTTRN.
type Transaction struct {
	// TransactionType is TRNTYPE as written, upper case: DEBIT, CREDIT,
	// PAYMENT, XFER, FEE and the rest of OFX's list.
	TransactionType string

	// PostedAt is DTPOSTED, and PostedOn the day it names as written, in
	// the file's own time zone, "2006-01-02". HasPostedTime says the file
	// gave a time of day and not only a day.
	PostedAt      time.Time
	PostedOn      string
	HasPostedTime bool

	// Amount is TRNAMT, a decimal with a point. Negative is money leaving
	// the account, for a card a purchase and for a bank a withdrawal.
	Amount string

	// FITID is the institution's identifier for the transaction, unique
	// within the account; empty when the file gives none.
	FITID string

	Name      string
	Memo      string
	PayeeName string

	// CurrencyCode is the transaction's own currency when it names one
	// (CURRENCY or ORIGCURRENCY), empty otherwise.
	CurrencyCode string
}

// IsOFX says whether content looks like an OFX file: an OFX 1.x header, an
// OFX 2.x processing instruction, or an <OFX> element near the start. For
// telling a statement sent with a generic content type, such as
// application/octet-stream, from any other file.
func IsOFX(content []byte) bool {
	head := content
	if len(head) > 4096 {
		head = head[:4096]
	}
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimSpace(head)
	upper := bytes.ToUpper(trimmed)
	if bytes.HasPrefix(upper, []byte("OFXHEADER:")) {
		return true
	}
	return bytes.Contains(upper, []byte("<?OFX")) || bytes.Contains(upper, []byte("<OFX>"))
}

// Parse reads every statement in an OFX file.
func Parse(content []byte) (*Document, error) {
	if len(content) > MaximumFileBytes {
		return nil, ErrTooLarge
	}
	if !IsOFX(content) {
		return nil, ErrNotOFX
	}
	text := decodeText(content)
	start := strings.Index(strings.ToUpper(text), "<OFX>")
	if start < 0 {
		return nil, ErrNotOFX
	}
	root, err := buildTree(text[start:])
	if err != nil {
		return nil, err
	}
	document := &Document{}
	if institution := root.find("SIGNONMSGSRSV1", "SONRS", "FI"); institution != nil {
		document.InstitutionOrganization = institution.leaf("ORG")
		document.InstitutionID = institution.leaf("FID")
	}
	for _, found := range root.descendants("STMTRS", "CCSTMTRS") {
		statement, err := readStatement(found)
		if err != nil {
			return nil, err
		}
		document.Statements = append(document.Statements, statement)
	}
	if len(document.Statements) == 0 {
		// A file that answers a request with an error says so in its
		// status, and that is the reason worth giving.
		for _, status := range root.descendants("STATUS") {
			if strings.EqualFold(status.leaf("SEVERITY"), "ERROR") {
				if message := status.leaf("MESSAGE"); message != "" {
					return nil, fmt.Errorf("%w: the institution's file says: %s", ErrNoStatement, message)
				}
			}
		}
		return nil, ErrNoStatement
	}
	return document, nil
}

// decodeText is the file as text. OFX 1.x names its character set in the
// header, and banks that say USASCII still write accented names in
// Windows-1252; a file that is not valid UTF-8 is read as Windows-1252,
// which every byte is, rather than refused.
func decodeText(content []byte) string {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	if utf8.Valid(content) {
		return string(content)
	}
	decoded, err := charmap.Windows1252.NewDecoder().Bytes(content)
	if err != nil {
		return strings.ToValidUTF8(string(content), "�")
	}
	return string(decoded)
}

func readStatement(found *element) (*Statement, error) {
	statement := &Statement{StatementKind: StatementKindBank, CurrencyCode: strings.ToUpper(found.leaf("CURDEF"))}
	if found.name == "CCSTMTRS" {
		statement.StatementKind = StatementKindCreditCard
		if account := found.child("CCACCTFROM"); account != nil {
			statement.AccountID = account.leaf("ACCTID")
		}
	} else if account := found.child("BANKACCTFROM"); account != nil {
		statement.AccountID = account.leaf("ACCTID")
		statement.BankID = account.leaf("BANKID")
		statement.AccountType = strings.ToUpper(account.leaf("ACCTTYPE"))
	}
	if statement.AccountID == "" {
		return nil, fmt.Errorf("ofx: a %s statement names no account (ACCTID)", statement.StatementKind)
	}
	var err error
	if list := found.child("BANKTRANLIST"); list != nil {
		if statement.StartedOn, err = optionalDay(list.leaf("DTSTART")); err != nil {
			return nil, err
		}
		if statement.EndedOn, err = optionalDay(list.leaf("DTEND")); err != nil {
			return nil, err
		}
		for _, entry := range list.childrenNamed("STMTTRN") {
			transaction, err := readTransaction(entry)
			if err != nil {
				return nil, err
			}
			statement.Transactions = append(statement.Transactions, transaction)
		}
	}
	if statement.LedgerBalance, err = readBalance(found.child("LEDGERBAL")); err != nil {
		return nil, err
	}
	if statement.AvailableBalance, err = readBalance(found.child("AVAILBAL")); err != nil {
		return nil, err
	}
	return statement, nil
}

func readTransaction(entry *element) (*Transaction, error) {
	transaction := &Transaction{
		TransactionType: strings.ToUpper(entry.leaf("TRNTYPE")),
		FITID:           entry.leaf("FITID"),
		Name:            entry.leaf("NAME"),
		Memo:            entry.leaf("MEMO"),
	}
	if payee := entry.child("PAYEE"); payee != nil {
		transaction.PayeeName = payee.leaf("NAME")
	}
	for _, currencyName := range []string{"CURRENCY", "ORIGCURRENCY"} {
		if currency := entry.child(currencyName); currency != nil {
			transaction.CurrencyCode = strings.ToUpper(currency.leaf("CURSYM"))
		}
	}
	postedText := entry.leaf("DTPOSTED")
	if postedText == "" {
		return nil, fmt.Errorf("ofx: a transaction has no posted date (DTPOSTED)")
	}
	postedAt, postedOn, hasTime, err := ParseDate(postedText)
	if err != nil {
		return nil, err
	}
	transaction.PostedAt, transaction.PostedOn, transaction.HasPostedTime = postedAt, postedOn, hasTime
	if transaction.Amount, err = ParseAmount(entry.leaf("TRNAMT")); err != nil {
		return nil, fmt.Errorf("ofx: the transaction posted %s: %w", postedOn, err)
	}
	return transaction, nil
}

func readBalance(found *element) (*Balance, error) {
	if found == nil || found.leaf("BALAMT") == "" {
		return nil, nil
	}
	amount, err := ParseAmount(found.leaf("BALAMT"))
	if err != nil {
		return nil, fmt.Errorf("ofx: the %s balance: %w", strings.ToLower(found.name), err)
	}
	balance := &Balance{Amount: amount}
	if asOf := found.leaf("DTASOF"); asOf != "" {
		if balance.AsOf, balance.AsOfDay, _, err = ParseDate(asOf); err != nil {
			return nil, err
		}
	}
	return balance, nil
}

func optionalDay(text string) (string, error) {
	if text == "" {
		return "", nil
	}
	_, day, _, err := ParseDate(text)
	return day, err
}

// ParseDate reads an OFX date: YYYYMMDD, then optionally HHMMSS, then
// optionally .XXX milliseconds, then optionally [offset:ZONE] where the
// offset is hours from UTC, signed or not, possibly with a fraction:
// "[-5:EST]", "[0:GMT]", "[5.5:IST]". Without a zone the time is UTC, as the
// specification says. It answers the moment,
// the day as written ("2006-01-02", in the file's own zone, which is the day
// the institution means), and whether a time of day was given.
func ParseDate(text string) (time.Time, string, bool, error) {
	original := text
	text = strings.TrimSpace(text)
	location := time.UTC
	if open := strings.IndexByte(text, '['); open >= 0 {
		closing := strings.IndexByte(text[open:], ']')
		if closing < 0 {
			return time.Time{}, "", false, fmt.Errorf("ofx: %q is not a date: the zone is not closed", original)
		}
		zone := text[open+1 : open+closing]
		text = strings.TrimSpace(text[:open])
		offsetText, zoneName, _ := strings.Cut(zone, ":")
		offsetText = strings.TrimSpace(offsetText)
		if offsetText != "" {
			offsetHours, err := strconv.ParseFloat(offsetText, 64)
			if err != nil || offsetHours < -14 || offsetHours > 14 {
				return time.Time{}, "", false, fmt.Errorf("ofx: %q is not a date: the zone's offset is not hours", original)
			}
			name := strings.TrimSpace(zoneName)
			if name == "" {
				name = "UTC" + offsetText
			}
			location = time.FixedZone(name, int(offsetHours*3600))
		}
	}
	if whole, _, hasFraction := strings.Cut(text, "."); hasFraction {
		text = whole
	}
	if len(text) < 8 || !isDigits(text) {
		return time.Time{}, "", false, fmt.Errorf("ofx: %q is not a date", original)
	}
	layout := ""
	switch len(text) {
	case 8:
		layout = "20060102"
	case 12:
		layout = "200601021504"
	case 14:
		layout = "20060102150405"
	default:
		return time.Time{}, "", false, fmt.Errorf("ofx: %q is not a date", original)
	}
	moment, err := time.ParseInLocation(layout, text, location)
	if err != nil {
		return time.Time{}, "", false, fmt.Errorf("ofx: %q is not a date", original)
	}
	return moment, moment.Format(time.DateOnly), len(text) > 8, nil
}

// ParseAmount reads an OFX amount into a decimal with a point. OFX allows a
// comma as the decimal separator, and some institutions write one; a point
// and a comma together are read with the last of them as the decimal
// separator and the other as a thousands separator. Anything else that is
// not a number is refused, because a misread amount is worse than a refused
// file.
func ParseAmount(text string) (string, error) {
	original := text
	text = strings.ReplaceAll(strings.TrimSpace(text), " ", "")
	if text == "" {
		return "", errors.New("ofx: no amount")
	}
	lastPoint, lastComma := strings.LastIndexByte(text, '.'), strings.LastIndexByte(text, ',')
	switch {
	case lastPoint >= 0 && lastComma >= 0 && lastComma > lastPoint:
		text = strings.ReplaceAll(text, ".", "")
		text = strings.Replace(text, ",", ".", 1)
	case lastPoint >= 0 && lastComma >= 0:
		text = strings.ReplaceAll(text, ",", "")
	case lastComma >= 0:
		if strings.Count(text, ",") > 1 {
			return "", fmt.Errorf("ofx: %q is not an amount", original)
		}
		text = strings.Replace(text, ",", ".", 1)
	}
	digits := strings.TrimLeft(text, "+-")
	if len(text)-len(digits) > 1 || len(digits) > 40 {
		return "", fmt.Errorf("ofx: %q is not an amount", original)
	}
	whole, fraction, hasPoint := strings.Cut(digits, ".")
	if (whole == "" && !hasPoint) || (whole != "" && !isDigits(whole)) || (hasPoint && (fraction == "" || !isDigits(fraction))) {
		return "", fmt.Errorf("ofx: %q is not an amount", original)
	}
	value, isParsed := new(big.Rat).SetString(text)
	if !isParsed {
		return "", fmt.Errorf("ofx: %q is not an amount", original)
	}
	places := len(fraction)
	if places < 2 {
		places = 2
	}
	return value.FloatString(places), nil
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// --- the tree ------------------------------------------------------------

// element is one tag: a leaf with text, or an aggregate with children.
type element struct {
	name     string
	text     string
	children []*element
	parent   *element
}

func (self *element) child(name string) *element {
	for _, candidate := range self.children {
		if candidate.name == name {
			return candidate
		}
	}
	return nil
}

func (self *element) childrenNamed(name string) []*element {
	var found []*element
	for _, candidate := range self.children {
		if candidate.name == name {
			found = append(found, candidate)
		}
	}
	return found
}

// find follows a path of child names from this element.
func (self *element) find(path ...string) *element {
	current := self
	for _, name := range path {
		if current = current.child(name); current == nil {
			return nil
		}
	}
	return current
}

// leaf is the text of the named child, or empty.
func (self *element) leaf(name string) string {
	if found := self.child(name); found != nil {
		return found.text
	}
	return ""
}

// descendants are every element below this one with one of the names, in
// document order, not looking inside a match.
func (self *element) descendants(names ...string) []*element {
	var found []*element
	var walk func(current *element)
	walk = func(current *element) {
		for _, candidate := range current.children {
			isMatch := false
			for _, name := range names {
				if candidate.name == name {
					isMatch = true
					break
				}
			}
			if isMatch {
				found = append(found, candidate)
				continue
			}
			walk(candidate)
		}
	}
	walk(self)
	return found
}

// buildTree reads the body of the file, from <OFX> on, into elements.
//
// A tag opens an element. Text right after an opening tag is that
// element's text, which makes it a leaf; a leaf ends at the next tag of any
// kind, closed or not, which is how SGML's unclosed leaves and XML's closed
// ones read the same. A closing tag closes the innermost open element of
// its name and everything still open inside it, and one that matches
// nothing open is ignored, as a stray closing tag in a hand-edited file
// should be.
func buildTree(body string) (*element, error) {
	root := &element{name: ""}
	current := root
	depth := 0
	elementCount := 0
	position := 0
	for position < len(body) {
		open := strings.IndexByte(body[position:], '<')
		if open < 0 {
			appendText(current, body[position:])
			break
		}
		if open > 0 {
			appendText(current, body[position:position+open])
		}
		position += open
		if strings.HasPrefix(body[position:], "<!--") {
			end := strings.Index(body[position:], "-->")
			if end < 0 {
				break
			}
			position += end + len("-->")
			continue
		}
		closing := strings.IndexByte(body[position:], '>')
		if closing < 0 {
			return nil, errors.New("ofx: the file ends inside a tag")
		}
		tag := strings.TrimSpace(body[position+1 : position+closing])
		position += closing + 1
		if tag == "" || strings.HasPrefix(tag, "?") || strings.HasPrefix(tag, "!") {
			continue
		}
		// A leaf ends here, whatever the tag is: one that has its text, or
		// one of the names OFX only uses for leaves, which an SGML file may
		// leave empty and unclosed.
		if current != root && len(current.children) == 0 && !strings.HasPrefix(tag, "/") && (current.text != "" || leafNames[current.name]) {
			current = current.parent
			depth--
		}
		if name, isClosing := strings.CutPrefix(tag, "/"); isClosing {
			name = strings.ToUpper(strings.TrimSpace(name))
			for candidate := current; candidate != root; candidate = candidate.parent {
				if candidate.name == name {
					for current != candidate.parent {
						current = current.parent
						depth--
					}
					break
				}
			}
			continue
		}
		isSelfClosing := strings.HasSuffix(tag, "/")
		name := strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(tag, "/")))
		if space := strings.IndexAny(name, " \t\r\n"); space >= 0 {
			name = name[:space]
		}
		elementCount++
		if elementCount > maximumElements {
			return nil, errors.New("ofx: the file holds too many elements")
		}
		opened := &element{name: name, parent: current}
		current.children = append(current.children, opened)
		if isSelfClosing {
			continue
		}
		depth++
		if depth > maximumDepth {
			return nil, errors.New("ofx: the file nests too deeply")
		}
		current = opened
	}
	ofxRoot := root.child("OFX")
	if ofxRoot == nil {
		return nil, ErrNotOFX
	}
	return ofxRoot, nil
}

// leafNames are the elements OFX defines as leaves, among those this
// package reads and those that commonly sit beside them.
var leafNames = map[string]bool{
	"ACCTID": true, "ACCTKEY": true, "ACCTTYPE": true, "BALAMT": true, "BANKID": true, "BRANCHID": true,
	"CHECKNUM": true, "CODE": true, "CORRECTACTION": true, "CORRECTFITID": true, "CURDEF": true, "CURRATE": true,
	"CURSYM": true, "DTASOF": true, "DTAVAIL": true, "DTEND": true, "DTPOSTED": true, "DTSERVER": true,
	"DTSTART": true, "DTUSER": true, "FID": true, "FITID": true, "LANGUAGE": true, "MEMO": true, "MESSAGE": true,
	"NAME": true, "ORG": true, "PAYEEID": true, "REFNUM": true, "SEVERITY": true, "SIC": true, "SRVRTID": true,
	"TRNAMT": true, "TRNTYPE": true, "TRNUID": true,
}

// appendText gives an element the text that follows its opening tag. Text
// between an aggregate's children is the whitespace a file is laid out
// with, and is dropped.
func appendText(current *element, text string) {
	text = strings.TrimSpace(text)
	if text == "" || len(current.children) > 0 || current.name == "" {
		return
	}
	current.text = strings.TrimSpace(current.text + unescape(text))
}

var entities = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&nbsp;", " ", "&amp;", "&")

func unescape(text string) string {
	if !strings.Contains(text, "&") {
		return text
	}
	return entities.Replace(text)
}
