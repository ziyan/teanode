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
		District    string `json:"district"`
		Locality    string `json:"locality"`
		Country     string `json:"country"`
	} `json:"properties"`
}

// photonOsmTypes maps the one letter Photon uses to the words the rest of the
// program uses.
var photonOsmTypes = map[string]string{"N": "node", "W": "way", "R": "relation"}

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
		OSMType:          photonOsmTypes[properties.OSMType],
		OSMID:            properties.OSMID,
		AddressText:      strings.Join(addressParts, ", "),
		LatitudeDegrees:  latitude,
		LongitudeDegrees: longitude,
	}
	if properties.OSMKey != "" && properties.OSMValue != "" {
		found.OSMCategory = properties.OSMKey + "=" + properties.OSMValue
	}
	// A house has no name, only its address: its number and street, then the
	// neighborhood, then the city.
	if found.PlaceName == "" {
		found.PlaceName = addressName(properties.HouseNumber, properties.Street, firstText(properties.Locality, properties.District), properties.City)
	}
	return found
}

// firstText is the first of the texts that is not empty.
func firstText(texts ...string) string {
	for _, text := range texts {
		if strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// toAddressFound is a feature that is not a point of interest (a road, a stop,
// an elevator) as the address it stands at: named by the address and not by
// what it is.
func (self *photonFeature) toAddressFound() *Found {
	cleared := *self
	cleared.Properties.Name = ""
	return cleared.toFound()
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

// Name answers the first place Photon finds at the point.
func (self *photonProvider) Name(ctx context.Context, latitudeDegrees, longitudeDegrees float64) (*Found, error) {
	var answer photonAnswer
	if err := self.get(ctx, "/reverse", url.Values{
		"lat": {strconv.FormatFloat(latitudeDegrees, 'f', 6, 64)},
		"lon": {strconv.FormatFloat(longitudeDegrees, 'f', 6, 64)},
	}, &answer); err != nil {
		return nil, err
	}
	// The first feature that is a point of interest. A road, a stop or an
	// elevator is not one, and when there is nothing else the first of them
	// stands for the address.
	var found *Found
	for index := range answer.Features {
		feature := &answer.Features[index]
		if isNotAPlace(feature.Properties.OSMKey + "=" + feature.Properties.OSMValue) {
			if found == nil {
				found = feature.toAddressFound()
			}
			continue
		}
		if candidate := feature.toFound(); candidate != nil {
			found = candidate
			break
		}
	}
	if found == nil {
		return nil, nil
	}
	found.DistanceMeters = DistanceMeters(latitudeDegrees, longitudeDegrees, found.LatitudeDegrees, found.LongitudeDegrees)
	return found, nil
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
