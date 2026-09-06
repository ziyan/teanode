package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// useTemporaryProfiles points the profiles file at a directory that belongs to
// this test, so nothing here reads or writes the developer's own.
func useTemporaryProfiles(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", directory)
	return filepath.Join(directory, "teanode", "profiles.json")
}

func TestProfilesRoundTrip(t *testing.T) {
	path := useTemporaryProfiles(t)

	profiles, err := LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles on a missing file: %s", err)
	}
	if len(profiles.Profiles) != 0 || profiles.Active != "" {
		t.Fatalf("a missing file should be empty, got %+v", profiles)
	}

	profiles.Set(&Profile{Name: "mail.example.com", URL: "https://mail.example.com", Token: "tnt_one", TokenID: "01A", Username: "ziyan"})
	profiles.Set(&Profile{Name: "staging", URL: "https://staging.example.com", Token: "tnt_two", Insecure: true})
	if profiles.Active != "staging" {
		t.Errorf("the last profile signed in to should be active, got %q", profiles.Active)
	}
	if err := profiles.Save(); err != nil {
		t.Fatalf("Save: %s", err)
	}

	// The file holds tokens, so nobody else on the machine may read it.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("the file was not written at %s: %s", path, err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("profiles.json is mode %o, want 0600", mode)
		}
		directoryInfo, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if mode := directoryInfo.Mode().Perm(); mode != 0o700 {
			t.Errorf("the directory is mode %o, want 0700", mode)
		}
	}

	loaded, err := LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles: %s", err)
	}
	if loaded.Active != "staging" {
		t.Errorf("active = %q, want staging", loaded.Active)
	}
	production := loaded.Find("mail.example.com")
	if production == nil || production.Token != "tnt_one" || production.TokenID != "01A" || production.Username != "ziyan" {
		t.Errorf("the production profile did not survive the round trip: %+v", production)
	}
	if staging := loaded.Find("staging"); staging == nil || !staging.Insecure || staging.Name != "staging" {
		t.Errorf("the staging profile did not survive the round trip: %+v", staging)
	}

	// Found by address too, however the address was spelled.
	if found := loaded.FindByURL("mail.example.com/"); found == nil || found.Name != "mail.example.com" {
		t.Errorf("FindByURL did not normalise: %+v", found)
	}

	// Removing the active profile promotes another rather than leaving the
	// client with profiles and none of them chosen.
	loaded.Remove("staging")
	if loaded.Active != "mail.example.com" {
		t.Errorf("after removing the active profile, active = %q", loaded.Active)
	}
	loaded.Remove("mail.example.com")
	if loaded.Active != "" || len(loaded.Profiles) != 0 {
		t.Errorf("after removing everything: %+v", loaded)
	}
}

func TestLoadProfilesRejectsGarbage(t *testing.T) {
	path := useTemporaryProfiles(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfiles(); err == nil {
		t.Error("a file that is not JSON should be an error, not an empty set of profiles")
	}
}

func TestResolveTarget(t *testing.T) {
	useTemporaryProfiles(t)
	profiles, _ := LoadProfiles()
	profiles.Set(&Profile{Name: "production", URL: "https://mail.example.com", Token: "tnt_production", ReadOnly: true})
	profiles.Set(&Profile{Name: "staging", URL: "https://staging.example.com", Token: "tnt_staging", Insecure: true})
	if err := profiles.Save(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		url, token  string
		profile     string
		insecure    bool
		wantURL     string
		wantToken   string
		wantLocal   bool
		wantProfile string
		wantError   bool
	}{
		{name: "url and token", url: "https://other.example.com", token: "tnt_x", wantURL: "https://other.example.com", wantToken: "tnt_x"},
		{name: "url without scheme", url: "other.example.com", token: "tnt_x", wantURL: "https://other.example.com", wantToken: "tnt_x"},
		{name: "url borrows the profile's token", url: "https://mail.example.com", wantURL: "https://mail.example.com", wantToken: "tnt_production", wantProfile: "production"},
		{name: "url with no token anywhere", url: "https://nowhere.example.com", wantError: true},
		{name: "token without url", token: "tnt_x", wantError: true},
		{name: "named profile", profile: "production", wantURL: "https://mail.example.com", wantToken: "tnt_production", wantProfile: "production"},
		{name: "unknown profile", profile: "nope", wantError: true},
		{name: "active profile", wantURL: "https://staging.example.com", wantToken: "tnt_staging", wantProfile: "staging"},
		{name: "the console by name", profile: "local", wantLocal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := resolveTarget(test.url, test.token, test.profile, test.insecure, false)
			if test.wantError {
				if err == nil {
					t.Fatalf("expected an error, got %+v", resolved)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTarget: %s", err)
			}
			if resolved.URL != test.wantURL || resolved.Token != test.wantToken || resolved.Local != test.wantLocal || resolved.Profile != test.wantProfile {
				t.Errorf("got %+v", resolved)
			}
		})
	}

	// The staging profile skips certificate checks; --url to it inherits that.
	resolved, err := resolveTarget("https://staging.example.com", "", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Insecure {
		t.Error("a profile's insecure flag should carry over to --url")
	}

	// A read-only profile makes a read-only target, whether named or reached
	// by --url; the global flag makes any target read-only; nothing turns it
	// off from a flag.
	for _, readOnly := range []struct {
		url, token, profile string
		flag                bool
		want                bool
	}{
		{profile: "production", want: true},
		{url: "https://mail.example.com", want: true},
		// An explicit token is a credential of its own, not the profile's.
		{url: "https://mail.example.com", token: "tnt_x", want: false},
		{profile: "staging", want: false},
		{profile: "staging", flag: true, want: true},
		{profile: "local", flag: true, want: true},
		{url: "https://other.example.com", token: "tnt_x", flag: true, want: true},
	} {
		resolved, err := resolveTarget(readOnly.url, readOnly.token, readOnly.profile, false, readOnly.flag)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.ReadOnly != readOnly.want {
			t.Errorf("%+v: read-only %v, want %v", readOnly, resolved.ReadOnly, readOnly.want)
		}
	}

	// With no profiles at all, the console is the default.
	if err := os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode", "profiles.json")); err != nil {
		t.Fatal(err)
	}
	resolved, err = resolveTarget("", "", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Local {
		t.Errorf("with nothing saved the console should be the target, got %+v", resolved)
	}
}

func TestHostOf(t *testing.T) {
	for url, want := range map[string]string{
		"https://mail.example.com":       "mail.example.com",
		"mail.example.com":               "mail.example.com",
		"http://127.0.0.1:10081/":        "127.0.0.1",
		"https://mail.example.com/path?": "mail.example.com",
	} {
		if got := hostOf(url); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", url, got, want)
		}
	}
}
