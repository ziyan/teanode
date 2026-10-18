package place

import (
	"fmt"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/version"
)

// UserAgent is what every request to a provider says this server is. The
// OpenStreetMap Foundation's usage policy asks each application to identify
// itself with a name that reaches whoever runs it, which for a TeaNode server
// is its own name.
func UserAgent(configuration *config.Configuration) string {
	userAgent := "TeaNode/" + version.Version()
	if serverName := configuration.Server.Name; serverName != "" {
		userAgent += fmt.Sprintf(" (+https://%s)", serverName)
	}
	return userAgent
}

// ProviderFor makes the provider this configuration names, or nil with no
// error when it names none. It is built from the configuration current at the
// time, the way a web search is, so that a change an operator makes in the
// settings is the next request's provider; the limiter all providers share is
// the process's, so building one for each request does not loosen it.
func ProviderFor(configuration *config.Configuration) (Provider, error) {
	return NewProvider(configuration.Places, UserAgent(configuration))
}

// IsStillThere reports whether a visit with no departure is believed to be
// going on. A phone that never sent the departure would otherwise leave the
// person at a place for ever, so the visit counts only while nothing the phone
// reported since contradicts it: there is no position at all, or the latest
// one is older than the arrival, or it is inside the place, allowing for the
// error the phone gave with it.
func IsStillThere(visit *models.PlaceVisit, latestPoint *models.LocationPoint) bool {
	if visit == nil || visit.DepartedAt != nil {
		return false
	}
	if latestPoint == nil || visit.ArrivedAt == nil || latestPoint.MeasuredAt.Before(*visit.ArrivedAt) {
		return true
	}
	if visit.Place == nil {
		return true
	}
	return DistanceMeters(latestPoint.LatitudeDegrees, latestPoint.LongitudeDegrees, visit.Place.LatitudeDegrees, visit.Place.LongitudeDegrees) <= visit.Place.RadiusMeters+latestPoint.AccuracyMeters
}
