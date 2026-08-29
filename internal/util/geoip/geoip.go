// Package geoip provides IP geolocation using MaxMind databases.
package geoip

import (
	"fmt"
	"net"
	"sync"

	"github.com/op/go-logging"
	"github.com/oschwald/maxminddb-golang"
)

var log = logging.MustGetLogger("geoip")

// Location represents geographical location information determined based on IP address.
type Location struct {
	// Latitude value
	Latitude float64 `json:"latitude,omitempty"`

	// Longitude value
	Longitude float64 `json:"longitude,omitempty"`

	// Country code
	Country string `json:"country,omitempty"`

	// City name
	City string `json:"city,omitempty"`
}

func (self *Location) String() string {
	if self != nil {
		if self.Country != "" && self.City != "" {
			return fmt.Sprintf("%s, %s", self.City, self.Country)
		} else if self.Country != "" {
			return self.Country
		}
	}
	return "Unknown location"
}

type Locator interface {
	Locate(net.IP) *Location
}

type locator struct {
	pool *sync.Pool
}

func NewLocator(filename string) Locator {
	return &locator{
		pool: &sync.Pool{
			New: func() interface{} {
				d, err := maxminddb.Open(filename)
				if err != nil {
					log.Errorf("failed to open geoip database %q: %s", filename, err)
					return nil
				}
				return d
			},
		},
	}
}

type record struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
}

func (self *locator) Locate(ip net.IP) *Location {
	d := self.pool.Get()
	if d == nil {
		return nil
	}
	defer self.pool.Put(d)

	var r record
	if err := d.(*maxminddb.Reader).Lookup(ip, &r); err != nil {
		log.Errorf("failed to locate %q: %s", ip, err)
		return nil
	}
	if r.Country.ISOCode == "" {
		return nil
	}

	return &Location{
		Country:   r.Country.ISOCode,
		City:      r.City.Names["en"],
		Latitude:  r.Location.Latitude,
		Longitude: r.Location.Longitude,
	}
}

// nullLocator locates nothing. It stands in when the operator has not
// supplied a MaxMind database, which is the default: none is bundled because
// the licence requires each user to accept it themselves.
type nullLocator struct{}

// NewNullLocator returns a Locator that always reports an unknown location.
func NewNullLocator() Locator {
	return &nullLocator{}
}

func (self *nullLocator) Locate(ip net.IP) *Location {
	return nil
}
