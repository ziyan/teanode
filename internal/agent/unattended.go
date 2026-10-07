package agent

import (
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// unattendedRiskWords are the kinds of action a person may let the agent
// take alone, as a turn with nobody present is told them.
var unattendedRiskWords = map[models.UnattendedRisk]string{
	models.UnattendedRiskOutward:     "speaking for them (sending mail, posting, replying)",
	models.UnattendedRiskMoney:       "spending or moving their money",
	models.UnattendedRiskDestructive: "what cannot be undone",
	models.UnattendedRiskGranting:    "giving somebody access",
	models.UnattendedRiskListed:      "the tools on their ask-me-first list",
}

// unattendedLine tells a turn with nobody present what it may do that would
// otherwise need the person's word, so that it does it rather than
// preparing it and stopping.
func unattendedLine(agent *models.Agent) string {
	var allowed []string
	if agent != nil {
		for _, unattendedRisk := range models.UnattendedRisks {
			for _, chosen := range agent.UnattendedAllowedRisks {
				if chosen == unattendedRisk {
					allowed = append(allowed, unattendedRiskWords[unattendedRisk])
				}
			}
		}
	}
	if len(allowed) == 0 {
		return "Anything that needs their confirmation cannot be done with nobody present: prepare it and say what you would have done."
	}
	return "They let you do some things without their word when they are not there: " + strings.Join(allowed, "; ") +
		". Do those when the work calls for them. Anything else that needs their confirmation cannot be done with nobody present: prepare it and say what you would have done."
}
