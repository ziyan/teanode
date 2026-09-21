package tools

import (
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// SearchMode says how a mailbox is searched: by meaning where it can be.
func SearchMode(configuration *config.Configuration, source *models.AgentMailbox) string {
	if source != nil && source.Search && configuration.Agent.Models.Embedding != "" && FeatureAllowed(configuration, "search") {
		return "meaning"
	}
	return "keyword"
}
