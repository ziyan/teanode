package apigraph

import (
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The walk over the rules is arrival's: disabled rules are skipped, every
// matching rule fires in order, and one that says stop ends the walk.
func TestMatchingRulesWalkLikeArrival(test *testing.T) {
	test.Parallel()

	mail := &models.Mail{From: "notifications@github.com", Subject: "[ziyan/teanode] a pull request"}
	rules := []models.MailboxRule{
		{Name: "off", Enabled: false, Conditions: []models.MailboxRuleCondition{{Field: "any"}}},
		{Name: "github", Enabled: true, Conditions: []models.MailboxRuleCondition{{Field: "from", Operator: "contains", Value: "@github.com"}}},
		{Name: "pull requests", Enabled: true, Stop: true, Conditions: []models.MailboxRuleCondition{{Field: "subject", Operator: "contains", Value: "pull request"}}},
		{Name: "everything", Enabled: true, Conditions: []models.MailboxRuleCondition{{Field: "any"}}},
	}
	if fired := matchingRules(rules, mail, false, nil); !reflect.DeepEqual(fired, []int{1, 2}) {
		test.Errorf("matchingRules = %v, want the GitHub rule then the stopping one", fired)
	}
	other := &models.Mail{From: "ada@example.com", Subject: "hello"}
	if fired := matchingRules(rules, other, false, nil); !reflect.DeepEqual(fired, []int{3}) {
		test.Errorf("matchingRules for a stranger = %v, want the catch-all alone", fired)
	}
}
