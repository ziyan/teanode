package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/util/atomicfile"
)

// The small files `teanode passwords` keeps on this computer: the keys
// `unlock` leaves open for a while, and the public keys of the people the
// person shares with. Both are named for the profile, so that two servers are
// not confused, and both are private to their owner.

// passwordsFileMostBytes bounds what is read back from one of them.
const passwordsFileMostBytes = 1 << 20

// passwordsProfile names the profile this command talks to and gives its server.
func passwordsProfile(command *cli.Command) (name, server string, err error) {
	resolved, err := resolveCommandTarget(command)
	if err != nil {
		return "", "", err
	}
	name = resolved.Profile
	server = resolved.URL
	if resolved.Local {
		name, server = LocalProfileName, LocalProfileName
	}
	if name == "" {
		name = strings.TrimPrefix(strings.TrimPrefix(resolved.URL, "https://"), "http://")
	}
	return unlockFileUnsafeCharacters.ReplaceAllString(name, "_"), server, nil
}

// passwordsConfigurationDirectory is the directory of the profiles file, where
// what must outlast a login session is kept.
func passwordsConfigurationDirectory() (string, error) {
	profilesPath, err := ProfilesPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(profilesPath), nil
}

// passwordsRuntimeDirectory is $XDG_RUNTIME_DIR when it is set to a directory
// that exists. It is made for what must not outlive a login session or reach
// a backup: private to the person and emptied at logout.
func passwordsRuntimeDirectory() string {
	directory := os.Getenv("XDG_RUNTIME_DIR")
	if directory == "" || !filepath.IsAbs(directory) {
		return ""
	}
	if information, err := os.Stat(directory); err != nil || !information.IsDir() {
		return ""
	}
	return directory
}

// readPrivateFile reads a file that only its owner may have been able to
// write, and refuses one that is not: a link, a file anyone else can read or
// change, or one that belongs to somebody else. A file that is not there is
// os.ErrNotExist.
func readPrivateFile(path string) ([]byte, error) {
	file, err := openPrivateFile(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()
	// The file that was opened, not the name: nothing can be swapped in between.
	information, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !information.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a plain file; remove it with 'teanode passwords lock' and try again", path)
	}
	if err := checkPrivateFile(path, information); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(file, passwordsFileMostBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > passwordsFileMostBytes {
		return nil, fmt.Errorf("%s is too large to be one of ours; remove it and try again", path)
	}
	return content, nil
}

// writePrivateFile replaces a file with the content, created readable by its
// owner only from the start, and leaves nothing behind but the file: a failure
// at any step removes the temporary file it was written to.
func writePrivateFile(path string, content []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	file, err := atomicfile.CreateWithMode(path, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = atomicfile.Discard(file)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := atomicfile.CommitAs(file, path); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// removeFile removes a file and says whether there was one.
func removeFile(path string) (bool, error) {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot remove %s: %w", path, err)
	}
	return true, nil
}
