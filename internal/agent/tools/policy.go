package tools

import "github.com/ziyan/teanode/internal/config"

// FeatureAllowed says whether the deployment offers a feature at all: the
// agent switched on, and the feature not switched off by the operator.
func FeatureAllowed(configuration *config.Configuration, feature string) bool {
	return configuration != nil && configuration.Agent.Enabled && configuration.Agent.FeatureOn(feature)
}
