package sources

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// A type can read files on the computer it runs on: a tool that keeps its
// own copy of a service -- an archive of a chat server, an export of a
// wiki -- is brought up to date by a refresh command, and the type reads
// the copy. Only a type that runs on a computer may, and the files are the
// person's own, read as them.

// expandHome turns a leading ~ into the person's home directory, since a
// command is not run through a shell that would.
func expandHome(value string) string {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return value
	}
	return filepath.Join(home, strings.TrimPrefix(value, "~"))
}

// matchFiles is every file under a directory whose path relative to it
// matches the pattern, in order of path, each as the item a template
// reads: path, name, stem, directory, absolute, bytes and modifiedAt.
func matchFiles(ctx context.Context, directory, pattern string) ([]map[string]any, error) {
	directory = expandHome(directory)
	if climbs(directory) {
		return nil, fmt.Errorf("%s has a .. in it, which steps out of its directory", directory)
	}
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("files names no directory")
	}
	if pattern == "" {
		pattern = "**"
	}
	wanted := strings.Split(pattern, "/")
	var found []map[string]any
	err := filepath.WalkDir(directory, func(each string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if each == directory {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(directory, each)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if !couldMatch(wanted, strings.Split(relative, "/")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !matchSegments(wanted, strings.Split(relative, "/")) {
			return nil
		}
		information, err := entry.Info()
		if err != nil {
			return nil
		}
		found = append(found, fileItem(directory, relative, information))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func fileItem(directory, relative string, information fs.FileInfo) map[string]any {
	name := path.Base(relative)
	folder := path.Dir(relative)
	if folder == "." {
		folder = ""
	}
	return map[string]any{
		"path": relative, "name": name, "stem": strings.TrimSuffix(name, path.Ext(name)),
		"directory": folder, "absolute": filepath.Join(directory, filepath.FromSlash(relative)),
		"bytes": information.Size(), "modifiedAt": information.ModTime().Format(time.RFC3339Nano),
	}
}

// matchSegments matches a path to a pattern one name at a time, ** being
// any number of names, none included.
func matchSegments(pattern, names []string) bool {
	if len(pattern) == 0 {
		return len(names) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(names); skip++ {
			if matchSegments(pattern[1:], names[skip:]) {
				return true
			}
		}
		return false
	}
	if len(names) == 0 {
		return false
	}
	if matched, err := path.Match(pattern[0], names[0]); err != nil || !matched {
		return false
	}
	return matchSegments(pattern[1:], names[1:])
}

// couldMatch says whether a directory could hold a file the pattern
// matches, so a walk does not go down a tree that cannot.
func couldMatch(pattern, names []string) bool {
	for index, name := range names {
		if index >= len(pattern) {
			return false
		}
		if pattern[index] == "**" {
			return true
		}
		if matched, err := path.Match(pattern[index], name); err != nil || !matched {
			return false
		}
	}
	return true
}

// readFile reads one file and parses it. A file that is not there is
// nothing to read where the reading says so.
func (self *Runner) readFile(ctx context.Context, file string, shape Parsing, missing string) ([]fetchedItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if climbs(file) {
		return nil, fmt.Errorf("%s has a .. in it, which steps out of its directory", file)
	}
	content, err := os.ReadFile(expandHome(file))
	if err != nil {
		if os.IsNotExist(err) && missing == "empty" {
			return nil, nil
		}
		return nil, err
	}
	result, err := parseOutput(shape, content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	fetched := make([]fetchedItem, 0, len(result.items))
	for _, item := range result.items {
		fetched = append(fetched, fetchedItem{item: item, response: result.response})
	}
	return fetched, nil
}

// loadLookups reads every lookup table once. A file that is not there is
// an empty table: an archive that keeps no attachments has no list of
// them.
func (self *Runner) loadLookups(ctx context.Context) error {
	if self.lookups != nil || len(self.Type.Lookups) == 0 {
		return nil
	}
	base := Scope{Values: map[string]any{"settings": self.settings()}, Secrets: self.Secrets}
	tables := map[string]any{}
	for _, name := range sortedKeys(self.Type.Lookups) {
		lookup := self.Type.Lookups[name]
		var items []map[string]any
		if lookup.Files != nil {
			directory, err := self.render(lookup.Files.In, base)
			if err != nil {
				return err
			}
			if _, err := os.Stat(expandHome(directory)); err == nil {
				if items, err = matchFiles(ctx, directory, lookup.Files.Match); err != nil {
					return err
				}
			}
		} else {
			file, err := self.render(lookup.File, base)
			if err != nil {
				return err
			}
			fetched, err := self.readFile(ctx, file, lookup.Parse, "empty")
			if err != nil {
				return fmt.Errorf("the lookup %s: %w", name, err)
			}
			for _, each := range fetched {
				items = append(items, each.item)
			}
		}
		table := map[string]any{}
		for _, item := range items {
			itemScope := base.with("item", item)
			key, err := self.render(lookup.Key, itemScope)
			if err != nil {
				return err
			}
			if key == "" {
				continue
			}
			var value any = item
			if lookup.Value != "" {
				if value, err = self.value(lookup.Value, itemScope); err != nil {
					return err
				}
			}
			if lookup.Many {
				existing, _ := table[key].([]any)
				table[key] = append(existing, value)
			} else {
				table[key] = value
			}
		}
		tables[name] = table
	}
	self.lookups = tables
	return nil
}

// refresh runs the type's refresh commands.
func (self *Runner) refresh(ctx context.Context, scope Scope) error {
	for _, refresh := range self.Type.Refresh {
		if refresh.When != "" {
			held, err := self.holds(refresh.When, scope)
			if err != nil {
				return err
			}
			if !held {
				continue
			}
		}
		words, err := self.words(refresh.Command, scope)
		if err != nil {
			return err
		}
		if _, err := self.Executor.Command(ctx, words); err != nil {
			return fmt.Errorf("bringing the copy up to date: %w", err)
		}
	}
	return nil
}

// climbs says whether a path has a .. in it: a path a type builds from what
// a tool answered must stay where the type pointed.
func climbs(path string) bool {
	for _, step := range strings.Split(filepath.ToSlash(path), "/") {
		if step == ".." {
			return true
		}
	}
	return false
}
