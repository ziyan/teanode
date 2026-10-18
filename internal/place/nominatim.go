package place

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// nominatimProvider names places with OpenStreetMap's own server. Its usage
// policy is operations.osmfoundation.org/policies/nominatim.
type nominatimProvider struct {
	*requester
}

// nominatimPlace is one result of reverse naming or of a search, in the
// jsonv2 format. Coordinates arrive as strings.
type nominatimPlace struct {
	ErrorMessage string `json:"error"`
	OSMType      string `json:"osm_type"`
	OSMID        int64  `json:"osm_id"`
	Latitude     string `json:"lat"`
	Longitude    string `json:"lon"`
	Category     string `json:"category"`
	ObjectType   string `json:"type"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`

	// Address holds the parts of the address, as addressdetails=1 asks for.
	Address map[string]any `json:"address"`
}

// addressPart is the first of the keys the address has, as text.
func (self *nominatimPlace) addressPart(keys ...string) string {
	for _, key := range keys {
		if text, isText := self.Address[key].(string); isText && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// toFound turns a result into a Found, or nil when it has no usable position.
func (self *nominatimPlace) toFound() *Found {
	latitude, latitudeErr := strconv.ParseFloat(self.Latitude, 64)
	longitude, longitudeErr := strconv.ParseFloat(self.Longitude, 64)
	if latitudeErr != nil || longitudeErr != nil || !IsOnEarth(latitude, longitude) {
		return nil
	}
	found := &Found{
		PlaceName:        self.Name,
		OSMType:          self.OSMType,
		OSMID:            self.OSMID,
		AddressText:      self.DisplayName,
		LatitudeDegrees:  latitude,
		LongitudeDegrees: longitude,
	}
	if self.Category != "" && self.ObjectType != "" {
		found.OSMCategory = self.Category + "=" + self.ObjectType
	}
	// A house or a street comes back with no name, only its address. The first
	// part of the display name is not it: for a house it can be a number alone.
	if found.PlaceName == "" {
		found.PlaceName = addressName(self.addressPart("house_number"), self.addressPart("road", "pedestrian", "footway", "path"),
			self.addressPart("neighbourhood", "suburb", "quarter", "city_district"),
			self.addressPart("city", "town", "village", "municipality", "hamlet"))
	}
	// An answer without address parts is named by the first part of its
	// display name, unless that is only a number.
	if found.PlaceName == "" {
		if first := strings.TrimSpace(strings.SplitN(self.DisplayName, ",", 2)[0]); strings.Trim(first, "0123456789 ") != "" {
			found.PlaceName = first
		}
	}
	// The area that holds the point is the provider's own choice.
	found.IsChosenForPoint = self.OSMType == "way" || self.OSMType == "relation"
	return found
}

// Name asks for the smallest named place at the point first (layer=poi), since
// naming a visit by its street when the map knows it was a café is a poorer
// answer, and for the address there when there is none.
func (self *nominatimProvider) Name(ctx context.Context, latitudeDegrees, longitudeDegrees float64) (*Found, error) {
	for _, layer := range []string{"poi", "address"} {
		var answer nominatimPlace
		if err := self.get(ctx, "/reverse", url.Values{
			"format":         {"jsonv2"},
			"lat":            {strconv.FormatFloat(latitudeDegrees, 'f', 6, 64)},
			"lon":            {strconv.FormatFloat(longitudeDegrees, 'f', 6, 64)},
			"zoom":           {"18"},
			"layer":          {layer},
			"namedetails":    {"1"},
			"addressdetails": {"1"},
		}, &answer); err != nil {
			return nil, err
		}
		// An answer of {"error": ...} is "nothing here", not a failure.
		if answer.ErrorMessage != "" {
			continue
		}
		// A road, a stop or an elevator is not a point of interest: the address
		// answer that follows says where the point is better.
		if layer == "poi" && isNotAPlace(answer.Category+"="+answer.ObjectType) {
			continue
		}
		if found := answer.toFound(); found != nil {
			found.DistanceMeters = DistanceMeters(latitudeDegrees, longitudeDegrees, found.LatitudeDegrees, found.LongitudeDegrees)
			return found, nil
		}
	}
	return nil, nil
}

// Search looks inside a box around the point (the server cannot search by
// radius) and keeps what is within the radius. Nominatim cannot search for
// nothing, so an empty query names the point instead, which is the one thing
// it knows about a position without words.
func (self *nominatimProvider) Search(ctx context.Context, query string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*Found, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		found, err := self.Name(ctx, latitudeDegrees, longitudeDegrees)
		if err != nil || found == nil {
			return nil, err
		}
		return nearest([]*Found{found}, latitudeDegrees, longitudeDegrees, radiusMeters, limitCount), nil
	}
	west, north, east, south := boundingBox(latitudeDegrees, longitudeDegrees, radiusMeters)
	var answers []nominatimPlace
	if err := self.get(ctx, "/search", url.Values{
		"format":         {"jsonv2"},
		"q":              {query},
		"viewbox":        {fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", west, north, east, south)},
		"bounded":        {"1"},
		"addressdetails": {"1"},
		"limit":          {strconv.Itoa(searchFetchCount(limitCount))},
	}, &answers); err != nil {
		return nil, err
	}
	found := make([]*Found, 0, len(answers))
	for index := range answers {
		if candidate := answers[index].toFound(); candidate != nil {
			found = append(found, candidate)
		}
	}
	return nearest(found, latitudeDegrees, longitudeDegrees, radiusMeters, limitCount), nil
}

// toAddressFound turns a result of an address search into a Found. What it is
// called, and the first part of its address, follow the naming rules of a
// reverse answer: a road, a stop or an elevator is called by the address it
// stands at and not by what it is, and a house has no name but its number and
// road.
func (self *nominatimPlace) toAddressFound() *Found {
	cleared := *self
	if isNotAPlace(self.Category + "=" + self.ObjectType) {
		cleared.Name = ""
	}
	found := cleared.toFound()
	if found == nil {
		return nil
	}
	found.IsChosenForPoint = false
	if line := addressLine(found.PlaceName, self.addressPart("neighbourhood", "suburb", "quarter", "city_district"),
		self.addressPart("city", "town", "village", "municipality", "hamlet"), self.addressPart("state", "province"),
		self.addressPart("country")); line != "" {
		found.AddressText = line
	}
	return found
}

// Addresses asks for matches anywhere: no viewbox, so the words alone decide.
func (self *nominatimProvider) Addresses(ctx context.Context, query string, limitCount int) ([]*Found, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	var answers []nominatimPlace
	if err := self.get(ctx, "/search", url.Values{
		"format":         {"jsonv2"},
		"q":              {query},
		"addressdetails": {"1"},
		"limit":          {strconv.Itoa(addressFetchCount(limitCount))},
	}, &answers); err != nil {
		return nil, err
	}
	found := make([]*Found, 0, len(answers))
	for index := range answers {
		if candidate := answers[index].toAddressFound(); candidate != nil {
			found = append(found, candidate)
		}
	}
	return found[:min(len(found), addressFetchCount(limitCount))], nil
}
