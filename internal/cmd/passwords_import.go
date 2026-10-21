package cmd

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/passwords"
)

// Import and export of Passwords: the pass password store, CSV exports of other
// managers, and a file of everything in the clear.

// passwordsGPGProgram is the program that decrypts the files of a pass store;
// a test replaces it. It is run once for each file, without a shell.
var passwordsGPGProgram = "gpg"

// passwordsImportCounts is what an import did, and says nothing about what the
// items hold.
type passwordsImportCounts struct {
	// FoundCount is how many items the source had.
	FoundCount int `json:"foundCount"`
	// ImportedCount is how many were saved, or with a dry run would be.
	ImportedCount int `json:"importedCount"`
	// SkippedCount is how many the person already had.
	SkippedCount int `json:"skippedCount"`
	// SkippedNames are the names of those, so that a skipped item is not a
	// silent loss: a name is not a secret.
	SkippedNames []string `json:"skippedNames"`
	// FailedCount is how many could not be read or saved.
	FailedCount int `json:"failedCount"`
	// LeftOutNames are items the importer does not take, such as a card in a
	// Bitwarden export.
	LeftOutNames []string `json:"leftOutNames"`
	IsDryRun     bool     `json:"isDryRun"`
}

func (self *passwordsImportCounts) print(command *cli.Command) error {
	if self.LeftOutNames == nil {
		self.LeftOutNames = []string{}
	}
	if self.SkippedNames == nil {
		self.SkippedNames = []string{}
	}
	if command.Bool("json") {
		return PrintJSON(self)
	}
	verb := "imported"
	if self.IsDryRun {
		verb = "would import"
	}
	fmt.Printf("found %s: %s %d, already there %d, failed %d\n", countWords(self.FoundCount, "item"), verb, self.ImportedCount, self.SkippedCount, self.FailedCount)
	if len(self.SkippedNames) > 0 {
		fmt.Printf("already there, so skipped: %s\n", forTerminal(strings.Join(self.SkippedNames, ", ")))
	}
	if len(self.LeftOutNames) > 0 {
		fmt.Printf("left out %s that the importer does not take: %s\n", countWords(len(self.LeftOutNames), "item"), forTerminal(strings.Join(self.LeftOutNames, ", ")))
	}
	return nil
}

// importItems saves the items the person does not have yet. An item is the
// same as one they have when the kind, the name, the folder, the first site,
// the user name and a hash of the password (or note text) are the same (see
// duplicateKey). The names of the ones skipped are listed, and nothing else is
// printed about an item.
func importItems(session *passwordsSession, imported []passwords.ImportedItem, isDryRun bool, counts *passwordsImportCounts) error {
	existing, err := session.loadItems(false)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, item := range existing {
		have[item.duplicateKey()] = true
	}
	for index := range imported {
		each := &imported[index]
		var fields any
		var key, name string
		switch {
		case each.ItemKind == passwords.ItemKindLogin && each.Login != nil:
			fields, name = each.Login, each.Login.Name
			key = duplicateKeyOf(each.ItemKind, each.Login.Name, each.Login.Folder, firstOf(each.Login.Sites), each.Login, nil)
		case each.ItemKind == passwords.ItemKindNote && each.Note != nil:
			fields, name = each.Note, each.Note.Name
			key = duplicateKeyOf(each.ItemKind, each.Note.Name, each.Note.Folder, "", nil, each.Note)
		default:
			counts.FailedCount++
			continue
		}
		if have[key] {
			counts.SkippedCount++
			counts.SkippedNames = append(counts.SkippedNames, name)
			continue
		}
		have[key] = true
		if isDryRun {
			counts.ImportedCount++
			continue
		}
		if _, err := session.saveNewItem(each.ItemKind, fields); err != nil {
			counts.FailedCount++
			fmt.Fprintf(os.Stderr, "note: could not save an item: %s\n", err)
			continue
		}
		counts.ImportedCount++
	}
	return nil
}

func firstOf(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// passwordStoreDirectory is the store to read: the flag, $PASSWORD_STORE_DIR
// (which pass itself reads) or ~/.password-store.
func passwordStoreDirectory(command *cli.Command) (string, error) {
	if store := command.String("store"); store != "" {
		return expandHome(store)
	}
	if store := os.Getenv("PASSWORD_STORE_DIR"); store != "" {
		return store, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the home directory: %w", err)
	}
	return filepath.Join(home, ".password-store"), nil
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the home directory: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// decryptWithGPG decrypts one file of a pass store with the gpg on this
// computer. gpg asks for its own passphrase through its agent, as pass does.
// The path comes after "--", so that a file named like an option is a file.
// Its error output is not passed on: it can quote what it was decrypting.
func decryptWithGPG(ctx context.Context, path string) (string, error) {
	process := exec.CommandContext(ctx, passwordsGPGProgram, "--quiet", "--batch", "--decrypt", "--", path)
	var output bytes.Buffer
	process.Stdout = &output
	if err := process.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return "", fmt.Errorf("gpg could not decrypt it (exit status %d)", exitError.ExitCode())
		}
		return "", fmt.Errorf("cannot run gpg: %w", err)
	}
	return output.String(), nil
}

// readPassStore decrypts and parses every .gpg file of a pass store. A file
// that cannot be decrypted or has nothing in it is counted as failed, with its
// path told on standard error, never its content.
func readPassStore(ctx context.Context, store string, counts *passwordsImportCounts) ([]passwords.ImportedItem, error) {
	information, err := os.Stat(store)
	if err != nil || !information.IsDir() {
		return nil, usage(fmt.Sprintf("%s is not a directory; --store says where your password store is", store))
	}
	var imported []passwords.ImportedItem
	err = filepath.WalkDir(store, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			// .git, .extensions and the like are not entries.
			if path != store && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".gpg") || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		counts.FoundCount++
		relative, err := filepath.Rel(store, path)
		if err != nil {
			return err
		}
		entryPath := filepath.ToSlash(relative)
		text, err := decryptWithGPG(ctx, path)
		if err != nil {
			counts.FailedCount++
			fmt.Fprintf(os.Stderr, "note: %s: %s\n", forTerminal(entryPath), err)
			return nil
		}
		item, err := passwords.ParsePassEntry(entryPath, text)
		if err != nil {
			counts.FailedCount++
			fmt.Fprintf(os.Stderr, "note: %s: %s\n", forTerminal(entryPath), err)
			return nil
		}
		imported = append(imported, *item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return imported, nil
}

func runPasswordsImportPass(ctx context.Context, command *cli.Command) error {
	store, err := passwordStoreDirectory(command)
	if err != nil {
		return err
	}
	if information, err := os.Stat(store); err != nil || !information.IsDir() {
		return usage(fmt.Sprintf("%s is not a directory; --store says where your password store is", store))
	}
	// Unlock first: the passphrase is read from standard input before gpg
	// starts, and gpg uses its agent, not standard input.
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	counts := &passwordsImportCounts{IsDryRun: command.Bool("dry-run")}
	imported, err := readPassStore(ctx, store, counts)
	if err != nil {
		return err
	}
	if err := importItems(session, imported, counts.IsDryRun, counts); err != nil {
		return describeError(command, err)
	}
	return counts.print(command)
}

func runPasswordsImportCSV(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode passwords import csv <file>")
	}
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	var reader io.Reader
	if path := command.Args().First(); path == "-" {
		reader = passwordsInput(command)
	} else {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
		defer func() {
			_ = file.Close()
		}()
		reader = file
	}
	parsed, err := passwords.ParseCSV(reader)
	if err != nil {
		return usage(err.Error())
	}
	counts := &passwordsImportCounts{IsDryRun: command.Bool("dry-run"), FoundCount: len(parsed.Items) + len(parsed.SkippedItemNames), LeftOutNames: parsed.SkippedItemNames}
	if err := importItems(session, parsed.Items, counts.IsDryRun, counts); err != nil {
		return describeError(command, err)
	}
	return counts.print(command)
}

// passwordsExportItem is one item in the JSON export: its kind and its fields,
// in the names the encryption uses.
type passwordsExportItem struct {
	ID       string `json:"id"`
	ItemKind string `json:"itemKind"`
	Fields   any    `json:"fields"`
}

// passwordsExportCSVHeader is Bitwarden's, which 'import csv' reads back.
var passwordsExportCSVHeader = []string{"folder", "favorite", "type", "name", "notes", "fields", "reprompt", "login_uri", "login_username", "login_password", "login_totp"}

// writeExportCSV writes the logins and notes as a Bitwarden-style CSV, and says
// how many items it left out because the format has no place for them (cards).
func writeExportCSV(writer io.Writer, items []*passwordsItem) (leftOutCount int, err error) {
	rows := csv.NewWriter(writer)
	if err := rows.Write(passwordsExportCSVHeader); err != nil {
		return 0, err
	}
	for _, item := range items {
		var row []string
		switch {
		case item.Login != nil:
			login := item.Login
			row = []string{login.Folder, "", "login", login.Name, login.Notes, "", "0", strings.Join(login.Sites, "\n"), login.UserName, login.Password, login.OneTimeCodeURI}
		case item.Note != nil:
			row = []string{item.Note.Folder, "", "note", item.Note.Name, item.Note.Text, "", "0", "", "", "", ""}
		default:
			leftOutCount++
			continue
		}
		if err := rows.Write(row); err != nil {
			return 0, err
		}
	}
	rows.Flush()
	return leftOutCount, rows.Error()
}

func writeExportJSON(writer io.Writer, items []*passwordsItem) error {
	exported := make([]passwordsExportItem, 0, len(items))
	for _, item := range items {
		exported = append(exported, passwordsExportItem{ID: item.View.ID, ItemKind: string(item.Kind), Fields: item.Fields()})
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"items": exported})
}

func runPasswordsExport(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode passwords export <file> [--format json|csv]")
	}
	path := command.Args().First()
	format := strings.ToLower(command.String("format"))
	if format == "" {
		format = "json"
		if strings.HasSuffix(strings.ToLower(path), ".csv") {
			format = "csv"
		}
	}
	if format != "json" && format != "csv" {
		return usage("--format is json or csv")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; export does not overwrite a file", path)
	}
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	loaded, err := session.loadItems(false)
	if err != nil {
		return describeError(command, err)
	}
	var items []*passwordsItem
	for _, item := range loaded {
		if item.IsOwner() {
			items = append(items, item)
		}
	}
	if err := confirm(command, fmt.Sprintf("This writes %s you own to %s in the clear, passwords, card numbers and passkey keys included. Anybody who gets the file has them; delete it when you are done.",
		countWords(len(items), "item"), path)); err != nil {
		return err
	}
	// Created private, and not over a file that appeared since: the secrets
	// are written into it.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	leftOutCount := 0
	if format == "csv" {
		leftOutCount, err = writeExportCSV(file, items)
	} else {
		err = writeExportJSON(file, items)
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"exportedCount": len(items) - leftOutCount, "leftOutCount": leftOutCount, "exportedFilePath": path})
	}
	fmt.Printf("wrote %s to %s, readable by you only\n", countWords(len(items)-leftOutCount, "item"), path)
	if leftOutCount > 0 {
		fmt.Printf("left out %s that a CSV file has no place for (cards); --format json has them\n", countWords(leftOutCount, "item"))
	}
	return nil
}
