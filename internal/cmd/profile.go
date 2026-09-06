package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/util/atomicfile"
)

// A profile is a server the client has signed in to: where it is and the
// token that acts as somebody there. One is active; the rest are reached with
// --profile. They live in one file under the user's configuration directory,
// readable only by them, because the tokens are the whole of what a token
// holder can do.

// LocalProfileName is the reserved name for the server this environment
// points at, reached over loopback with a token minted from the stored
// secret rather than with a saved one. It is never written to the file.
const LocalProfileName = "local"

// Profile is one saved server.
type Profile struct {
	// Name is how --profile names it; the server's host name by default.
	Name string `json:"name"`

	// URL of the server, normalised the way the client normalises it.
	URL string `json:"url"`

	// Token sent as "Authorization: Bearer".
	Token string `json:"token,omitempty"`

	// TokenID is the identifier of that token on the server, so that logging
	// out can revoke it. Empty for a token pasted in by hand, whose id the
	// client was never told.
	TokenID string `json:"tokenId,omitempty"`

	// Username the token acts as, for display. The token is the credential.
	Username string `json:"username,omitempty"`

	// Insecure skips verifying the server's certificate. Off unless
	// "auth login --insecure" said so.
	Insecure bool `json:"insecure,omitempty"`

	// ReadOnly refuses every mutation through this profile before it is
	// sent. For a profile handed to a script or an agent that should be able
	// to look but not change. Off unless "auth login --read-only" or "auth
	// set-read-only" said so.
	ReadOnly bool `json:"readOnly,omitempty"`
}

// Profiles is the file.
type Profiles struct {
	Active   string              `json:"active,omitempty"`
	Profiles map[string]*Profile `json:"profiles"`
}

// ProfilesPath is where the file lives: $XDG_CONFIG_HOME/teanode/profiles.json,
// or ~/.config/teanode/profiles.json.
func ProfilesPath() (string, error) {
	directory := os.Getenv("XDG_CONFIG_HOME")
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot find the home directory: %w", err)
		}
		directory = filepath.Join(home, ".config")
	}
	return filepath.Join(directory, "teanode", "profiles.json"), nil
}

// LoadProfiles reads the file. A missing file is an empty set of profiles,
// which is the state before the first "auth login".
func LoadProfiles() (*Profiles, error) {
	profiles := &Profiles{Profiles: map[string]*Profile{}}
	path, err := ProfilesPath()
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return profiles, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if err := json.Unmarshal(content, profiles); err != nil {
		return nil, fmt.Errorf("%s is not a profiles file: %w", path, err)
	}
	if profiles.Profiles == nil {
		profiles.Profiles = map[string]*Profile{}
	}
	for name, profile := range profiles.Profiles {
		if profile == nil {
			delete(profiles.Profiles, name)
			continue
		}
		profile.Name = name
	}
	return profiles, nil
}

// Save writes the file, readable by its owner only, atomically: an
// interrupted write leaves the previous file rather than half of a new one.
func (self *Profiles) Save() error {
	path, err := ProfilesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}

	content, err := json.MarshalIndent(self, "", "  ")
	if err != nil {
		return err
	}
	file, err := atomicfile.Create(path)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	defer func() {
		_ = atomicfile.Discard(file)
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if _, err := file.Write(append(content, '\n')); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := atomicfile.Commit(file); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// Find returns the profile with this name, or nil.
func (self *Profiles) Find(name string) *Profile {
	if self == nil {
		return nil
	}
	return self.Profiles[name]
}

// FindByURL returns the profile for this server, or nil. Compared after
// normalising, so that "mail.example.com" finds "https://mail.example.com".
func (self *Profiles) FindByURL(url string) *Profile {
	if self == nil {
		return nil
	}
	wanted := client.NormalizeURL(url)
	for _, name := range self.Names() {
		if strings.EqualFold(self.Profiles[name].URL, wanted) {
			return self.Profiles[name]
		}
	}
	return nil
}

// Set stores a profile under its name and makes it the active one. A login
// is an act of choosing a server, so the one just signed in to is the one
// the next command should talk to.
func (self *Profiles) Set(profile *Profile) {
	self.Profiles[profile.Name] = profile
	self.Active = profile.Name
}

// Remove forgets a profile. When it was the active one, the alphabetically
// first of the rest becomes active, so that a client with one profile left
// does not need to be told to switch to it.
func (self *Profiles) Remove(name string) {
	delete(self.Profiles, name)
	if self.Active == name {
		self.Active = ""
		if names := self.Names(); len(names) > 0 {
			self.Active = names[0]
		}
	}
}

// Names lists the profiles in a stable order.
func (self *Profiles) Names() []string {
	names := make([]string, 0, len(self.Profiles))
	for name := range self.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
