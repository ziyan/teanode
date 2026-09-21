package computer

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func isRepository(path string) bool {
	information, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && (information.IsDir() || information.Mode().IsRegular())
}

func git(ctx context.Context, directory string, arguments ...string) (string, error) {
	callContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(callContext, "git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// trackedFiles is what git tracks, plus what is new and not ignored.
func trackedFiles(ctx context.Context, directory string) ([]string, error) {
	output, err := git(ctx, directory, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, path := range strings.Split(output, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	// What is new and not ignored: work in progress is what somebody is
	// most likely to ask about.
	//
	// Git collapses a new directory to its own name, `notes/`, and that
	// is not a file: offered as one it came back refused, "cannot be
	// read", once per directory. It is how a checkout inside this one
	// appears here as well, and that one is offered under the same
	// identifier as the checkout it is -- so a name ending in a slash is
	// a directory and is left to the walk.
	if status, err := git(ctx, directory, "status", "--porcelain", "-z", "--untracked-files=normal"); err == nil {
		for _, line := range strings.Split(status, "\x00") {
			if len(line) > 3 && strings.HasPrefix(line, "?? ") && !strings.HasSuffix(line, "/") {
				paths = append(paths, line[3:])
			}
		}
	}
	return paths, nil
}

// repositoryProfile is what git says about a checkout, in one pass.
//
// The checkout's files are handed in rather than asked for again: the
// caller has just listed them, git is what a walk over a tree of
// checkouts spends its time on, and this used to double the bill.
func repositoryProfile(ctx context.Context, directory string, tracked []string) *RepositoryProfile {
	profile := &RepositoryProfile{Languages: map[string]int{}}
	if head, err := git(ctx, directory, "rev-parse", "HEAD"); err == nil {
		profile.Head = strings.TrimSpace(head)
	}
	if branch, err := git(ctx, directory, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		profile.DefaultBranch = strings.TrimSpace(branch)
	}
	if tag, err := git(ctx, directory, "describe", "--tags", "--abbrev=0"); err == nil {
		profile.NewestTag = strings.TrimSpace(tag)
	}
	if remotes, err := git(ctx, directory, "remote", "-v"); err == nil {
		seen := map[string]bool{}
		for _, line := range strings.Split(remotes, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && !seen[fields[1]] {
				seen[fields[1]] = true
				profile.Remotes = append(profile.Remotes, fields[1])
			}
		}
	}
	if status, err := git(ctx, directory, "status", "--porcelain"); err == nil {
		profile.Dirty = strings.TrimSpace(status) != ""
	}
	for _, name := range []string{"README.md", "README.rst", "README.txt", "README"} {
		if content, err := os.ReadFile(filepath.Join(directory, name)); err == nil {
			text := strings.TrimSpace(string(content))
			profile.Readme = firstRunes(text, 4000)
			profile.Description = readmeDescription(text)
			break
		}
	}
	profile.Module = manifestName(directory)
	top := map[string]bool{}
	for _, path := range tracked {
		if extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."); extension != "" {
			profile.Languages[extension]++
		}
		// The first segment of a path that has one is a top-level
		// directory; a dotted one is tooling, not a module.
		if first, _, found := strings.Cut(filepath.ToSlash(path), "/"); found && !strings.HasPrefix(first, ".") {
			top[first] = true
		}
	}
	for name := range top {
		profile.Directories = append(profile.Directories, name)
	}
	sort.Strings(profile.Directories)
	// Everyone who has committed, with how much and when. Which of them
	// the person actually worked with is the server's judgment; this is
	// the evidence for it.
	if log, err := git(ctx, directory, "log", "--no-merges", "--format=%aN%x09%aE%x09%ad", "--date=short"); err == nil {
		byAddress := map[string]*ScanAuthor{}
		for _, line := range strings.Split(log, "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) < 3 {
				continue
			}
			when, err := time.Parse("2006-01-02", fields[2])
			if err != nil {
				continue
			}
			profile.Commits++
			if profile.First == nil || when.Before(*profile.First) {
				first := when
				profile.First = &first
			}
			if profile.Last == nil || when.After(*profile.Last) {
				last := when
				profile.Last = &last
			}
			address := strings.ToLower(fields[1])
			author := byAddress[address]
			if author == nil {
				author = &ScanAuthor{Name: fields[0], Address: address}
				byAddress[address] = author
			}
			author.Commits++
			if author.First == nil || when.Before(*author.First) {
				first := when
				author.First = &first
			}
			if author.Last == nil || when.After(*author.Last) {
				last := when
				author.Last = &last
			}
		}
		profile.Contributors = len(byAddress)
		for _, author := range byAddress {
			profile.Authors = append(profile.Authors, *author)
		}
		sort.Slice(profile.Authors, func(left, right int) bool {
			return profile.Authors[left].Commits > profile.Authors[right].Commits
		})
		if len(profile.Authors) > 200 {
			profile.Authors = profile.Authors[:200]
		}
	}
	return profile
}

// firstRunes is the first n characters of a string, never a partial
// character: a cut that lands inside one is a byte sequence PostgreSQL
// refuses.
func firstRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

// readmeDescription is what a README says the thing is: the first
// paragraph that is prose rather than a heading, a badge, a code block or
// a list, with the inline markdown taken off.
//
// It is the opening of the project's page, so it has to read as a
// sentence about the thing. A README that opens with six badges and an
// install command has its description further down, and one that is all
// badges and commands has none -- which is the right answer for it.
func readmeDescription(text string) string {
	inCode := false
	for _, block := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		// A fence opens or closes code. A block holding an odd number of
		// them changes state; one holding a whole fenced snippet does
		// not, and either way a block that touches a fence is not prose.
		fences := strings.Count(block, "```") + strings.Count(block, "~~~")
		if fences%2 == 1 {
			inCode = !inCode
		}
		if fences > 0 || inCode {
			continue
		}
		first := block[0]
		switch {
		case first == '#', first == '!', first == '[', first == '<', first == '|', first == '-', first == '*', first == '>':
			continue // a heading, a badge, a link line, html, a table, a list, a quote
		case first >= '0' && first <= '9' && strings.Contains(block[:min(len(block), 4)], "."):
			continue // a numbered list
		case strings.HasPrefix(block, "    ") || strings.HasPrefix(block, "\t"):
			continue // indented code
		}
		line := strings.Join(strings.Fields(block), " ")
		line = stripInlineMarkdown(line)
		if len([]rune(line)) < 24 {
			continue // "Usage" on its own is not a description
		}
		return firstRunes(line, 600)
	}
	return ""
}

// stripInlineMarkdown takes the emphasis, links and code marks off a
// line of markdown and leaves the words.
func stripInlineMarkdown(line string) string {
	// [text](url) -> text, then the rest are single characters to drop.
	for {
		open := strings.Index(line, "[")
		if open < 0 {
			break
		}
		close := strings.Index(line[open:], "](")
		if close < 0 {
			break
		}
		end := strings.Index(line[open+close:], ")")
		if end < 0 {
			break
		}
		line = line[:open] + line[open+1:open+close] + line[open+close+end+1:]
	}
	return strings.NewReplacer("**", "", "__", "", "`", "", "*", "", "_", " ").Replace(line)
}

// manifestName is what a checkout calls itself, from whichever manifest
// it has. Empty when it has none or the name cannot be read.
func manifestName(directory string) string {
	if content, err := os.ReadFile(filepath.Join(directory, "go.mod")); err == nil {
		for _, line := range strings.Split(string(content), "\n") {
			if module, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
				return strings.TrimSpace(module)
			}
		}
	}
	if content, err := os.ReadFile(filepath.Join(directory, "package.json")); err == nil {
		var manifest struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(content, &manifest) == nil {
			return manifest.Name
		}
	}
	for _, name := range []string{"pyproject.toml", "Cargo.toml"} {
		if content, err := os.ReadFile(filepath.Join(directory, name)); err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				if value, found := strings.CutPrefix(strings.TrimSpace(line), "name"); found {
					value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "="))
					return strings.Trim(value, "\"'")
				}
			}
		}
	}
	return ""
}
