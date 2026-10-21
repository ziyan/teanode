package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/passwords"
)

// passwordsTapPlace is where a change that needs the person's tap is done. Each
// command puts what it does in front and any word on what does work from here
// after.
const passwordsTapPlace = " is done on the Passwords page of the dashboard or in the iPhone app; a command line cannot do this"

// The sentence each command ends with when the server refuses it for want of
// the person's tap.
const (
	passwordsAgentTapSentence            = "Letting the agent use an item" + passwordsTapPlace + ", but turning it off works here."
	passwordsChangePassphraseTapSentence = "Changing the passphrase" + passwordsTapPlace + "."
	passwordsNewRecoveryKeyTapSentence   = "Replacing the recovery key" + passwordsTapPlace + "."
	passwordsEraseTapSentence            = "Erasing Passwords" + passwordsTapPlace + "."
)

// clipboardClearSeconds is how long a copied secret stays on the clipboard
// before the command clears it, the way pass does.
const clipboardClearSeconds = 45

// NewPasswordsCommand is `teanode passwords`: logins, cards and notes that are
// encrypted on your devices, so that this server only ever keeps what it cannot
// read. The passphrase and the keys it opens never leave this computer.
func NewPasswordsCommand() *cli.Command {
	return &cli.Command{
		Name:  "passwords",
		Usage: "your logins, cards and notes, encrypted on your devices",
		Description: "Passwords keeps logins (with one-time codes and passkeys), cards and notes. They\n" +
			"are encrypted before they leave this computer, and opened here with a passphrase\n" +
			"that is not your sign-in password; the server, whoever runs it and a copy of its\n" +
			"database see only encrypted data, except for an item you let the agent use.\n" +
			"'teanode passwords unlock' asks for the passphrase and keeps the opened keys in a\n" +
			"file only you can read (in $XDG_RUNTIME_DIR when it is set) until the lock time\n" +
			"passes, at most 8 hours, or 'teanode passwords lock' removes them, as gpg-agent\n" +
			"keeps a passphrase; without it, each command asks.\n" +
			"The passphrase is never an argument or an environment variable; scripts pass it\n" +
			"on standard input with --passphrase-stdin. An item is named by its name, a word\n" +
			"in its name, site or user name, or its id from 'teanode passwords list'.\n\n" +
			"  teanode passwords unlock\n" +
			"  teanode passwords list\n" +
			"  teanode passwords copy example.com\n" +
			"  teanode passwords copy example.com --code\n" +
			"  teanode passwords add example.com --user-name ada@example.com --generate\n" +
			"  teanode passwords import pass --dry-run",
		Commands: []*cli.Command{
			{
				Name:  "setup",
				Usage: "set Passwords up with a passphrase, and make the recovery key",
				Description: "Asks for a new passphrase twice (at least 12 characters), makes the keys on this\n" +
					"computer, sends only encrypted keys to the server, and prints the recovery key\n" +
					"once. Keep the recovery key somewhere safe: it and the passphrase are the only\n" +
					"ways to open your passwords, and if both are lost the only way out is to erase\n" +
					"Passwords and start again. The server asks for your tap to set up, which a command\n" +
					"line cannot give, and it is asked before the passphrase is: if it refuses, set up\n" +
					"on the Passwords page of the dashboard or in the iPhone app, then 'teanode\n" +
					"passwords unlock' here.",
				Flags:  []cli.Flag{JSONFlag(), passphraseFlag()},
				Action: runPasswordsSetup,
			},
			{
				Name:  "unlock",
				Usage: "open Passwords and keep it open for the lock time",
				Description: "Asks for the passphrase (or, with --recovery-key, the recovery key), checks it\n" +
					"with the server, and keeps the opened keys in a file only you can read, named for\n" +
					"the profile, until the lock time you chose in 'teanode passwords settings' passes\n" +
					"(but at most 8 hours) or 'teanode passwords lock' removes it. The file is in\n" +
					"$XDG_RUNTIME_DIR when that is set, and in the configuration directory otherwise;\n" +
					"a file anybody else could read or change is refused. The keys in it are not\n" +
					"encrypted: anyone who can read your files while it exists can open your passwords.",
				Flags:  []cli.Flag{JSONFlag(), passphraseFlag(), &cli.BoolFlag{Name: "recovery-key", Usage: "unlock with the recovery key instead of the passphrase"}},
				Action: runPasswordsUnlock,
			},
			{Name: "lock", Usage: "forget the keys that unlock kept", Flags: []cli.Flag{JSONFlag()}, Action: runPasswordsLock},
			{
				Name:      "list",
				Usage:     "your items: name, kind, user name, first site, folder",
				ArgsUsage: "[search]",
				Description: "With a word, only the items with it in their name, site or user name. The last\n" +
					"column marks the items you share (shared), the ones shared with you (shared by\n" +
					"their owner) and the ones your agent may use (agent). Nothing secret is listed.",
				Flags: []cli.Flag{
					JSONFlag(), passphraseFlag(),
					&cli.StringFlag{Name: "kind", Usage: "only login, card or note"},
					&cli.StringFlag{Name: "folder", Usage: "only this folder"},
					&cli.BoolFlag{Name: "deleted", Usage: "list the recently deleted items instead, which 'restore' brings back within 30 days"},
				},
				Action: runPasswordsList,
			},
			{
				Name:      "show",
				Usage:     "one item, with its secrets hidden",
				ArgsUsage: "<item>",
				Description: "A password, a card number and security code, and a note's text are hidden\n" +
					"unless --reveal. A one-time code is shown, with the seconds it has left.",
				Flags:  []cli.Flag{JSONFlag(), passphraseFlag(), &cli.BoolFlag{Name: "reveal", Usage: "show the secrets too"}},
				Action: runPasswordsShow,
			},
			{
				Name:      "copy",
				Usage:     "copy a password, code or other secret to the clipboard",
				ArgsUsage: "<item>",
				Description: "Copies a login's password, a card's number or a note's text, or with --code the\n" +
					"login's one-time code, with --user-name its user name, with --number a card's\n" +
					"number, with --security-code a card's security code. The clipboard is cleared after 45 seconds, which keeps the\n" +
					"command running, or at once on Ctrl-C; --clear-after 0 leaves it. It is cleared\n" +
					"only if it still holds the secret (read back with wl-paste, xclip, xsel or\n" +
					"pbpaste), so something you copied since is not lost. With no clipboard program\n" +
					"on this computer (wl-copy, xclip, xsel, pbcopy) it warns and prints the value\n" +
					"instead.",
				Flags: []cli.Flag{
					passphraseFlag(),
					&cli.BoolFlag{Name: "code", Usage: "the one-time code"},
					&cli.BoolFlag{Name: "user-name", Usage: "the user name"},
					&cli.BoolFlag{Name: "number", Usage: "a card's number"},
					&cli.BoolFlag{Name: "security-code", Usage: "a card's security code"},
					&cli.IntFlag{Name: "clear-after", Usage: "seconds before the clipboard is cleared; 0 never", Value: clipboardClearSeconds},
				},
				Action: runPasswordsCopy,
			},
			{
				Name:      "add",
				Usage:     "add a login, card or note",
				ArgsUsage: "[name]",
				Description: "A login by default; --kind card or --kind note for the others. A password is\n" +
					"typed at a prompt when you do not give --password, --password-stdin (the line\n" +
					"after the passphrase) or --generate; --password is in your shell history and in\n" +
					"the process list while the command runs.\n\n" +
					"  teanode passwords add example.com --user-name ada@example.com --site https://example.com --generate\n" +
					"  teanode passwords add \"Spare key\" --kind note --text \"under the blue pot\"",
				Flags:  append([]cli.Flag{JSONFlag(), passphraseFlag(), &cli.StringFlag{Name: "kind", Usage: "login, card or note", Value: "login"}}, passwordsFieldFlags()...),
				Action: runPasswordsAdd,
			},
			{
				Name:      "edit",
				Usage:     "change an item you own",
				ArgsUsage: "<item>",
				Description: "Changes only the fields you give. The change is based on the revision it read,\n" +
					"so one made on another device in between is refused instead of overwritten; run\n" +
					"the command again. --site replaces the sites, --add-site adds one. A shared\n" +
					"item can only be changed by its owner.",
				Flags: append(append([]cli.Flag{JSONFlag(), passphraseFlag()}, passwordsFieldFlags()...),
					&cli.StringSliceFlag{Name: "add-site", Usage: "add a site to a login"}),
				Action: runPasswordsEdit,
			},
			{Name: "delete", Usage: "move an item to recently deleted", ArgsUsage: "<item>", Flags: []cli.Flag{JSONFlag(), passphraseFlag()}, Action: runPasswordsDelete},
			{Name: "restore", Usage: "bring back an item deleted in the last 30 days", ArgsUsage: "<item>", Flags: []cli.Flag{JSONFlag(), passphraseFlag()}, Action: runPasswordsRestore},
			{
				Name:      "share",
				Usage:     "share an item with a person or a circle",
				ArgsUsage: "<item> <username|circle>",
				Description: "Seals the item's key for each person it newly reaches, with the public key they\n" +
					"set up. It seals only for a key this computer knows: one you confirmed before\n" +
					"(kept in a file in the configuration directory) or one you confirm now by its\n" +
					"safety code, which it prints and asks you to check with the person (in person,\n" +
					"or a call you trust). A server that swapped their key for its own could otherwise\n" +
					"read what you share. When a person's key has changed it shows the code you knew\n" +
					"and the new one, and without a terminal it refuses unless you pass --force. A\n" +
					"person who has not set Passwords up yet waits, and 'teanode passwords share\n" +
					"--waiting' seals for them once they have. People who already have the item keep\n" +
					"the key they have. The people it reaches can use the item; only you change it.\n" +
					"Adding somebody new is done on the Passwords page of the dashboard or in the\n" +
					"iPhone app; a command line cannot do this. Taking somebody away works here.",
				Flags: []cli.Flag{
					JSONFlag(), passphraseFlag(), ForceFlag(),
					&cli.BoolFlag{Name: "waiting", Usage: "instead, seal your items for the people who have set Passwords up since you shared them"},
				},
				Action: runPasswordsShare,
			},
			{
				Name:      "unshare",
				Usage:     "stop sharing an item with a person or a circle",
				ArgsUsage: "<item> [username|circle]",
				Description: "With no person or circle it stops sharing the item with everyone. It sends no\n" +
					"keys: the people who still have the item keep the key they have.",
				Flags:  []cli.Flag{JSONFlag(), passphraseFlag()},
				Action: runPasswordsUnshare,
			},
			{
				Name:      "agent",
				Usage:     "let the agent use an item, or stop it",
				ArgsUsage: "on|off <item>",
				Description: "Seals the item's key for the server's agent key, so that the agent can sign in\n" +
					"with this login in its browser: TeaNode types the password, code or passkey in\n" +
					"only on the item's own sites and never shows it to the model. A note the agent\n" +
					"may use is read by the model. Off for every item until you turn it on. A card\n" +
					"cannot be used by the agent, and only the owner can turn it on. Turning it on is\n" +
					"done on the Passwords page of the dashboard or in the iPhone app; a command line\n" +
					"cannot do this, and says so before it asks for anything. Turning it off works\n" +
					"here.",
				Flags:  []cli.Flag{JSONFlag(), passphraseFlag(), ForceFlag()},
				Action: runPasswordsAgent,
			},
			{
				Name:  "generate",
				Usage: "make a random password",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "length", Usage: "characters, 8 to 128", Value: generatedPasswordLengthUsual},
					&cli.BoolFlag{Name: "no-symbols", Usage: "letters and digits only"},
				},
				Action: runPasswordsGenerate,
			},
			{
				Name:  "settings",
				Usage: "whether Passwords is set up, and the lock time",
				Description: "Shows whether Passwords is set up, how many items it holds, and whether this\n" +
					"computer is unlocked. --lock-after-minutes sets how long unlocking lasts, from 1\n" +
					"minute to 7 days (10080), on every device.",
				Flags:  []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "lock-after-minutes", Usage: "how long a device stays unlocked, 1 to 10080"}},
				Action: runPasswordsSettings,
			},
			{
				Name:  "change-passphrase",
				Usage: "change the passphrase",
				Description: "Asks for the current passphrase (or, with --recovery-key, the recovery key if you\n" +
					"forgot it) and then the new one twice; with --passphrase-stdin, the first line is\n" +
					"the current one and the second the new one. A new passphrase has at least 12\n" +
					"characters. The old passphrase no longer opens Passwords on this server. It does\n" +
					"not make a new account key and your items are not re-encrypted: only the key that\n" +
					"opens them is sealed again, so a copy of the database from before still opens\n" +
					"with the old passphrase. Done on the Passwords page of the dashboard or in the\n" +
					"iPhone app; a command line cannot do this, and says so before it asks for\n" +
					"anything.",
				Flags:  []cli.Flag{JSONFlag(), passphraseFlag(), &cli.BoolFlag{Name: "recovery-key", Usage: "prove it with the recovery key instead of the current passphrase"}},
				Action: runPasswordsChangePassphrase,
			},
			{
				Name:  "new-recovery-key",
				Usage: "make a new recovery key, which replaces the old one",
				Description: "Asks for the passphrase, makes a new recovery key and prints it once. The old\n" +
					"one no longer opens Passwords on this server. It does not make a new account key,\n" +
					"so a copy of the database from before still opens with the old recovery key.\n" +
					"Done on the Passwords page of the dashboard or in the iPhone app; a command line\n" +
					"cannot do this, and says so before it asks for anything.",
				Flags:  []cli.Flag{JSONFlag(), passphraseFlag()},
				Action: runPasswordsNewRecoveryKey,
			},
			{
				Name:  "import",
				Usage: "bring in logins from pass or a CSV export",
				Commands: []*cli.Command{
					{
						Name:  "pass",
						Usage: "import a pass (password-store) directory",
						Description: "Walks the store, decrypts each .gpg file with the gpg on this computer (one\n" +
							"process per file, so gpg asks for its own passphrase as pass does), and saves a\n" +
							"login for each: the first line is the password; login:, user:, username: and\n" +
							"email: lines are the user name, url: lines are sites, an otpauth:// line is the\n" +
							"one-time code and the rest is notes. The path is the folder and name. An item\n" +
							"with the same name, folder, first site, user name and password as one you\n" +
							"already have is skipped and named, so running it again adds nothing. It prints\n" +
							"counts and names, never a secret.",
						Flags: []cli.Flag{
							JSONFlag(), passphraseFlag(),
							&cli.StringFlag{Name: "store", Usage: "the password store; $PASSWORD_STORE_DIR or ~/.password-store by default"},
							&cli.BoolFlag{Name: "dry-run", Usage: "say what would be imported, and save nothing"},
						},
						Action: runPasswordsImportPass,
					},
					{
						Name:      "csv",
						Usage:     "import a CSV export from Chrome, Apple Passwords or Bitwarden",
						ArgsUsage: "<file>",
						Description: "The manager is found from the header row. An item you already have (same name,\n" +
							"folder, first site, user name and password) is skipped and named. Cards and\n" +
							"identities in a Bitwarden export are left out and named. A file of passwords is\n" +
							"as secret as the passwords: delete it afterwards. '-' reads standard input.",
						Flags: []cli.Flag{
							JSONFlag(), passphraseFlag(),
							&cli.BoolFlag{Name: "dry-run", Usage: "say what would be imported, and save nothing"},
						},
						Action: runPasswordsImportCSV,
					},
				},
			},
			{
				Name:      "export",
				Usage:     "write your items to a file, unencrypted",
				ArgsUsage: "<file>",
				Description: "Writes the items you own, in the clear, as JSON (everything, passkeys and cards\n" +
					"included) or as a Bitwarden-style CSV (logins and notes). The file is readable by\n" +
					"you only, an existing file is not overwritten, and the command asks first. Anybody\n" +
					"who gets the file has your passwords: delete it when you are done.",
				Flags: []cli.Flag{
					JSONFlag(), passphraseFlag(), ForceFlag(),
					&cli.StringFlag{Name: "format", Usage: "json or csv; by the file's ending, json otherwise"},
				},
				Action: runPasswordsExport,
			},
			{
				Name:  "erase",
				Usage: "erase Passwords: every item, share and key",
				Description: "Deletes your setup, every item, its shares and keys, for you and for the people\n" +
					"you share with. This cannot be undone. If you have lost both your passphrase and\n" +
					"your recovery key it is the only way to start again. Done on the Passwords page\n" +
					"of the dashboard or in the iPhone app; a command line cannot do this, and says\n" +
					"so before it asks you to confirm.",
				Flags:  []cli.Flag{JSONFlag(), ForceFlag()},
				Action: runPasswordsErase,
			},
		},
	}
}

// passwordsFieldFlags are the flags that give the fields of an item, for add
// and edit. Each belongs to some kinds of item (passwordsFlagKinds).
func passwordsFieldFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "name", Usage: "the item's name"},
		&cli.StringFlag{Name: "folder", Usage: "the folder it is in"},
		&cli.StringFlag{Name: "user-name", Usage: "a login's user name"},
		&cli.StringFlag{Name: "password", Usage: "a login's password; shows in your shell history, prefer --password-stdin, --generate or the prompt"},
		&cli.BoolFlag{Name: "password-stdin", Usage: "read the login's password from a line of standard input, after the passphrase"},
		&cli.BoolFlag{Name: "generate", Usage: "make the login's password at random"},
		&cli.IntFlag{Name: "length", Usage: "characters of a generated password, 8 to 128", Value: generatedPasswordLengthUsual},
		&cli.BoolFlag{Name: "no-symbols", Usage: "a generated password has letters and digits only"},
		&cli.StringSliceFlag{Name: "site", Usage: "a login's site, such as https://example.com; repeat it for more"},
		&cli.StringFlag{Name: "one-time-code", Usage: "a login's one-time code: an otpauth:// address, or its base32 secret"},
		&cli.StringFlag{Name: "notes", Usage: "a login's or card's notes"},
		&cli.StringFlag{Name: "cardholder-name", Usage: "a card's holder"},
		&cli.StringFlag{Name: "number", Usage: "a card's number; typed at a prompt when left out"},
		&cli.StringFlag{Name: "expiry-month", Usage: "a card's expiry month, as printed"},
		&cli.StringFlag{Name: "expiry-year", Usage: "a card's expiry year, as printed"},
		&cli.StringFlag{Name: "security-code", Usage: "a card's security code; typed at a prompt when left out"},
		&cli.StringFlag{Name: "brand", Usage: "a card's brand"},
		&cli.StringFlag{Name: "text", Usage: "a note's text"},
		&cli.BoolFlag{Name: "text-stdin", Usage: "read the note's text from standard input, to its end, after the passphrase"},
	}
}

// passwordsFlagKinds are the kinds of item a field flag is for. A flag not
// listed is for every kind.
var passwordsFlagKinds = map[string][]passwords.ItemKind{
	"user-name":       {passwords.ItemKindLogin},
	"password":        {passwords.ItemKindLogin},
	"password-stdin":  {passwords.ItemKindLogin},
	"generate":        {passwords.ItemKindLogin},
	"length":          {passwords.ItemKindLogin},
	"no-symbols":      {passwords.ItemKindLogin},
	"site":            {passwords.ItemKindLogin},
	"add-site":        {passwords.ItemKindLogin},
	"one-time-code":   {passwords.ItemKindLogin},
	"notes":           {passwords.ItemKindLogin, passwords.ItemKindCard},
	"cardholder-name": {passwords.ItemKindCard},
	"number":          {passwords.ItemKindCard},
	"expiry-month":    {passwords.ItemKindCard},
	"expiry-year":     {passwords.ItemKindCard},
	"security-code":   {passwords.ItemKindCard},
	"brand":           {passwords.ItemKindCard},
	"text":            {passwords.ItemKindNote},
	"text-stdin":      {passwords.ItemKindNote},
}

// checkFlagsForKind refuses a field flag that the kind of item has no field
// for, so that --user-name on a note is an error and not a silent nothing.
func checkFlagsForKind(command *cli.Command, kind passwords.ItemKind) error {
	for _, flag := range sortedKeys(passwordsFlagKinds) {
		kinds := passwordsFlagKinds[flag]
		if !command.IsSet(flag) {
			continue
		}
		isForKind := false
		for _, each := range kinds {
			isForKind = isForKind || each == kind
		}
		if !isForKind {
			return usage(fmt.Sprintf("--%s is not a field of a %s", flag, kind))
		}
	}
	return nil
}

// tapRequiredError is the one sentence a command prints when the server
// refuses it for want of the person's tap. The server's own refusal stays
// wrapped, so client.IsTapRequired still recognizes it, but it is not printed:
// it says the same thing as the sentence.
type tapRequiredError struct {
	sentence string
	err      error
}

func (self *tapRequiredError) Error() string {
	return self.sentence
}

func (self *tapRequiredError) Unwrap() error {
	return self.err
}

// describeTapRequired replaces an error that says only the person's tap may do
// this with the sentence for the command that was refused.
func describeTapRequired(err error, sentence string) error {
	if client.IsTapRequired(err) {
		return &describedError{&tapRequiredError{sentence: sentence, err: err}}
	}
	return err
}

// readStandardInputToEnd reads what is left of standard input, for a note's
// text.
func readStandardInputToEnd(command *cli.Command) (string, error) {
	text, err := io.ReadAll(passwordsInput(command))
	if err != nil {
		return "", fmt.Errorf("cannot read from standard input: %w", err)
	}
	return strings.TrimRight(string(text), "\r\n"), nil
}

// openPasswordsForCommand connects and opens Passwords, the unlock a previous
// command kept or a passphrase asked for.
func openPasswordsForCommand(ctx context.Context, command *cli.Command) (*passwordsSession, error) {
	connection, err := openClient(command)
	if err != nil {
		return nil, err
	}
	session, err := openPasswords(ctx, command, connection)
	if err != nil {
		return nil, describeError(command, err)
	}
	return session, nil
}

// lockAfterDuration is how long the server says a device stays unlocked.
func lockAfterDuration(setup *client.PasswordsSetupView) time.Duration {
	minutes := setup.LockAfterMinutes
	if minutes < models.PasswordsLockAfterMinutesLeast {
		minutes = models.PasswordsLockAfterMinutesDefault
	}
	return time.Duration(minutes) * time.Minute
}

func runPasswordsSetup(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	current, err := client.ReadPasswordsSetup(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if current != nil && current.IsSetUp {
		return errors.New("you have already set Passwords up; 'teanode passwords unlock' opens it. If you have lost both the passphrase and the recovery key, 'teanode passwords erase' is the only way to start again")
	}
	// Setting up needs a person's tap, which a command line cannot give. Find
	// that out before asking for a passphrase that would be typed for nothing.
	if err := client.CheckPasswordsSetUpAllowed(ctx, connection); err != nil {
		return passwordsSetupRefusal(command, err)
	}
	passphrase, err := readNewPassphrase(command, "New Passwords passphrase: ")
	if err != nil {
		return err
	}
	made, err := makePasswordsSetup(passphrase)
	if err != nil {
		return err
	}
	setup, err := client.SetUpPasswords(ctx, connection, made.Input)
	if err != nil {
		return passwordsSetupRefusal(command, err)
	}
	// The keys are open on this computer already; leave them open for the lock
	// time, as unlock would.
	expiresAt, unlockErr := writePasswordsUnlock(command, made.Keys, lockAfterDuration(setup))
	if command.Bool("json") {
		return PrintJSON(map[string]any{"isSetUp": true, "recoveryKey": made.RecoveryKey, "unlockedUntil": expiresAt})
	}
	fmt.Println("Passwords is set up.")
	fmt.Println()
	fmt.Println("Your recovery key. It is shown only once. Write it down or print it, and keep it")
	fmt.Println("somewhere safe, away from this computer:")
	fmt.Println()
	fmt.Println("    " + made.RecoveryKey)
	fmt.Println()
	fmt.Println("If you forget your passphrase, this key is the only other way to open your")
	fmt.Println("passwords. Anyone who has it can open them, and neither TeaNode nor whoever runs")
	fmt.Println("this server can recover them for you. If you lose both, the only way out is to")
	fmt.Println("erase Passwords and start again.")
	if unlockErr != nil {
		fmt.Fprintf(os.Stderr, "note: could not keep Passwords unlocked here: %s\n", unlockErr)
	}
	return nil
}

// refuseWithoutPasswordsTap asks the server, before anything is asked of the
// person, whether this connection is a person's own tap, which changing the
// passphrase, replacing the recovery key, erasing and letting the agent use an
// item need. It reuses the probe that setup sends (an empty setup, which
// changes nothing): the server checks the tap the same way for all of them. The
// refusal is the one sentence the command would end with anyway, so that nobody
// types a passphrase or confirms an erase for nothing.
func refuseWithoutPasswordsTap(ctx context.Context, command *cli.Command, connection *client.Client, sentence string) error {
	if err := client.CheckPasswordsSetUpAllowed(ctx, connection); err != nil {
		return describeTapRequired(describeError(command, err), sentence)
	}
	return nil
}

// passwordsSetupRefusal is the server's refusal of a setup, with where to go
// when it is for want of a tap.
func passwordsSetupRefusal(command *cli.Command, err error) error {
	return describeTapRequired(describeError(command, err), "Setting up Passwords"+passwordsTapPlace+"; once it is set up there, run 'teanode passwords unlock' here.")
}

// passwordsMade is what setup makes on this computer: what is sent, the keys
// it opens and the recovery key to show.
type passwordsMade struct {
	Input       *client.SetUpPasswordsInput
	Keys        *passwordsKeys
	RecoveryKey string
}

// makePasswordsSetup makes the account key, the key pair and the recovery key,
// and seals the account key under the passphrase and under the recovery key.
func makePasswordsSetup(passphrase string) (*passwordsMade, error) {
	accountKey, err := passwords.NewKey()
	if err != nil {
		return nil, err
	}
	salt, err := passwords.NewSalt()
	if err != nil {
		return nil, err
	}
	passphraseKey, err := passwords.DerivePassphraseKey(passphrase, salt, passwords.DefaultIterationCount)
	if err != nil {
		return nil, err
	}
	accountKeyByPassphrase, passphraseProofHash, err := sealAccountKeyUnder(accountKey, passphraseKey)
	if err != nil {
		return nil, err
	}
	recoveryKeyBytes, err := passwords.NewRecoveryKey()
	if err != nil {
		return nil, err
	}
	recoveryKey, err := passwords.FormatRecoveryKey(recoveryKeyBytes)
	if err != nil {
		return nil, err
	}
	accountKeyByRecoveryKey, recoveryKeyProofHash, err := sealAccountKeyUnder(accountKey, recoveryKeyBytes)
	if err != nil {
		return nil, err
	}
	keyPair, err := passwords.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	privateKeyByAccountKey, err := passwords.Seal(accountKey, keyPair.PrivateKey(), passwords.PrivateKeyData())
	if err != nil {
		return nil, err
	}
	return &passwordsMade{
		Input: &client.SetUpPasswordsInput{
			AccountKeyByPassphrase:   accountKeyByPassphrase,
			PassphraseSalt:           base64.StdEncoding.EncodeToString(salt),
			PassphraseIterationCount: passwords.DefaultIterationCount,
			PassphraseProofHash:      passphraseProofHash,
			AccountKeyByRecoveryKey:  accountKeyByRecoveryKey,
			RecoveryKeyProofHash:     recoveryKeyProofHash,
			PublicKey:                base64.StdEncoding.EncodeToString(keyPair.PublicKey()),
			PrivateKeyByAccountKey:   privateKeyByAccountKey.String(),
		},
		Keys:        &passwordsKeys{AccountKey: accountKey, KeyPair: keyPair},
		RecoveryKey: recoveryKey,
	}, nil
}

// sealAccountKeyUnder seals the account key with the wrap key made from a
// 32 byte secret, and gives the hash of the secret's proof, which is what the
// server stores to check it later.
func sealAccountKeyUnder(accountKey, secret []byte) (sealedAccountKey, proofHash string, err error) {
	wrapKey, err := passwords.WrapKey(secret)
	if err != nil {
		return "", "", err
	}
	proofKey, err := passwords.ProofKey(secret)
	if err != nil {
		return "", "", err
	}
	sealed, err := passwords.Seal(wrapKey, accountKey, passwords.AccountKeyData())
	if err != nil {
		return "", "", err
	}
	return sealed.String(), base64.StdEncoding.EncodeToString(passwords.ProofHash(proofKey)), nil
}

func runPasswordsUnlock(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	setup, err := readSetUpPasswords(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	var secret passwordsSecret
	if command.Bool("recovery-key") {
		secret.RecoveryKey, err = readRecoveryKey(command)
	} else {
		secret.Passphrase, err = readPassphrase(command)
	}
	if err != nil {
		return err
	}
	keys, err := unlockPasswords(ctx, connection, setup, secret)
	if err != nil {
		return describeError(command, err)
	}
	lockAfter := lockAfterDuration(setup)
	expiresAt, err := writePasswordsUnlock(command, keys, lockAfter)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"isUnlocked": true, "unlockedUntil": expiresAt})
	}
	fmt.Printf("unlocked until %s (%s); 'teanode passwords lock' locks it now\n", expiresAt.Format("15:04"), lockAfter)
	return nil
}

func runPasswordsLock(ctx context.Context, command *cli.Command) error {
	wasUnlocked, err := removePasswordsUnlock(command)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"isUnlocked": false, "wasUnlocked": wasUnlocked})
	}
	if wasUnlocked {
		fmt.Println("locked")
	} else {
		fmt.Println("it was not unlocked here")
	}
	return nil
}

// passwordsItemSummary is an item as list and show tell it, without a secret.
type passwordsItemSummary struct {
	ID              string     `json:"id"`
	ItemKind        string     `json:"itemKind"`
	Name            string     `json:"name"`
	UserName        string     `json:"userName"`
	Sites           []string   `json:"sites"`
	Folder          string     `json:"folder"`
	IsShared        bool       `json:"isShared"`
	IsSharedWithMe  bool       `json:"isSharedWithMe"`
	OwnerName       string     `json:"ownerName"`
	IsUsableByAgent bool       `json:"isUsableByAgent"`
	DeletedAt       *time.Time `json:"deletedAt"`
	Revision        int64      `json:"revision"`
}

func summarizeItem(item *passwordsItem) passwordsItemSummary {
	sites := item.Sites()
	if sites == nil {
		sites = []string{}
	}
	return passwordsItemSummary{
		ID: item.View.ID, ItemKind: string(item.Kind), Name: item.Name(), UserName: item.UserName(), Sites: sites, Folder: item.Folder(),
		IsShared: len(item.View.Shares) > 0, IsSharedWithMe: item.View.IsSharedWithMe, OwnerName: item.View.OwnerName,
		IsUsableByAgent: item.View.IsUsableByAgent, DeletedAt: item.View.DeletedAt, Revision: item.View.Revision,
	}
}

// itemMarkers are the short words that go after an item in a list.
func itemMarkers(summary passwordsItemSummary) string {
	var markers []string
	if summary.IsSharedWithMe {
		markers = append(markers, "shared by "+summary.OwnerName)
	} else if summary.IsShared {
		markers = append(markers, "shared")
	}
	if summary.IsUsableByAgent {
		markers = append(markers, "agent")
	}
	if summary.DeletedAt != nil {
		markers = append(markers, "deleted "+summary.DeletedAt.In(time.Local).Format(time.DateOnly))
	}
	return strings.Join(markers, ", ")
}

func runPasswordsList(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() > 1 {
		return usage("usage: teanode passwords list [search]")
	}
	if kind := command.String("kind"); kind != "" && !passwords.ItemKind(kind).IsValid() {
		return usage("--kind is login, card or note")
	}
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	items, err := session.loadItems(command.Bool("deleted"))
	if err != nil {
		return describeError(command, err)
	}
	search := strings.ToLower(strings.TrimSpace(command.Args().First()))
	summaries := []passwordsItemSummary{}
	for _, item := range items {
		if command.Bool("deleted") != (item.View.DeletedAt != nil) {
			continue
		}
		if kind := command.String("kind"); kind != "" && string(item.Kind) != kind {
			continue
		}
		if folder := command.String("folder"); folder != "" && !strings.EqualFold(item.Folder(), folder) {
			continue
		}
		if search != "" && len(searchMatches([]*passwordsItem{item}, search)) == 0 {
			continue
		}
		summaries = append(summaries, summarizeItem(item))
	}
	if command.Bool("json") {
		return PrintJSON(summaries)
	}
	if len(summaries) == 0 {
		fmt.Println("no items")
		return nil
	}
	rows := make([][]string, 0, len(summaries))
	for _, summary := range summaries {
		site := ""
		if len(summary.Sites) > 0 {
			site = summary.Sites[0]
		}
		rows = append(rows, []string{summary.Name, summary.ItemKind, summary.UserName, site, summary.Folder, itemMarkers(summary), summary.ID})
	}
	return printTable([]string{"name", "kind", "user name", "site", "folder", "", "id"}, rows)
}

// searchMatches are the items with the word in their name, user name or sites.
func searchMatches(items []*passwordsItem, lowered string) []*passwordsItem {
	var matches []*passwordsItem
	for _, item := range items {
		isMatch := strings.Contains(strings.ToLower(item.Name()), lowered) || strings.Contains(strings.ToLower(item.UserName()), lowered)
		for _, site := range item.Sites() {
			isMatch = isMatch || strings.Contains(strings.ToLower(site), lowered)
		}
		if isMatch {
			matches = append(matches, item)
		}
	}
	return matches
}

// itemFindingArgument is the one argument that names an item.
func itemFindingArgument(command *cli.Command, usageLine string) (string, error) {
	if command.Args().Len() != 1 {
		return "", usage(usageLine)
	}
	return command.Args().First(), nil
}

// findOpenItem opens Passwords and picks the one item the person named.
func findOpenItem(ctx context.Context, command *cli.Command, typed string, includeDeleted bool) (*passwordsSession, *passwordsItem, error) {
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return nil, nil, err
	}
	items, err := session.loadItems(includeDeleted)
	if err != nil {
		return nil, nil, describeError(command, err)
	}
	if includeDeleted {
		var deleted []*passwordsItem
		for _, item := range items {
			if item.View.DeletedAt != nil {
				deleted = append(deleted, item)
			}
		}
		items = deleted
	}
	item, err := findItem(items, typed)
	if err != nil {
		return nil, nil, err
	}
	return session, item, nil
}

// itemRow is one line of what show tells: its label, its key in JSON, its
// value, and whether the value is a secret that stays hidden without --reveal.
type itemRow struct {
	Label    string
	Key      string
	Value    string
	IsSecret bool
	// Hidden is what is printed instead of a secret that stays hidden.
	Hidden string
}

const hiddenSecretWords = "hidden; --reveal shows it"

// lastDigits is the last four characters of a card number.
func lastDigits(number string) string {
	number = strings.Join(strings.Fields(number), "")
	if len(number) > 4 {
		return number[len(number)-4:]
	}
	return number
}

func itemRows(item *passwordsItem, moment time.Time) []itemRow {
	var rows []itemRow
	switch {
	case item.Login != nil:
		login := item.Login
		rows = append(rows, itemRow{Label: "user name", Key: "userName", Value: login.UserName})
		rows = append(rows, itemRow{Label: "password", Key: "password", Value: login.Password, IsSecret: login.Password != "", Hidden: hiddenSecretWords})
		rows = append(rows, itemRow{Label: "sites", Key: "sites", Value: strings.Join(login.Sites, "\n")})
		if login.OneTimeCodeURI != "" {
			rows = append(rows, itemRow{Label: "one-time code", Key: "oneTimeCode", Value: oneTimeCodeWords(login.OneTimeCodeURI, moment)})
		}
		rows = append(rows, itemRow{Label: "notes", Key: "notes", Value: login.Notes})
		var passkeys []string
		for _, passkey := range login.Passkeys {
			passkeys = append(passkeys, strings.TrimSpace(passkey.RelyingPartyID+" "+passkey.UserName))
		}
		rows = append(rows, itemRow{Label: "passkeys", Key: "passkeys", Value: strings.Join(passkeys, "\n")})
	case item.Card != nil:
		card := item.Card
		rows = append(rows, itemRow{Label: "cardholder", Key: "cardholderName", Value: card.CardholderName})
		rows = append(rows, itemRow{Label: "number", Key: "number", Value: card.Number, IsSecret: card.Number != "", Hidden: "ending " + lastDigits(card.Number) + "; --reveal shows it"})
		expiry := ""
		if card.ExpiryMonth != "" || card.ExpiryYear != "" {
			expiry = card.ExpiryMonth + "/" + card.ExpiryYear
		}
		rows = append(rows, itemRow{Label: "expires", Key: "expiry", Value: expiry})
		rows = append(rows, itemRow{Label: "security code", Key: "securityCode", Value: card.SecurityCode, IsSecret: card.SecurityCode != "", Hidden: hiddenSecretWords})
		rows = append(rows, itemRow{Label: "brand", Key: "brand", Value: card.Brand})
		rows = append(rows, itemRow{Label: "notes", Key: "notes", Value: card.Notes})
	case item.Note != nil:
		rows = append(rows, itemRow{Label: "text", Key: "text", Value: item.Note.Text, IsSecret: item.Note.Text != "", Hidden: hiddenSecretWords})
	}
	return rows
}

// oneTimeCodeWords is a login's current code with the seconds it has left, or
// why there is none.
func oneTimeCodeWords(address string, moment time.Time) string {
	setting, err := passwords.ParseOneTimeCodeURI(address)
	if err != nil {
		return "not a usable one-time code address"
	}
	code, err := setting.CodeAt(moment)
	if err != nil {
		return "not a usable one-time code address"
	}
	return fmt.Sprintf("%s (%d s left)", code, setting.SecondsRemainingAt(moment))
}

func runPasswordsShow(ctx context.Context, command *cli.Command) error {
	typed, err := itemFindingArgument(command, "usage: teanode passwords show <item>")
	if err != nil {
		return err
	}
	_, item, err := findOpenItem(ctx, command, typed, false)
	if err != nil {
		return err
	}
	reveal := command.Bool("reveal")
	rows := itemRows(item, time.Now())
	summary := summarizeItem(item)
	if command.Bool("json") {
		fields := map[string]any{}
		hidden := []string{}
		for _, row := range rows {
			switch {
			case row.IsSecret && !reveal:
				hidden = append(hidden, row.Key)
			case row.Key == "sites" || row.Key == "passkeys":
				fields[row.Key] = splitLines(row.Value)
			default:
				fields[row.Key] = row.Value
			}
		}
		return PrintJSON(map[string]any{"item": summary, "fields": fields, "hiddenFields": hidden})
	}
	header := [][2]string{
		{"name", summary.Name}, {"kind", summary.ItemKind}, {"id", summary.ID},
	}
	if summary.Folder != "" {
		header = append(header, [2]string{"folder", summary.Folder})
	}
	if markers := itemMarkers(summary); markers != "" {
		header = append(header, [2]string{"marks", markers})
	}
	fields := header
	for _, row := range rows {
		if row.Value == "" {
			continue
		}
		value := row.Value
		if row.IsSecret && !reveal {
			value = row.Hidden
		}
		lines := splitLines(value)
		for index, line := range lines {
			label := ""
			if index == 0 {
				label = row.Label
			}
			fields = append(fields, [2]string{label, line})
		}
	}
	return printFields(fields)
}

func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

func runPasswordsCopy(ctx context.Context, command *cli.Command) error {
	typed, err := itemFindingArgument(command, "usage: teanode passwords copy <item> [--code|--user-name|--number|--security-code]")
	if err != nil {
		return err
	}
	selected := 0
	for _, flag := range []string{"code", "user-name", "number", "security-code"} {
		if command.Bool(flag) {
			selected++
		}
	}
	if selected > 1 {
		return usage("copy one thing at a time: --code, --user-name, --number or --security-code")
	}
	_, item, err := findOpenItem(ctx, command, typed, false)
	if err != nil {
		return err
	}
	what, value, err := valueToCopy(command, item)
	if err != nil {
		return err
	}
	clearAfter := int(command.Int("clear-after"))
	// Whatever ends the wait, the secret is not left on the clipboard: the time
	// passing, Ctrl-C, the terminal closing (SIGHUP), or the process being asked
	// to end. The signals are caught
	// before the secret is copied, so there is no moment it is there and they
	// are not.
	signals := make(chan os.Signal, 1)
	if clearAfter > 0 {
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer signal.Stop(signals)
	}
	tool, err := copyToClipboard(ctx, value)
	if err != nil {
		return err
	}
	if tool == "" {
		fmt.Fprintln(os.Stderr, "warning: this computer has no clipboard program (wl-copy, xclip, xsel or pbcopy), so the value is printed instead")
		// The person asked for the value; it goes to the terminal, not a log.
		if _, err := os.Stdout.WriteString(value + "\n"); err != nil {
			return err
		}
		return nil
	}
	if clearAfter <= 0 {
		fmt.Printf("copied the %s of %s to the clipboard\n", what, item.Name())
		return nil
	}
	fmt.Printf("copied the %s of %s to the clipboard; clearing it in %d seconds (Ctrl-C clears it now)\n", what, item.Name(), clearAfter)
	select {
	case <-time.After(time.Duration(clearAfter) * time.Second):
	case <-signals:
	case <-ctx.Done():
	}
	// A context that was cancelled cannot run the program that clears.
	if _, err := clearClipboardIfStillHolding(context.WithoutCancel(ctx), value); err != nil {
		return fmt.Errorf("cannot clear the clipboard: %w", err)
	}
	return nil
}

// valueToCopy is the secret a copy asks for, and what to call it.
func valueToCopy(command *cli.Command, item *passwordsItem) (what, value string, err error) {
	isMissing := func(name string) error {
		return fmt.Errorf("%s has no %s", item.Name(), name)
	}
	switch {
	case command.Bool("code"):
		if item.Login == nil || item.Login.OneTimeCodeURI == "" {
			return "", "", isMissing("one-time code")
		}
		setting, err := passwords.ParseOneTimeCodeURI(item.Login.OneTimeCodeURI)
		if err != nil {
			return "", "", fmt.Errorf("%s has a one-time code address that cannot be used: %w", item.Name(), err)
		}
		code, err := setting.CodeAt(time.Now())
		return "one-time code", code, err
	case command.Bool("user-name"):
		if item.Login == nil || item.Login.UserName == "" {
			return "", "", isMissing("user name")
		}
		return "user name", item.Login.UserName, nil
	case command.Bool("number"):
		if item.Card == nil || item.Card.Number == "" {
			return "", "", isMissing("card number")
		}
		return "card number", item.Card.Number, nil
	case command.Bool("security-code"):
		if item.Card == nil || item.Card.SecurityCode == "" {
			return "", "", isMissing("security code")
		}
		return "security code", item.Card.SecurityCode, nil
	}
	switch {
	case item.Login != nil:
		if item.Login.Password == "" {
			return "", "", isMissing("password")
		}
		return "password", item.Login.Password, nil
	case item.Card != nil:
		if item.Card.Number == "" {
			return "", "", isMissing("card number")
		}
		return "card number", item.Card.Number, nil
	}
	if item.Note.Text == "" {
		return "", "", isMissing("text")
	}
	return "text", item.Note.Text, nil
}

// oneTimeCodeAddress turns what a person typed for a one-time code into the
// address that is kept: an otpauth:// address as it is, or a base32 secret
// made into one named for the item.
func oneTimeCodeAddress(typed, name string) (string, error) {
	address, err := passwords.OneTimeCodeURIFromText(typed, name)
	if err != nil {
		return "", usage("--one-time-code is not an otpauth:// address or a base32 secret: " + err.Error())
	}
	return address, nil
}

// isTerminalInput is whether the person is typing at this command.
func isTerminalInput() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// applyFieldFlags sets the fields the person gave a flag for on an item. A new
// item asks for the secrets it was not given when it can ask on a terminal.
func applyFieldFlags(command *cli.Command, item *passwordsItem, isNew bool) error {
	if err := checkFlagsForKind(command, item.Kind); err != nil {
		return err
	}
	if command.IsSet("name") {
		name := strings.TrimSpace(command.String("name"))
		if name == "" {
			return usage("--name is empty")
		}
		setItemName(item, name)
	}
	if command.IsSet("folder") {
		setItemFolder(item, strings.TrimSpace(command.String("folder")))
	}
	switch {
	case item.Login != nil:
		return applyLoginFlags(command, item, isNew)
	case item.Card != nil:
		return applyCardFlags(command, item.Card, isNew)
	}
	return applyNoteFlags(command, item.Note)
}

func setItemName(item *passwordsItem, name string) {
	switch {
	case item.Login != nil:
		item.Login.Name = name
	case item.Card != nil:
		item.Card.Name = name
	case item.Note != nil:
		item.Note.Name = name
	}
}

func setItemFolder(item *passwordsItem, folder string) {
	switch {
	case item.Login != nil:
		item.Login.Folder = folder
	case item.Card != nil:
		item.Card.Folder = folder
	case item.Note != nil:
		item.Note.Folder = folder
	}
}

func applyLoginFlags(command *cli.Command, item *passwordsItem, isNew bool) error {
	login := item.Login
	if command.IsSet("user-name") {
		login.UserName = command.String("user-name")
	}
	if command.IsSet("site") {
		login.Sites = nil
		for _, site := range command.StringSlice("site") {
			login.Sites = append(login.Sites, strings.TrimSpace(site))
		}
	}
	for _, site := range command.StringSlice("add-site") {
		login.Sites = append(login.Sites, strings.TrimSpace(site))
	}
	if command.IsSet("notes") {
		login.Notes = command.String("notes")
	}
	if command.IsSet("one-time-code") {
		address, err := oneTimeCodeAddress(command.String("one-time-code"), login.Name)
		if err != nil {
			return err
		}
		login.OneTimeCodeURI = address
	}
	isPasswordGiven := 0
	for _, flag := range []string{"password", "password-stdin", "generate"} {
		if command.IsSet(flag) {
			isPasswordGiven++
		}
	}
	if isPasswordGiven > 1 {
		return usage("give the password one way: --password, --password-stdin or --generate")
	}
	switch {
	case command.IsSet("password"):
		login.Password = command.String("password")
	case command.Bool("password-stdin"):
		password, err := readHiddenLine(command, "", true, "password-stdin")
		if err != nil {
			return err
		}
		login.Password = password
	case command.Bool("generate"):
		password, err := generatePassword(int(command.Int("length")), !command.Bool("no-symbols"))
		if err != nil {
			return err
		}
		login.Password = password
	case isNew && isTerminalInput():
		password, err := readHiddenLine(command, "Password (empty for none): ", false, "password-stdin")
		if err != nil {
			return err
		}
		login.Password = password
	}
	return nil
}

func applyCardFlags(command *cli.Command, card *passwords.CardFields, isNew bool) error {
	for _, field := range []struct {
		flag   string
		target *string
	}{
		{"cardholder-name", &card.CardholderName}, {"expiry-month", &card.ExpiryMonth}, {"expiry-year", &card.ExpiryYear}, {"brand", &card.Brand}, {"notes", &card.Notes},
	} {
		if command.IsSet(field.flag) {
			*field.target = command.String(field.flag)
		}
	}
	for _, field := range []struct {
		flag   string
		prompt string
		target *string
	}{
		{"number", "Card number: ", &card.Number}, {"security-code", "Security code (empty for none): ", &card.SecurityCode},
	} {
		switch {
		case command.IsSet(field.flag):
			*field.target = command.String(field.flag)
		case isNew && isTerminalInput():
			typed, err := readHiddenLine(command, field.prompt, false, "passphrase-stdin")
			if err != nil {
				return err
			}
			*field.target = typed
		}
	}
	return nil
}

func applyNoteFlags(command *cli.Command, note *passwords.NoteFields) error {
	switch {
	case command.IsSet("text") && command.Bool("text-stdin"):
		return usage("give the text one way: --text or --text-stdin")
	case command.IsSet("text"):
		note.Text = command.String("text")
	case command.Bool("text-stdin"):
		text, err := readStandardInputToEnd(command)
		if err != nil {
			return err
		}
		note.Text = text
	}
	return nil
}

func runPasswordsAdd(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() > 1 {
		return usage("usage: teanode passwords add [name] [flags]")
	}
	kind := passwords.ItemKind(command.String("kind"))
	if !kind.IsValid() {
		return usage("--kind is login, card or note")
	}
	if command.IsSet("name") && command.Args().Len() == 1 {
		return usage("give the name as an argument or with --name, not both")
	}
	item := &passwordsItem{Kind: kind}
	switch kind {
	case passwords.ItemKindLogin:
		item.Login = &passwords.LoginFields{}
	case passwords.ItemKindCard:
		item.Card = &passwords.CardFields{}
	case passwords.ItemKindNote:
		item.Note = &passwords.NoteFields{}
	}
	if name := strings.TrimSpace(command.Args().First()); name != "" {
		setItemName(item, name)
	}
	// Unlock first: the passphrase is the first line of standard input, and a
	// password or a note's text comes after it.
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	if err := applyFieldFlags(command, item, true); err != nil {
		return err
	}
	if item.Name() == "" {
		return usage("give the item a name: teanode passwords add <name>")
	}
	view, err := session.saveNewItem(kind, item.Fields())
	if err != nil {
		return describeError(command, err)
	}
	item.View = view
	if command.Bool("json") {
		return PrintJSON(summarizeItem(item))
	}
	fmt.Printf("added %s (%s)\n", item.Name(), view.ID)
	if command.Bool("generate") {
		fmt.Printf("it has a generated password; 'teanode passwords copy %s' copies it\n", view.ID)
	}
	return nil
}

func runPasswordsEdit(ctx context.Context, command *cli.Command) error {
	typed, err := itemFindingArgument(command, "usage: teanode passwords edit <item> [flags]")
	if err != nil {
		return err
	}
	session, item, err := findOpenItem(ctx, command, typed, false)
	if err != nil {
		return err
	}
	if !item.IsOwner() {
		return fmt.Errorf("%s is shared with you by %s, and only its owner changes it", item.Name(), item.View.OwnerName)
	}
	if err := applyFieldFlags(command, item, false); err != nil {
		return err
	}
	view, err := session.saveChangedItem(item)
	if err != nil {
		return describeError(command, err)
	}
	item.View = view
	if command.Bool("json") {
		return PrintJSON(summarizeItem(item))
	}
	fmt.Printf("changed %s\n", item.Name())
	return nil
}

func runPasswordsDelete(ctx context.Context, command *cli.Command) error {
	typed, err := itemFindingArgument(command, "usage: teanode passwords delete <item>")
	if err != nil {
		return err
	}
	session, item, err := findOpenItem(ctx, command, typed, false)
	if err != nil {
		return err
	}
	if !item.IsOwner() {
		return fmt.Errorf("%s is shared with you by %s, and only its owner deletes it", item.Name(), item.View.OwnerName)
	}
	view, err := client.DeletePasswordItem(ctx, session.connection, item.View.ID)
	if err != nil {
		return describeError(command, err)
	}
	item.View = view
	if command.Bool("json") {
		return PrintJSON(summarizeItem(item))
	}
	fmt.Printf("deleted %s; 'teanode passwords restore %s' brings it back for 30 days\n", item.Name(), view.ID)
	return nil
}

func runPasswordsRestore(ctx context.Context, command *cli.Command) error {
	typed, err := itemFindingArgument(command, "usage: teanode passwords restore <item>; 'teanode passwords list --deleted' lists them")
	if err != nil {
		return err
	}
	session, item, err := findOpenItem(ctx, command, typed, true)
	if err != nil {
		return err
	}
	view, err := client.RestorePasswordItem(ctx, session.connection, item.View.ID)
	if err != nil {
		return describeError(command, err)
	}
	item.View = view
	if command.Bool("json") {
		return PrintJSON(summarizeItem(item))
	}
	fmt.Printf("restored %s\n", item.Name())
	return nil
}

func runPasswordsShare(ctx context.Context, command *cli.Command) error {
	if command.Bool("waiting") {
		if command.Args().Len() != 0 {
			return usage("usage: teanode passwords share --waiting")
		}
		return runPasswordsShareWaiting(ctx, command)
	}
	if command.Args().Len() != 2 {
		return usage("usage: teanode passwords share <item> <username|circle>; --waiting seals for people who have set Passwords up since")
	}
	session, item, err := findOpenItem(ctx, command, command.Args().Get(0), false)
	if err != nil {
		return err
	}
	if !item.IsOwner() {
		return fmt.Errorf("%s is shared with you by %s, and only its owner shares it", item.Name(), item.View.OwnerName)
	}
	typed := command.Args().Get(1)
	granteeUserID, granteeCircleID, described, err := shareTarget(ctx, session.connection, typed)
	if err != nil {
		return describeError(command, err)
	}
	newRecipients, err := client.ListPasswordRecipients(ctx, session.connection, granteeUserID, granteeCircleID)
	if err != nil {
		return describeNotFound(command, err, typed)
	}
	grantees := existingGrantees(item.View)
	isAlreadyShared := false
	for _, grantee := range grantees {
		isAlreadyShared = isAlreadyShared || sameGrantee(grantee, granteeUserID, granteeCircleID)
	}
	if !isAlreadyShared {
		grantees = append(grantees, newGrantee(granteeUserID, granteeCircleID))
	}
	return setItemShares(ctx, command, session, item, grantees, append(existingRecipients(item.View), newRecipients...), "sharing "+item.Name()+" with "+forTerminal(described))
}

func newGrantee(granteeUserID, granteeCircleID string) *client.PasswordShareGranteeInput {
	grantee := &client.PasswordShareGranteeInput{}
	if granteeUserID != "" {
		grantee.GranteeUserID = &granteeUserID
	}
	if granteeCircleID != "" {
		grantee.GranteeCircleID = &granteeCircleID
	}
	return grantee
}

func sameGrantee(grantee *client.PasswordShareGranteeInput, granteeUserID, granteeCircleID string) bool {
	return (grantee.GranteeUserID != nil && *grantee.GranteeUserID == granteeUserID) ||
		(grantee.GranteeCircleID != nil && *grantee.GranteeCircleID == granteeCircleID)
}

// existingGrantees are who the item is shared with now.
func existingGrantees(view *client.PasswordItemView) []*client.PasswordShareGranteeInput {
	grantees := []*client.PasswordShareGranteeInput{}
	for _, share := range view.Shares {
		grantee := &client.PasswordShareGranteeInput{GranteeUserID: share.GranteeUserID, GranteeCircleID: share.GranteeCircleID}
		grantees = append(grantees, grantee)
	}
	return grantees
}

// existingRecipients are everyone the item's shares reach now.
func existingRecipients(view *client.PasswordItemView) []*client.PasswordRecipientView {
	var recipients []*client.PasswordRecipientView
	for _, share := range view.Shares {
		recipients = append(recipients, share.Recipients...)
	}
	return recipients
}

// setItemShares replaces the item's shares. A key is sealed only for a
// recipient who has Passwords set up and has no key for the item yet, and only
// once their public key is known to this computer or confirmed now by its safety
// code. People who already have a key keep it: nothing is sealed again for them.
func setItemShares(ctx context.Context, command *cli.Command, session *passwordsSession, item *passwordsItem, grantees []*client.PasswordShareGranteeInput, recipients []*client.PasswordRecipientView, doing string) error {
	var newlyReached, waiting []*client.PasswordRecipientView
	reachedCount := 0
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if seen[recipient.UserID] {
			continue
		}
		seen[recipient.UserID] = true
		switch {
		case recipient.PublicKey == "":
			waiting = append(waiting, recipient)
		case recipient.HasKey:
			// Already holds the item key; nothing is sealed for them again.
			reachedCount++
		default:
			newlyReached = append(newlyReached, recipient)
		}
	}
	if err := session.confirmRecipientKeys(newlyReached); err != nil {
		return err
	}
	keys := []*client.PasswordShareKeyInput{}
	for _, recipient := range newlyReached {
		key, err := session.sealItemKeyFor(item.View.ID, item.ItemKey, recipient)
		if err != nil {
			return err
		}
		keys = append(keys, key.ForShare())
	}
	view, err := client.SetPasswordItemShares(ctx, session.connection, &client.SetPasswordItemSharesInput{ItemID: item.View.ID, Grantees: grantees, Keys: keys})
	if err != nil {
		return describeTapRequired(describeError(command, err), "Sharing with someone new is done on the Passwords page of the dashboard or in the iPhone app; a command line cannot do this, but taking someone away works here.")
	}
	item.View = view
	if command.Bool("json") {
		return PrintJSON(summarizeItem(item))
	}
	fmt.Printf("%s: %s can open it\n", doing, peopleWords(reachedCount+len(keys)))
	for _, recipient := range waiting {
		fmt.Printf("%s (%s) has not set up Passwords yet; run 'teanode passwords share --waiting' once they have\n", recipient.Name, recipient.Username)
	}
	return nil
}

func runPasswordsUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 || command.Args().Len() > 2 {
		return usage("usage: teanode passwords unshare <item> [username|circle]")
	}
	session, item, err := findOpenItem(ctx, command, command.Args().Get(0), false)
	if err != nil {
		return err
	}
	if !item.IsOwner() {
		return fmt.Errorf("%s is shared with you by %s, and only its owner changes who it is shared with", item.Name(), item.View.OwnerName)
	}
	remaining := []*client.PasswordShareView{}
	doing := "stopped sharing " + item.Name()
	if command.Args().Len() == 2 {
		typed := command.Args().Get(1)
		granteeUserID, granteeCircleID, described, err := shareTarget(ctx, session.connection, typed)
		if err != nil {
			return describeError(command, err)
		}
		isShared := false
		for _, share := range item.View.Shares {
			if (share.GranteeUserID != nil && *share.GranteeUserID == granteeUserID) || (share.GranteeCircleID != nil && *share.GranteeCircleID == granteeCircleID) {
				isShared = true
				continue
			}
			remaining = append(remaining, share)
		}
		if !isShared {
			return fmt.Errorf("%s is not shared with %s", item.Name(), forTerminal(described))
		}
		doing = "stopped sharing " + item.Name() + " with " + forTerminal(described)
	}
	// No keys are sent: the people who are still reached keep the key they
	// have, and the server forgets the keys of the people no longer reached.
	// Sealing again here would be sealing for public keys nobody checked.
	remainingView := &client.PasswordItemView{Shares: remaining}
	view, err := client.SetPasswordItemShares(ctx, session.connection, &client.SetPasswordItemSharesInput{
		ItemID: item.View.ID, Grantees: existingGrantees(remainingView), Keys: []*client.PasswordShareKeyInput{},
	})
	if err != nil {
		return describeError(command, err)
	}
	item.View = view
	if command.Bool("json") {
		return PrintJSON(summarizeItem(item))
	}
	fmt.Println(doing)
	return nil
}

func runPasswordsShareWaiting(ctx context.Context, command *cli.Command) error {
	session, err := openPasswordsForCommand(ctx, command)
	if err != nil {
		return err
	}
	waiting, err := client.ListWaitingPasswordShares(ctx, session.connection)
	if err != nil {
		return describeError(command, err)
	}
	if len(waiting) == 0 {
		if command.Bool("json") {
			return PrintJSON(map[string]any{"sealedCount": 0})
		}
		fmt.Println("nobody is waiting")
		return nil
	}
	items, err := session.loadItems(false)
	if err != nil {
		return describeError(command, err)
	}
	owned := map[string]*passwordsItem{}
	for _, item := range items {
		if item.IsOwner() {
			owned[item.View.ID] = item
		}
	}
	var recipients []*client.PasswordRecipientView
	seen := map[string]bool{}
	for _, each := range waiting {
		if each.Recipient != nil && each.Recipient.PublicKey != "" && !seen[each.Recipient.UserID] && owned[each.ItemID] != nil {
			seen[each.Recipient.UserID] = true
			recipients = append(recipients, each.Recipient)
		}
	}
	if err := session.confirmRecipientKeys(recipients); err != nil {
		return err
	}
	keys := []*client.PasswordItemShareKeyInput{}
	for _, each := range waiting {
		item := owned[each.ItemID]
		if item == nil || each.Recipient == nil || each.Recipient.PublicKey == "" {
			continue
		}
		key, err := session.sealItemKeyFor(item.View.ID, item.ItemKey, each.Recipient)
		if err != nil {
			return err
		}
		keys = append(keys, key)
	}
	sealedCount, err := client.AddPasswordShareKeys(ctx, session.connection, keys)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"sealedCount": sealedCount})
	}
	fmt.Printf("sealed %s for the people who were waiting\n", countWords(sealedCount, "item"))
	return nil
}

func runPasswordsAgent(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode passwords agent on|off <item>")
	}
	isOn := false
	switch strings.ToLower(command.Args().Get(0)) {
	case "on":
		isOn = true
	case "off":
	default:
		return usage("usage: teanode passwords agent on|off <item>")
	}
	if isOn {
		connection, err := openClient(command)
		if err != nil {
			return err
		}
		if err := refuseWithoutPasswordsTap(ctx, command, connection, passwordsAgentTapSentence); err != nil {
			return err
		}
	}
	session, item, err := findOpenItem(ctx, command, command.Args().Get(1), false)
	if err != nil {
		return err
	}
	if !item.IsOwner() {
		return fmt.Errorf("%s is shared with you by %s, and only its owner can let the agent use it", item.Name(), item.View.OwnerName)
	}
	if item.Card != nil {
		return errors.New("a card cannot be used by the agent")
	}
	input := &client.SetPasswordItemUsableByAgentInput{ItemID: item.View.ID, IsUsableByAgent: isOn}
	if isOn {
		warning := "This lets TeaNode's agent sign in with " + item.Name() + " in its browser, on the item's own sites, in any run of your agent, scheduled ones included. It never shows the password to the model."
		if item.Note != nil {
			warning = "This lets TeaNode's agent read the note " + item.Name() + ", which goes to your agent's model provider."
		}
		if err := confirm(command, warning); err != nil {
			return err
		}
		agentKey, err := client.ReadPasswordsAgentKey(ctx, session.connection)
		if err != nil {
			return describeError(command, err)
		}
		publicKey, err := decodePublicKey(agentKey)
		if err != nil {
			return errors.New("the agent key the server gave is not a key")
		}
		ownerID, err := session.currentUserID()
		if err != nil {
			return describeError(command, err)
		}
		sealed, err := passwords.SealForKeyPair(session.keys.KeyPair, publicKey, item.ItemKey, passwords.ShareData(item.View.ID, ownerID, passwords.AgentRecipientID))
		if err != nil {
			return fmt.Errorf("cannot seal the item for the agent key: %w", err)
		}
		input.ItemKeyByAgentKey = sealed.String()
	}
	view, err := client.SetPasswordItemUsableByAgent(ctx, session.connection, input)
	if err != nil {
		return describeTapRequired(describeError(command, err), passwordsAgentTapSentence)
	}
	item.View = view
	if command.Bool("json") {
		return PrintJSON(summarizeItem(item))
	}
	if isOn {
		fmt.Printf("the agent may use %s\n", item.Name())
	} else {
		fmt.Printf("the agent may no longer use %s\n", item.Name())
	}
	return nil
}

func runPasswordsGenerate(ctx context.Context, command *cli.Command) error {
	password, err := generatePassword(int(command.Int("length")), !command.Bool("no-symbols"))
	if err != nil {
		return err
	}
	// The person asked for a password; it goes to the terminal, not a log.
	_, err = os.Stdout.WriteString(password + "\n")
	return err
}

func runPasswordsSettings(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var setup *client.PasswordsSetupView
	if command.IsSet("lock-after-minutes") {
		minutes := int(command.Int("lock-after-minutes"))
		if minutes < models.PasswordsLockAfterMinutesLeast || minutes > models.PasswordsLockAfterMinutesMost {
			return usage(fmt.Sprintf("--lock-after-minutes is between %d and %d", models.PasswordsLockAfterMinutesLeast, models.PasswordsLockAfterMinutesMost))
		}
		setup, err = client.UpdatePasswordsSettings(ctx, connection, minutes)
	} else {
		setup, err = client.ReadPasswordsSetup(ctx, connection)
	}
	if err != nil {
		return describeError(command, err)
	}
	_, unlockedUntil, unlockErr := readPasswordsUnlock(command)
	if unlockErr != nil {
		return unlockErr
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"setup": setup, "isUnlockedHere": !unlockedUntil.IsZero(), "unlockedUntil": nullableTime(unlockedUntil)})
	}
	if !setup.IsSetUp {
		fmt.Println("Passwords is not set up yet; 'teanode passwords setup' does it (or the Passwords page on the dashboard, or the iPhone app)")
		return nil
	}
	here := "locked"
	if !unlockedUntil.IsZero() {
		here = "unlocked until " + unlockedUntil.Format("15:04")
	}
	return printFields([][2]string{
		{"set up", "yes"}, {"items", fmt.Sprint(setup.ItemCount)},
		{"lock after", countWords(setup.LockAfterMinutes, "minute")}, {"this computer", here},
	})
}

// peopleWords is a count of people: 1 person, 3 people.
func peopleWords(count int) string {
	if count == 1 {
		return "1 person"
	}
	return fmt.Sprintf("%d people", count)
}

func nullableTime(moment time.Time) *time.Time {
	if moment.IsZero() {
		return nil
	}
	return &moment
}

func runPasswordsChangePassphrase(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := refuseWithoutPasswordsTap(ctx, command, connection, passwordsChangePassphraseTapSentence); err != nil {
		return err
	}
	setup, err := readSetUpPasswords(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	var secret passwordsSecret
	if command.Bool("recovery-key") {
		secret.RecoveryKey, err = readRecoveryKey(command)
	} else {
		secret.Passphrase, err = readPassphrase(command)
	}
	if err != nil {
		return err
	}
	keys, err := unlockPasswords(ctx, connection, setup, secret)
	if err != nil {
		return describeError(command, err)
	}
	proof, _, err := secret.proof(setup)
	if err != nil {
		return err
	}
	newPassphrase, err := readNewPassphrase(command, "New Passwords passphrase: ")
	if err != nil {
		return err
	}
	salt, err := passwords.NewSalt()
	if err != nil {
		return err
	}
	passphraseKey, err := passwords.DerivePassphraseKey(newPassphrase, salt, passwords.DefaultIterationCount)
	if err != nil {
		return err
	}
	accountKeyByPassphrase, passphraseProofHash, err := sealAccountKeyUnder(keys.AccountKey, passphraseKey)
	if err != nil {
		return err
	}
	if _, err := client.ChangePasswordsPassphrase(ctx, connection, &client.ChangePasswordsPassphraseInput{
		Proof: proof, IsRecoveryKey: secret.RecoveryKey != "", AccountKeyByPassphrase: accountKeyByPassphrase,
		PassphraseSalt: base64.StdEncoding.EncodeToString(salt), PassphraseIterationCount: passwords.DefaultIterationCount, PassphraseProofHash: passphraseProofHash,
	}); err != nil {
		return describeTapRequired(describeError(command, err), passwordsChangePassphraseTapSentence)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"isPassphraseChanged": true})
	}
	fmt.Println("the passphrase is changed; the old one no longer opens Passwords on this server, and your other devices ask for the new one when they lock")
	fmt.Println("your account key is not replaced, so a copy of the database from before this change still opens with the old passphrase")
	return nil
}

func runPasswordsNewRecoveryKey(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := refuseWithoutPasswordsTap(ctx, command, connection, passwordsNewRecoveryKeyTapSentence); err != nil {
		return err
	}
	setup, err := readSetUpPasswords(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	passphrase, err := readPassphrase(command)
	if err != nil {
		return err
	}
	secret := passwordsSecret{Passphrase: passphrase}
	keys, err := unlockPasswords(ctx, connection, setup, secret)
	if err != nil {
		return describeError(command, err)
	}
	proof, _, err := secret.proof(setup)
	if err != nil {
		return err
	}
	recoveryKeyBytes, err := passwords.NewRecoveryKey()
	if err != nil {
		return err
	}
	recoveryKey, err := passwords.FormatRecoveryKey(recoveryKeyBytes)
	if err != nil {
		return err
	}
	accountKeyByRecoveryKey, recoveryKeyProofHash, err := sealAccountKeyUnder(keys.AccountKey, recoveryKeyBytes)
	if err != nil {
		return err
	}
	if _, err := client.ReplacePasswordsRecoveryKey(ctx, connection, &client.ReplacePasswordsRecoveryKeyInput{
		Proof: proof, AccountKeyByRecoveryKey: accountKeyByRecoveryKey, RecoveryKeyProofHash: recoveryKeyProofHash,
	}); err != nil {
		return describeTapRequired(describeError(command, err), passwordsNewRecoveryKeyTapSentence)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"recoveryKey": recoveryKey})
	}
	fmt.Println("Your new recovery key. It is shown only once, and the old one no longer opens Passwords on this server:")
	fmt.Println()
	fmt.Println("    " + recoveryKey)
	fmt.Println()
	fmt.Println("Keep it somewhere safe, away from this computer. Anyone who has it can open your passwords.")
	fmt.Println("Your account key is not replaced, so a copy of the database from before this change still opens with the old recovery key.")
	return nil
}

func runPasswordsErase(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := refuseWithoutPasswordsTap(ctx, command, connection, passwordsEraseTapSentence); err != nil {
		return err
	}
	if err := confirm(command, "This erases every item in Passwords, its shares and keys, and the passphrase and recovery key that open it, for you and for everyone you share with. "+
		"Nobody can get them back, not even whoever runs this server. If you have lost both your passphrase and your recovery key, this is the only way to start again."); err != nil {
		return err
	}
	if _, err := client.ErasePasswords(ctx, connection); err != nil {
		return describeTapRequired(describeError(command, err), passwordsEraseTapSentence)
	}
	_, _ = removePasswordsUnlock(command)
	if command.Bool("json") {
		return PrintJSON(map[string]any{"isErased": true})
	}
	fmt.Println("Passwords is erased")
	return nil
}

// sortedKeys are a map's keys in order, so that output does not change from
// one run to the next.
func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
