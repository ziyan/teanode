package computer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// allowedRoots is what this program will scan, read from its own
// configuration. Empty means nothing: a server that asks before the
// person has allowed anything gets a refusal, not the whole disk.
type allowedRoots struct {
	Roots []string `json:"roots"`
}

// allowedRoot resolves what the server asked for and refuses anything the
// person has not allowed on this machine.
//
// This is the guard that lets a scan run with nobody watching. Every
// other action of this program happens while the person is in the
// conversation and confirms what matters; a scan happens at three in the
// morning, so what it may reach is settled here rather than by whoever is
// on the other end of the socket.
func allowedRoot(options *Options, asked string) (string, error) {
	resolved, err := resolveIn(options, asked)
	if err != nil {
		return "", err
	}
	roots, err := scanRoots(options)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		return "", &RefusedError{Reason: "this computer has no directories allowed for scanning; run `teanode computer allow <path>` to add one"}
	}
	for _, root := range roots {
		if resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", &RefusedError{Reason: fmt.Sprintf("%s is not one of the directories allowed for scanning on this computer", asked)}
}

// allowedFile is a file this program may read for a scan: inside a
// directory the person allowed, and still inside one once every link on
// the way to it has been followed. It answers where the file really is.
//
// Following the links is the point. A record may name any path on the
// machine, and a folder of records is often a folder somebody else's
// program filled, so a link dropped in it must not carry a scan into
// ~/.ssh. The allowed roots are followed too, because a root may itself
// be reached through one -- /tmp on a Mac is a link to /private/tmp --
// and comparing a followed file against an unfollowed root would refuse
// everything under it.
func allowedFile(options *Options, path string) (string, error) {
	resolved, err := allowedRoot(options, path)
	if err != nil {
		return "", err
	}
	followed, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	if followed == resolved {
		return followed, nil
	}
	if _, err := allowedRoot(options, followed); err == nil {
		return followed, nil
	}
	roots, err := scanRoots(options)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		root, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if followed == root || strings.HasPrefix(followed, root+string(filepath.Separator)) {
			return followed, nil
		}
	}
	return "", &RefusedError{Reason: fmt.Sprintf("%s leads to %s, which is not one of the directories allowed for scanning on this computer", path, followed)}
}

// resolveIn is a path of the person's as an absolute one, and an error
// where it is empty: a scan of "" would be a scan of their home.
func resolveIn(options *Options, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("which directory?")
	}
	return resolve(options.Home, path), nil
}

// scanRootsFile is where the allowed roots are kept, beside the profile
// the person signed in with.
func scanRootsFile(options *Options) string {
	if options.ScanRootsFile != "" {
		return options.ScanRootsFile
	}
	return filepath.Join(options.Home, ".config", "teanode", "scan-roots.json")
}

// scanRoots is what the person has allowed, cleaned and absolute.
func scanRoots(options *Options) ([]string, error) {
	content, err := os.ReadFile(scanRootsFile(options))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var allowed allowedRoots
	if err := json.Unmarshal(content, &allowed); err != nil {
		return nil, fmt.Errorf("the list of directories allowed for scanning is not readable: %w", err)
	}
	roots := make([]string, 0, len(allowed.Roots))
	for _, root := range allowed.Roots {
		resolved, err := resolveIn(options, root)
		if err != nil {
			continue
		}
		roots = append(roots, resolved)
	}
	return roots, nil
}

// AllowScanRoot adds a directory to what this computer will scan, and is
// idempotent. Run by the person on their own machine.
func AllowScanRoot(options *Options, path string) (string, error) {
	filled := withDefaults(options)
	resolved, err := resolveIn(filled, path)
	if err != nil {
		return "", err
	}
	information, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !information.IsDir() {
		return "", fmt.Errorf("%s is not a directory", resolved)
	}
	roots, err := scanRoots(filled)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		if root == resolved {
			return resolved, nil
		}
	}
	roots = append(roots, resolved)
	sort.Strings(roots)
	content, err := json.MarshalIndent(allowedRoots{Roots: roots}, "", "  ")
	if err != nil {
		return "", err
	}
	file := scanRootsFile(filled)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, content, 0o600); err != nil {
		return "", err
	}
	return resolved, nil
}

// ForgetScanRoot removes one.
func ForgetScanRoot(options *Options, path string) error {
	filled := withDefaults(options)
	resolved, err := resolveIn(filled, path)
	if err != nil {
		return err
	}
	roots, err := scanRoots(filled)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(roots))
	for _, root := range roots {
		if root != resolved {
			kept = append(kept, root)
		}
	}
	content, err := json.MarshalIndent(allowedRoots{Roots: kept}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(scanRootsFile(filled), content, 0o600)
}

// ListScanRoots is what this computer will scan.
func ListScanRoots(options *Options) ([]string, error) {
	return scanRoots(withDefaults(options))
}
