package db

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// FinanceTaxOperation is a person's paystubs and tax forms, each read out
// of one uploaded file. Every call takes the agent id and filters on it,
// so an id from another person finds nothing. The checking is the finance
// package's; this keeps what it decided.
type FinanceTaxOperation interface {
	// PutFinancePaystub writes a paystub with its lines. When the agent
	// already holds a paystub read from the same upload, this one takes
	// its place under the same id; a tax form read from it before is
	// deleted, since one file is one document. ErrNotFound when the
	// upload is not the agent's; ErrInvalidArguments for an amount larger
	// than its column holds.
	PutFinancePaystub(paystub *models.FinancePaystub) (*models.FinancePaystub, error)

	// GetFinancePaystub is one paystub of the agent with its lines, or nil.
	GetFinancePaystub(agentId, paystubId string) (*models.FinancePaystub, error)

	// ListFinancePaystubs is one page of the agent's paystubs the filter
	// keeps, with their lines, the latest pay date first.
	ListFinancePaystubs(agentId string, filter *FinancePaystubFilter) (*FinancePaystubPage, error)

	// DeleteFinancePaystub deletes a paystub with its lines and its
	// uploaded file, unless a message of a conversation holds the same
	// file. ErrNotFound when the agent has no such paystub.
	DeleteFinancePaystub(agentId, paystubId string) (*FinanceTaxDocumentDeleted, error)

	// PutFinanceTaxForm writes a tax form with its boxes, as
	// PutFinancePaystub writes a paystub: in the place of the one read
	// from the same upload, deleting a paystub read from it before.
	PutFinanceTaxForm(taxForm *models.FinanceTaxForm) (*models.FinanceTaxForm, error)

	// DeleteFinanceTaxFormsOfAttachment deletes the tax forms read from an
	// upload of the agent's whose kind is not one of keptKinds, with their
	// boxes, keeping the upload: a statement read again that holds fewer
	// forms than before.
	DeleteFinanceTaxFormsOfAttachment(agentId, attachmentId string, keptKinds []models.TaxFormKind) error

	// GetFinanceTaxForm is one tax form of the agent with its boxes, or nil.
	GetFinanceTaxForm(agentId, taxFormId string) (*models.FinanceTaxForm, error)

	// ListFinanceTaxForms is one page of the agent's tax forms the filter
	// keeps, with their boxes, the latest tax year first.
	ListFinanceTaxForms(agentId string, filter *FinanceTaxFormFilter) (*FinanceTaxFormPage, error)

	// DeleteFinanceTaxForm deletes a tax form with its boxes and its
	// uploaded file, as DeleteFinancePaystub does.
	DeleteFinanceTaxForm(agentId, taxFormId string) (*FinanceTaxDocumentDeleted, error)
}

// FinancePaystubFilter narrows a listing of paystubs. Every field is
// optional.
type FinancePaystubFilter struct {
	// From and To bound the pay date, both included, "2006-01-02".
	From string
	To   string

	// Limit is at most FinanceTaxDocumentLimitMost; zero is
	// FinanceTaxDocumentLimitDefault. Offset is how many to pass over.
	Limit  int
	Offset int

	// ShouldCountTotal fills the page's TotalCount.
	ShouldCountTotal bool
}

// FinancePaystubPage is one page of paystubs, and whether there are more.
type FinancePaystubPage struct {
	FinancePaystubs []*models.FinancePaystub
	HasMore         bool
	TotalCount      int
}

// FinanceTaxFormFilter narrows a listing of tax forms. Every field is
// optional.
type FinanceTaxFormFilter struct {
	// TaxYear keeps one year's forms; zero keeps every year's.
	TaxYear int

	Limit            int
	Offset           int
	ShouldCountTotal bool
}

// FinanceTaxFormPage is one page of tax forms, and whether there are more.
type FinanceTaxFormPage struct {
	FinanceTaxForms []*models.FinanceTaxForm
	HasMore         bool
	TotalCount      int
}

// How many paystubs or tax forms one listing holds. A year of weekly
// paystubs is 53.
const (
	FinanceTaxDocumentLimitDefault = 60
	FinanceTaxDocumentLimitMost    = 200
)

// FinanceTaxDocumentDeleted is the uploaded file whose row went with a
// deleted paystub or tax form, whose bytes the caller removes from
// storage; empty when no file went.
type FinanceTaxDocumentDeleted struct {
	DeletedAgentAttachmentID string
}

// The bounds on what a paystub or tax form holds: more than any prints,
// few enough that a model gone wrong cannot fill a table.
const (
	maximumPaystubLineCount     = 300
	maximumTaxFormEntryCount    = 200
	maximumTaxDocumentTextCount = 500
)

type agentFinancePaystubModel struct {
	ID                    string     `gorm:"column:id;primaryKey"`
	AgentID               string     `gorm:"column:agent_id"`
	AgentAttachmentID     string     `gorm:"column:agent_attachment_id"`
	EmployerName          string     `gorm:"column:employer_name"`
	EmployerTaxID         string     `gorm:"column:employer_tax_id"`
	EmployeeName          string     `gorm:"column:employee_name"`
	EmployeeTaxIDMask     string     `gorm:"column:employee_tax_id_mask"`
	PayDate               time.Time  `gorm:"column:pay_date"`
	PeriodStartOn         *time.Time `gorm:"column:period_start_on"`
	PeriodEndOn           *time.Time `gorm:"column:period_end_on"`
	CurrencyCode          string     `gorm:"column:currency_code"`
	GrossAmount           string     `gorm:"column:gross_amount"`
	YearToDateGrossAmount *string    `gorm:"column:year_to_date_gross_amount"`
	TotalTaxAmount        *string    `gorm:"column:total_tax_amount"`
	TotalDeductionAmount  *string    `gorm:"column:total_deduction_amount"`
	NetAmount             string     `gorm:"column:net_amount"`
	PaystubCheckState     string     `gorm:"column:paystub_check_state"`
	CheckDifferenceAmount string     `gorm:"column:check_difference_amount"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	ModifiedAt            time.Time  `gorm:"column:modified_at"`
}

func (agentFinancePaystubModel) TableName() string { return "agent_finance_paystub" }

func (self *agentFinancePaystubModel) toModel() *models.FinancePaystub {
	return &models.FinancePaystub{
		ID: self.ID, AgentID: self.AgentID, AgentAttachmentID: self.AgentAttachmentID,
		EmployerName: self.EmployerName, EmployerTaxID: self.EmployerTaxID, EmployeeName: self.EmployeeName, EmployeeTaxIDMask: self.EmployeeTaxIDMask,
		PayDate: self.PayDate.Format(time.DateOnly), PeriodStartOn: formatOptionalDay(self.PeriodStartOn), PeriodEndOn: formatOptionalDay(self.PeriodEndOn),
		CurrencyCode: self.CurrencyCode, GrossAmount: self.GrossAmount, YearToDateGrossAmount: optionalString(self.YearToDateGrossAmount),
		TotalTaxAmount: optionalString(self.TotalTaxAmount), TotalDeductionAmount: optionalString(self.TotalDeductionAmount), NetAmount: self.NetAmount,
		PaystubCheckState: models.PaystubCheckState(self.PaystubCheckState), CheckDifferenceAmount: self.CheckDifferenceAmount,
		PaystubLines: []*models.FinancePaystubLine{},
		CreatedAt:    self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

type agentFinancePaystubLineModel struct {
	ID                       string  `gorm:"column:id;primaryKey"`
	AgentID                  string  `gorm:"column:agent_id"`
	PaystubID                string  `gorm:"column:paystub_id"`
	LineNumber               int     `gorm:"column:line_number"`
	PaystubLineKind          string  `gorm:"column:paystub_line_kind"`
	Description              string  `gorm:"column:description"`
	Hours                    *string `gorm:"column:hours"`
	RateAmount               *string `gorm:"column:rate_amount"`
	CurrentAmount            *string `gorm:"column:current_amount"`
	YearToDateAmount         *string `gorm:"column:year_to_date_amount"`
	PaystubTaxKind           string  `gorm:"column:paystub_tax_kind"`
	TaxJurisdictionCode      string  `gorm:"column:tax_jurisdiction_code"`
	CurrentWagesAmount       *string `gorm:"column:current_wages_amount"`
	YearToDateWagesAmount    *string `gorm:"column:year_to_date_wages_amount"`
	PaystubDeductionKind     string  `gorm:"column:paystub_deduction_kind"`
	EmployerCurrentAmount    *string `gorm:"column:employer_current_amount"`
	EmployerYearToDateAmount *string `gorm:"column:employer_year_to_date_amount"`
}

func (agentFinancePaystubLineModel) TableName() string { return "agent_finance_paystub_line" }

func (self *agentFinancePaystubLineModel) toModel() *models.FinancePaystubLine {
	return &models.FinancePaystubLine{
		ID: self.ID, LineNumber: self.LineNumber, PaystubLineKind: models.PaystubLineKind(self.PaystubLineKind), Description: self.Description,
		Hours: optionalString(self.Hours), RateAmount: optionalString(self.RateAmount),
		CurrentAmount: optionalString(self.CurrentAmount), YearToDateAmount: optionalString(self.YearToDateAmount),
		PaystubTaxKind: models.PaystubTaxKind(self.PaystubTaxKind), TaxJurisdictionCode: self.TaxJurisdictionCode,
		CurrentWagesAmount: optionalString(self.CurrentWagesAmount), YearToDateWagesAmount: optionalString(self.YearToDateWagesAmount),
		PaystubDeductionKind:  models.PaystubDeductionKind(self.PaystubDeductionKind),
		EmployerCurrentAmount: optionalString(self.EmployerCurrentAmount), EmployerYearToDateAmount: optionalString(self.EmployerYearToDateAmount),
	}
}

type agentFinanceTaxFormModel struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	AgentID            string    `gorm:"column:agent_id"`
	AgentAttachmentID  string    `gorm:"column:agent_attachment_id"`
	TaxFormKind        string    `gorm:"column:tax_form_kind"`
	TaxYear            int       `gorm:"column:tax_year"`
	IsCorrected        bool      `gorm:"column:is_corrected"`
	PayerName          string    `gorm:"column:payer_name"`
	PayerTaxID         string    `gorm:"column:payer_tax_id"`
	RecipientName      string    `gorm:"column:recipient_name"`
	RecipientTaxIDMask string    `gorm:"column:recipient_tax_id_mask"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	ModifiedAt         time.Time `gorm:"column:modified_at"`
}

func (agentFinanceTaxFormModel) TableName() string { return "agent_finance_tax_form" }

func (self *agentFinanceTaxFormModel) toModel() *models.FinanceTaxForm {
	return &models.FinanceTaxForm{
		ID: self.ID, AgentID: self.AgentID, AgentAttachmentID: self.AgentAttachmentID,
		TaxFormKind: models.TaxFormKind(self.TaxFormKind), TaxYear: self.TaxYear, IsCorrected: self.IsCorrected,
		PayerName: self.PayerName, PayerTaxID: self.PayerTaxID, RecipientName: self.RecipientName, RecipientTaxIDMask: self.RecipientTaxIDMask,
		TaxFormEntries: []*models.FinanceTaxFormEntry{},
		CreatedAt:      self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

type agentFinanceTaxFormEntryModel struct {
	ID           string  `gorm:"column:id;primaryKey"`
	AgentID      string  `gorm:"column:agent_id"`
	TaxFormID    string  `gorm:"column:tax_form_id"`
	EntryNumber  int     `gorm:"column:entry_number"`
	BoxCode      string  `gorm:"column:box_code"`
	BoxQualifier string  `gorm:"column:box_qualifier"`
	Amount       *string `gorm:"column:amount"`
	TextValue    string  `gorm:"column:text_value"`
	IsChecked    bool    `gorm:"column:is_checked"`
}

func (agentFinanceTaxFormEntryModel) TableName() string { return "agent_finance_tax_form_entry" }

func (self *agentFinanceTaxFormEntryModel) toModel() *models.FinanceTaxFormEntry {
	return &models.FinanceTaxFormEntry{
		ID: self.ID, EntryNumber: self.EntryNumber, BoxCode: self.BoxCode, BoxQualifier: self.BoxQualifier,
		Amount: optionalString(self.Amount), TextValue: self.TextValue, IsChecked: self.IsChecked,
	}
}

// paystubAudit is what the audit log keeps of a paystub: never its lines'
// amounts, only how many there are.
func paystubAudit(paystub *models.FinancePaystub) map[string]any {
	return map[string]any{
		"employerName": paystub.EmployerName, "payDate": paystub.PayDate, "grossAmount": paystub.GrossAmount,
		"netAmount": paystub.NetAmount, "paystubCheckState": paystub.PaystubCheckState, "paystubLineCount": len(paystub.PaystubLines),
	}
}

// taxFormAudit is what the audit log keeps of a tax form.
func taxFormAudit(taxForm *models.FinanceTaxForm) map[string]any {
	return map[string]any{
		"taxFormKind": taxForm.TaxFormKind, "taxYear": taxForm.TaxYear, "payerName": taxForm.PayerName,
		"isCorrected": taxForm.IsCorrected, "taxFormEntryCount": len(taxForm.TaxFormEntries),
	}
}

// checkTaxDocumentText refuses text longer than a column of a paystub or
// tax form holds.
func checkTaxDocumentText(what string, texts ...string) error {
	for _, text := range texts {
		if len([]rune(text)) > maximumTaxDocumentTextCount {
			return fmt.Errorf("%w: %s is at most %d characters", ErrInvalidArguments, what, maximumTaxDocumentTextCount)
		}
	}
	return nil
}

// checkTaxDocumentMask refuses anything but four digits, or nothing, as
// the kept part of a person's taxpayer number.
func checkTaxDocumentMask(mask string) error {
	if mask != "" && (len(mask) != 4 || strings.Trim(mask, "0123456789") != "") {
		return fmt.Errorf("%w: only the last four digits of a taxpayer number are kept", ErrInvalidArguments)
	}
	return nil
}

// ownTaxDocumentAttachment checks the upload is the agent's, and locks its
// documents so two reads of it finishing together write one: a row lock
// takes nothing on the first read, when there is no row yet.
func (self *transaction) ownTaxDocumentAttachment(agentId, attachmentId string) error {
	attachment, err := self.GetAgentAttachment(attachmentId)
	if err != nil {
		return err
	}
	if attachment == nil || attachment.AgentID != agentId {
		return ErrNotFound
	}
	digest := sha256.Sum256([]byte("agent_finance_tax_document\x00" + agentId + "\x00" + attachmentId))
	return self.tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(binary.BigEndian.Uint64(digest[:8])&math.MaxInt64)).Error
}

func (self *transaction) PutFinancePaystub(paystub *models.FinancePaystub) (*models.FinancePaystub, error) {
	if paystub == nil || paystub.AgentID == "" || paystub.AgentAttachmentID == "" {
		return nil, fmt.Errorf("%w: a paystub needs its agent and the upload it was read from", ErrInvalidArguments)
	}
	if !paystub.PaystubCheckState.IsValid() {
		return nil, fmt.Errorf("%w: %q is not a paystub check state", ErrInvalidArguments, paystub.PaystubCheckState)
	}
	if err := checkTaxDocumentText("a paystub's names", paystub.EmployerName, paystub.EmployerTaxID, paystub.EmployeeName); err != nil {
		return nil, err
	}
	if err := checkTaxDocumentMask(paystub.EmployeeTaxIDMask); err != nil {
		return nil, err
	}
	if len(paystub.PaystubLines) > maximumPaystubLineCount {
		return nil, fmt.Errorf("%w: a paystub has at most %d lines", ErrInvalidArguments, maximumPaystubLineCount)
	}
	payDate, err := time.Parse(time.DateOnly, strings.TrimSpace(paystub.PayDate))
	if err != nil {
		return nil, fmt.Errorf("%w: the pay date %q is not a day", ErrInvalidArguments, paystub.PayDate)
	}
	row := &agentFinancePaystubModel{
		AgentID: paystub.AgentID, AgentAttachmentID: paystub.AgentAttachmentID,
		EmployerName: paystub.EmployerName, EmployerTaxID: paystub.EmployerTaxID, EmployeeName: paystub.EmployeeName,
		EmployeeTaxIDMask: paystub.EmployeeTaxIDMask, PayDate: payDate, CurrencyCode: strings.ToUpper(strings.TrimSpace(paystub.CurrencyCode)),
		PaystubCheckState: string(paystub.PaystubCheckState), CheckDifferenceAmount: "0",
	}
	for _, day := range []struct {
		text  string
		field **time.Time
	}{{paystub.PeriodStartOn, &row.PeriodStartOn}, {paystub.PeriodEndOn, &row.PeriodEndOn}} {
		parsed, err := parseOptionalDay(day.text)
		if err != nil {
			return nil, err
		}
		if parsed != "" {
			value, _ := time.Parse(time.DateOnly, parsed)
			*day.field = &value
		}
	}
	if row.GrossAmount, err = canonicalReceiptAmount("the gross pay", paystub.GrossAmount); err != nil {
		return nil, err
	}
	if row.NetAmount, err = canonicalReceiptAmount("the take-home pay", paystub.NetAmount); err != nil {
		return nil, err
	}
	if strings.TrimSpace(paystub.CheckDifferenceAmount) != "" {
		if row.CheckDifferenceAmount, err = canonicalReceiptAmount("the check difference", paystub.CheckDifferenceAmount); err != nil {
			return nil, err
		}
	}
	for _, optional := range []struct {
		what  string
		text  string
		field **string
	}{
		{"the year-to-date gross pay", paystub.YearToDateGrossAmount, &row.YearToDateGrossAmount},
		{"the total of the taxes", paystub.TotalTaxAmount, &row.TotalTaxAmount},
		{"the total of the deductions", paystub.TotalDeductionAmount, &row.TotalDeductionAmount},
	} {
		if *optional.field, err = canonicalOptionalReceiptAmount(optional.what, optional.text); err != nil {
			return nil, err
		}
	}
	lineRows := make([]*agentFinancePaystubLineModel, 0, len(paystub.PaystubLines))
	seenLineNumbers := map[int]bool{}
	for _, line := range paystub.PaystubLines {
		if line == nil || line.LineNumber <= 0 || seenLineNumbers[line.LineNumber] || !line.PaystubLineKind.IsValid() {
			return nil, fmt.Errorf("%w: paystub lines are numbered from 1, each once, and are an earning, a tax or a deduction", ErrInvalidArguments)
		}
		seenLineNumbers[line.LineNumber] = true
		if err := checkTaxDocumentText(fmt.Sprintf("line %d", line.LineNumber), line.Description, line.TaxJurisdictionCode); err != nil {
			return nil, err
		}
		lineRow := &agentFinancePaystubLineModel{
			ID: newID(), AgentID: paystub.AgentID, LineNumber: line.LineNumber, PaystubLineKind: string(line.PaystubLineKind),
			Description: line.Description, PaystubTaxKind: string(line.PaystubTaxKind), TaxJurisdictionCode: line.TaxJurisdictionCode,
			PaystubDeductionKind: string(line.PaystubDeductionKind),
		}
		for _, optional := range []struct {
			what  string
			text  string
			field **string
		}{
			{"rate", line.RateAmount, &lineRow.RateAmount}, {"amount this period", line.CurrentAmount, &lineRow.CurrentAmount},
			{"amount for the year to date", line.YearToDateAmount, &lineRow.YearToDateAmount},
			{"wages this period", line.CurrentWagesAmount, &lineRow.CurrentWagesAmount},
			{"wages for the year to date", line.YearToDateWagesAmount, &lineRow.YearToDateWagesAmount},
			{"employer part this period", line.EmployerCurrentAmount, &lineRow.EmployerCurrentAmount},
			{"employer part for the year to date", line.EmployerYearToDateAmount, &lineRow.EmployerYearToDateAmount},
		} {
			if *optional.field, err = canonicalOptionalReceiptAmount(fmt.Sprintf("line %d's %s", line.LineNumber, optional.what), optional.text); err != nil {
				return nil, err
			}
		}
		if strings.TrimSpace(line.Hours) != "" {
			parsed, isParsed := new(big.Rat).SetString(strings.TrimSpace(line.Hours))
			if !isParsed || strings.ContainsAny(line.Hours, "eE/") {
				return nil, fmt.Errorf("%w: line %d's hours %q are not a decimal", ErrInvalidArguments, line.LineNumber, line.Hours)
			}
			hours := parsed.FloatString(8)
			if err := checkReceiptBound(fmt.Sprintf("line %d's hours", line.LineNumber), hours, maximumReceiptQuantityBound); err != nil {
				return nil, err
			}
			lineRow.Hours = &hours
		}
		lineRows = append(lineRows, lineRow)
	}
	if err := self.ownTaxDocumentAttachment(paystub.AgentID, paystub.AgentAttachmentID); err != nil {
		return nil, err
	}
	if err := self.deleteTaxFormsOfAttachment(paystub.AgentID, paystub.AgentAttachmentID); err != nil {
		return nil, err
	}
	var existing []agentFinancePaystubModel
	if err := self.tx.Where(`"agent_id" = ? AND "agent_attachment_id" = ?`, paystub.AgentID, paystub.AgentAttachmentID).Limit(1).Find(&existing).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	row.ModifiedAt = now
	action := models.AuditActionCreate
	var before map[string]any
	if len(existing) > 0 {
		action = models.AuditActionUpdate
		previous, err := self.GetFinancePaystub(paystub.AgentID, existing[0].ID)
		if err != nil {
			return nil, err
		}
		before = paystubAudit(previous)
		row.ID, row.CreatedAt = existing[0].ID, existing[0].CreatedAt
	} else {
		row.ID, row.CreatedAt = newID(), now
	}
	stored := row.toModel()
	stored.PaystubLines = paystub.PaystubLines
	if err := self.applyMutation(models.AuditResourceFinancePaystub, row.ID, action, before, paystubAudit(stored), func(tx *gorm.DB) error {
		if action == models.AuditActionCreate {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Save(row).Error; err != nil {
				return err
			}
			if err := tx.Where(`"agent_id" = ? AND "paystub_id" = ?`, row.AgentID, row.ID).Delete(&agentFinancePaystubLineModel{}).Error; err != nil {
				return err
			}
		}
		for _, lineRow := range lineRows {
			lineRow.PaystubID = row.ID
		}
		if len(lineRows) == 0 {
			return nil
		}
		return tx.Create(lineRows).Error
	}); err != nil {
		return nil, err
	}
	return self.GetFinancePaystub(paystub.AgentID, row.ID)
}

func (self *transaction) GetFinancePaystub(agentId, paystubId string) (*models.FinancePaystub, error) {
	var found []agentFinancePaystubModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, paystubId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	paystubs, err := self.paystubsWithLines(agentId, found)
	if err != nil {
		return nil, err
	}
	return paystubs[0], nil
}

func (self *transaction) paystubsWithLines(agentId string, rows []agentFinancePaystubModel) ([]*models.FinancePaystub, error) {
	paystubs := make([]*models.FinancePaystub, 0, len(rows))
	byId := map[string]*models.FinancePaystub{}
	ids := make([]string, 0, len(rows))
	for index := range rows {
		paystub := rows[index].toModel()
		paystubs = append(paystubs, paystub)
		byId[paystub.ID] = paystub
		ids = append(ids, paystub.ID)
	}
	if len(ids) == 0 {
		return paystubs, nil
	}
	var lineRows []agentFinancePaystubLineModel
	if err := self.tx.Where(`"agent_id" = ? AND "paystub_id" = ANY(?::text[])`, agentId, pq.Array(ids)).
		Order(`"paystub_id", "line_number"`).Find(&lineRows).Error; err != nil {
		return nil, err
	}
	for index := range lineRows {
		byId[lineRows[index].PaystubID].PaystubLines = append(byId[lineRows[index].PaystubID].PaystubLines, lineRows[index].toModel())
	}
	return paystubs, nil
}

func taxDocumentLimit(limit int) int {
	switch {
	case limit <= 0:
		return FinanceTaxDocumentLimitDefault
	case limit > FinanceTaxDocumentLimitMost:
		return FinanceTaxDocumentLimitMost
	}
	return limit
}

func (self *transaction) ListFinancePaystubs(agentId string, filter *FinancePaystubFilter) (*FinancePaystubPage, error) {
	if filter == nil {
		filter = &FinancePaystubFilter{}
	}
	query := self.tx.Model(&agentFinancePaystubModel{}).Where(`"agent_id" = ?`, agentId)
	for _, bound := range []struct {
		text, condition string
	}{{filter.From, `"pay_date" >= ?`}, {filter.To, `"pay_date" <= ?`}} {
		day, err := parseOptionalDay(bound.text)
		if err != nil {
			return nil, err
		}
		if day != "" {
			query = query.Where(bound.condition, day)
		}
	}
	page := &FinancePaystubPage{}
	if filter.ShouldCountTotal {
		var total int64
		if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, err
		}
		page.TotalCount = int(total)
	}
	limit := taxDocumentLimit(filter.Limit)
	var rows []agentFinancePaystubModel
	if err := query.Order(`"pay_date" DESC, "id" DESC`).Offset(max(filter.Offset, 0)).Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > limit {
		rows, page.HasMore = rows[:limit], true
	}
	paystubs, err := self.paystubsWithLines(agentId, rows)
	if err != nil {
		return nil, err
	}
	page.FinancePaystubs = paystubs
	return page, nil
}

// deleteTaxDocumentAttachment deletes a paystub's or tax form's upload
// once nothing else holds it: a message of a conversation, a receipt, or
// another paystub or tax form.
func (self *transaction) deleteTaxDocumentAttachment(agentId, attachmentId string) (*FinanceTaxDocumentDeleted, error) {
	deleted := &FinanceTaxDocumentDeleted{}
	removed := self.tx.Exec(`DELETE FROM "agent_attachment" WHERE "agent_id" = ? AND "id" = ? AND "message_id" = ''
		AND NOT EXISTS (SELECT 1 FROM "agent_finance_receipt" WHERE "agent_attachment_id" = "agent_attachment"."id")
		AND NOT EXISTS (SELECT 1 FROM "agent_finance_paystub" WHERE "agent_attachment_id" = "agent_attachment"."id")
		AND NOT EXISTS (SELECT 1 FROM "agent_finance_tax_form" WHERE "agent_attachment_id" = "agent_attachment"."id")`,
		agentId, attachmentId)
	if removed.Error != nil {
		return nil, removed.Error
	}
	if removed.RowsAffected > 0 {
		deleted.DeletedAgentAttachmentID = attachmentId
	}
	return deleted, nil
}

func (self *transaction) DeleteFinancePaystub(agentId, paystubId string) (*FinanceTaxDocumentDeleted, error) {
	before, err := self.GetFinancePaystub(agentId, paystubId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	if err := self.applyMutation(models.AuditResourceFinancePaystub, paystubId, models.AuditActionDelete, paystubAudit(before), nil, func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, paystubId).Delete(&agentFinancePaystubModel{}).Error
	}); err != nil {
		return nil, err
	}
	return self.deleteTaxDocumentAttachment(agentId, before.AgentAttachmentID)
}

// deletePaystubsOfAttachment and deleteTaxFormsOfAttachment delete what
// was read out of an upload before, when it is read again as the other
// kind of document. The upload stays.
func (self *transaction) deletePaystubsOfAttachment(agentId, attachmentId string) error {
	var rows []agentFinancePaystubModel
	if err := self.tx.Where(`"agent_id" = ? AND "agent_attachment_id" = ?`, agentId, attachmentId).Find(&rows).Error; err != nil {
		return err
	}
	for index := range rows {
		before, err := self.GetFinancePaystub(agentId, rows[index].ID)
		if err != nil || before == nil {
			return err
		}
		if err := self.applyMutation(models.AuditResourceFinancePaystub, before.ID, models.AuditActionDelete, paystubAudit(before), nil, func(tx *gorm.DB) error {
			return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, before.ID).Delete(&agentFinancePaystubModel{}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) deleteTaxFormsOfAttachment(agentId, attachmentId string) error {
	var rows []agentFinanceTaxFormModel
	if err := self.tx.Where(`"agent_id" = ? AND "agent_attachment_id" = ?`, agentId, attachmentId).Find(&rows).Error; err != nil {
		return err
	}
	for index := range rows {
		before, err := self.GetFinanceTaxForm(agentId, rows[index].ID)
		if err != nil || before == nil {
			return err
		}
		if err := self.applyMutation(models.AuditResourceFinanceTaxForm, before.ID, models.AuditActionDelete, taxFormAudit(before), nil, func(tx *gorm.DB) error {
			return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, before.ID).Delete(&agentFinanceTaxFormModel{}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) PutFinanceTaxForm(taxForm *models.FinanceTaxForm) (*models.FinanceTaxForm, error) {
	if taxForm == nil || taxForm.AgentID == "" || taxForm.AgentAttachmentID == "" {
		return nil, fmt.Errorf("%w: a tax form needs its agent and the upload it was read from", ErrInvalidArguments)
	}
	if !taxForm.TaxFormKind.IsValid() || taxForm.TaxYear < 1900 || taxForm.TaxYear > 2999 {
		return nil, fmt.Errorf("%w: a tax form needs a known kind and a tax year", ErrInvalidArguments)
	}
	if err := checkTaxDocumentText("a tax form's names", taxForm.PayerName, taxForm.PayerTaxID, taxForm.RecipientName); err != nil {
		return nil, err
	}
	if err := checkTaxDocumentMask(taxForm.RecipientTaxIDMask); err != nil {
		return nil, err
	}
	if len(taxForm.TaxFormEntries) > maximumTaxFormEntryCount {
		return nil, fmt.Errorf("%w: a tax form has at most %d boxes", ErrInvalidArguments, maximumTaxFormEntryCount)
	}
	row := &agentFinanceTaxFormModel{
		AgentID: taxForm.AgentID, AgentAttachmentID: taxForm.AgentAttachmentID,
		TaxFormKind: string(taxForm.TaxFormKind), TaxYear: taxForm.TaxYear, IsCorrected: taxForm.IsCorrected,
		PayerName: taxForm.PayerName, PayerTaxID: taxForm.PayerTaxID, RecipientName: taxForm.RecipientName, RecipientTaxIDMask: taxForm.RecipientTaxIDMask,
	}
	entryRows := make([]*agentFinanceTaxFormEntryModel, 0, len(taxForm.TaxFormEntries))
	seenEntryNumbers, seenBoxes := map[int]bool{}, map[string]bool{}
	for _, entry := range taxForm.TaxFormEntries {
		if entry == nil || entry.EntryNumber <= 0 || seenEntryNumbers[entry.EntryNumber] || strings.TrimSpace(entry.BoxCode) == "" {
			return nil, fmt.Errorf("%w: tax form boxes are numbered from 1, each once, and each names its box", ErrInvalidArguments)
		}
		seenEntryNumbers[entry.EntryNumber] = true
		boxKey := entry.BoxCode + "\x00" + entry.BoxQualifier
		if seenBoxes[boxKey] {
			return nil, fmt.Errorf("%w: box %s %s is given twice", ErrInvalidArguments, entry.BoxCode, entry.BoxQualifier)
		}
		seenBoxes[boxKey] = true
		if err := checkTaxDocumentText(fmt.Sprintf("box %s", entry.BoxCode), entry.BoxCode, entry.BoxQualifier, entry.TextValue); err != nil {
			return nil, err
		}
		amount, err := canonicalOptionalReceiptAmount(fmt.Sprintf("box %s", entry.BoxCode), entry.Amount)
		if err != nil {
			return nil, err
		}
		entryRows = append(entryRows, &agentFinanceTaxFormEntryModel{
			ID: newID(), AgentID: taxForm.AgentID, EntryNumber: entry.EntryNumber, BoxCode: entry.BoxCode, BoxQualifier: entry.BoxQualifier,
			Amount: amount, TextValue: entry.TextValue, IsChecked: entry.IsChecked,
		})
	}
	if err := self.ownTaxDocumentAttachment(taxForm.AgentID, taxForm.AgentAttachmentID); err != nil {
		return nil, err
	}
	if err := self.deletePaystubsOfAttachment(taxForm.AgentID, taxForm.AgentAttachmentID); err != nil {
		return nil, err
	}
	var existing []agentFinanceTaxFormModel
	if err := self.tx.Where(`"agent_id" = ? AND "agent_attachment_id" = ? AND "tax_form_kind" = ?`, taxForm.AgentID, taxForm.AgentAttachmentID, string(taxForm.TaxFormKind)).Limit(1).Find(&existing).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	row.ModifiedAt = now
	action := models.AuditActionCreate
	var before map[string]any
	if len(existing) > 0 {
		action = models.AuditActionUpdate
		previous, err := self.GetFinanceTaxForm(taxForm.AgentID, existing[0].ID)
		if err != nil {
			return nil, err
		}
		before = taxFormAudit(previous)
		row.ID, row.CreatedAt = existing[0].ID, existing[0].CreatedAt
	} else {
		row.ID, row.CreatedAt = newID(), now
	}
	stored := row.toModel()
	stored.TaxFormEntries = taxForm.TaxFormEntries
	if err := self.applyMutation(models.AuditResourceFinanceTaxForm, row.ID, action, before, taxFormAudit(stored), func(tx *gorm.DB) error {
		if action == models.AuditActionCreate {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Save(row).Error; err != nil {
				return err
			}
			if err := tx.Where(`"agent_id" = ? AND "tax_form_id" = ?`, row.AgentID, row.ID).Delete(&agentFinanceTaxFormEntryModel{}).Error; err != nil {
				return err
			}
		}
		for _, entryRow := range entryRows {
			entryRow.TaxFormID = row.ID
		}
		if len(entryRows) == 0 {
			return nil
		}
		return tx.Create(entryRows).Error
	}); err != nil {
		return nil, err
	}
	return self.GetFinanceTaxForm(taxForm.AgentID, row.ID)
}

func (self *transaction) GetFinanceTaxForm(agentId, taxFormId string) (*models.FinanceTaxForm, error) {
	var found []agentFinanceTaxFormModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, taxFormId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	taxForms, err := self.taxFormsWithEntries(agentId, found)
	if err != nil {
		return nil, err
	}
	return taxForms[0], nil
}

func (self *transaction) taxFormsWithEntries(agentId string, rows []agentFinanceTaxFormModel) ([]*models.FinanceTaxForm, error) {
	taxForms := make([]*models.FinanceTaxForm, 0, len(rows))
	byId := map[string]*models.FinanceTaxForm{}
	ids := make([]string, 0, len(rows))
	for index := range rows {
		taxForm := rows[index].toModel()
		taxForms = append(taxForms, taxForm)
		byId[taxForm.ID] = taxForm
		ids = append(ids, taxForm.ID)
	}
	if len(ids) == 0 {
		return taxForms, nil
	}
	var entryRows []agentFinanceTaxFormEntryModel
	if err := self.tx.Where(`"agent_id" = ? AND "tax_form_id" = ANY(?::text[])`, agentId, pq.Array(ids)).
		Order(`"tax_form_id", "entry_number"`).Find(&entryRows).Error; err != nil {
		return nil, err
	}
	for index := range entryRows {
		byId[entryRows[index].TaxFormID].TaxFormEntries = append(byId[entryRows[index].TaxFormID].TaxFormEntries, entryRows[index].toModel())
	}
	return taxForms, nil
}

func (self *transaction) ListFinanceTaxForms(agentId string, filter *FinanceTaxFormFilter) (*FinanceTaxFormPage, error) {
	if filter == nil {
		filter = &FinanceTaxFormFilter{}
	}
	query := self.tx.Model(&agentFinanceTaxFormModel{}).Where(`"agent_id" = ?`, agentId)
	if filter.TaxYear != 0 {
		query = query.Where(`"tax_year" = ?`, filter.TaxYear)
	}
	page := &FinanceTaxFormPage{}
	if filter.ShouldCountTotal {
		var total int64
		if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, err
		}
		page.TotalCount = int(total)
	}
	limit := taxDocumentLimit(filter.Limit)
	var rows []agentFinanceTaxFormModel
	if err := query.Order(`"tax_year" DESC, "id" DESC`).Offset(max(filter.Offset, 0)).Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > limit {
		rows, page.HasMore = rows[:limit], true
	}
	taxForms, err := self.taxFormsWithEntries(agentId, rows)
	if err != nil {
		return nil, err
	}
	page.FinanceTaxForms = taxForms
	return page, nil
}

func (self *transaction) DeleteFinanceTaxForm(agentId, taxFormId string) (*FinanceTaxDocumentDeleted, error) {
	before, err := self.GetFinanceTaxForm(agentId, taxFormId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	if err := self.applyMutation(models.AuditResourceFinanceTaxForm, taxFormId, models.AuditActionDelete, taxFormAudit(before), nil, func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, taxFormId).Delete(&agentFinanceTaxFormModel{}).Error
	}); err != nil {
		return nil, err
	}
	return self.deleteTaxDocumentAttachment(agentId, before.AgentAttachmentID)
}

func (self *transaction) DeleteFinanceTaxFormsOfAttachment(agentId, attachmentId string, keptKinds []models.TaxFormKind) error {
	var stale []agentFinanceTaxFormModel
	query := self.tx.Where(`"agent_id" = ? AND "agent_attachment_id" = ?`, agentId, attachmentId)
	if len(keptKinds) > 0 {
		kinds := make([]string, 0, len(keptKinds))
		for _, kind := range keptKinds {
			kinds = append(kinds, string(kind))
		}
		query = query.Where(`"tax_form_kind" NOT IN ?`, kinds)
	}
	if err := query.Find(&stale).Error; err != nil {
		return err
	}
	for _, row := range stale {
		before, err := self.GetFinanceTaxForm(agentId, row.ID)
		if err != nil || before == nil {
			return err
		}
		if err := self.applyMutation(models.AuditResourceFinanceTaxForm, row.ID, models.AuditActionDelete, taxFormAudit(before), nil, func(tx *gorm.DB) error {
			return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, row.ID).Delete(&agentFinanceTaxFormModel{}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}
