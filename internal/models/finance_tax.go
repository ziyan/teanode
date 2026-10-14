package models

import "time"

// PaystubLineKind is what a printed line of a paystub is.
type PaystubLineKind string

const (
	// PaystubLineKindEarning is pay: regular, overtime, a bonus.
	PaystubLineKindEarning PaystubLineKind = "earning"
	// PaystubLineKindTax is a tax withheld, with the wages it was figured on.
	PaystubLineKindTax PaystubLineKind = "tax"
	// PaystubLineKindDeduction is taken out of the pay for something other
	// than a tax: a retirement plan, a health premium, an HSA. The
	// employer's part, when the paystub prints one, is beside it.
	PaystubLineKindDeduction PaystubLineKind = "deduction"
	// PaystubLineKindReimbursement is money paid back that is not pay:
	// expenses, printed as non-taxable earnings. It is outside gross pay
	// and its taxes, and is added to take-home pay.
	PaystubLineKindReimbursement PaystubLineKind = "reimbursement"
)

// IsValid says the kind is one of the four.
func (self PaystubLineKind) IsValid() bool {
	switch self {
	case PaystubLineKindEarning, PaystubLineKindTax, PaystubLineKindDeduction, PaystubLineKindReimbursement:
		return true
	}
	return false
}

// PaystubTaxKind is which tax a paystub's tax line withholds.
type PaystubTaxKind string

const (
	PaystubTaxKindFederalIncome  PaystubTaxKind = "federal_income"
	PaystubTaxKindSocialSecurity PaystubTaxKind = "social_security"
	PaystubTaxKindMedicare       PaystubTaxKind = "medicare"
	// PaystubTaxKindMedicareAdditional is the 0.9% withheld on Medicare
	// wages above 200,000 in the year, when the paystub prints it apart;
	// a W-2 counts it in box 6 with the rest of Medicare.
	PaystubTaxKindMedicareAdditional PaystubTaxKind = "medicare_additional"
	PaystubTaxKindStateIncome        PaystubTaxKind = "state_income"
	PaystubTaxKindLocalIncome        PaystubTaxKind = "local_income"
	// PaystubTaxKindStateDisability is a state's disability or family
	// leave insurance withheld from pay.
	PaystubTaxKindStateDisability PaystubTaxKind = "state_disability"
	PaystubTaxKindOther           PaystubTaxKind = "other"
)

// IsValid says the kind is one of the known taxes.
func (self PaystubTaxKind) IsValid() bool {
	switch self {
	case PaystubTaxKindFederalIncome, PaystubTaxKindSocialSecurity, PaystubTaxKindMedicare, PaystubTaxKindMedicareAdditional,
		PaystubTaxKindStateIncome, PaystubTaxKindLocalIncome, PaystubTaxKindStateDisability, PaystubTaxKindOther:
		return true
	}
	return false
}

// PaystubDeductionKind is what a paystub's deduction pays for, as far as
// a W-2 and a tax return tell them apart.
type PaystubDeductionKind string

const (
	// PaystubDeductionKindRetirementTraditional is a traditional 401(k),
	// 403(b) or 457 contribution, before income tax: W-2 box 12 D, E or G.
	PaystubDeductionKindRetirementTraditional PaystubDeductionKind = "retirement_traditional"
	// PaystubDeductionKindRetirementRoth is a Roth contribution, after
	// income tax: W-2 box 12 AA, BB or EE.
	PaystubDeductionKindRetirementRoth PaystubDeductionKind = "retirement_roth"
	// PaystubDeductionKindHSA is a health savings account contribution:
	// W-2 box 12 W, with the employer's.
	PaystubDeductionKindHSA PaystubDeductionKind = "hsa"
	// PaystubDeductionKindHealthPremium is a medical, dental or vision
	// premium: W-2 box 12 DD, with the employer's.
	PaystubDeductionKindHealthPremium PaystubDeductionKind = "health_premium"
	PaystubDeductionKindFSAHealth     PaystubDeductionKind = "fsa_health"
	// PaystubDeductionKindFSADependentCare is a dependent care account:
	// W-2 box 10.
	PaystubDeductionKindFSADependentCare PaystubDeductionKind = "fsa_dependent_care"
	PaystubDeductionKindCommuter         PaystubDeductionKind = "commuter"
	// PaystubDeductionKindInsurance is life or disability insurance.
	PaystubDeductionKindInsurance PaystubDeductionKind = "insurance"
	PaystubDeductionKindOther     PaystubDeductionKind = "other"
)

// IsValid says the kind is one of the known deductions.
func (self PaystubDeductionKind) IsValid() bool {
	switch self {
	case PaystubDeductionKindRetirementTraditional, PaystubDeductionKindRetirementRoth, PaystubDeductionKindHSA,
		PaystubDeductionKindHealthPremium, PaystubDeductionKindFSAHealth, PaystubDeductionKindFSADependentCare,
		PaystubDeductionKindCommuter, PaystubDeductionKindInsurance, PaystubDeductionKindOther:
		return true
	}
	return false
}

// PaystubCheckState says whether a paystub's lines add up to its printed
// totals.
type PaystubCheckState string

const (
	PaystubCheckStateBalanced   PaystubCheckState = "balanced"
	PaystubCheckStateUnbalanced PaystubCheckState = "unbalanced"
)

// IsValid says the state is one of the two.
func (self PaystubCheckState) IsValid() bool {
	return self == PaystubCheckStateBalanced || self == PaystubCheckStateUnbalanced
}

// FinancePaystub is one paycheck's pay statement, stored line by line as
// printed and checked against its own totals. Amounts are decimals,
// positive as printed. Only the last four digits of the person's taxpayer
// number are ever kept.
type FinancePaystub struct {
	ID                string `json:"id"`
	AgentID           string `json:"agentId"`
	AgentAttachmentID string `json:"agentAttachmentId"`

	EmployerName string `json:"employerName"`
	// EmployerTaxID is the employer's identification number as printed.
	EmployerTaxID string `json:"employerTaxId,omitempty" graphapi:"nullable"`
	EmployeeName  string `json:"employeeName"`
	// EmployeeTaxIDMask is the last four digits of the person's taxpayer
	// number, empty when the paystub prints none.
	EmployeeTaxIDMask string `json:"employeeTaxIdMask,omitempty" graphapi:"nullable"`

	// PayDate is the day paid, and PeriodStartOn and PeriodEndOn the days
	// the pay is for, "2006-01-02".
	PayDate       string `json:"payDate"`
	PeriodStartOn string `json:"periodStartOn,omitempty" graphapi:"nullable"`
	PeriodEndOn   string `json:"periodEndOn,omitempty" graphapi:"nullable"`
	CurrencyCode  string `json:"currencyCode"`

	// The totals as printed; empty when the paystub prints none.
	GrossAmount           string `json:"grossAmount"`
	YearToDateGrossAmount string `json:"yearToDateGrossAmount,omitempty" graphapi:"nullable"`
	TotalTaxAmount        string `json:"totalTaxAmount,omitempty" graphapi:"nullable"`
	TotalDeductionAmount  string `json:"totalDeductionAmount,omitempty" graphapi:"nullable"`
	NetAmount             string `json:"netAmount"`

	// PaystubCheckState says whether the lines add up, and
	// CheckDifferenceAmount by how much take-home pay misses: gross less
	// the taxes and deductions less the printed net, zero when balanced.
	PaystubCheckState     PaystubCheckState `json:"paystubCheckState"`
	CheckDifferenceAmount string            `json:"checkDifferenceAmount"`

	PaystubLines []*FinancePaystubLine `json:"paystubLines"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// FinancePaystubLine is one printed line of a paystub that has an amount.
type FinancePaystubLine struct {
	ID string `json:"id"`

	// LineNumber is its place on the paystub, from 1.
	LineNumber      int             `json:"lineNumber"`
	PaystubLineKind PaystubLineKind `json:"paystubLineKind"`
	Description     string          `json:"description"`

	// Hours and RateAmount are an earning's, when printed.
	Hours      string `json:"hours,omitempty" graphapi:"nullable"`
	RateAmount string `json:"rateAmount,omitempty" graphapi:"nullable"`

	// CurrentAmount is an earning or a tax this period, or a deduction's
	// employee part; YearToDateAmount the same for the year so far.
	CurrentAmount    string `json:"currentAmount,omitempty" graphapi:"nullable"`
	YearToDateAmount string `json:"yearToDateAmount,omitempty" graphapi:"nullable"`

	// PaystubTaxKind and TaxJurisdictionCode (a state's code or a
	// locality) are a tax's, and CurrentWagesAmount and
	// YearToDateWagesAmount the wages it was figured on, when printed.
	PaystubTaxKind        PaystubTaxKind `json:"paystubTaxKind,omitempty" graphapi:"nullable"`
	TaxJurisdictionCode   string         `json:"taxJurisdictionCode,omitempty" graphapi:"nullable"`
	CurrentWagesAmount    string         `json:"currentWagesAmount,omitempty" graphapi:"nullable"`
	YearToDateWagesAmount string         `json:"yearToDateWagesAmount,omitempty" graphapi:"nullable"`

	// PaystubDeductionKind is a deduction's, and EmployerCurrentAmount and
	// EmployerYearToDateAmount the employer's part of it, when printed.
	PaystubDeductionKind     PaystubDeductionKind `json:"paystubDeductionKind,omitempty" graphapi:"nullable"`
	EmployerCurrentAmount    string               `json:"employerCurrentAmount,omitempty" graphapi:"nullable"`
	EmployerYearToDateAmount string               `json:"employerYearToDateAmount,omitempty" graphapi:"nullable"`
}

// TaxFormKind is which tax form a finance tax form is.
type TaxFormKind string

const (
	// TaxFormKindW2 is a Form W-2, Wage and Tax Statement.
	TaxFormKindW2 TaxFormKind = "w2"
)

// IsValid says the kind is a known form.
func (self TaxFormKind) IsValid() bool {
	return self == TaxFormKindW2
}

// FinanceTaxForm is one tax form for one tax year, its boxes as entries.
// Only the last four digits of the recipient's taxpayer number are kept.
type FinanceTaxForm struct {
	ID                string `json:"id"`
	AgentID           string `json:"agentId"`
	AgentAttachmentID string `json:"agentAttachmentId"`

	TaxFormKind TaxFormKind `json:"taxFormKind"`
	TaxYear     int         `json:"taxYear"`
	// IsCorrected says it is a corrected form (a W-2c).
	IsCorrected bool `json:"isCorrected"`

	// PayerName and PayerTaxID are the employer's on a W-2.
	PayerName  string `json:"payerName"`
	PayerTaxID string `json:"payerTaxId,omitempty" graphapi:"nullable"`
	// RecipientName and RecipientTaxIDMask (the last four digits) are the
	// employee's on a W-2.
	RecipientName      string `json:"recipientName"`
	RecipientTaxIDMask string `json:"recipientTaxIdMask,omitempty" graphapi:"nullable"`

	TaxFormEntries []*FinanceTaxFormEntry `json:"taxFormEntries"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// FinanceTaxFormEntry is one box of a tax form.
type FinanceTaxFormEntry struct {
	ID string `json:"id"`

	// EntryNumber is its place in the form as read, from 1.
	EntryNumber int `json:"entryNumber"`
	// BoxCode is the box's number as the form prints it ("1", "12").
	BoxCode string `json:"boxCode"`
	// BoxQualifier tells boxes of one number apart: box 12's letter code,
	// a state's code in boxes 15 to 17, a locality in boxes 18 to 20,
	// box 14's label, box 13's checkbox. Empty for a box that is one.
	BoxQualifier string `json:"boxQualifier,omitempty" graphapi:"nullable"`
	// Amount is the box's amount, TextValue its text (an employer's state
	// id), IsChecked its mark.
	Amount    string `json:"amount,omitempty" graphapi:"nullable"`
	TextValue string `json:"textValue,omitempty" graphapi:"nullable"`
	IsChecked bool   `json:"isChecked"`
}
