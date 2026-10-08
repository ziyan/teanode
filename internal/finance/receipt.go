package finance

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode"

	normalization "golang.org/x/text/unicode/norm"

	"github.com/ziyan/teanode/internal/models"
)

// Receipts: one merchant's record of one purchase, read line by line off
// an email, a photo or a PDF, as printed. CheckReceipt says whether the
// lines add up to the printed totals, to the cent, which is how a misread
// line is caught; ProposeReceiptMatches finds the charges a receipt could
// explain, and says which one, if any, is sure enough to match without
// asking.

// ErrReceiptRefused begins every refusal of a receipt.
var ErrReceiptRefused = errors.New("the receipt was not recorded")

func refuseReceipt(format string, arguments ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrReceiptRefused}, arguments...)...)
}

// parseReceiptAmount reads one of a receipt's amounts, refusing more
// places than its currency has, which is an amount misread.
func parseReceiptAmount(what, text, currencyCode string) (*big.Rat, error) {
	text = strings.ReplaceAll(strings.TrimSpace(normalization.NFKC.String(text)), "−", "-")
	amountValue, err := parseDecimal(text)
	if err != nil {
		return nil, refuseReceipt("%s %q is not a decimal amount written without separators", what, text)
	}
	if _, fraction, hasPoint := strings.Cut(text, "."); hasPoint && len(strings.TrimRight(fraction, "0")) > CurrencyMinorUnits(currencyCode) {
		return nil, refuseReceipt("%s %s has more places than %s has", what, text, currencyCode)
	}
	return amountValue, nil
}

// formatReceiptAmount writes an amount with as many places as its
// currency has.
func formatReceiptAmount(amountValue *big.Rat, currencyCode string) string {
	return amountValue.FloatString(CurrencyMinorUnits(currencyCode))
}

// FormatReceiptAmount writes a stored amount with as many places as its
// currency has: 0.1000 is 0.10 in dollars.
func FormatReceiptAmount(amount, currencyCode string) string {
	amountValue, err := parseDecimal(amount)
	if err != nil {
		return amount
	}
	return formatReceiptAmount(amountValue, currencyCode)
}

// CheckReceipt checks a receipt as read and normalizes it in place: the
// merchant, the descriptions and the currency code, NFKC and trimmed, as
// transaction rows are, so full-width letters and digits and half-width
// katakana on a receipt read the same as on a statement. It
// refuses a receipt with no total or no lines, a line of a kind it does
// not know, an item below zero, a discount above zero or of a line that is
// not an item, and an amount with more places than the currency has. A
// unit price may have more, since a weighed item is priced finer than a
// cent. Then it answers whether the lines add up and by how much they
// miss: every line against the total, then, when a subtotal is printed,
// the items and discounts with the fees against it, or the items and
// discounts alone, since a receipt prints its fees (shipping, delivery,
// a bag) before the subtotal or after it. The difference is what the
// lines come to less what is printed, against the total when that
// misses, and against the subtotal when only the subtotal does (the
// nearer of with the fees and without them), "0" when balanced. It sets IsFeeAfterSubtotal, as
// IsReceiptFeeAfterSubtotal says.
func CheckReceipt(receipt *models.FinanceReceipt) (models.ReceiptCheckState, string, error) {
	if receipt == nil {
		return "", "", refuseReceipt("there is no receipt")
	}
	receipt.CurrencyCode = strings.ToUpper(strings.TrimSpace(receipt.CurrencyCode))
	if len(receipt.CurrencyCode) != 3 {
		return "", "", refuseReceipt("%q is not a currency code like USD", receipt.CurrencyCode)
	}
	receipt.MerchantName = strings.TrimSpace(normalization.NFKC.String(receipt.MerchantName))
	receipt.MerchantReceiptNumber = strings.TrimSpace(normalization.NFKC.String(receipt.MerchantReceiptNumber))
	currencyCode := receipt.CurrencyCode
	if strings.TrimSpace(receipt.TotalAmount) == "" {
		return "", "", refuseReceipt("give the total the receipt prints")
	}
	totalAmount, err := parseReceiptAmount("the total", receipt.TotalAmount, currencyCode)
	if err != nil {
		return "", "", err
	}
	var subtotalAmount *big.Rat
	if strings.TrimSpace(receipt.SubtotalAmount) != "" {
		if subtotalAmount, err = parseReceiptAmount("the subtotal", receipt.SubtotalAmount, currencyCode); err != nil {
			return "", "", err
		}
	}
	if len(receipt.ReceiptLines) == 0 {
		return "", "", refuseReceipt("give the receipt's lines, one per printed line")
	}
	kindByLineNumber := map[int]models.ReceiptLineKind{}
	for _, line := range receipt.ReceiptLines {
		kindByLineNumber[line.LineNumber] = line.ReceiptLineKind
	}
	linesAmount, itemsAmount, feesAmount := new(big.Rat), new(big.Rat), new(big.Rat)
	for _, line := range receipt.ReceiptLines {
		line.Description = strings.TrimSpace(normalization.NFKC.String(line.Description))
		line.TaxClassCode = strings.TrimSpace(normalization.NFKC.String(line.TaxClassCode))
		line.QuantityUnit = strings.TrimSpace(normalization.NFKC.String(line.QuantityUnit))
		label := fmt.Sprintf("line %d", line.LineNumber)
		if line.Description != "" {
			label += fmt.Sprintf(" (%s)", line.Description)
		}
		if !line.ReceiptLineKind.IsValid() {
			return "", "", refuseReceipt("%s is of kind %q; a line is an item, a discount, a tax, a fee or a tip", label, line.ReceiptLineKind)
		}
		lineAmount, err := parseReceiptAmount(label+"'s amount", line.LineAmount, currencyCode)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(line.UnitPriceAmount) != "" {
			if _, err := parseDecimal(strings.TrimSpace(normalization.NFKC.String(line.UnitPriceAmount))); err != nil {
				return "", "", refuseReceipt("%s's unit price %q is not a decimal amount", label, line.UnitPriceAmount)
			}
		}
		switch line.ReceiptLineKind {
		case models.ReceiptLineKindItem:
			if lineAmount.Sign() < 0 {
				return "", "", refuseReceipt("%s is an item below zero; a price taken off is a discount line of its own", label)
			}
		case models.ReceiptLineKindDiscount:
			if lineAmount.Sign() > 0 {
				return "", "", refuseReceipt("%s is a discount above zero; a discount is negative, as printed", label)
			}
		}
		if line.DiscountedLineNumber != 0 {
			if line.ReceiptLineKind != models.ReceiptLineKindDiscount {
				return "", "", refuseReceipt("%s names a discounted line but is not a discount", label)
			}
			if kindByLineNumber[line.DiscountedLineNumber] != models.ReceiptLineKindItem {
				return "", "", refuseReceipt("%s discounts line %d, which is not an item", label, line.DiscountedLineNumber)
			}
		}
		linesAmount.Add(linesAmount, lineAmount)
		switch line.ReceiptLineKind {
		case models.ReceiptLineKindItem, models.ReceiptLineKindDiscount:
			itemsAmount.Add(itemsAmount, lineAmount)
		case models.ReceiptLineKindFee:
			feesAmount.Add(feesAmount, lineAmount)
		}
	}
	receipt.IsFeeAfterSubtotal = isFeeAfterSubtotal(itemsAmount, feesAmount, subtotalAmount)
	if difference := new(big.Rat).Sub(linesAmount, totalAmount); difference.Sign() != 0 {
		return models.ReceiptCheckStateUnbalanced, formatReceiptAmount(difference, currencyCode), nil
	}
	if subtotalAmount != nil {
		withFeesDifference := new(big.Rat).Sub(new(big.Rat).Add(itemsAmount, feesAmount), subtotalAmount)
		withoutFeesDifference := new(big.Rat).Sub(itemsAmount, subtotalAmount)
		if withFeesDifference.Sign() != 0 && withoutFeesDifference.Sign() != 0 {
			difference := withFeesDifference
			if new(big.Rat).Abs(withoutFeesDifference).Cmp(new(big.Rat).Abs(withFeesDifference)) < 0 {
				difference = withoutFeesDifference
			}
			return models.ReceiptCheckStateUnbalanced, formatReceiptAmount(difference, currencyCode), nil
		}
	}
	return models.ReceiptCheckStateBalanced, "0", nil
}

// isFeeAfterSubtotal says the fees are printed after the subtotal: there
// is one, the fees come to something, and the items and discounts alone
// come to it. Fees in part before and in part after are not looked for;
// which side each sits on would need saying line by line, and a receipt
// that does it would show as unbalanced rather than be guessed at.
func isFeeAfterSubtotal(itemsAmount, feesAmount, subtotalAmount *big.Rat) bool {
	return subtotalAmount != nil && feesAmount.Sign() != 0 && itemsAmount.Cmp(subtotalAmount) == 0
}

// IsReceiptFeeAfterSubtotal says whether a stored receipt prints its fees
// after its subtotal, with its taxes and tips, rather than among the
// items it adds up; it is worked out from the lines and the subtotal each
// time a receipt is read, never stored. An amount that cannot be read
// counts as nothing, since a stored receipt was checked when written.
func IsReceiptFeeAfterSubtotal(receipt *models.FinanceReceipt) bool {
	if receipt == nil || strings.TrimSpace(receipt.SubtotalAmount) == "" {
		return false
	}
	subtotalAmount, err := parseDecimal(strings.TrimSpace(receipt.SubtotalAmount))
	if err != nil {
		return false
	}
	itemsAmount, feesAmount := new(big.Rat), new(big.Rat)
	for _, line := range receipt.ReceiptLines {
		if line == nil {
			continue
		}
		lineAmount, err := parseDecimal(strings.TrimSpace(line.LineAmount))
		if err != nil {
			continue
		}
		switch line.ReceiptLineKind {
		case models.ReceiptLineKindItem, models.ReceiptLineKindDiscount:
			itemsAmount.Add(itemsAmount, lineAmount)
		case models.ReceiptLineKindFee:
			feesAmount.Add(feesAmount, lineAmount)
		}
	}
	return isFeeAfterSubtotal(itemsAmount, feesAmount, subtotalAmount)
}

// ReceiptCheckSummary says in a sentence what CheckReceipt found.
func ReceiptCheckSummary(receiptCheckState models.ReceiptCheckState, checkDifferenceAmount, currencyCode string) string {
	if receiptCheckState == models.ReceiptCheckStateBalanced {
		return "balanced: the lines add up to the printed totals"
	}
	return "unbalanced: " + ReceiptCheckDifferenceWords(checkDifferenceAmount, currencyCode)
}

// ReceiptCheckDifferenceWords says by how much a receipt's lines miss its
// printed totals, the sign said in words: "the lines come to 2.20 USD
// less than printed" for -2.20.
func ReceiptCheckDifferenceWords(checkDifferenceAmount, currencyCode string) string {
	differenceAmount, err := parseDecimal(strings.TrimSpace(checkDifferenceAmount))
	if err != nil {
		return fmt.Sprintf("the lines come to %s %s more than printed", checkDifferenceAmount, currencyCode)
	}
	direction := "more"
	if differenceAmount.Sign() < 0 {
		direction = "less"
	}
	return fmt.Sprintf("the lines come to %s %s %s than printed", formatReceiptAmount(new(big.Rat).Abs(differenceAmount), currencyCode), currencyCode, direction)
}

// The window a charge may post in around the day of purchase: a card
// authorization can carry an earlier day than the receipt's, and an order
// is often charged when it ships, days later.
const (
	ReceiptMatchDaysBefore = 3
	ReceiptMatchDaysAfter  = 7
)

// receiptMatchConfidence is what the matcher's confidence starts from for
// an exact amount that is the only candidate, and what each sign that
// agrees adds: the same card's last digits, a word of the merchant.
var (
	receiptMatchBaseConfidence    = big.NewRat(70, 100)
	receiptMatchSignConfidence    = big.NewRat(15, 100)
	receiptMatchHighestConfidence = big.NewRat(1, 1)
)

// ReceiptMatchCandidate is a charge a receipt could explain: how much of
// it, what agrees, and whether the matcher takes it without asking.
type ReceiptMatchCandidate struct {
	FinanceTransactionID string `json:"financeTransactionId"`

	// MatchedAmount is what the receipt would explain of the charge: the
	// charge's whole amount, or the receipt's total when that is less.
	MatchedAmount string `json:"matchedAmount"`

	// IsExactAmount says the charge is the receipt's total to the cent,
	// IsSameAccount that it is on the card or account whose last digits
	// the receipt prints, and IsMerchantNameShared that its description or
	// merchant shares a word with the receipt's merchant.
	IsExactAmount        bool `json:"isExactAmount"`
	IsSameAccount        bool `json:"isSameAccount"`
	IsMerchantNameShared bool `json:"isMerchantNameShared"`

	// DayDistanceCount is how many days from the day of purchase it posted.
	DayDistanceCount int `json:"dayDistanceCount"`

	// IsAutomatic says the matcher matches it without asking, with
	// MatchConfidence: an exact amount that is the only candidate (the
	// only one on the same account when the receipt prints its digits),
	// on the card the receipt prints or with a word of its merchant, and
	// explained by no other receipt yet.
	IsAutomatic     bool   `json:"isAutomatic"`
	MatchConfidence string `json:"matchConfidence,omitempty"`
}

// ReceiptMatchCoverage is what receipts already explain of one charge:
// MatchedAmount from every receipt, OtherMatchedAmount from receipts other
// than the one being matched, and HasOtherReceipt whether any other
// receipt is matched to it at all.
type ReceiptMatchCoverage struct {
	MatchedAmount      string
	OtherMatchedAmount string
	HasOtherReceipt    bool
}

// genericMerchantWords say nothing about which merchant it was.
var genericMerchantWords = map[string]bool{
	"the": true, "and": true, "inc": true, "llc": true, "ltd": true, "corp": true, "com": true, "www": true, "store": true,
	"shop": true, "online": true, "payment": true, "payments": true, "purchase": true, "pos": true, "debit": true, "card": true,
}

// merchantWords are the words of a merchant's name or a description that
// could tell a merchant apart: NFKC, lower case, letters and digits, three
// or more characters, not a word every merchant uses.
func merchantWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(normalization.NFKC.String(text)), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) {
		if len([]rune(word)) >= 3 && !genericMerchantWords[word] && strings.Trim(word, "0123456789") != "" {
			words[word] = true
		}
	}
	return words
}

// ProposeReceiptMatches is the charges a receipt could explain among the
// candidates, the likeliest first: money out in the receipt's currency,
// posted from ReceiptMatchDaysBefore days before the day of purchase to
// ReceiptMatchDaysAfter after, not a mirrored copy, and not already
// explained in full by the receipts matched to it
// (coverageByFinanceTransactionId says what they explain of each). The
// exact amount comes first, then the account whose last digits the
// receipt prints (accountMaskByFinanceAccountId says each candidate's),
// then a merchant that shares a word, then the nearest day. A candidate's
// matched amount is what no other receipt explains of it yet, or what the
// receipt's matches to its other charges leave of its total when that is
// less; a receipt they explain in full has no candidates.
//
// At most one is automatic: the one exact amount, or the one exact amount
// on the receipt's account when it prints digits; and only when it is on
// that account or shares a word of the merchant, since an amount alone is
// a coincidence often enough, and only when no other receipt is matched
// to it, since an order email and its shipping email, or a photo and the
// email of the same purchase, would otherwise both explain one charge.
// Anything else is left for the person, never guessed. A receipt with no
// day of purchase has no candidates.
func ProposeReceiptMatches(receipt *models.FinanceReceipt, candidates []*models.FinanceTransaction, accountMaskByFinanceAccountId map[string]string, coverageByFinanceTransactionId map[string]*ReceiptMatchCoverage) []ReceiptMatchCandidate {
	proposals := []ReceiptMatchCandidate{}
	if receipt == nil || receipt.PurchasedOn == "" {
		return proposals
	}
	purchasedOn, err := time.Parse(time.DateOnly, receipt.PurchasedOn)
	if err != nil {
		return proposals
	}
	totalAmount, err := parseDecimal(receipt.TotalAmount)
	if err != nil || totalAmount.Sign() <= 0 {
		return proposals
	}
	receiptWords := merchantWords(receipt.MerchantName)
	paymentAccountMask := strings.TrimSpace(receipt.PaymentAccountMask)
	hasOtherReceipt := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == nil || candidate.DuplicateOfTransactionID != "" || !strings.EqualFold(candidate.CurrencyCode, receipt.CurrencyCode) {
			continue
		}
		amountValue, err := parseDecimal(candidate.Amount)
		if err != nil || amountValue.Sign() >= 0 {
			continue
		}
		postedOn, err := time.Parse(time.DateOnly, candidate.PostedOn)
		if err != nil {
			continue
		}
		dayDistance := int(postedOn.Sub(purchasedOn).Hours() / 24)
		if dayDistance < -ReceiptMatchDaysBefore || dayDistance > ReceiptMatchDaysAfter {
			continue
		}
		chargedAmount := new(big.Rat).Neg(amountValue)
		unexplainedAmount := chargedAmount
		if coverage := coverageByFinanceTransactionId[candidate.ID]; coverage != nil {
			if explainedAmount, err := parseDecimal(coverage.MatchedAmount); err == nil && explainedAmount.Cmp(chargedAmount) >= 0 {
				continue
			}
			if otherAmount, err := parseDecimal(coverage.OtherMatchedAmount); err == nil {
				unexplainedAmount = new(big.Rat).Sub(chargedAmount, otherAmount)
			}
			hasOtherReceipt[candidate.ID] = coverage.HasOtherReceipt
		}
		if unexplainedAmount.Sign() <= 0 {
			continue
		}
		receiptLeftAmount := new(big.Rat).Set(totalAmount)
		for _, match := range receipt.ReceiptMatches {
			if match == nil || match.FinanceTransactionID == candidate.ID {
				continue
			}
			if elsewhereAmount, err := parseDecimal(match.MatchedAmount); err == nil {
				receiptLeftAmount.Sub(receiptLeftAmount, elsewhereAmount)
			}
		}
		if receiptLeftAmount.Sign() <= 0 {
			continue
		}
		matchedAmount := unexplainedAmount
		if receiptLeftAmount.Cmp(unexplainedAmount) < 0 {
			matchedAmount = receiptLeftAmount
		}
		accountMask := accountMaskByFinanceAccountId[candidate.FinanceAccountID]
		isMerchantNameShared := false
		for word := range merchantWords(candidate.Description + " " + candidate.MerchantName) {
			isMerchantNameShared = isMerchantNameShared || receiptWords[word]
		}
		if dayDistance < 0 {
			dayDistance = -dayDistance
		}
		proposals = append(proposals, ReceiptMatchCandidate{
			FinanceTransactionID: candidate.ID, MatchedAmount: FormatAmount(matchedAmount),
			IsExactAmount:        chargedAmount.Cmp(totalAmount) == 0,
			IsSameAccount:        isMaskMatch(accountMask, paymentAccountMask),
			IsMerchantNameShared: isMerchantNameShared, DayDistanceCount: dayDistance,
		})
	}
	sort.SliceStable(proposals, func(left, right int) bool {
		for _, signs := range [][2]bool{
			{proposals[left].IsExactAmount, proposals[right].IsExactAmount},
			{proposals[left].IsSameAccount, proposals[right].IsSameAccount},
			{proposals[left].IsMerchantNameShared, proposals[right].IsMerchantNameShared},
		} {
			if signs[0] != signs[1] {
				return signs[0]
			}
		}
		if proposals[left].DayDistanceCount != proposals[right].DayDistanceCount {
			return proposals[left].DayDistanceCount < proposals[right].DayDistanceCount
		}
		return proposals[left].FinanceTransactionID < proposals[right].FinanceTransactionID
	})
	var exactIndexes, sameAccountExactIndexes []int
	for index, proposal := range proposals {
		if !proposal.IsExactAmount {
			continue
		}
		exactIndexes = append(exactIndexes, index)
		if proposal.IsSameAccount {
			sameAccountExactIndexes = append(sameAccountExactIndexes, index)
		}
	}
	automaticIndex := -1
	switch {
	case len(exactIndexes) == 1:
		automaticIndex = exactIndexes[0]
	case paymentAccountMask != "" && len(sameAccountExactIndexes) == 1:
		automaticIndex = sameAccountExactIndexes[0]
	}
	if automaticIndex >= 0 {
		chosen := proposals[automaticIndex]
		// Automatic only for the whole of the charge and of the receipt:
		// one explained in part already is the person's to judge.
		if (!chosen.IsSameAccount && !chosen.IsMerchantNameShared) || hasOtherReceipt[chosen.FinanceTransactionID] ||
			chosen.MatchedAmount != FormatAmount(totalAmount) {
			automaticIndex = -1
		}
	}
	if automaticIndex >= 0 {
		confidence := new(big.Rat).Set(receiptMatchBaseConfidence)
		if proposals[automaticIndex].IsSameAccount {
			confidence.Add(confidence, receiptMatchSignConfidence)
		}
		if proposals[automaticIndex].IsMerchantNameShared {
			confidence.Add(confidence, receiptMatchSignConfidence)
		}
		if confidence.Cmp(receiptMatchHighestConfidence) > 0 {
			confidence = receiptMatchHighestConfidence
		}
		proposals[automaticIndex].IsAutomatic = true
		proposals[automaticIndex].MatchConfidence = confidence.FloatString(2)
	}
	return proposals
}
