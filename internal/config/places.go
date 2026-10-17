package config

import (
	"net"
	"net/url"
	"strings"
)

// The kinds of provider that name places.
const (
	// PlacesProviderNominatim is OpenStreetMap's own naming and search server.
	PlacesProviderNominatim = "nominatim"

	// PlacesProviderPhoton is a server over the same data that is faster at
	// searching as one types, which an operator can run themselves.
	PlacesProviderPhoton = "photon"
)

// The defaults of a fresh server.
const (
	// PlacesDefaultProviderURL is the OpenStreetMap Foundation's public
	// Nominatim server. Its usage policy allows light use from an application
	// that identifies itself and asks for each place once, which is what a
	// server with a handful of people does.
	PlacesDefaultProviderURL = "https://nominatim.openstreetmap.org"

	// PlacesDefaultMapStyleURL is the OpenFreeMap style the dashboard draws
	// its maps from, through the server.
	PlacesDefaultMapStyleURL = "https://tiles.openfreemap.org/styles/liberty"
)

// Places is where the names of the spots a person goes to come from, and the
// map they are drawn on.
type Places struct {
	// ProviderKind is "nominatim" or "photon". Empty is off: a place is then
	// named after its city, from the table built into the server, and the
	// person can still name it themselves. A fresh configuration defaults to
	// nominatim, and the stored section writes an empty kind out (there is no
	// omitempty), so that off stays off after a restart rather than reading
	// as a missing key and taking the default back.
	ProviderKind string `yaml:"providerKind"`

	// ProviderURL is the provider's address, without a trailing path. Every
	// request carries a User-Agent that names this server, as the public
	// server's policy asks.
	ProviderURL string `yaml:"providerUrl,omitempty"`

	// MapStyleURL is the style the dashboard's maps are drawn from.
	MapStyleURL string `yaml:"mapStyleUrl,omitempty"`
}

// defaultPlaces is a fresh server's: names from the public Nominatim server.
func defaultPlaces() Places {
	return Places{
		ProviderKind: PlacesProviderNominatim,
		ProviderURL:  PlacesDefaultProviderURL,
		MapStyleURL:  PlacesDefaultMapStyleURL,
	}
}

// IsProviderEnabled reports whether places are named by a provider.
func (self *Places) IsProviderEnabled() bool {
	return self.ProviderKind != ""
}

func (self *Configuration) validatePlaces(validator *validator) {
	switch self.Places.ProviderKind {
	case "", PlacesProviderNominatim, PlacesProviderPhoton:
	default:
		validator.add("places.providerKind", `must be "nominatim" or "photon", or empty for no provider`)
	}
	if self.Places.IsProviderEnabled() {
		if self.Places.ProviderURL == "" {
			validator.add("places.providerUrl", "required while a provider is set")
		} else if !isPlacesURL(self.Places.ProviderURL) {
			validator.add("places.providerUrl", "%q is not an https URL (http is allowed only for localhost and private addresses)", self.Places.ProviderURL)
		}
	}
	if self.Places.MapStyleURL != "" && !isPlacesURL(self.Places.MapStyleURL) {
		validator.add("places.mapStyleUrl", "%q is not an https URL (http is allowed only for localhost and private addresses)", self.Places.MapStyleURL)
	}
}

// isPlacesURL is whether an address is absolute and encrypted. Plain http is
// allowed to a machine on this host or the operator's own network, where a
// provider they run themselves answers, and nowhere else.
func isPlacesURL(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		host := parsed.Hostname()
		if strings.EqualFold(host, "localhost") {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
	}
	return false
}
