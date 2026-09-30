package finance

import (
	"strconv"
	"strings"
)

// The default spending categories a person starts with. The provider
// category mapping below answers with these names; a person who renamed
// or deleted one simply gets no mapping for it.
const (
	SpendingCategoryIncome            = "income"
	SpendingCategoryHousing           = "housing"
	SpendingCategoryUtilities         = "utilities"
	SpendingCategoryGroceries         = "groceries"
	SpendingCategoryDining            = "dining"
	SpendingCategoryTransport         = "transport"
	SpendingCategoryTravel            = "travel"
	SpendingCategoryShopping          = "shopping"
	SpendingCategoryHealth            = "health"
	SpendingCategoryEntertainment     = "entertainment"
	SpendingCategorySubscriptions     = "subscriptions"
	SpendingCategoryFees              = "fees"
	SpendingCategoryGiftsAndDonations = "gifts and donations"
	SpendingCategoryOther             = "other"
)

// DefaultSpendingCategoryNames are the default spending categories, in the
// order a person sees them.
var DefaultSpendingCategoryNames = []string{
	SpendingCategoryIncome,
	SpendingCategoryHousing,
	SpendingCategoryUtilities,
	SpendingCategoryGroceries,
	SpendingCategoryDining,
	SpendingCategoryTransport,
	SpendingCategoryTravel,
	SpendingCategoryShopping,
	SpendingCategoryHealth,
	SpendingCategoryEntertainment,
	SpendingCategorySubscriptions,
	SpendingCategoryFees,
	SpendingCategoryGiftsAndDonations,
	SpendingCategoryOther,
}

// Plaid's personal finance categories whose every detailed category is
// money moving between a person's own accounts, rather than spending or
// income, apart from the few singled out below. Money in from a loan is
// here too: it is borrowed, not earned.
var plaidTransferPrimaries = map[string]bool{
	"TRANSFER_IN":        true,
	"TRANSFER_OUT":       true,
	"LOAN_DISBURSEMENTS": true,
}

// Plaid's detailed categories that are transfers although the rest of
// their primary is not. A card payment is the person paying their own
// card: counting it as spending would count every purchase on the card
// twice. The other loan payments are not here, since only a slice of each
// is principal moving to the person's own liability; the interest is
// money gone, and the payment is spending unless the person says
// otherwise.
var plaidTransferDetailed = map[string]bool{
	"LOAN_PAYMENTS_CREDIT_CARD_PAYMENT": true,
}

// Plaid's detailed categories of a transfer primary that are not
// transfers and that nothing maps: a deposit of cash or a check may as
// well be a paycheck as money from the person's own account elsewhere, so
// it is left to a spending rule or the categorize model.
var plaidUnmappedDetailed = map[string]bool{
	"TRANSFER_IN_DEPOSIT": true,
}

// Plaid's detailed categories that answer differently from the rest of
// their primary. Cash taken out at a machine leaves the person's accounts
// for good, as far as a budget can see, so it is spending in other rather
// than a transfer.
var plaidSpendingCategoryByDetailed = map[string]string{
	"FOOD_AND_DRINK_GROCERIES":            SpendingCategoryGroceries,
	"RENT_AND_UTILITIES_RENT":             SpendingCategoryHousing,
	"GOVERNMENT_AND_NON_PROFIT_DONATIONS": SpendingCategoryGiftsAndDonations,
	"LOAN_PAYMENTS_MORTGAGE_PAYMENT":      SpendingCategoryHousing,
	"TRANSFER_OUT_WITHDRAWAL":             SpendingCategoryOther,
}

var plaidSpendingCategoryByPrimary = map[string]string{
	"LOAN_PAYMENTS":       SpendingCategoryOther,
	"INCOME":              SpendingCategoryIncome,
	"BANK_FEES":           SpendingCategoryFees,
	"ENTERTAINMENT":       SpendingCategoryEntertainment,
	"FOOD_AND_DRINK":      SpendingCategoryDining,
	"GENERAL_MERCHANDISE": SpendingCategoryShopping,
	"HOME_IMPROVEMENT":    SpendingCategoryHousing,
	"MEDICAL":             SpendingCategoryHealth,
	"PERSONAL_CARE":       SpendingCategoryHealth,
	"GENERAL_SERVICES":    SpendingCategoryOther,
	// Taxes and government fees are most of this primary and are not
	// gifts; donations, its one detailed category that is, is mapped above.
	"GOVERNMENT_AND_NON_PROFIT": SpendingCategoryOther,
	"TRANSPORTATION":            SpendingCategoryTransport,
	"TRAVEL":                    SpendingCategoryTravel,
	"RENT_AND_UTILITIES":        SpendingCategoryUtilities,
}

// merchantCodeRange is a span of card network merchant category codes,
// both ends included, and the spending category they mean.
type merchantCodeRange struct {
	firstCode        int
	lastCode         int
	spendingCategory string
}

// The merchant category codes SimpleFIN passes through, for the categories
// they settle unambiguously. Checked in order, so a single code listed
// before a range it falls in wins.
var merchantCodeRanges = []merchantCodeRange{
	{5411, 5411, SpendingCategoryGroceries}, // grocery stores, supermarkets
	{5422, 5422, SpendingCategoryGroceries}, // meat provisioners
	{5441, 5441, SpendingCategoryGroceries}, // candy and confectionery
	{5451, 5451, SpendingCategoryGroceries}, // dairy
	{5462, 5462, SpendingCategoryGroceries}, // bakeries
	{5499, 5499, SpendingCategoryGroceries}, // convenience and specialty food
	{5812, 5814, SpendingCategoryDining},    // restaurants, bars, fast food
	{3000, 3999, SpendingCategoryTravel},    // airlines, car rental, lodging by brand
	{4511, 4511, SpendingCategoryTravel},    // airlines
	{7011, 7011, SpendingCategoryTravel},    // lodging
	{4722, 4722, SpendingCategoryTravel},    // travel agencies
	{4111, 4111, SpendingCategoryTransport}, // commuter transport
	{4121, 4121, SpendingCategoryTransport}, // taxis and ride hailing
	{4131, 4131, SpendingCategoryTransport}, // bus lines
	{4789, 4789, SpendingCategoryTransport}, // tolls, other transportation
	{5541, 5542, SpendingCategoryTransport}, // fuel
	{4900, 4900, SpendingCategoryUtilities}, // electric, gas, water
	{5912, 5912, SpendingCategoryHealth},    // pharmacies
	{8011, 8099, SpendingCategoryHealth},    // doctors, dentists, hospitals
	{4899, 4899, SpendingCategorySubscriptions},
	{5815, 5818, SpendingCategorySubscriptions}, // digital goods and media
	{7832, 7832, SpendingCategoryEntertainment}, // cinemas
	{7922, 7922, SpendingCategoryEntertainment}, // theater and tickets
	{7941, 7941, SpendingCategoryEntertainment}, // sports clubs and events
	{7991, 7999, SpendingCategoryEntertainment}, // attractions and recreation
	{8398, 8398, SpendingCategoryGiftsAndDonations},
	{8661, 8661, SpendingCategoryGiftsAndDonations}, // religious organizations
	{5300, 5399, SpendingCategoryShopping},          // department and discount stores
	{5611, 5699, SpendingCategoryShopping},          // clothing
	{5732, 5732, SpendingCategoryShopping},          // electronics
	{5942, 5942, SpendingCategoryShopping},          // books
	{5999, 5999, SpendingCategoryShopping},          // other retail
}

// MapProviderCategory turns a provider category into a default spending
// category name, and says whether it is a transfer instead of spending or
// income. Plaid's personal finance category arrives as primary and
// detailed; SimpleFIN's merchant category code arrives as detailed only,
// written "mcc:5411". Anything not mapped answers "" and false, and is
// left for a spending rule or the categorize model.
func MapProviderCategory(primary, detailed string) (spendingCategoryName string, isTransfer bool) {
	primary = strings.TrimSpace(primary)
	detailed = strings.TrimSpace(detailed)

	if merchantCode, isMerchantCode := strings.CutPrefix(detailed, "mcc:"); isMerchantCode {
		return merchantCodeSpendingCategory(merchantCode), false
	}

	// Plaid's detailed category always begins with its primary, so a
	// primary lost along the way can be recovered from it.
	if primary == "" && detailed != "" {
		for candidate := range plaidSpendingCategoryByPrimary {
			if strings.HasPrefix(detailed, candidate+"_") {
				primary = candidate
				break
			}
		}
		for candidate := range plaidTransferPrimaries {
			if strings.HasPrefix(detailed, candidate+"_") {
				primary = candidate
				break
			}
		}
	}

	if spendingCategory, isMapped := plaidSpendingCategoryByDetailed[detailed]; isMapped {
		return spendingCategory, false
	}
	if plaidUnmappedDetailed[detailed] {
		return "", false
	}
	if plaidTransferDetailed[detailed] || plaidTransferPrimaries[primary] {
		return "", true
	}
	return plaidSpendingCategoryByPrimary[primary], false
}

func merchantCodeSpendingCategory(merchantCode string) string {
	code, err := strconv.Atoi(strings.TrimSpace(merchantCode))
	if err != nil {
		return ""
	}
	for _, candidate := range merchantCodeRanges {
		if code >= candidate.firstCode && code <= candidate.lastCode {
			return candidate.spendingCategory
		}
	}
	return ""
}
