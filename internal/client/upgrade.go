package client

import (
	"context"
	"time"
)

// Upgrade is what version is running, what has been released since, and
// whether anything can be done about the difference from here.
type Upgrade struct {
	Current     string     `json:"current"`
	Latest      string     `json:"latest"`
	Available   bool       `json:"available"`
	Notes       string     `json:"notes"`
	URL         string     `json:"url"`
	CheckedAt   *time.Time `json:"checkedAt"`
	AttemptedAt *time.Time `json:"attemptedAt"`
	Checking    bool       `json:"checking"`
	Error       string     `json:"error"`
	Applicable  bool       `json:"applicable"`
	Reason      string     `json:"reason"`
	Automatic   bool       `json:"automatic"`
	Upgrading   bool       `json:"upgrading"`
	Enabled     bool       `json:"enabled"`
	Window      string     `json:"window"`
	CheckError  string     `json:"checkError"`
}

const upgradeFields = `{
	current latest available notes url checkedAt attemptedAt checking error
	applicable reason automatic upgrading enabled window checkError
}`

// GetUpgrade returns the upgrade state, asking the release list again first
// when check is set.
func GetUpgrade(ctx context.Context, connection *Client, check bool) (*Upgrade, error) {
	var result struct {
		GetUpgrade *Upgrade `json:"GetUpgrade"`
	}
	query := `query ($check: Boolean) { GetUpgrade(check: $check) ` + upgradeFields + ` }`
	variables := map[string]any{}
	if check {
		variables["check"] = true
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.GetUpgrade, nil
}

// ApplyUpgrade installs a release: the newest, or the version named. It
// answers as soon as the upgrade has started.
func ApplyUpgrade(ctx context.Context, connection *Client, version string) (*Upgrade, error) {
	var result struct {
		ApplyUpgrade *Upgrade `json:"ApplyUpgrade"`
	}
	query := `mutation ($version: String) { ApplyUpgrade(version: $version) ` + upgradeFields + ` }`
	variables := map[string]any{}
	if version != "" {
		variables["version"] = version
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ApplyUpgrade, nil
}
