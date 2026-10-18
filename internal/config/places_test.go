package config

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
)

// A fresh server names places from the public Nominatim server, and the
// dashboard's map is the OpenFreeMap style.
func TestAFreshServerNamesPlacesFromNominatim(t *testing.T) {
	t.Parallel()

	places := Default().Places
	if places.ProviderKind != PlacesProviderNominatim || places.ProviderURL != PlacesDefaultProviderURL || places.MapStyleURL != PlacesDefaultMapStyleURL {
		t.Errorf("the defaults are %+v", places)
	}
	if !places.IsProviderEnabled() {
		t.Error("a fresh server has its provider off")
	}
	if err := validConfiguration().Validate(); err != nil {
		t.Errorf("the defaults should be valid: %s", err)
	}
}

// An operator who switches the provider off keeps it off. The stored section
// writes the empty kind out, so that reading it back is "off" and not a
// missing key that takes the default again.
func TestAProviderSwitchedOffStaysOffAfterARestart(t *testing.T) {
	t.Parallel()

	configuration := validConfiguration()
	configuration.Places.ProviderKind = ""
	rows, err := ToRows(configuration, 1, nil)
	if err != nil {
		t.Fatalf("ToRows: %s", err)
	}
	if !strings.Contains(rows.Settings[settingPlaces], "providerKind: \"\"") {
		t.Fatalf("the stored section does not say the provider is off:\n%s", rows.Settings[settingPlaces])
	}
	restored, err := decodeRows(rows)
	if err != nil {
		t.Fatalf("decodeRows: %s", err)
	}
	if restored.Places.IsProviderEnabled() {
		t.Errorf("the provider came back on as %q", restored.Places.ProviderKind)
	}

	// A database written before the section existed has none, and takes the
	// defaults.
	older, err := decodeRows(&db.ConfigurationRows{Settings: map[string]string{}})
	if err != nil {
		t.Fatalf("decodeRows: %s", err)
	}
	if older.Places.ProviderKind != PlacesProviderNominatim {
		t.Errorf("a database without the section has kind %q", older.Places.ProviderKind)
	}
}

// The kind is one of two, and the addresses are absolute and encrypted, with
// plain http only to a machine the operator runs themselves.
func TestPlacesAreValidated(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(*Places){
		"photon":            func(places *Places) { places.ProviderKind = PlacesProviderPhoton },
		"off":               func(places *Places) { places.ProviderKind = ""; places.ProviderURL = "" },
		"localhost":         func(places *Places) { places.ProviderURL = "http://localhost:2322" },
		"loopback":          func(places *Places) { places.ProviderURL = "http://127.0.0.1:2322" },
		"private":           func(places *Places) { places.ProviderURL = "http://192.168.1.7:2322" },
		"private style":     func(places *Places) { places.MapStyleURL = "http://10.0.0.5/style.json" },
		"no map style":      func(places *Places) { places.MapStyleURL = "" },
		"https other host":  func(places *Places) { places.ProviderURL = "https://geocoder.example.com" },
		"https with a path": func(places *Places) { places.ProviderURL = "https://example.com/nominatim" },
	} {
		configuration := validConfiguration()
		change(&configuration.Places)
		if err := configuration.Validate(); err != nil {
			t.Errorf("%s should be valid: %s", name, err)
		}
	}
	for name, change := range map[string]func(*Places){
		"unknown kind":        func(places *Places) { places.ProviderKind = "google" },
		"empty url":           func(places *Places) { places.ProviderURL = "" },
		"relative url":        func(places *Places) { places.ProviderURL = "nominatim.example.com" },
		"plain http":          func(places *Places) { places.ProviderURL = "http://nominatim.example.com" },
		"plain http public":   func(places *Places) { places.ProviderURL = "http://8.8.8.8" },
		"other scheme":        func(places *Places) { places.ProviderURL = "ftp://nominatim.example.com" },
		"credentials in url":  func(places *Places) { places.ProviderURL = "https://user:secret@example.com" },
		"plain http for maps": func(places *Places) { places.MapStyleURL = "http://tiles.example.com/style.json" },
	} {
		configuration := validConfiguration()
		change(&configuration.Places)
		if err := configuration.Validate(); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}
