package finance

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// inventedGroceryReceipt is shaped like a grocery receipt: food taxed
// under one mark (t) and other goods under another (T), a promotion under
// the item it takes money off, a weighed item, a subtotal, the two taxes
// on lines of their own, the total and the card's last digits. Every name
// and number is invented, and they add up: 3.29 + 6.99 + 3.99 - 2.00 +
// 4.02 is 16.29; the 7% sales tax on 6.99 is 0.49 and the 2% food tax on
// 9.30 is 0.19; 16.29 + 0.49 + 0.19 is 16.97.
func inventedGroceryReceipt() *models.FinanceReceipt {
	return &models.FinanceReceipt{
		ReceiptSourceKind: models.ReceiptSourceKindMail, MailID: "mail-receipt-one",
		MerchantName: "Maple Street Market", PurchasedOn: "2026-09-10", CurrencyCode: "usd",
		SubtotalAmount: "16.29", TotalAmount: "16.97", PaymentAccountMask: "4821",
		ReceiptLines: []*models.FinanceReceiptLine{
			{LineNumber: 1, ReceiptLineKind: models.ReceiptLineKindItem, Description: "CHICKEN STOCK", LineAmount: "3.29", TaxClassCode: "t"},
			{LineNumber: 2, ReceiptLineKind: models.ReceiptLineKindItem, Description: "TOOTHPASTE", LineAmount: "6.99", TaxClassCode: "T"},
			{LineNumber: 3, ReceiptLineKind: models.ReceiptLineKindItem, Description: "PEACHES", LineAmount: "3.99", TaxClassCode: "t"},
			{LineNumber: 4, ReceiptLineKind: models.ReceiptLineKindDiscount, Description: "Promotion", LineAmount: "-2.00", TaxClassCode: "t", DiscountedLineNumber: 3},
			{LineNumber: 5, ReceiptLineKind: models.ReceiptLineKindItem, Description: "ONIONS", Quantity: "2.53", QuantityUnit: "lb", UnitPriceAmount: "1.59", LineAmount: "4.02", TaxClassCode: "t"},
			{LineNumber: 6, ReceiptLineKind: models.ReceiptLineKindTax, Description: "Sales Tax", LineAmount: "0.49", TaxClassCode: "T"},
			{LineNumber: 7, ReceiptLineKind: models.ReceiptLineKindTax, Description: "Food Tax", LineAmount: "0.19", TaxClassCode: "t"},
		},
	}
}

// The grocery receipt balances, its currency and descriptions normalized.
func TestCheckReceiptBalancesAGroceryReceipt(t *testing.T) {
	receipt := inventedGroceryReceipt()
	receipt.ReceiptLines[0].Description = "ＣＨＩＣＫＥＮ ＳＴＯＣＫ "
	receiptCheckState, checkDifferenceAmount, err := CheckReceipt(receipt)
	if err != nil {
		t.Fatalf("CheckReceipt: %s", err)
	}
	if receiptCheckState != models.ReceiptCheckStateBalanced || checkDifferenceAmount != "0" {
		t.Fatalf("the receipt balances: %s %s", receiptCheckState, checkDifferenceAmount)
	}
	if receipt.CurrencyCode != "USD" || receipt.ReceiptLines[0].Description != "CHICKEN STOCK" {
		t.Fatalf("the currency and the descriptions are normalized: %s %q", receipt.CurrencyCode, receipt.ReceiptLines[0].Description)
	}
}

// A misread line is reported as unbalanced by how much the lines miss:
// an item against the subtotal, a tax against the total, and with no
// subtotal printed, every line against the total.
func TestCheckReceiptReportsAMisreadLineByItsDifference(t *testing.T) {
	for _, testCase := range []struct {
		description        string
		misread            func(*models.FinanceReceipt)
		expectedDifference string
	}{
		{"an item read ten cents low", func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[1].LineAmount = "6.89" }, "-0.10"},
		{"a tax read a cent high", func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[6].LineAmount = "0.20" }, "0.01"},
		{"a promotion left out", func(receipt *models.FinanceReceipt) {
			receipt.ReceiptLines = append(receipt.ReceiptLines[:3], receipt.ReceiptLines[4:]...)
		}, "2.00"},
		{"no subtotal and an item read high", func(receipt *models.FinanceReceipt) {
			receipt.SubtotalAmount = ""
			receipt.ReceiptLines[0].LineAmount = "3.39"
		}, "0.10"},
	} {
		receipt := inventedGroceryReceipt()
		testCase.misread(receipt)
		receiptCheckState, checkDifferenceAmount, err := CheckReceipt(receipt)
		if err != nil {
			t.Fatalf("%s: %s", testCase.description, err)
		}
		if receiptCheckState != models.ReceiptCheckStateUnbalanced || checkDifferenceAmount != testCase.expectedDifference {
			t.Errorf("%s: %s by %s, not unbalanced by %s", testCase.description, receiptCheckState, checkDifferenceAmount, testCase.expectedDifference)
		}
	}
	receipt := inventedGroceryReceipt()
	receipt.SubtotalAmount = ""
	if receiptCheckState, _, err := CheckReceipt(receipt); err != nil || receiptCheckState != models.ReceiptCheckStateBalanced {
		t.Fatalf("with no subtotal, every line against the total balances: %s %v", receiptCheckState, err)
	}
}

// inventedOrderReceipt is shaped like an online order email: two items, a
// subtotal, then shipping, tax and the total, and no payment line. Every
// name and number is invented: 84.00 + 22.50 is 106.50; 106.50 + 7.95 +
// 8.79 is 123.24.
func inventedOrderReceipt() *models.FinanceReceipt {
	return &models.FinanceReceipt{
		ReceiptSourceKind: models.ReceiptSourceKindMail, MailID: "mail-order-one",
		MerchantName: "Brightwater Outfitters", MerchantReceiptNumber: "BW-55120", PurchasedOn: "2026-09-14", CurrencyCode: "USD",
		SubtotalAmount: "106.50", TotalAmount: "123.24",
		ReceiptLines: []*models.FinanceReceiptLine{
			{LineNumber: 1, ReceiptLineKind: models.ReceiptLineKindItem, Description: "Trail Jacket", LineAmount: "84.00"},
			{LineNumber: 2, ReceiptLineKind: models.ReceiptLineKindItem, Description: "Wool Socks", LineAmount: "22.50"},
			{LineNumber: 3, ReceiptLineKind: models.ReceiptLineKindFee, Description: "Shipping", LineAmount: "7.95"},
			{LineNumber: 4, ReceiptLineKind: models.ReceiptLineKindTax, Description: "Tax", LineAmount: "8.79"},
		},
	}
}

// A receipt may print its fees before the subtotal or after it, and
// balances either way, saying which; a misread line still misses the
// total by its difference.
func TestCheckReceiptTakesFeesOnEitherSideOfTheSubtotal(t *testing.T) {
	order := inventedOrderReceipt()
	receiptCheckState, checkDifferenceAmount, err := CheckReceipt(order)
	if err != nil {
		t.Fatalf("CheckReceipt: %s", err)
	}
	if receiptCheckState != models.ReceiptCheckStateBalanced || checkDifferenceAmount != "0" || !order.IsFeeAfterSubtotal || !IsReceiptFeeAfterSubtotal(order) {
		t.Fatalf("shipping after the subtotal balances, after it: %s %s %v", receiptCheckState, checkDifferenceAmount, order.IsFeeAfterSubtotal)
	}

	// A bag fee among the items: 16.29 + 0.10 is the subtotal 16.39, and
	// 16.39 + 0.49 + 0.19 the total 17.07.
	grocery := inventedGroceryReceipt()
	grocery.ReceiptLines = append(grocery.ReceiptLines[:5:5], append([]*models.FinanceReceiptLine{
		{LineNumber: 6, ReceiptLineKind: models.ReceiptLineKindFee, Description: "Bag Fee", LineAmount: "0.10"},
	}, grocery.ReceiptLines[5:]...)...)
	grocery.ReceiptLines[6].LineNumber, grocery.ReceiptLines[7].LineNumber = 7, 8
	grocery.SubtotalAmount, grocery.TotalAmount = "16.39", "17.07"
	if receiptCheckState, checkDifferenceAmount, err := CheckReceipt(grocery); err != nil || receiptCheckState != models.ReceiptCheckStateBalanced ||
		grocery.IsFeeAfterSubtotal || IsReceiptFeeAfterSubtotal(grocery) {
		t.Fatalf("a bag fee before the subtotal balances, before it: %s %s %v %v", receiptCheckState, checkDifferenceAmount, grocery.IsFeeAfterSubtotal, err)
	}
	if IsReceiptFeeAfterSubtotal(inventedGroceryReceipt()) {
		t.Fatalf("a receipt with no fees has none after its subtotal")
	}

	for _, testCase := range []struct {
		description        string
		misread            func(*models.FinanceReceipt)
		expectedDifference string
	}{
		{"an item read a dollar high", func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[0].LineAmount = "85.00" }, "1.00"},
		{"shipping read low", func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[2].LineAmount = "7.59" }, "-0.36"},
		{"the subtotal misread", func(receipt *models.FinanceReceipt) { receipt.SubtotalAmount = "105.60" }, "0.90"},
	} {
		receipt := inventedOrderReceipt()
		testCase.misread(receipt)
		receiptCheckState, checkDifferenceAmount, err := CheckReceipt(receipt)
		if err != nil {
			t.Fatalf("%s: %s", testCase.description, err)
		}
		if receiptCheckState != models.ReceiptCheckStateUnbalanced || checkDifferenceAmount != testCase.expectedDifference {
			t.Errorf("%s: %s by %s, not unbalanced by %s", testCase.description, receiptCheckState, checkDifferenceAmount, testCase.expectedDifference)
		}
	}
}

// What cannot be a receipt as printed is refused, saying why.
func TestCheckReceiptRefusesWhatCannotBePrinted(t *testing.T) {
	for description, broken := range map[string]func(*models.FinanceReceipt){
		"an unknown kind":                 func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[0].ReceiptLineKind = "coupon" },
		"an item below zero":              func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[0].LineAmount = "-3.29" },
		"a discount above zero":           func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[3].LineAmount = "2.00" },
		"a discount of a tax":             func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[3].DiscountedLineNumber = 6 },
		"an item naming a discounted one": func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[0].DiscountedLineNumber = 2 },
		"three places in dollars":         func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[0].LineAmount = "3.295" },
		"a thousands separator":           func(receipt *models.FinanceReceipt) { receipt.TotalAmount = "1,016.97" },
		"cents in yen": func(receipt *models.FinanceReceipt) {
			receipt.CurrencyCode = "JPY"
			receipt.TotalAmount = "1697.50"
		},
		"no total":          func(receipt *models.FinanceReceipt) { receipt.TotalAmount = "" },
		"no lines":          func(receipt *models.FinanceReceipt) { receipt.ReceiptLines = nil },
		"no currency":       func(receipt *models.FinanceReceipt) { receipt.CurrencyCode = "" },
		"a word for a unit": func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[4].UnitPriceAmount = "cheap" },
	} {
		receipt := inventedGroceryReceipt()
		broken(receipt)
		if _, _, err := CheckReceipt(receipt); !errors.Is(err, ErrReceiptRefused) {
			t.Errorf("%s is refused: %v", description, err)
		}
	}
	receipt := inventedGroceryReceipt()
	receipt.ReceiptLines[4].UnitPriceAmount = "1.599"
	if _, _, err := CheckReceipt(receipt); err != nil {
		t.Fatalf("a unit price may be finer than a cent: %s", err)
	}
}

// inventedCharge is money out on an account on a day.
func inventedCharge(id, financeAccountId, postedOn, amount, description string) *models.FinanceTransaction {
	return &models.FinanceTransaction{ID: id, FinanceAccountID: financeAccountId, PostedOn: postedOn, Amount: amount, CurrencyCode: "USD", Description: description}
}

// An exact amount that is the only candidate is matched without asking,
// with a confidence that grows with the account's digits and the
// merchant's name; what is outside the window, money in, another currency
// or a mirrored copy is no candidate.
func TestProposeReceiptMatchesTakesTheOneExactAmount(t *testing.T) {
	receipt := inventedGroceryReceipt()
	receipt.CurrencyCode = "USD"
	masks := map[string]string{"account-card": "4821", "account-other": "1111"}
	inOtherCurrency := inventedCharge("charge-euro", "account-card", "2026-09-10", "-16.97", "MAPLE STREET MARKET")
	inOtherCurrency.CurrencyCode = "EUR"
	mirrored := inventedCharge("charge-copy", "account-card", "2026-09-10", "-16.97", "MAPLE STREET MARKET")
	mirrored.DuplicateOfTransactionID = "charge-exact"
	proposals := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{
		inventedCharge("charge-near", "account-other", "2026-09-11", "-15.00", "CORNER CAFE"),
		inventedCharge("charge-exact", "account-card", "2026-09-12", "-16.97", "MAPLE STREET MKT #12"),
		inventedCharge("charge-late", "account-card", "2026-09-18", "-16.97", "MAPLE STREET MARKET"),
		inventedCharge("charge-refund", "account-card", "2026-09-10", "16.97", "MAPLE STREET MARKET"),
		inOtherCurrency, mirrored,
	}, masks, nil)
	if len(proposals) != 2 || proposals[0].FinanceTransactionID != "charge-exact" || proposals[1].FinanceTransactionID != "charge-near" {
		t.Fatalf("the exact charge first, the near one after, and nothing else: %+v", proposals)
	}
	exact := proposals[0]
	if !exact.IsAutomatic || exact.MatchConfidence != "1.00" || !exact.IsSameAccount || !exact.IsMerchantNameShared || exact.MatchedAmount != "16.9700" || exact.DayDistanceCount != 2 {
		t.Fatalf("the one exact amount is automatic, sure of it with the card's digits and the merchant's name: %+v", exact)
	}
	if proposals[1].IsAutomatic || proposals[1].MatchedAmount != "15.0000" {
		t.Fatalf("another amount is only a candidate, explaining what it charged: %+v", proposals[1])
	}
	receipt.PurchasedOn = ""
	if none := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{inventedCharge("charge-exact", "account-card", "2026-09-12", "-16.97", "")}, masks, nil); len(none) != 0 {
		t.Fatalf("a receipt with no day has no candidates: %+v", none)
	}
}

// Two charges of the exact amount are left for the person, unless the
// receipt prints the digits of the card exactly one of them is on.
func TestProposeReceiptMatchesNeverGuessesBetweenEqualAmounts(t *testing.T) {
	receipt := inventedGroceryReceipt()
	receipt.CurrencyCode = "USD"
	twins := []*models.FinanceTransaction{
		inventedCharge("charge-card", "account-card", "2026-09-10", "-16.97", "MAPLE STREET MARKET"),
		inventedCharge("charge-other", "account-other", "2026-09-10", "-16.97", "MAPLE STREET MARKET"),
	}
	masks := map[string]string{"account-card": "4821", "account-other": "1111"}
	proposals := ProposeReceiptMatches(receipt, twins, masks, nil)
	if len(proposals) != 2 || !proposals[0].IsAutomatic || proposals[0].FinanceTransactionID != "charge-card" || proposals[1].IsAutomatic {
		t.Fatalf("the card the receipt names is taken: %+v", proposals)
	}
	receipt.PaymentAccountMask = ""
	for _, proposal := range ProposeReceiptMatches(receipt, twins, masks, nil) {
		if proposal.IsAutomatic {
			t.Fatalf("without the card's digits, neither is guessed: %+v", proposal)
		}
	}
}

// One order charged in two shipments has no exact amount: both charges
// are candidates, each explaining what it charged, and neither is matched
// without the person.
func TestProposeReceiptMatchesLeavesASplitShipmentToThePerson(t *testing.T) {
	receipt := inventedGroceryReceipt()
	receipt.CurrencyCode, receipt.PaymentAccountMask = "USD", ""
	proposals := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{
		inventedCharge("shipment-one", "account-card", "2026-09-11", "-10.00", "MAPLE STREET MARKET"),
		inventedCharge("shipment-two", "account-card", "2026-09-14", "-6.97", "MAPLE STREET MARKET"),
	}, nil, nil)
	if len(proposals) != 2 {
		t.Fatalf("both shipments are candidates: %+v", proposals)
	}
	for _, proposal := range proposals {
		if proposal.IsAutomatic || proposal.IsExactAmount {
			t.Fatalf("neither is matched without the person: %+v", proposal)
		}
	}
	if proposals[0].MatchedAmount != "10.0000" || proposals[1].MatchedAmount != "6.9700" {
		t.Fatalf("each explains what it charged: %+v", proposals)
	}
}

// The one exact amount is matched without asking only with something else
// that agrees: the card's digits or a word of the merchant. An amount
// alone stays a candidate for the person.
func TestProposeReceiptMatchesWantsMoreThanTheAmount(t *testing.T) {
	receipt := inventedGroceryReceipt()
	receipt.CurrencyCode, receipt.PaymentAccountMask = "USD", ""
	masks := map[string]string{"account-other": "1111"}
	alone := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{
		inventedCharge("charge-stranger", "account-other", "2026-09-10", "-16.97", "HILLTOP HARDWARE"),
	}, masks, nil)
	if len(alone) != 1 || !alone[0].IsExactAmount || alone[0].IsAutomatic {
		t.Fatalf("the amount alone is only a candidate: %+v", alone)
	}
	named := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{
		inventedCharge("charge-named", "account-other", "2026-09-10", "-16.97", "MAPLE STREET MKT"),
	}, masks, nil)
	if len(named) != 1 || !named[0].IsAutomatic || named[0].MatchConfidence != "0.85" {
		t.Fatalf("the amount and a word of the merchant are matched: %+v", named)
	}
	receipt.PaymentAccountMask = "1111"
	onTheCard := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{
		inventedCharge("charge-card", "account-other", "2026-09-10", "-16.97", "SQ *HILLTOP"),
	}, masks, nil)
	if len(onTheCard) != 1 || !onTheCard[0].IsAutomatic || onTheCard[0].MatchConfidence != "0.85" {
		t.Fatalf("the amount on the card the receipt prints is matched: %+v", onTheCard)
	}
}

// A charge other receipts already explain in full is no candidate; one
// they explain in part is a candidate for what is left, and never matched
// without the person, so a second record of one purchase (its order email
// and its shipping email, say) does not explain the charge twice.
func TestProposeReceiptMatchesLeavesOutWhatIsExplained(t *testing.T) {
	receipt := inventedGroceryReceipt()
	receipt.CurrencyCode = "USD"
	masks := map[string]string{"account-card": "4821"}
	charge := inventedCharge("charge-exact", "account-card", "2026-09-10", "-16.97", "MAPLE STREET MARKET")
	explained := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{charge}, masks, map[string]*ReceiptMatchCoverage{
		"charge-exact": {MatchedAmount: "16.9700", OtherMatchedAmount: "16.9700", HasOtherReceipt: true},
	})
	if len(explained) != 0 {
		t.Fatalf("a charge explained in full is left out: %+v", explained)
	}
	partly := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{charge}, masks, map[string]*ReceiptMatchCoverage{
		"charge-exact": {MatchedAmount: "10.0000", OtherMatchedAmount: "10.0000", HasOtherReceipt: true},
	})
	if len(partly) != 1 || partly[0].IsAutomatic || partly[0].MatchedAmount != "6.9700" {
		t.Fatalf("a charge explained in part is a candidate for the rest, never automatic: %+v", partly)
	}
}

// A receipt matched in part to other charges offers only what is left of
// its total, never automatically, and one they explain in full offers
// nothing, so a receipt never explains more than it printed.
func TestProposeReceiptMatchesOffersWhatIsLeftOfTheReceipt(t *testing.T) {
	receipt := inventedGroceryReceipt()
	receipt.CurrencyCode = "USD"
	masks := map[string]string{"account-card": "4821"}
	charge := inventedCharge("charge-exact", "account-card", "2026-09-10", "-16.97", "MAPLE STREET MARKET")
	receipt.ReceiptMatches = []*models.FinanceReceiptMatch{{FinanceTransactionID: "shipment-one", MatchedAmount: "10.0000"}}
	partly := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{charge}, masks, nil)
	if len(partly) != 1 || partly[0].IsAutomatic || partly[0].MatchedAmount != "6.9700" {
		t.Fatalf("what is left of the receipt is offered, never automatically: %+v", partly)
	}
	receipt.ReceiptMatches = append(receipt.ReceiptMatches, &models.FinanceReceiptMatch{FinanceTransactionID: "shipment-two", MatchedAmount: "6.9700"})
	if spent := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{charge}, masks, nil); len(spent) != 0 {
		t.Fatalf("a receipt explained in full offers nothing: %+v", spent)
	}
	receipt.ReceiptMatches = []*models.FinanceReceiptMatch{{FinanceTransactionID: "charge-exact", MatchedAmount: "16.9700"}}
	if again := ProposeReceiptMatches(receipt, []*models.FinanceTransaction{charge}, masks, nil); len(again) != 1 || again[0].MatchedAmount != "16.9700" {
		t.Fatalf("its own match to the candidate does not count against it: %+v", again)
	}
}

// The difference is said with its sign in words.
func TestReceiptCheckSummarySaysMoreOrLess(t *testing.T) {
	if summary := ReceiptCheckSummary(models.ReceiptCheckStateUnbalanced, "-2.20", "USD"); summary != "unbalanced: the lines come to 2.20 USD less than printed" {
		t.Errorf("less: %q", summary)
	}
	if summary := ReceiptCheckSummary(models.ReceiptCheckStateUnbalanced, "0.6300", "USD"); summary != "unbalanced: the lines come to 0.63 USD more than printed" {
		t.Errorf("more: %q", summary)
	}
}
