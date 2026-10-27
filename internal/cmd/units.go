package cmd

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// usesUSUnits says whether this person reads Fahrenheit and miles: as their
// account says, else as the region of the language this shell speaks says,
// else metric. A token that is no account has no choice to read.
func usesUSUnits(ctx context.Context, connection *client.Client) bool {
	measurementSystem := models.MeasurementSystemAutomatic
	if user, err := client.GetCurrentUser(ctx, connection); err == nil && user != nil {
		measurementSystem = user.MeasurementSystem
	}
	return models.UsesUSUnits(measurementSystem, client.LocalLanguage())
}

// metersPerFoot is the length of a foot.
const metersPerFoot = 0.3048

// distanceWords is a distance in meters as a person reads it, as the
// dashboard writes it: meters under a tenth of a kilometer, else kilometers
// with one decimal; in US units, feet under a tenth of a mile, else miles.
func distanceWords(distanceMeters float64, isUSUnits bool) string {
	if isUSUnits {
		miles := models.MilesFromMeters(distanceMeters)
		if miles < 0.1 {
			return fmt.Sprintf("%.0f ft", distanceMeters/metersPerFoot)
		}
		return fmt.Sprintf("%.1f mi", miles)
	}
	if distanceMeters < 100 {
		return fmt.Sprintf("%.0f m", distanceMeters)
	}
	return fmt.Sprintf("%.1f km", distanceMeters/1000)
}
