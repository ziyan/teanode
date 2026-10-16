package agent

import (
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Closing a bill from the bank: a transaction that leaves an account, for
// the bill's amount to the cent, posted around the bill's due day, with the
// payee's name in its description, is the bill paid. No model is asked, and
// nobody is told: a paid bill needs no message, and the missing payment is
// the thing worth saying (follow_up_sweep.go).

const (
	// followUpPaymentDays is how far from the due day a payment may post
	// and still be this bill's: people pay early, and banks take days.
	followUpPaymentDays = 10

	// followUpPaymentLookbackDays is how far back transactions are read, so
	// that a bill extracted after its payment posted still closes.
	followUpPaymentLookbackDays = 45

	// followUpPayeeWordLength is how short a payee's word may be and still
	// count: "tax" is in half the bills there are.
	followUpPayeeWordLength = 4
)

// followUpGenericWords are words in a payee's name that say what sort of
// payee it is and not which one, so a payment to another county's office
// is not taken for this one's.
var followUpGenericWords = map[string]bool{
	"county": true, "city": true, "town": true, "state": true, "office": true, "department": true,
	"services": true, "service": true, "company": true, "corporation": true, "authority": true,
	"payment": true, "payments": true, "bill": true, "billing": true, "online": true,
	"with": true, "from": true, "your": true, "that": true, "this": true,
}

// followUpPayeeWords are the words of a payee's name that tell it from
// another's: lower case, letters and digits, four characters or more, and
// not one of the generic ones.
func followUpPayeeWords(payee string) []string {
	words := []string{}
	for _, word := range strings.FieldsFunc(strings.ToLower(payee), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) {
		if len([]rune(word)) >= followUpPayeeWordLength && !followUpGenericWords[word] {
			words = append(words, word)
		}
	}
	return words
}

// followUpPaymentDayDistance is how many days from the bill's due day the
// transaction posted, and whether the transaction is this bill's payment at
// all: money leaving, the bill's amount to the cent, in its currency when
// both have one, within followUpPaymentDays of the due day, and naming the
// payee by at least one of its significant words.
func followUpPaymentDayDistance(followUp *models.AgentFollowUp, financeTransaction *models.FinanceTransaction) (int, bool) {
	if followUp.FollowUpKind != models.AgentFollowUpBill ||
		(followUp.FollowUpState != models.AgentFollowUpOpen && followUp.FollowUpState != models.AgentFollowUpOverdue) {
		return 0, false
	}
	billAmount, err := finance.ParseAmount(followUp.Amount)
	if err != nil || billAmount.Sign() <= 0 {
		return 0, false
	}
	paidAmount, err := finance.ParseAmount(financeTransaction.Amount)
	if err != nil || paidAmount.Sign() >= 0 || new(big.Rat).Neg(paidAmount).Cmp(billAmount) != 0 {
		return 0, false
	}
	if followUp.CurrencyCode != "" && financeTransaction.CurrencyCode != "" && !strings.EqualFold(followUp.CurrencyCode, financeTransaction.CurrencyCode) {
		return 0, false
	}
	dueOn, err := time.Parse(time.DateOnly, followUp.DueOn)
	if err != nil {
		return 0, false
	}
	postedOn, err := time.Parse(time.DateOnly, financeTransaction.PostedOn)
	if err != nil {
		return 0, false
	}
	distance := int(postedOn.Sub(dueOn).Hours() / 24)
	if distance < 0 {
		distance = -distance
	}
	if distance > followUpPaymentDays {
		return 0, false
	}
	text := strings.ToLower(financeTransaction.Description + " " + financeTransaction.MerchantName)
	for _, word := range followUpPayeeWords(followUp.Payee) {
		if strings.Contains(text, word) {
			return distance, true
		}
	}
	return 0, false
}

// isFollowUpPaidBy says the transaction is the payment of the bill.
func isFollowUpPaidBy(followUp *models.AgentFollowUp, financeTransaction *models.FinanceTransaction) bool {
	_, isPaid := followUpPaymentDayDistance(followUp, financeTransaction)
	return isPaid
}

// matchFollowUpPayments closes the agent's open and overdue bills that a
// transaction of the last forty-five days paid, and says how many it closed.
// It reads what is already stored and not only what the sync brought, so a
// bill extracted after its payment posted closes too. A transaction closes
// one bill: two halves of a tax bill for one amount are not both paid by
// one payment.
func matchFollowUpPayments(tx db.Transaction, agentId string, today time.Time) (int, error) {
	followUps, err := tx.ListOpenAgentFollowUps(agentId)
	if err != nil {
		return 0, err
	}
	earliest := today.AddDate(0, 0, -followUpPaymentLookbackDays)
	usedTransactionIds := map[string]bool{}
	closedCount := 0
	for _, followUp := range followUps {
		if followUp.FollowUpKind != models.AgentFollowUpBill {
			continue
		}
		billAmount, err := finance.ParseAmount(followUp.Amount)
		dueOn, dueErr := time.Parse(time.DateOnly, followUp.DueOn)
		if err != nil || dueErr != nil || billAmount.Sign() <= 0 {
			continue
		}
		from := dueOn.AddDate(0, 0, -followUpPaymentDays)
		if from.Before(earliest) {
			from = earliest
		}
		// The database narrows to the amount and the days; the function
		// above decides, so that what a test checks is what runs.
		page, err := tx.ListFinanceTransactions(agentId, &db.FinanceTransactionFilter{
			From: from.Format(time.DateOnly), To: dueOn.AddDate(0, 0, followUpPaymentDays).Format(time.DateOnly),
			MinimumAmount: finance.FormatAmount(new(big.Rat).Neg(billAmount)), MaximumAmount: finance.FormatAmount(new(big.Rat).Neg(billAmount)),
			// Transfers are kept: paying a card's bill moves money from one
			// of the person's accounts to another, and is filed as a transfer.
			IsDuplicateExcluded: true, Limit: db.FinanceTransactionLimitMost,
		})
		if err != nil {
			return closedCount, err
		}
		var paidBy *models.FinanceTransaction
		paidDistance := 0
		for _, financeTransaction := range page.FinanceTransactions {
			distance, isPaid := followUpPaymentDayDistance(followUp, financeTransaction)
			if !isPaid || usedTransactionIds[financeTransaction.ID] {
				continue
			}
			if paidBy == nil || distance < paidDistance {
				paidBy, paidDistance = financeTransaction, distance
			}
		}
		if paidBy == nil {
			continue
		}
		usedTransactionIds[paidBy.ID] = true
		closedReason := fmt.Sprintf("paid on %s: %s", paidBy.PostedOn, cutRunes(strings.TrimSpace(paidBy.Description), 120))
		if _, err := tx.UpdateAgentFollowUp(followUp.ID, func(locked *models.AgentFollowUp) error {
			locked.FollowUpState = models.AgentFollowUpClosed
			locked.ClosedReason = closedReason
			return nil
		}); err != nil {
			return closedCount, err
		}
		closedCount++
	}
	return closedCount, nil
}
