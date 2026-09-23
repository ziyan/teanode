package computer

import (
	"fmt"
	"path/filepath"
	"strings"
)

// scanRoot is the directory a scan asked for, as an absolute path.
//
// There is no list of directories a scan may reach. The agent already runs
// commands and reads files on this computer as the person, so a list the
// person had to fill by hand kept nothing from it; it only stopped a scan
// the person had asked for until they typed a command.
func scanRoot(options *Options, asked string) (string, error) {
	return resolveIn(options, asked)
}

// scanFile is a file a scan reads, where it really is once every link on
// the way to it has been followed.
func scanFile(options *Options, path string) (string, error) {
	resolved, err := resolveIn(options, path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(resolved)
}

// resolveIn is a path of the person's as an absolute one, and an error
// where it is empty: a scan of "" would be a scan of their home.
func resolveIn(options *Options, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("which directory?")
	}
	return resolve(options.Home, path), nil
}
