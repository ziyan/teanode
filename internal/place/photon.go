package place

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// photonProvider names places with a Photon server, which answers from the
// same OpenStreetMap data as Nominatim, faster at search, in GeoJSON. An
// operator can run one themselves.
type photonProvider struct {
	*requester
}

type photonAnswer struct {
	Features []photonFeature `json:"features"`
}

type photonFeature struct {
	Geometry struct {
		// Coordinates are longitude first, as GeoJSON has them.
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Name        string `json:"name"`
		OSMType     string `json:"osm_type"`
		OSMID       int64  `json:"osm_id"`
		OSMKey      string `json:"osm_key"`
		OSMValue    string `json:"osm_value"`
		Street      string `json:"street"`
		HouseNumber string `json:"housenumber"`
		City        string `json:"city"`
		Country     string `json:"country"`
	} `json:"properties"`
}

// photonOSMTypes maps the one letter Photon uses to the words the rest of the
// program uses.
var photonOSMTypes = map[string]string{"N": "node", "W": "way", "R": "relation"}

// toFound turns a feature into a Found, or nil when it has no usable position.
func (self *photonFeature) toFound() *Found {
	if len(self.Geometry.Coordinates) < 2 {
		return nil
	}
	longitude, latitude := self.Geometry.Coordinates[0], self.Geometry.Coordinates[1]
	if !IsOnEarth(latitude, longitude) {
		return nil
	}
	properties := self.Properties
	street := strings.TrimSpace(properties.HouseNumber + " " + properties.Street)
	addressParts := []string{}
	for _, part := range []string{street, properties.City, properties.Country} {
		if part = strings.TrimSpace(part); part != "" {
			addressParts = append(addressParts, part)
		}
	}
	found := &Found{
		PlaceName:        properties.Name,
		OSMType:          photonOSMTypes[properties.OSMType],
		OSMID:            properties.OSMID,
		AddressText:      strings.Join(addressParts, ", "),
		LatitudeDegrees:  latitude,
		LongitudeDegrees: longitude,
	}
	if properties.OSMKey != "" && properties.OSMValue != "" {
		found.OSMCategory = properties.OSMKey + "=" + properties.OSMValue
	}
	// A house has no name, only its address, whose first part is its number
	// and street.
	if found.PlaceName == "" {
		found.PlaceName = street
	}
	if found.PlaceName == "" {
		found.PlaceName = properties.City
	}
	return found
}

func (self *photonAnswer) found() []*Found {
	found := make([]*Found, 0, len(self.Features))
	for index := range self.Features {
		if candidate := self.Features[index].toFound(); candidate != nil {
			found = append(found, candidate)
		}
	}
	return found
}

// Name answers the first thing Photon finds at the point.
func (self *photonProvider) Name(ctx context.Context, latitudeDegrees, longitudeDegrees float64) (*Found, error) {
	var answer photonAnswer
	if err := self.get(ctx, "/reverse", url.Values{
		"lat": {strconv.FormatFloat(latitudeDegrees, 'f', 6, 64)},
		"lon": {strconv.FormatFloat(longitudeDegrees, 'f', 6, 64)},
	}, &answer); err != nil {
		return nil, err
	}
	for _, found := range answer.found() {
		found.DistanceMeters = DistanceMeters(latitudeDegrees, longitudeDegrees, found.LatitudeDegrees, found.LongitudeDegrees)
		return found, nil
	}
	return nil, nil
}

// Search matches the words, leaning toward the point, and keeps what is within
// the radius. Photon can list what is near a point without words, so an empty
// query asks for that.
func (self *photonProvider) Search(ctx context.Context, query string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*Found, error) {
	var answer photonAnswer
	query = strings.TrimSpace(query)
	if query == "" {
		if err := self.get(ctx, "/reverse", url.Values{
			"lat":    {strconv.FormatFloat(latitudeDegrees, 'f', 6, 64)},
			"lon":    {strconv.FormatFloat(longitudeDegrees, 'f', 6, 64)},
			"radius": {strconv.FormatFloat(radiusMeters/1000, 'f', 3, 64)},
			"limit":  {strconv.Itoa(searchFetchCount(limitCount))},
		}, &answer); err != nil {
			return nil, err
		}
	} else if err := self.get(ctx, "/api", url.Values{
		"q":     {query},
		"lat":   {strconv.FormatFloat(latitudeDegrees, 'f', 6, 64)},
		"lon":   {strconv.FormatFloat(longitudeDegrees, 'f', 6, 64)},
		"limit": {strconv.Itoa(searchFetchCount(limitCount))},
	}, &answer); err != nil {
		return nil, err
	}
	return nearest(answer.found(), latitudeDegrees, longitudeDegrees, radiusMeters, limitCount), nil
}
