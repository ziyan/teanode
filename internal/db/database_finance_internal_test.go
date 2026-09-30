package db

import (
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A liability's value is what is owed without its sign, whichever way the
// provider wrote it; anything else keeps the balance as reported, an
// overdrawn account included.
func TestFinanceValuationValueTakesTheSignFromTheAsset(t *testing.T) {
	for _, testCase := range []struct {
		currentBalance string
		isLiability    bool
		expectedValue  string
	}{
		{currentBalance: "340.25", isLiability: true, expectedValue: "340.2500"},
		{currentBalance: "-340.25", isLiability: true, expectedValue: "340.2500"},
		{currentBalance: "1200.5", isLiability: false, expectedValue: "1200.5000"},
		{currentBalance: "-12.00", isLiability: false, expectedValue: "-12.0000"},
		{currentBalance: "0", isLiability: true, expectedValue: "0.0000"},
	} {
		value, err := financeValuationValue(testCase.currentBalance, testCase.isLiability)
		if err != nil {
			t.Fatalf("financeValuationValue(%q, %v): %s", testCase.currentBalance, testCase.isLiability, err)
		}
		if value != testCase.expectedValue {
			t.Errorf("financeValuationValue(%q, %v) = %q, want %q", testCase.currentBalance, testCase.isLiability, value, testCase.expectedValue)
		}
	}
	if _, err := financeValuationValue("twelve", false); err == nil {
		t.Error("a balance that is not a decimal must be refused")
	}
}

func TestFinanceAccountKindMapsToAnAssetKind(t *testing.T) {
	for _, testCase := range []struct {
		accountKind       models.FinanceAccountKind
		providerMetadata  string
		expectedAssetKind models.AssetKind
	}{
		{accountKind: models.FinanceAccountKindDepository, expectedAssetKind: models.AssetKindCash},
		{accountKind: models.FinanceAccountKindInvestment, expectedAssetKind: models.AssetKindInvestment},
		{accountKind: models.FinanceAccountKindCredit, expectedAssetKind: models.AssetKindCreditCard},
		{accountKind: models.FinanceAccountKindLoan, providerMetadata: `{"subtype":"student"}`, expectedAssetKind: models.AssetKindLoan},
		{accountKind: models.FinanceAccountKindLoan, providerMetadata: `{"subtype":"mortgage"}`, expectedAssetKind: models.AssetKindMortgage},
		{accountKind: models.FinanceAccountKindOther, expectedAssetKind: models.AssetKindOtherAsset},
	} {
		assetKind := assetKindForFinanceAccount(testCase.accountKind, json.RawMessage(testCase.providerMetadata))
		if assetKind != testCase.expectedAssetKind {
			t.Errorf("%s %s: got %s, want %s", testCase.accountKind, testCase.providerMetadata, assetKind, testCase.expectedAssetKind)
		}
	}
	if !models.AssetKindCreditCard.IsLiability() || !models.AssetKindMortgage.IsLiability() || models.AssetKindCash.IsLiability() {
		t.Error("cards and mortgages subtract, cash does not")
	}
}
