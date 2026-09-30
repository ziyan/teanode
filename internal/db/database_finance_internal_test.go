package db

import (
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A liability's value is the amount owed, read the way its provider signs
// it: Plaid's positive balance is a debt, SimpleFIN's negative one is, and
// the other sign is a credit in the person's favour, a negative
// liability. Anything else keeps the balance as reported, an overdrawn
// account included.
func TestFinanceValuationValueTakesTheSignFromTheProvider(t *testing.T) {
	for _, testCase := range []struct {
		description           string
		currentBalance        string
		isLiability           bool
		isOwedBalancePositive bool
		expectedValue         string
	}{
		{description: "a card Plaid says is owed", currentBalance: "340.25", isLiability: true, isOwedBalancePositive: true, expectedValue: "340.2500"},
		{description: "a card SimpleFIN says is owed", currentBalance: "-340.25", isLiability: true, expectedValue: "340.2500"},
		{description: "a refund left on a card through Plaid", currentBalance: "-25.00", isLiability: true, isOwedBalancePositive: true, expectedValue: "-25.0000"},
		{description: "a refund left on a card through SimpleFIN", currentBalance: "25.00", isLiability: true, expectedValue: "-25.0000"},
		{description: "checking through Plaid", currentBalance: "1200.5", isOwedBalancePositive: true, expectedValue: "1200.5000"},
		{description: "checking through SimpleFIN", currentBalance: "1200.5", expectedValue: "1200.5000"},
		{description: "an overdrawn account", currentBalance: "-12.00", expectedValue: "-12.0000"},
		{description: "a card paid off", currentBalance: "0", isLiability: true, expectedValue: "0.0000"},
	} {
		value, err := financeValuationValue(testCase.currentBalance, testCase.isLiability, testCase.isOwedBalancePositive)
		if err != nil {
			t.Fatalf("%s: %s", testCase.description, err)
		}
		if value != testCase.expectedValue {
			t.Errorf("%s: got %q, want %q", testCase.description, value, testCase.expectedValue)
		}
	}
	if _, err := financeValuationValue("twelve", false, false); err == nil {
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
