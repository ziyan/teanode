package finance

import "testing"

func TestMapProviderCategory(t *testing.T) {
	for _, testCase := range []struct {
		primary          string
		detailed         string
		wantCategoryName string
		wantIsTransfer   bool
	}{
		// Plaid.
		{"INCOME", "INCOME_WAGES", SpendingCategoryIncome, false},
		{"TRANSFER_IN", "TRANSFER_IN_ACCOUNT_TRANSFER", "", true},
		{"TRANSFER_OUT", "TRANSFER_OUT_SAVINGS", "", true},
		{"LOAN_PAYMENTS", "LOAN_PAYMENTS_CREDIT_CARD_PAYMENT", "", true},
		{"BANK_FEES", "BANK_FEES_OVERDRAFT_FEES", SpendingCategoryFees, false},
		{"ENTERTAINMENT", "ENTERTAINMENT_TV_AND_MOVIES", SpendingCategoryEntertainment, false},
		{"FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES", SpendingCategoryGroceries, false},
		{"FOOD_AND_DRINK", "FOOD_AND_DRINK_RESTAURANT", SpendingCategoryDining, false},
		{"FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE", SpendingCategoryDining, false},
		{"GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_ONLINE_MARKETPLACES", SpendingCategoryShopping, false},
		{"HOME_IMPROVEMENT", "HOME_IMPROVEMENT_HARDWARE", SpendingCategoryHousing, false},
		{"MEDICAL", "MEDICAL_PHARMACIES_AND_SUPPLEMENTS", SpendingCategoryHealth, false},
		{"PERSONAL_CARE", "PERSONAL_CARE_GYMS_AND_FITNESS_CENTERS", SpendingCategoryHealth, false},
		{"GENERAL_SERVICES", "GENERAL_SERVICES_INSURANCE", SpendingCategoryOther, false},
		{"GOVERNMENT_AND_NON_PROFIT", "GOVERNMENT_AND_NON_PROFIT_DONATIONS", SpendingCategoryGiftsAndDonations, false},
		{"GOVERNMENT_AND_NON_PROFIT", "GOVERNMENT_AND_NON_PROFIT_TAX_PAYMENT", SpendingCategoryOther, false},
		{"TRANSPORTATION", "TRANSPORTATION_GAS", SpendingCategoryTransport, false},
		{"TRAVEL", "TRAVEL_FLIGHTS", SpendingCategoryTravel, false},
		{"RENT_AND_UTILITIES", "RENT_AND_UTILITIES_RENT", SpendingCategoryHousing, false},
		{"RENT_AND_UTILITIES", "RENT_AND_UTILITIES_GAS_AND_ELECTRICITY", SpendingCategoryUtilities, false},
		{"FOOD_AND_DRINK", "", SpendingCategoryDining, false},
		// A primary lost along the way is recovered from the detailed.
		{"", "FOOD_AND_DRINK_GROCERIES", SpendingCategoryGroceries, false},
		{"", "TRANSFER_OUT_WITHDRAWAL", "", true},
		{"", "TRAVEL_LODGING", SpendingCategoryTravel, false},
		// Merchant category codes from SimpleFIN.
		{"", "mcc:5411", SpendingCategoryGroceries, false},
		{"", "mcc:5499", SpendingCategoryGroceries, false},
		{"", "mcc:5812", SpendingCategoryDining, false},
		{"", "mcc:5814", SpendingCategoryDining, false},
		{"", "mcc:3001", SpendingCategoryTravel, false},
		{"", "mcc:4511", SpendingCategoryTravel, false},
		{"", "mcc:7011", SpendingCategoryTravel, false},
		{"", "mcc:4722", SpendingCategoryTravel, false},
		{"", "mcc:4121", SpendingCategoryTransport, false},
		{"", "mcc:5542", SpendingCategoryTransport, false},
		{"", "mcc:4900", SpendingCategoryUtilities, false},
		{"", "mcc:8062", SpendingCategoryHealth, false},
		{"", "mcc:5912", SpendingCategoryHealth, false},
		{"", "mcc:4899", SpendingCategorySubscriptions, false},
		{"", "mcc:5817", SpendingCategorySubscriptions, false},
		{"", "mcc:7832", SpendingCategoryEntertainment, false},
		{"", "mcc:7996", SpendingCategoryEntertainment, false},
		{"", "mcc:8398", SpendingCategoryGiftsAndDonations, false},
		{"", "mcc:8661", SpendingCategoryGiftsAndDonations, false},
		{"", "mcc:5311", SpendingCategoryShopping, false},
		{"", "mcc:5651", SpendingCategoryShopping, false},
		{"", "mcc:5732", SpendingCategoryShopping, false},
		{"", "mcc:5942", SpendingCategoryShopping, false},
		{"", "mcc:5999", SpendingCategoryShopping, false},
		// Nothing to go on.
		{"", "mcc:6011", "", false},
		{"", "mcc:abcd", "", false},
		{"", "", "", false},
		{"SOMETHING_NEW", "SOMETHING_NEW_ENTIRELY", "", false},
	} {
		categoryName, isTransfer := MapProviderCategory(testCase.primary, testCase.detailed)
		if categoryName != testCase.wantCategoryName || isTransfer != testCase.wantIsTransfer {
			t.Errorf("%q %q maps to %q %v, want %q %v", testCase.primary, testCase.detailed,
				categoryName, isTransfer, testCase.wantCategoryName, testCase.wantIsTransfer)
		}
	}
}

func TestMappedCategoriesAreDefaults(t *testing.T) {
	isDefault := map[string]bool{}
	for _, name := range DefaultSpendingCategoryNames {
		isDefault[name] = true
	}
	if len(isDefault) != 14 {
		t.Errorf("%d default spending categories, want 14", len(isDefault))
	}
	for _, name := range plaidSpendingCategoryByPrimary {
		if !isDefault[name] {
			t.Errorf("%q is mapped to and is not a default", name)
		}
	}
	for _, name := range plaidSpendingCategoryByDetailed {
		if !isDefault[name] {
			t.Errorf("%q is mapped to and is not a default", name)
		}
	}
	for _, codeRange := range merchantCodeRanges {
		if !isDefault[codeRange.spendingCategory] {
			t.Errorf("%q is mapped to and is not a default", codeRange.spendingCategory)
		}
	}
}
