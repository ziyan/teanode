package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// Repository is where releases are looked for, as "owner/name".
//
// Compiled in rather than configured. A mail server that can be pointed at
// somebody else's builds is a mail server whose dashboard session is worth
// stealing for that alone; this way the worst a stolen session can do is
// install a release of this program.
const Repository = "ziyan/teanode"

// releaseEndpoint is GitHub's release endpoint. Public releases need no credential, and
// none is ever sent: this request says which program is asking and nothing
// about the deployment asking it.
const releaseEndpoint = "https://api.github.com/repos/%s/releases/latest"

// checksumsAsset is the file every release carries listing the SHA-256 of the
// binaries beside it.
const checksumsAsset = "SHA256SUMS"

// release is the part of GitHub's answer this needs.
type release struct {
	Tag    string `json:"tag_name"`
	Name   string `json:"name"`
	Notes  string `json:"body"`
	Draft  bool   `json:"draft"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// assetName is what the binary for this machine is called in a release, and
// matches the names .github/scripts/release.sh writes.
func assetName() string {
	return fmt.Sprintf("teanode-%s-%s", runtime.GOOS, runtime.GOARCH)
}

// assetURL finds one asset by name.
func (self *release) assetURL(name string) string {
	for _, asset := range self.Assets {
		if asset.Name == name {
			return asset.URL
		}
	}
	return ""
}

// version is the release's version without the leading "v".
func (self *release) version() string {
	return strings.TrimPrefix(strings.TrimSpace(self.Tag), "v")
}

// latestRelease asks what the newest release is.
//
// The timeout is short and the body is capped: this runs on a schedule on a
// machine whose job is mail, and an endpoint that hangs must not become a
// goroutine that stays.
func latestRelease(ctx context.Context, client *http.Client, endpoint string) (*release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "teanode")

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("upgrade: cannot reach the release list: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upgrade: the release list answered %s", response.Status)
	}

	// A release description is prose and a few kilobytes; anything of this
	// size is not an answer to this question.
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("upgrade: cannot read the release list: %w", err)
	}

	var found release
	if err := json.Unmarshal(body, &found); err != nil {
		return nil, fmt.Errorf("upgrade: cannot read the release list: %w", err)
	}
	if found.Draft || found.version() == "" {
		return nil, fmt.Errorf("upgrade: the newest release has no version")
	}
	return &found, nil
}
