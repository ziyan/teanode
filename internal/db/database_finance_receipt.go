package db

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// FinanceReceiptOperation is the annotations on a person's finance
// transactions and their receipts: what a merchant printed, line by line,
// and which charges each receipt explains. Every call takes the agent id
// and filters on it, so an id from another person finds nothing. The
// checking and the matching are the finance package's; this keeps what
// they decided.
type FinanceReceiptOperation interface {
	// SetFinanceTransactionAnnotation writes what the person or their
	// agent says about a finance transaction; empty takes it away. It
	// refuses, answering false and writing no audit event, to let the
	// agent overwrite or take away what the person wrote, unless
	// canReplacePersonAnnotation (the person asked for it, there with the
	// agent); the person always may. ErrNotFound when the agent has no
	// such finance transaction.
	SetFinanceTransactionAnnotation(agentId, financeTransactionId, annotation string, annotatedBy models.AnnotatedBy, canReplacePersonAnnotation bool) (bool, error)

	// PutFinanceReceipt writes a receipt with its lines. When the agent
	// already holds a receipt read from the same source, this one takes
	// its place under the same id: its lines are replaced and its matches
	// kept, for the caller to match again and to check with
	// DropUnfittingFinanceReceiptMatches. The check state and difference
	// are written as given. It answers the receipt as stored, with its
	// lines and matches. ErrInvalidArguments for an amount or a quantity
	// larger than its column holds.
	PutFinanceReceipt(receipt *models.FinanceReceipt) (*models.FinanceReceipt, error)

	// DropUnfittingFinanceReceiptMatches takes off a receipt the matches
	// that no longer fit it, as PutFinanceReceiptMatch would refuse them
	// now: after the receipt was read again with a lower total or in
	// another currency, say. The oldest match is kept first, and each
	// later one is weighed against those kept before it. It answers the
	// matches taken off, each with why. ErrNotFound when the agent has no
	// such receipt.
	DropUnfittingFinanceReceiptMatches(agentId, receiptId string) ([]*FinanceReceiptMatchDropped, error)

	// GetFinanceReceipt is one receipt of the agent with its lines and
	// matches, or nil.
	GetFinanceReceipt(agentId, receiptId string) (*models.FinanceReceipt, error)

	// FindFinanceReceiptBySource is the agent's receipt read from a
	// source, or nil: the kind and the id the kind names.
	FindFinanceReceiptBySource(agentId string, receiptSourceKind models.ReceiptSourceKind, sourceId string) (*models.FinanceReceipt, error)

	// ListFinanceReceipts is one page of the agent's receipts the filter
	// keeps, with their lines and matches, the newest purchase first and
	// the receipts that print no day after every dated one.
	ListFinanceReceipts(agentId string, filter *FinanceReceiptFilter) (*FinanceReceiptPage, error)

	// FinanceReceiptMatchCoverage is what receipts already explain of each
	// of these finance transactions, for matching receiptId: from every
	// receipt, from receipts other than it, and whether another is matched
	// at all. A finance transaction no receipt is matched to is left out.
	FinanceReceiptMatchCoverage(agentId, receiptId string, financeTransactionIds []string) (map[string]*finance.ReceiptMatchCoverage, error)

	// DeleteFinanceReceipt deletes a receipt with its lines and matches,
	// and its uploaded photo or PDF unless a message of a conversation
	// holds the same file. ErrNotFound when the agent has no such receipt.
	DeleteFinanceReceipt(agentId, receiptId string) (*FinanceReceiptDeleted, error)

	// PutFinanceReceiptMatch matches a receipt to a finance transaction,
	// or changes the amount and source of the match they have. The
	// receipt matcher never replaces a match the person made, and answers
	// false when it would have. ErrNotFound when either is not the
	// agent's. ErrInvalidArguments when the charge is in another currency
	// than the receipt, is not money out, the amount has more places than
	// the currency has, or the receipts matched to the charge would then
	// explain more than it took.
	PutFinanceReceiptMatch(agentId string, match *models.FinanceReceiptMatch) (bool, error)

	// DeleteFinanceReceiptMatch takes a receipt off a finance transaction,
	// answering whether there was a match. With a source, only a match of
	// that source is taken off, so the receipt matcher never removes the
	// person's.
	DeleteFinanceReceiptMatch(agentId, receiptId, financeTransactionId string, receiptMatchSource models.ReceiptMatchSource) (bool, error)
}

// FinanceReceiptFilter narrows a listing of finance receipts. Every field
// is optional.
type FinanceReceiptFilter struct {
	// FinanceTransactionID keeps the receipts matched to this finance
	// transaction.
	FinanceTransactionID string

	// From and To bound the day of purchase, both included, "2006-01-02".
	// A receipt that prints no day is left out when either is given, and
	// listed after every dated one when neither is.
	From string
	To   string

	// IsUndated keeps only the receipts that print no day of purchase;
	// it cannot be given with From or To.
	IsUndated bool

	// IsUnmatched keeps the receipts matched to no finance transaction.
	IsUnmatched bool

	// Text keeps the receipts whose merchant, receipt number or any line's
	// description holds it, matched case-insensitively.
	Text string

	// Limit is at most FinanceReceiptLimitMost; zero is
	// FinanceReceiptLimitDefault.
	Limit int

	// After is the NextCursor of the page before. Offset is how many to
	// pass over first, for a page by its number; with After it counts
	// from the cursor.
	After  string
	Offset int

	// ShouldCountTotal fills the page's TotalCount, which costs one more
	// statement.
	ShouldCountTotal bool
}

// FinanceReceiptPage is one page of receipts, and the cursor for the
// next, empty on the last. TotalCount is how many match the filter on
// every page, when it was asked for.
type FinanceReceiptPage struct {
	FinanceReceipts []*models.FinanceReceipt
	NextCursor      string
	TotalCount      int
}

// How many finance receipts one listing holds.
const (
	FinanceReceiptLimitDefault = 50
	FinanceReceiptLimitMost    = 200
)

// FinanceReceiptMatchDropped is a match DropUnfittingFinanceReceiptMatches
// took off, and why it no longer fits, in words.
type FinanceReceiptMatchDropped struct {
	ReceiptMatch *models.FinanceReceiptMatch
	DropReason   string
}

// FinanceReceiptDeleted is what one DeleteFinanceReceipt deleted: the
// receipt as it was, and the uploaded file whose row went with it, whose
// bytes the caller removes from storage. Empty when no file went.
type FinanceReceiptDeleted struct {
	FinanceReceipt           *models.FinanceReceipt
	DeletedAgentAttachmentID string
}

// The bounds on what a receipt holds: more than any receipt prints, few
// enough that a model gone wrong cannot fill a table.
const (
	maximumReceiptLineCount         = 500
	maximumReceiptTextLength        = 500
	maximumAnnotationLength         = 2000
	maximumReceiptSourceIDLength    = 200
	maximumReceiptAccountMaskLength = 8
)

// What the receipt columns hold: an amount is numeric(19,4), fifteen
// digits before the point, and a quantity numeric(24,8), sixteen. A model
// can misread more; it is refused here, as an argument, rather than
// failing the statement as an error the receipt job would retry.
var (
	maximumReceiptAmountBound   = new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(15), nil))
	maximumReceiptQuantityBound = new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil))
)

// checkReceiptBound refuses a canonical amount or quantity whose size
// reaches the bound its column holds.
func checkReceiptBound(field, canonical string, bound *big.Rat) error {
	parsed, isParsed := new(big.Rat).SetString(canonical)
	if !isParsed {
		return fmt.Errorf("%w: %s %q is not a decimal", ErrInvalidArguments, field, canonical)
	}
	if new(big.Rat).Abs(parsed).Cmp(bound) >= 0 {
		return fmt.Errorf("%w: %s %s is larger than any receipt prints; read it again", ErrInvalidArguments, field, canonical)
	}
	return nil
}

// canonicalReceiptAmount is canonicalAmount for a receipt's amount,
// refusing one larger than its column holds.
func canonicalReceiptAmount(field, amount string) (string, error) {
	canonical, err := canonicalAmount(field, amount)
	if err != nil {
		return "", err
	}
	if err := checkReceiptBound(field, canonical, maximumReceiptAmountBound); err != nil {
		return "", err
	}
	return canonical, nil
}

// canonicalOptionalReceiptAmount is canonicalReceiptAmount for a nullable
// column: nil for empty.
func canonicalOptionalReceiptAmount(field, amount string) (*string, error) {
	if strings.TrimSpace(amount) == "" {
		return nil, nil
	}
	canonical, err := canonicalReceiptAmount(field, amount)
	if err != nil {
		return nil, err
	}
	return &canonical, nil
}

type agentFinanceReceiptModel struct {
	ID                    string     `gorm:"column:id;primaryKey"`
	AgentID               string     `gorm:"column:agent_id"`
	ReceiptSourceKind     string     `gorm:"column:receipt_source_kind"`
	MailID                *string    `gorm:"column:mail_id"`
	GmailMessageID        *string    `gorm:"column:gmail_message_id"`
	AgentAttachmentID     *string    `gorm:"column:agent_attachment_id"`
	MerchantName          string     `gorm:"column:merchant_name"`
	MerchantReceiptNumber string     `gorm:"column:merchant_receipt_number"`
	PurchasedOn           *time.Time `gorm:"column:purchased_on"`
	PurchasedAt           *time.Time `gorm:"column:purchased_at"`
	CurrencyCode          string     `gorm:"column:currency_code"`
	SubtotalAmount        *string    `gorm:"column:subtotal_amount"`
	TotalAmount           string     `gorm:"column:total_amount"`
	PaymentAccountMask    string     `gorm:"column:payment_account_mask"`
	ReceiptCheckState     string     `gorm:"column:receipt_check_state"`
	CheckDifferenceAmount string     `gorm:"column:check_difference_amount"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	ModifiedAt            time.Time  `gorm:"column:modified_at"`
}

func (agentFinanceReceiptModel) TableName() string { return "agent_finance_receipt" }

func (self *agentFinanceReceiptModel) toModel() *models.FinanceReceipt {
	return &models.FinanceReceipt{
		ID: self.ID, AgentID: self.AgentID, ReceiptSourceKind: models.ReceiptSourceKind(self.ReceiptSourceKind),
		MailID: optionalString(self.MailID), GmailMessageID: optionalString(self.GmailMessageID),
		AgentAttachmentID: optionalString(self.AgentAttachmentID), MerchantName: self.MerchantName,
		MerchantReceiptNumber: self.MerchantReceiptNumber, PurchasedOn: formatOptionalDay(self.PurchasedOn),
		PurchasedAt: localTime(self.PurchasedAt), CurrencyCode: self.CurrencyCode,
		SubtotalAmount: optionalString(self.SubtotalAmount), TotalAmount: self.TotalAmount,
		PaymentAccountMask: self.PaymentAccountMask, ReceiptCheckState: models.ReceiptCheckState(self.ReceiptCheckState),
		CheckDifferenceAmount: self.CheckDifferenceAmount,
		ReceiptLines:          []*models.FinanceReceiptLine{}, ReceiptMatches: []*models.FinanceReceiptMatch{},
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

type agentFinanceReceiptLineModel struct {
	ID                 string  `gorm:"column:id;primaryKey"`
	AgentID            string  `gorm:"column:agent_id"`
	ReceiptID          string  `gorm:"column:receipt_id"`
	LineNumber         int     `gorm:"column:line_number"`
	ReceiptLineKind    string  `gorm:"column:receipt_line_kind"`
	Description        string  `gorm:"column:description"`
	Quantity           *string `gorm:"column:quantity"`
	QuantityUnit       string  `gorm:"column:quantity_unit"`
	UnitPriceAmount    *string `gorm:"column:unit_price_amount"`
	LineAmount         string  `gorm:"column:line_amount"`
	TaxClassCode       string  `gorm:"column:tax_class_code"`
	DiscountedLineID   *string `gorm:"column:discounted_line_id"`
	SpendingCategoryID *string `gorm:"column:spending_category_id"`
}

func (agentFinanceReceiptLineModel) TableName() string { return "agent_finance_receipt_line" }

func (self *agentFinanceReceiptLineModel) toModel() *models.FinanceReceiptLine {
	return &models.FinanceReceiptLine{
		ID: self.ID, LineNumber: self.LineNumber, ReceiptLineKind: models.ReceiptLineKind(self.ReceiptLineKind),
		Description: self.Description, Quantity: optionalString(self.Quantity), QuantityUnit: self.QuantityUnit,
		UnitPriceAmount: optionalString(self.UnitPriceAmount), LineAmount: self.LineAmount, TaxClassCode: self.TaxClassCode,
		DiscountedLineID: optionalString(self.DiscountedLineID), SpendingCategoryID: optionalString(self.SpendingCategoryID),
	}
}

type agentFinanceReceiptMatchModel struct {
	ReceiptID            string    `gorm:"column:receipt_id;primaryKey"`
	FinanceTransactionID string    `gorm:"column:finance_transaction_id;primaryKey"`
	AgentID              string    `gorm:"column:agent_id"`
	MatchedAmount        string    `gorm:"column:matched_amount"`
	ReceiptMatchSource   string    `gorm:"column:receipt_match_source"`
	MatchConfidence      *string   `gorm:"column:match_confidence"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (agentFinanceReceiptMatchModel) TableName() string { return "agent_finance_receipt_match" }

func (self *agentFinanceReceiptMatchModel) toModel() *models.FinanceReceiptMatch {
	return &models.FinanceReceiptMatch{
		ReceiptID: self.ReceiptID, FinanceTransactionID: self.FinanceTransactionID, MatchedAmount: self.MatchedAmount,
		ReceiptMatchSource: models.ReceiptMatchSource(self.ReceiptMatchSource), MatchConfidence: optionalString(self.MatchConfidence),
		CreatedAt: self.CreatedAt.In(time.Local),
	}
}

// --- annotations ----------------------------------------------------------

// annotationAudit is what the audit log keeps of an annotation.
func annotationAudit(annotation string, annotatedBy models.AnnotatedBy) map[string]any {
	return map[string]any{"annotation": annotation, "annotatedBy": annotatedBy}
}

// errAnnotationNotWritten undoes the audit event of an annotation the
// guarded statement did not write.
var errAnnotationNotWritten = fmt.Errorf("db: the annotation was not written")

func (self *transaction) SetFinanceTransactionAnnotation(agentId, financeTransactionId, annotation string, annotatedBy models.AnnotatedBy, canReplacePersonAnnotation bool) (bool, error) {
	if !annotatedBy.IsValid() {
		return false, fmt.Errorf("%w: %q is not who annotated it", ErrInvalidArguments, annotatedBy)
	}
	annotation = strings.TrimSpace(annotation)
	if len([]rune(annotation)) > maximumAnnotationLength {
		return false, fmt.Errorf("%w: an annotation is at most %d characters", ErrInvalidArguments, maximumAnnotationLength)
	}
	before, err := self.GetFinanceTransaction(agentId, financeTransactionId)
	if err != nil {
		return false, err
	}
	if before == nil {
		return false, ErrNotFound
	}
	isAllowed := annotatedBy == models.AnnotatedByPerson || canReplacePersonAnnotation
	if before.AnnotatedBy == models.AnnotatedByPerson && !isAllowed {
		return false, nil
	}
	storedBy := annotatedBy
	if annotation == "" {
		storedBy = ""
	}
	if err := self.applyMutation(models.AuditResourceFinanceTransaction, financeTransactionId, models.AuditActionUpdate,
		annotationAudit(before.Annotation, before.AnnotatedBy), annotationAudit(annotation, storedBy), func(tx *gorm.DB) error {
			// The person's annotation is checked again in the statement, so
			// the agent cannot overwrite one the person wrote in between;
			// when it did not write, neither does the audit log.
			updated := tx.Exec(`UPDATE "agent_finance_transaction" SET "annotation" = ?, "annotated_by" = ?, "modified_at" = ?
				WHERE "agent_id" = ? AND "id" = ? AND ("annotated_by" <> 'person' OR ?)`,
				annotation, string(storedBy), time.Now(), agentId, financeTransactionId, isAllowed)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 0 {
				return errAnnotationNotWritten
			}
			return nil
		}); err != nil {
		if errors.Is(err, errAnnotationNotWritten) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// fillReceiptCounts sets how many receipts are matched to each finance
// transaction, in one statement.
func (self *transaction) fillReceiptCounts(agentId string, financeTransactions []*models.FinanceTransaction) error {
	if len(financeTransactions) == 0 {
		return nil
	}
	financeTransactionIds := make([]string, 0, len(financeTransactions))
	for _, financeTransaction := range financeTransactions {
		financeTransactionIds = append(financeTransactionIds, financeTransaction.ID)
	}
	var counted []struct {
		FinanceTransactionID string `gorm:"column:finance_transaction_id"`
		ReceiptCount         int    `gorm:"column:receipt_count"`
	}
	if err := self.tx.Raw(`SELECT "finance_transaction_id", COUNT(*) AS "receipt_count" FROM "agent_finance_receipt_match"
		WHERE "agent_id" = ? AND "finance_transaction_id" = ANY(?::text[]) GROUP BY "finance_transaction_id"`,
		agentId, pq.Array(financeTransactionIds)).Scan(&counted).Error; err != nil {
		return err
	}
	countById := map[string]int{}
	for _, row := range counted {
		countById[row.FinanceTransactionID] = row.ReceiptCount
	}
	for _, financeTransaction := range financeTransactions {
		financeTransaction.ReceiptCount = countById[financeTransaction.ID]
	}
	return nil
}

// --- receipts -------------------------------------------------------------

// receiptAudit is what the audit log keeps of a receipt: what it is and
// how it was read, without its lines, which can run to hundreds.
func receiptAudit(receipt *models.FinanceReceipt) map[string]any {
	return map[string]any{
		"receiptSourceKind": receipt.ReceiptSourceKind, "merchantName": receipt.MerchantName, "purchasedOn": receipt.PurchasedOn,
		"currencyCode": receipt.CurrencyCode, "totalAmount": receipt.TotalAmount, "receiptCheckState": receipt.ReceiptCheckState,
		"receiptLineCount": len(receipt.ReceiptLines),
	}
}

// receiptMatchesAudit is what the audit log keeps of a receipt's matches.
func receiptMatchesAudit(matches []*models.FinanceReceiptMatch) map[string]any {
	kept := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		kept = append(kept, map[string]any{
			"financeTransactionId": match.FinanceTransactionID, "matchedAmount": match.MatchedAmount,
			"receiptMatchSource": match.ReceiptMatchSource,
		})
	}
	return map[string]any{"receiptMatches": kept}
}

// receiptSourceId is the id of the source a receipt's kind names, and the
// column it is kept in.
func receiptSourceId(receipt *models.FinanceReceipt) (string, string) {
	switch receipt.ReceiptSourceKind {
	case models.ReceiptSourceKindMail:
		return strings.TrimSpace(receipt.MailID), "mail_id"
	case models.ReceiptSourceKindGmailMessage:
		return strings.TrimSpace(receipt.GmailMessageID), "gmail_message_id"
	case models.ReceiptSourceKindAttachment:
		return strings.TrimSpace(receipt.AgentAttachmentID), "agent_attachment_id"
	}
	return "", ""
}

// receiptSourceColumn is the column a kind of source is kept in.
func receiptSourceColumn(receiptSourceKind models.ReceiptSourceKind) string {
	_, column := receiptSourceId(&models.FinanceReceipt{ReceiptSourceKind: receiptSourceKind})
	return column
}

// checkedReceiptRow is a receipt as its row is written, refused with the
// first thing wrong with it.
func checkedReceiptRow(receipt *models.FinanceReceipt) (*agentFinanceReceiptModel, error) {
	if !receipt.ReceiptSourceKind.IsValid() {
		return nil, fmt.Errorf("%w: %q is not where a receipt is read from", ErrInvalidArguments, receipt.ReceiptSourceKind)
	}
	sourceId, _ := receiptSourceId(receipt)
	if sourceId == "" {
		return nil, fmt.Errorf("%w: a receipt read from %s needs its id", ErrInvalidArguments, receipt.ReceiptSourceKind)
	}
	if len(sourceId) > maximumReceiptSourceIDLength {
		return nil, fmt.Errorf("%w: the id of a receipt's source is at most %d characters", ErrInvalidArguments, maximumReceiptSourceIDLength)
	}
	if !receipt.ReceiptCheckState.IsValid() {
		return nil, fmt.Errorf("%w: %q is not a receipt check state", ErrInvalidArguments, receipt.ReceiptCheckState)
	}
	currencyCode := strings.ToUpper(strings.TrimSpace(receipt.CurrencyCode))
	if len(currencyCode) != 3 {
		return nil, fmt.Errorf("%w: %q is not a currency code like USD", ErrInvalidArguments, receipt.CurrencyCode)
	}
	for _, text := range []string{receipt.MerchantName, receipt.MerchantReceiptNumber} {
		if len([]rune(text)) > maximumReceiptTextLength {
			return nil, fmt.Errorf("%w: a receipt's merchant and number are at most %d characters each", ErrInvalidArguments, maximumReceiptTextLength)
		}
	}
	paymentAccountMask := strings.TrimSpace(receipt.PaymentAccountMask)
	if len(paymentAccountMask) > maximumReceiptAccountMaskLength || strings.Trim(paymentAccountMask, "0123456789") != "" {
		return nil, fmt.Errorf("%w: the payment account mask %q is not the last digits of a card or account", ErrInvalidArguments, receipt.PaymentAccountMask)
	}
	purchasedOn, err := parseOptionalDay(receipt.PurchasedOn)
	if err != nil {
		return nil, err
	}
	totalAmount, err := canonicalReceiptAmount("the total", receipt.TotalAmount)
	if err != nil {
		return nil, err
	}
	subtotalAmount, err := canonicalOptionalReceiptAmount("the subtotal", receipt.SubtotalAmount)
	if err != nil {
		return nil, err
	}
	checkDifferenceAmount := "0"
	if strings.TrimSpace(receipt.CheckDifferenceAmount) != "" {
		if checkDifferenceAmount, err = canonicalReceiptAmount("the check difference", receipt.CheckDifferenceAmount); err != nil {
			return nil, err
		}
	}
	row := &agentFinanceReceiptModel{
		AgentID: receipt.AgentID, ReceiptSourceKind: string(receipt.ReceiptSourceKind),
		MerchantName: strings.TrimSpace(receipt.MerchantName), MerchantReceiptNumber: strings.TrimSpace(receipt.MerchantReceiptNumber),
		PurchasedAt: receipt.PurchasedAt, CurrencyCode: currencyCode, SubtotalAmount: subtotalAmount, TotalAmount: totalAmount,
		PaymentAccountMask: paymentAccountMask, ReceiptCheckState: string(receipt.ReceiptCheckState), CheckDifferenceAmount: checkDifferenceAmount,
	}
	if purchasedOn != "" {
		day, _ := time.Parse(time.DateOnly, purchasedOn)
		row.PurchasedOn = &day
	}
	switch receipt.ReceiptSourceKind {
	case models.ReceiptSourceKindMail:
		row.MailID = &sourceId
	case models.ReceiptSourceKindGmailMessage:
		row.GmailMessageID = &sourceId
	case models.ReceiptSourceKindAttachment:
		row.AgentAttachmentID = &sourceId
	}
	return row, nil
}

// checkedReceiptLines are a receipt's lines as their rows are written, in
// their order, each discount's item still named by its number.
func checkedReceiptLines(receipt *models.FinanceReceipt) ([]*agentFinanceReceiptLineModel, map[int]int, error) {
	if len(receipt.ReceiptLines) > maximumReceiptLineCount {
		return nil, nil, fmt.Errorf("%w: a receipt has at most %d lines", ErrInvalidArguments, maximumReceiptLineCount)
	}
	rows := make([]*agentFinanceReceiptLineModel, 0, len(receipt.ReceiptLines))
	discountedByLineNumber := map[int]int{}
	isNumbered := map[int]bool{}
	for _, line := range receipt.ReceiptLines {
		if line.LineNumber <= 0 || isNumbered[line.LineNumber] {
			return nil, nil, fmt.Errorf("%w: receipt line numbers start at 1 and are each used once, not %d", ErrInvalidArguments, line.LineNumber)
		}
		isNumbered[line.LineNumber] = true
		if !line.ReceiptLineKind.IsValid() {
			return nil, nil, fmt.Errorf("%w: line %d: %q is not a receipt line kind", ErrInvalidArguments, line.LineNumber, line.ReceiptLineKind)
		}
		for _, text := range []string{line.Description, line.QuantityUnit, line.TaxClassCode} {
			if len([]rune(text)) > maximumReceiptTextLength {
				return nil, nil, fmt.Errorf("%w: line %d is longer than %d characters", ErrInvalidArguments, line.LineNumber, maximumReceiptTextLength)
			}
		}
		lineAmount, err := canonicalReceiptAmount(fmt.Sprintf("line %d's amount", line.LineNumber), line.LineAmount)
		if err != nil {
			return nil, nil, err
		}
		unitPriceAmount, err := canonicalOptionalReceiptAmount(fmt.Sprintf("line %d's unit price", line.LineNumber), line.UnitPriceAmount)
		if err != nil {
			return nil, nil, err
		}
		var quantity *string
		if strings.TrimSpace(line.Quantity) != "" {
			canonical, isParsed := new(big.Rat).SetString(strings.TrimSpace(line.Quantity))
			if !isParsed || strings.ContainsAny(line.Quantity, "eE/") {
				return nil, nil, fmt.Errorf("%w: line %d's quantity %q is not a decimal", ErrInvalidArguments, line.LineNumber, line.Quantity)
			}
			canonicalQuantity := canonical.FloatString(8)
			if err := checkReceiptBound(fmt.Sprintf("line %d's quantity", line.LineNumber), canonicalQuantity, maximumReceiptQuantityBound); err != nil {
				return nil, nil, err
			}
			quantity = &canonicalQuantity
		}
		if line.DiscountedLineNumber != 0 {
			discountedByLineNumber[line.LineNumber] = line.DiscountedLineNumber
		}
		rows = append(rows, &agentFinanceReceiptLineModel{
			ID: newID(), AgentID: receipt.AgentID, LineNumber: line.LineNumber, ReceiptLineKind: string(line.ReceiptLineKind),
			Description: strings.TrimSpace(line.Description), Quantity: quantity, QuantityUnit: strings.TrimSpace(line.QuantityUnit),
			UnitPriceAmount: unitPriceAmount, LineAmount: lineAmount, TaxClassCode: strings.TrimSpace(line.TaxClassCode),
		})
	}
	for lineNumber, discountedLineNumber := range discountedByLineNumber {
		if !isNumbered[discountedLineNumber] {
			return nil, nil, fmt.Errorf("%w: line %d discounts line %d, which the receipt does not have", ErrInvalidArguments, lineNumber, discountedLineNumber)
		}
	}
	return rows, discountedByLineNumber, nil
}

func (self *transaction) PutFinanceReceipt(receipt *models.FinanceReceipt) (*models.FinanceReceipt, error) {
	if receipt == nil || receipt.AgentID == "" {
		return nil, fmt.Errorf("%w: a receipt needs its agent", ErrInvalidArguments)
	}
	row, err := checkedReceiptRow(receipt)
	if err != nil {
		return nil, err
	}
	lineRows, discountedByLineNumber, err := checkedReceiptLines(receipt)
	if err != nil {
		return nil, err
	}
	sourceId, _ := receiptSourceId(receipt)
	if receipt.ReceiptSourceKind == models.ReceiptSourceKindAttachment {
		attachment, err := self.GetAgentAttachment(sourceId)
		if err != nil {
			return nil, err
		}
		if attachment == nil || attachment.AgentID != receipt.AgentID {
			return nil, ErrNotFound
		}
	}
	// The source is locked before its receipt is read, so two reads of one
	// message finishing together write one receipt: a row lock alone takes
	// nothing on the first read, when there is no row yet, and the second
	// insert would fail on the unique index. Hash collisions only make
	// unrelated receipts wait for each other.
	digest := sha256.Sum256([]byte("agent_finance_receipt\x00" + receipt.AgentID + "\x00" + string(receipt.ReceiptSourceKind) + "\x00" + sourceId))
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	if err := self.tx.Exec("SELECT pg_advisory_xact_lock(?)", lockKey).Error; err != nil {
		return nil, err
	}
	var existing []agentFinanceReceiptModel
	if err := self.tx.Raw(`SELECT * FROM "agent_finance_receipt" WHERE "agent_id" = ? AND "`+receiptSourceColumn(receipt.ReceiptSourceKind)+`" = ? FOR UPDATE`,
		receipt.AgentID, sourceId).Scan(&existing).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	row.ModifiedAt = now
	action := models.AuditActionCreate
	var before map[string]any
	if len(existing) > 0 {
		action = models.AuditActionUpdate
		previous, err := self.GetFinanceReceipt(receipt.AgentID, existing[0].ID)
		if err != nil {
			return nil, err
		}
		before = receiptAudit(previous)
		row.ID, row.CreatedAt = existing[0].ID, existing[0].CreatedAt
	} else {
		row.ID, row.CreatedAt = newID(), now
	}
	stored := row.toModel()
	stored.ReceiptLines = receipt.ReceiptLines
	if err := self.applyMutation(models.AuditResourceFinanceReceipt, row.ID, action, before, receiptAudit(stored), func(tx *gorm.DB) error {
		if action == models.AuditActionCreate {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Save(row).Error; err != nil {
				return err
			}
			if err := tx.Where(`"agent_id" = ? AND "receipt_id" = ?`, row.AgentID, row.ID).Delete(&agentFinanceReceiptLineModel{}).Error; err != nil {
				return err
			}
		}
		if len(lineRows) == 0 {
			return nil
		}
		idByLineNumber := map[int]string{}
		for _, lineRow := range lineRows {
			lineRow.ReceiptID = row.ID
			idByLineNumber[lineRow.LineNumber] = lineRow.ID
		}
		// Each discount names its item by id once every id is known.
		for _, lineRow := range lineRows {
			if discountedLineNumber, isDiscount := discountedByLineNumber[lineRow.LineNumber]; isDiscount {
				discountedLineId := idByLineNumber[discountedLineNumber]
				lineRow.DiscountedLineID = &discountedLineId
			}
		}
		// Items before the discounts that point at them, so each reference
		// is to a row already written.
		var withoutReference, withReference []*agentFinanceReceiptLineModel
		for _, lineRow := range lineRows {
			if lineRow.DiscountedLineID == nil {
				withoutReference = append(withoutReference, lineRow)
			} else {
				withReference = append(withReference, lineRow)
			}
		}
		for _, batch := range [][]*agentFinanceReceiptLineModel{withoutReference, withReference} {
			if len(batch) == 0 {
				continue
			}
			if err := tx.Create(batch).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return self.GetFinanceReceipt(receipt.AgentID, row.ID)
}

func (self *transaction) GetFinanceReceipt(agentId, receiptId string) (*models.FinanceReceipt, error) {
	var found []agentFinanceReceiptModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, receiptId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	receipts, err := self.receiptsWithLines(agentId, found)
	if err != nil {
		return nil, err
	}
	return receipts[0], nil
}

func (self *transaction) FindFinanceReceiptBySource(agentId string, receiptSourceKind models.ReceiptSourceKind, sourceId string) (*models.FinanceReceipt, error) {
	column := receiptSourceColumn(receiptSourceKind)
	if column == "" {
		return nil, fmt.Errorf("%w: %q is not where a receipt is read from", ErrInvalidArguments, receiptSourceKind)
	}
	var found []agentFinanceReceiptModel
	if err := self.tx.Where(`"agent_id" = ? AND "`+column+`" = ?`, agentId, strings.TrimSpace(sourceId)).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	receipts, err := self.receiptsWithLines(agentId, found)
	if err != nil {
		return nil, err
	}
	return receipts[0], nil
}

// financeReceiptQuery is the agent's receipts the filter keeps, unordered.
func (self *transaction) financeReceiptQuery(agentId string, filter *FinanceReceiptFilter) (*gorm.DB, error) {
	query := self.tx.Model(&agentFinanceReceiptModel{}).Where(`"agent_id" = ?`, agentId)
	from, err := parseOptionalDay(filter.From)
	if err != nil {
		return nil, err
	}
	to, err := parseOptionalDay(filter.To)
	if err != nil {
		return nil, err
	}
	if filter.IsUndated && (from != "" || to != "") {
		return nil, fmt.Errorf("%w: a receipt that prints no day has no day to fall between from and to; ask for one or the other", ErrInvalidArguments)
	}
	if from != "" {
		query = query.Where(`"purchased_on" >= ?::date`, from)
	}
	if to != "" {
		query = query.Where(`"purchased_on" <= ?::date`, to)
	}
	if filter.IsUndated {
		query = query.Where(`"purchased_on" IS NULL`)
	}
	if financeTransactionId := strings.TrimSpace(filter.FinanceTransactionID); financeTransactionId != "" {
		query = query.Where(`"id" IN (SELECT "receipt_id" FROM "agent_finance_receipt_match" WHERE "agent_id" = ? AND "finance_transaction_id" = ?)`,
			agentId, financeTransactionId)
	}
	if filter.IsUnmatched {
		query = query.Where(`NOT EXISTS (SELECT 1 FROM "agent_finance_receipt_match" AS "match" WHERE "match"."receipt_id" = "agent_finance_receipt"."id")`)
	}
	if text := strings.TrimSpace(filter.Text); text != "" {
		pattern := "%" + escapeLike(text) + "%"
		query = query.Where(`("merchant_name" ILIKE ? OR "merchant_receipt_number" ILIKE ?
			OR EXISTS (SELECT 1 FROM "agent_finance_receipt_line" AS "line"
				WHERE "line"."receipt_id" = "agent_finance_receipt"."id" AND "line"."description" ILIKE ?))`,
			pattern, pattern, pattern)
	}
	return query, nil
}

// undatedReceiptCursorDay stands for "no day" in a cursor, which a day
// written 2006-01-02 can never be.
const undatedReceiptCursorDay = "undated"

// financeReceiptCursor is where the next page starts: after this receipt.
func financeReceiptCursor(row *agentFinanceReceiptModel) string {
	if row.PurchasedOn == nil {
		return undatedReceiptCursorDay + "/" + row.ID
	}
	return row.PurchasedOn.Format(time.DateOnly) + "/" + row.ID
}

func (self *transaction) ListFinanceReceipts(agentId string, filter *FinanceReceiptFilter) (*FinanceReceiptPage, error) {
	if filter == nil {
		filter = &FinanceReceiptFilter{}
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = FinanceReceiptLimitDefault
	}
	if limit > FinanceReceiptLimitMost {
		limit = FinanceReceiptLimitMost
	}
	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: an offset cannot be negative", ErrInvalidArguments)
	}
	page := &FinanceReceiptPage{}
	if filter.ShouldCountTotal {
		counted, err := self.financeReceiptQuery(agentId, filter)
		if err != nil {
			return nil, err
		}
		var totalCount int64
		if err := counted.Count(&totalCount).Error; err != nil {
			return nil, err
		}
		page.TotalCount = int(totalCount)
	}
	query, err := self.financeReceiptQuery(agentId, filter)
	if err != nil {
		return nil, err
	}
	if filter.After != "" {
		purchasedOn, receiptId, isCut := strings.Cut(filter.After, "/")
		if !isCut || receiptId == "" {
			return nil, fmt.Errorf("%w: %q is not a cursor from a page of receipts", ErrInvalidArguments, filter.After)
		}
		// Dated receipts come first, newest first, then the undated ones;
		// a cursor on a dated one goes on through the undated ones.
		if purchasedOn == undatedReceiptCursorDay {
			query = query.Where(`"purchased_on" IS NULL AND "id" < ?`, receiptId)
		} else {
			day, err := parseDay(purchasedOn)
			if err != nil {
				return nil, fmt.Errorf("%w: %q is not a cursor from a page of receipts", ErrInvalidArguments, filter.After)
			}
			query = query.Where(`(("purchased_on", "id") < (?::date, ?) OR "purchased_on" IS NULL)`, day, receiptId)
		}
	}
	var found []agentFinanceReceiptModel
	// One more than the page, to know whether there is another.
	if err := query.Order(`"purchased_on" DESC NULLS LAST, "id" DESC`).Offset(filter.Offset).Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) > limit {
		found = found[:limit]
		page.NextCursor = financeReceiptCursor(&found[len(found)-1])
	}
	if page.FinanceReceipts, err = self.receiptsWithLines(agentId, found); err != nil {
		return nil, err
	}
	return page, nil
}

func (self *transaction) FinanceReceiptMatchCoverage(agentId, receiptId string, financeTransactionIds []string) (map[string]*finance.ReceiptMatchCoverage, error) {
	coverageById := map[string]*finance.ReceiptMatchCoverage{}
	if len(financeTransactionIds) == 0 {
		return coverageById, nil
	}
	var rows []struct {
		FinanceTransactionID string `gorm:"column:finance_transaction_id"`
		MatchedAmount        string `gorm:"column:matched_amount"`
		OtherMatchedAmount   string `gorm:"column:other_matched_amount"`
		OtherReceiptCount    int    `gorm:"column:other_receipt_count"`
	}
	if err := self.tx.Raw(`SELECT "finance_transaction_id", SUM("matched_amount")::text AS "matched_amount",
			COALESCE(SUM("matched_amount") FILTER (WHERE "receipt_id" <> ?), 0)::text AS "other_matched_amount",
			COUNT(*) FILTER (WHERE "receipt_id" <> ?) AS "other_receipt_count"
		FROM "agent_finance_receipt_match"
		WHERE "agent_id" = ? AND "finance_transaction_id" = ANY(?::text[])
		GROUP BY "finance_transaction_id"`,
		receiptId, receiptId, agentId, pq.Array(financeTransactionIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		coverageById[row.FinanceTransactionID] = &finance.ReceiptMatchCoverage{
			MatchedAmount: row.MatchedAmount, OtherMatchedAmount: row.OtherMatchedAmount, HasOtherReceipt: row.OtherReceiptCount > 0,
		}
	}
	return coverageById, nil
}

// receiptsWithLines are the receipts with their lines, in order, and their
// matches, two statements for all of them.
func (self *transaction) receiptsWithLines(agentId string, rows []agentFinanceReceiptModel) ([]*models.FinanceReceipt, error) {
	receipts := make([]*models.FinanceReceipt, 0, len(rows))
	byId := map[string]*models.FinanceReceipt{}
	receiptIds := make([]string, 0, len(rows))
	for index := range rows {
		receipt := rows[index].toModel()
		receipts = append(receipts, receipt)
		byId[receipt.ID] = receipt
		receiptIds = append(receiptIds, receipt.ID)
	}
	if len(receiptIds) == 0 {
		return receipts, nil
	}
	var lineRows []agentFinanceReceiptLineModel
	if err := self.tx.Where(`"agent_id" = ? AND "receipt_id" = ANY(?::text[])`, agentId, pq.Array(receiptIds)).
		Order(`"receipt_id", "line_number"`).Find(&lineRows).Error; err != nil {
		return nil, err
	}
	lineNumberById := map[string]int{}
	for index := range lineRows {
		lineNumberById[lineRows[index].ID] = lineRows[index].LineNumber
	}
	for index := range lineRows {
		line := lineRows[index].toModel()
		line.DiscountedLineNumber = lineNumberById[line.DiscountedLineID]
		byId[lineRows[index].ReceiptID].ReceiptLines = append(byId[lineRows[index].ReceiptID].ReceiptLines, line)
	}
	var matchRows []agentFinanceReceiptMatchModel
	if err := self.tx.Where(`"agent_id" = ? AND "receipt_id" = ANY(?::text[])`, agentId, pq.Array(receiptIds)).
		Order(`"receipt_id", "created_at", "finance_transaction_id"`).Find(&matchRows).Error; err != nil {
		return nil, err
	}
	for index := range matchRows {
		byId[matchRows[index].ReceiptID].ReceiptMatches = append(byId[matchRows[index].ReceiptID].ReceiptMatches, matchRows[index].toModel())
	}
	for _, receipt := range receipts {
		receipt.IsFeeAfterSubtotal = finance.IsReceiptFeeAfterSubtotal(receipt)
	}
	return receipts, nil
}

func (self *transaction) DeleteFinanceReceipt(agentId, receiptId string) (*FinanceReceiptDeleted, error) {
	before, err := self.GetFinanceReceipt(agentId, receiptId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	deleted := &FinanceReceiptDeleted{FinanceReceipt: before}
	if err := self.applyMutation(models.AuditResourceFinanceReceipt, receiptId, models.AuditActionDelete, receiptAudit(before), nil, func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, receiptId).Delete(&agentFinanceReceiptModel{}).Error
	}); err != nil {
		return nil, err
	}
	if before.AgentAttachmentID == "" {
		return deleted, nil
	}
	// The photo goes with the receipt it was uploaded for, unless a turn of
	// a conversation carries the same file, or another receipt still
	// points at it.
	removed := self.tx.Exec(`DELETE FROM "agent_attachment" WHERE "agent_id" = ? AND "id" = ? AND "message_id" = ''
		AND NOT EXISTS (SELECT 1 FROM "agent_finance_receipt" WHERE "agent_attachment_id" = "agent_attachment"."id")`,
		agentId, before.AgentAttachmentID)
	if removed.Error != nil {
		return nil, removed.Error
	}
	if removed.RowsAffected > 0 {
		deleted.DeletedAgentAttachmentID = before.AgentAttachmentID
	}
	return deleted, nil
}

// --- matches --------------------------------------------------------------

// errReceiptMatchNotWritten undoes the audit event of a match the guarded
// statement did not write.
var errReceiptMatchNotWritten = fmt.Errorf("db: the receipt match was not written")

func (self *transaction) PutFinanceReceiptMatch(agentId string, match *models.FinanceReceiptMatch) (bool, error) {
	if match == nil || !match.ReceiptMatchSource.IsValid() {
		return false, fmt.Errorf("%w: a match needs what made it, receipt_matcher or person", ErrInvalidArguments)
	}
	matchedAmount, err := canonicalAmount("the matched amount", match.MatchedAmount)
	if err != nil {
		return false, err
	}
	if parsedMatchedAmount, _ := new(big.Rat).SetString(matchedAmount); parsedMatchedAmount == nil || parsedMatchedAmount.Sign() <= 0 {
		return false, fmt.Errorf("%w: the matched amount is what the receipt explains of the charge, more than zero", ErrInvalidArguments)
	}
	var matchConfidence *string
	if strings.TrimSpace(match.MatchConfidence) != "" {
		canonical, err := canonicalAmount("the match confidence", match.MatchConfidence)
		parsedMatchConfidence, _ := new(big.Rat).SetString(canonical)
		if err != nil || parsedMatchConfidence == nil || parsedMatchConfidence.Sign() < 0 || parsedMatchConfidence.Cmp(big.NewRat(1, 1)) > 0 {
			return false, fmt.Errorf("%w: the match confidence %q is not a decimal between 0 and 1", ErrInvalidArguments, match.MatchConfidence)
		}
		matchConfidence = &canonical
	}
	receipt, err := self.GetFinanceReceipt(agentId, match.ReceiptID)
	if err != nil {
		return false, err
	}
	financeTransaction, err := self.GetFinanceTransaction(agentId, match.FinanceTransactionID)
	if err != nil {
		return false, err
	}
	if receipt == nil || financeTransaction == nil {
		return false, ErrNotFound
	}
	for _, existing := range receipt.ReceiptMatches {
		if existing.FinanceTransactionID == match.FinanceTransactionID && existing.ReceiptMatchSource == models.ReceiptMatchSourcePerson &&
			match.ReceiptMatchSource != models.ReceiptMatchSourcePerson {
			return false, nil
		}
	}
	if err := self.checkReceiptMatchFits(agentId, receipt, financeTransaction, match.MatchedAmount); err != nil {
		return false, err
	}
	after := make([]*models.FinanceReceiptMatch, 0, len(receipt.ReceiptMatches)+1)
	for _, existing := range receipt.ReceiptMatches {
		if existing.FinanceTransactionID != match.FinanceTransactionID {
			after = append(after, existing)
		}
	}
	after = append(after, &models.FinanceReceiptMatch{
		ReceiptID: receipt.ID, FinanceTransactionID: financeTransaction.ID, MatchedAmount: matchedAmount, ReceiptMatchSource: match.ReceiptMatchSource,
	})
	if err := self.applyMutation(models.AuditResourceFinanceReceipt, receipt.ID, models.AuditActionUpdate,
		receiptMatchesAudit(receipt.ReceiptMatches), receiptMatchesAudit(after), func(tx *gorm.DB) error {
			// The person's match is checked again in the statement, so the
			// matcher cannot replace one the person made in between; when it
			// did not write, neither does the audit log.
			inserted := tx.Exec(`INSERT INTO "agent_finance_receipt_match" AS "existing" ("receipt_id", "finance_transaction_id", "agent_id",
					"matched_amount", "receipt_match_source", "match_confidence", "created_at")
				VALUES (?, ?, ?, ?::numeric, ?, ?::numeric, ?)
				ON CONFLICT ("receipt_id", "finance_transaction_id") DO UPDATE SET
					"matched_amount" = EXCLUDED."matched_amount", "receipt_match_source" = EXCLUDED."receipt_match_source",
					"match_confidence" = EXCLUDED."match_confidence"
				WHERE "existing"."receipt_match_source" <> 'person' OR EXCLUDED."receipt_match_source" = 'person'`,
				receipt.ID, financeTransaction.ID, agentId, matchedAmount, string(match.ReceiptMatchSource), matchConfidence, time.Now())
			if inserted.Error != nil {
				return inserted.Error
			}
			if inserted.RowsAffected == 0 {
				return errReceiptMatchNotWritten
			}
			return nil
		}); err != nil {
		if errors.Is(err, errReceiptMatchNotWritten) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (self *transaction) DropUnfittingFinanceReceiptMatches(agentId, receiptId string) ([]*FinanceReceiptMatchDropped, error) {
	receipt, err := self.GetFinanceReceipt(agentId, receiptId)
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, ErrNotFound
	}
	dropped := []*FinanceReceiptMatchDropped{}
	if len(receipt.ReceiptMatches) == 0 {
		return dropped, nil
	}
	// Every match is taken off and each put back that fits, oldest first,
	// so each is weighed against the receipt's total with only the ones
	// kept before it, never with a later one that is about to go.
	if err := self.tx.Where(`"agent_id" = ? AND "receipt_id" = ?`, agentId, receipt.ID).Delete(&agentFinanceReceiptMatchModel{}).Error; err != nil {
		return nil, err
	}
	kept := make([]*models.FinanceReceiptMatch, 0, len(receipt.ReceiptMatches))
	for _, match := range receipt.ReceiptMatches {
		financeTransaction, err := self.GetFinanceTransaction(agentId, match.FinanceTransactionID)
		if err != nil {
			return nil, err
		}
		if financeTransaction == nil {
			dropped = append(dropped, &FinanceReceiptMatchDropped{ReceiptMatch: match, DropReason: "that charge is no longer there"})
			continue
		}
		if err := self.checkReceiptMatchFits(agentId, receipt, financeTransaction, match.MatchedAmount); err != nil {
			if !errors.Is(err, ErrInvalidArguments) {
				return nil, err
			}
			dropped = append(dropped, &FinanceReceiptMatchDropped{
				ReceiptMatch: match, DropReason: strings.TrimPrefix(err.Error(), ErrInvalidArguments.Error()+": "),
			})
			continue
		}
		var matchConfidence *string
		if match.MatchConfidence != "" {
			matchConfidence = &match.MatchConfidence
		}
		if err := self.tx.Exec(`INSERT INTO "agent_finance_receipt_match" ("receipt_id", "finance_transaction_id", "agent_id",
				"matched_amount", "receipt_match_source", "match_confidence", "created_at")
			VALUES (?, ?, ?, ?::numeric, ?, ?::numeric, ?)`,
			receipt.ID, financeTransaction.ID, agentId, match.MatchedAmount, string(match.ReceiptMatchSource),
			matchConfidence, match.CreatedAt).Error; err != nil {
			return nil, err
		}
		kept = append(kept, match)
	}
	if len(dropped) == 0 {
		return dropped, nil
	}
	// The rows are already as they end; the mutation only records them.
	if err := self.applyMutation(models.AuditResourceFinanceReceipt, receipt.ID, models.AuditActionUpdate,
		receiptMatchesAudit(receipt.ReceiptMatches), receiptMatchesAudit(kept), func(*gorm.DB) error { return nil }); err != nil {
		return nil, err
	}
	return dropped, nil
}

// checkReceiptMatchFits refuses a match that cannot be true of the charge
// or of the receipt: in another currency than the receipt, of money in,
// finer than the currency counts, explaining with the other receipts'
// matches more than the charge took, or explaining with the receipt's
// matches to its other charges more than the receipt's total. The
// receipt's row and then the charge's are locked first, so two matches
// written together cannot both fit on their own and overflow either
// between them.
func (self *transaction) checkReceiptMatchFits(agentId string, receipt *models.FinanceReceipt, financeTransaction *models.FinanceTransaction, matchedAmountText string) error {
	if !strings.EqualFold(strings.TrimSpace(receipt.CurrencyCode), strings.TrimSpace(financeTransaction.CurrencyCode)) {
		return fmt.Errorf("%w: the receipt is in %s and the charge in %s; a receipt explains only a charge in its own currency",
			ErrInvalidArguments, receipt.CurrencyCode, financeTransaction.CurrencyCode)
	}
	chargedAmount, isParsed := new(big.Rat).SetString(strings.TrimSpace(financeTransaction.Amount))
	if !isParsed || chargedAmount.Sign() >= 0 {
		return fmt.Errorf("%w: the transaction is not money out, so no receipt explains it", ErrInvalidArguments)
	}
	chargedAmount.Neg(chargedAmount)
	matchedText := strings.TrimSpace(matchedAmountText)
	if _, fraction, hasPoint := strings.Cut(matchedText, "."); hasPoint && len(strings.TrimRight(fraction, "0")) > finance.CurrencyMinorUnits(receipt.CurrencyCode) {
		return fmt.Errorf("%w: the matched amount %s has more places than %s has", ErrInvalidArguments, matchedText, receipt.CurrencyCode)
	}
	matchedAmount, isParsed := new(big.Rat).SetString(matchedText)
	if !isParsed {
		return fmt.Errorf("%w: the matched amount %q is not a decimal", ErrInvalidArguments, matchedText)
	}
	totalAmount, isParsed := new(big.Rat).SetString(strings.TrimSpace(receipt.TotalAmount))
	if !isParsed {
		return fmt.Errorf("db: cannot read the total of receipt %s: %q", receipt.ID, receipt.TotalAmount)
	}
	// No key update rather than update: two matches to the same receipt or
	// charge still wait for each other, while a concurrent insert of a match
	// row (a sync carrying a pending charge's match) takes only the key share
	// lock its foreign key check needs, which this does not block, so the two
	// cannot deadlock.
	if err := self.tx.Exec(`SELECT 1 FROM "agent_finance_receipt" WHERE "agent_id" = ? AND "id" = ? FOR NO KEY UPDATE`, agentId, receipt.ID).Error; err != nil {
		return err
	}
	if err := self.tx.Exec(`SELECT 1 FROM "agent_finance_transaction" WHERE "agent_id" = ? AND "id" = ? FOR NO KEY UPDATE`, agentId, financeTransaction.ID).Error; err != nil {
		return err
	}
	var elsewhereMatchedText string
	if err := self.tx.Raw(`SELECT COALESCE(SUM("matched_amount"), 0)::text FROM "agent_finance_receipt_match"
		WHERE "agent_id" = ? AND "receipt_id" = ? AND "finance_transaction_id" <> ?`, agentId, receipt.ID, financeTransaction.ID).Scan(&elsewhereMatchedText).Error; err != nil {
		return err
	}
	elsewhereMatchedAmount, isParsed := new(big.Rat).SetString(elsewhereMatchedText)
	if !isParsed {
		return fmt.Errorf("db: cannot read what receipt %s explains of its other charges: %q", receipt.ID, elsewhereMatchedText)
	}
	if receiptExplainedAmount := new(big.Rat).Add(elsewhereMatchedAmount, matchedAmount); receiptExplainedAmount.Cmp(totalAmount) > 0 {
		receiptLeftAmount := new(big.Rat).Sub(totalAmount, elsewhereMatchedAmount)
		if receiptLeftAmount.Sign() < 0 {
			receiptLeftAmount.SetInt64(0)
		}
		return fmt.Errorf("%w: the receipt comes to %s %s and its matches would explain %s; at most %s of it is left to match",
			ErrInvalidArguments, finance.FormatReceiptAmount(totalAmount.FloatString(4), receipt.CurrencyCode), receipt.CurrencyCode,
			finance.FormatReceiptAmount(receiptExplainedAmount.FloatString(4), receipt.CurrencyCode),
			finance.FormatReceiptAmount(receiptLeftAmount.FloatString(4), receipt.CurrencyCode))
	}
	coverageById, err := self.FinanceReceiptMatchCoverage(agentId, receipt.ID, []string{financeTransaction.ID})
	if err != nil {
		return err
	}
	explainedAmount := new(big.Rat).Set(matchedAmount)
	if coverage := coverageById[financeTransaction.ID]; coverage != nil {
		otherAmount, isParsed := new(big.Rat).SetString(coverage.OtherMatchedAmount)
		if !isParsed {
			return fmt.Errorf("db: cannot read what other receipts explain of %s: %q", financeTransaction.ID, coverage.OtherMatchedAmount)
		}
		explainedAmount.Add(explainedAmount, otherAmount)
	}
	if explainedAmount.Cmp(chargedAmount) > 0 {
		unexplainedAmount := new(big.Rat).Sub(chargedAmount, new(big.Rat).Sub(explainedAmount, matchedAmount))
		return fmt.Errorf("%w: the charge took %s %s and its receipts would explain %s; at most %s of it is left to explain",
			ErrInvalidArguments, finance.FormatReceiptAmount(chargedAmount.FloatString(4), receipt.CurrencyCode), receipt.CurrencyCode,
			finance.FormatReceiptAmount(explainedAmount.FloatString(4), receipt.CurrencyCode),
			finance.FormatReceiptAmount(unexplainedAmount.FloatString(4), receipt.CurrencyCode))
	}
	return nil
}

func (self *transaction) DeleteFinanceReceiptMatch(agentId, receiptId, financeTransactionId string, receiptMatchSource models.ReceiptMatchSource) (bool, error) {
	receipt, err := self.GetFinanceReceipt(agentId, receiptId)
	if err != nil {
		return false, err
	}
	if receipt == nil {
		return false, ErrNotFound
	}
	var removed *models.FinanceReceiptMatch
	after := make([]*models.FinanceReceiptMatch, 0, len(receipt.ReceiptMatches))
	for _, existing := range receipt.ReceiptMatches {
		if existing.FinanceTransactionID == financeTransactionId && (receiptMatchSource == "" || existing.ReceiptMatchSource == receiptMatchSource) {
			removed = existing
			continue
		}
		after = append(after, existing)
	}
	if removed == nil {
		return false, nil
	}
	if err := self.applyMutation(models.AuditResourceFinanceReceipt, receipt.ID, models.AuditActionUpdate,
		receiptMatchesAudit(receipt.ReceiptMatches), receiptMatchesAudit(after), func(tx *gorm.DB) error {
			return tx.Where(`"agent_id" = ? AND "receipt_id" = ? AND "finance_transaction_id" = ? AND "receipt_match_source" = ?`,
				agentId, receipt.ID, financeTransactionId, string(removed.ReceiptMatchSource)).Delete(&agentFinanceReceiptMatchModel{}).Error
		}); err != nil {
		return false, err
	}
	return true, nil
}
