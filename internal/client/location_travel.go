package client

import (
	"context"
	"time"
)

// How a person traveled: the operations behind `teanode location trips`,
// `travel` and `travel-mode`.

// TripLeg is a part of a trip with one way of travel. TravelMode is the way as
// the person corrected it, if they did, else DetectedTravelMode.
type TripLeg struct {
	ID                    string    `json:"id"`
	StartedAt             time.Time `json:"startedAt"`
	EndedAt               time.Time `json:"endedAt"`
	DurationSeconds       int       `json:"durationSeconds"`
	DistanceMeters        float64   `json:"distanceMeters"`
	TravelMode            string    `json:"travelMode"`
	DetectedTravelMode    string    `json:"detectedTravelMode"`
	IsTravelModeCorrected bool      `json:"isTravelModeCorrected"`
	DetectionSource       string    `json:"detectionSource"`
}

// Trip is the travel between the departure from one visit and the arrival at
// the next. FromPlace and ToPlace are null when the place is gone.
type Trip struct {
	ID              string     `json:"id"`
	StartedAt       time.Time  `json:"startedAt"`
	EndedAt         time.Time  `json:"endedAt"`
	DurationSeconds int        `json:"durationSeconds"`
	DistanceMeters  float64    `json:"distanceMeters"`
	FromPlace       *Place     `json:"fromPlace"`
	ToPlace         *Place     `json:"toPlace"`
	Legs            []*TripLeg `json:"legs"`
}

// TripPage is a page of trips, newest first; NextOffset is null on the last.
type TripPage struct {
	Trips      []*Trip `json:"trips"`
	NextOffset *int    `json:"nextOffset"`
}

// TravelTotal is the distance traveled one way in one period. PeriodStartDay is
// a date written YYYY-MM-DD.
type TravelTotal struct {
	PeriodStartDay  string  `json:"periodStartDay"`
	TravelMode      string  `json:"travelMode"`
	DistanceMeters  float64 `json:"distanceMeters"`
	DurationSeconds int     `json:"durationSeconds"`
	LegCount        int     `json:"legCount"`
}

const (
	tripLegFields = `id startedAt endedAt durationSeconds distanceMeters travelMode detectedTravelMode isTravelModeCorrected detectionSource`

	DocumentListTrips = `query ($userId: String, $from: DateTime!, $until: DateTime!, $offset: Int, $limitCount: Int) {
  ListTrips(userId: $userId, from: $from, until: $until, offset: $offset, limitCount: $limitCount) {
    nextOffset
    trips {
      id startedAt endedAt durationSeconds distanceMeters
      fromPlace { ` + placeFields + ` }
      toPlace { ` + placeFields + ` }
      legs { ` + tripLegFields + ` }
    }
  }
}`

	DocumentSetTripLegTravelMode = `mutation ($tripLegId: String!, $travelMode: String) {
  SetTripLegTravelMode(tripLegId: $tripLegId, travelMode: $travelMode) { ` + tripLegFields + ` }
}`

	DocumentListTravelTotals = `query ($userId: String, $period: String!, $from: DateTime!, $until: DateTime!) {
  ListTravelTotals(userId: $userId, period: $period, from: $from, until: $until) {
    periodStartDay travelMode distanceMeters durationSeconds legCount
  }
}`
)

// ListTrips is a page of trips that overlap the range, newest first, of the
// caller or, with a userId, of somebody who shares their history with them.
func ListTrips(ctx context.Context, connection *Client, userId *string, from, until time.Time, offset, limitCount *int) (*TripPage, error) {
	variables := map[string]any{"from": from.UTC().Format(time.RFC3339), "until": until.UTC().Format(time.RFC3339)}
	if userId != nil {
		variables["userId"] = *userId
	}
	if offset != nil {
		variables["offset"] = *offset
	}
	if limitCount != nil {
		variables["limitCount"] = *limitCount
	}
	var result struct {
		ListTrips *TripPage `json:"ListTrips"`
	}
	if err := connection.Execute(ctx, DocumentListTrips, variables, &result); err != nil {
		return nil, err
	}
	return result.ListTrips, nil
}

// SetTripLegTravelMode corrects the way of travel of one of the caller's legs;
// a nil travelMode clears the correction.
func SetTripLegTravelMode(ctx context.Context, connection *Client, tripLegId string, travelMode *string) (*TripLeg, error) {
	variables := map[string]any{"tripLegId": tripLegId}
	if travelMode != nil {
		variables["travelMode"] = *travelMode
	}
	var result struct {
		SetTripLegTravelMode *TripLeg `json:"SetTripLegTravelMode"`
	}
	if err := connection.Execute(ctx, DocumentSetTripLegTravelMode, variables, &result); err != nil {
		return nil, err
	}
	return result.SetTripLegTravelMode, nil
}

// ListTravelTotals is the distance traveled by each way in each period
// (day, week, month or year) of the range, of the caller or, with a userId, of
// somebody who shares their history with them.
func ListTravelTotals(ctx context.Context, connection *Client, userId *string, period string, from, until time.Time) ([]*TravelTotal, error) {
	variables := map[string]any{"period": period, "from": from.UTC().Format(time.RFC3339), "until": until.UTC().Format(time.RFC3339)}
	if userId != nil {
		variables["userId"] = *userId
	}
	var result struct {
		ListTravelTotals []*TravelTotal `json:"ListTravelTotals"`
	}
	if err := connection.Execute(ctx, DocumentListTravelTotals, variables, &result); err != nil {
		return nil, err
	}
	return result.ListTravelTotals, nil
}
