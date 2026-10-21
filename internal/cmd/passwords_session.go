package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// The part of `teanode passwords` that handles keys and items. Nothing the
// person keeps ever reaches the server in the clear: this file opens the keys
// the server hands back, opens and seals items, and keeps the opened keys for
// the lock time. The commands in passwords.go call it.

// passwordsMinimumPassphraseLength is the fewest characters a new passphrase
// may have. Sealed keys are only as strong as the passphrase they come from,
// and whoever holds a copy of the database can try passphrases at the speed of
// their own computers.
const passwordsMinimumPassphraseLength = 12

// passwordsUnlockLifetimeMost is the longest the opened keys are left in their
// file, whatever lock time the person chose: a week is right for a phone that
// opens with Face ID, and too long for keys written to a disk.
const passwordsUnlockLifetimeMost = 8 * time.Hour

// passwordsKeys is what unlocking opens: the account key, which opens every
// item the person owns, and the key pair, which opens what is shared with them.
type passwordsKeys struct {
	AccountKey []byte
	KeyPair    *passwords.KeyPair
}

// passwordsUnlockFile is what `teanode passwords unlock` leaves behind so that
// the commands after it do not ask again. The two keys are base64 and not
// encrypted, so the file is kept only in $XDG_RUNTIME_DIR, a directory of the
// person's login that is emptied at logout and is not backed up. Where there is
// none (macOS, some Linux sessions) `unlock` refuses rather than write the keys
// to a disk. The file is readable by its owner only and is refused when anyone
// else could have read or changed it. It is removed by `teanode passwords lock`,
// and by the next passwords command that finds it past ExpiresAt; nothing
// removes it at ExpiresAt itself, which is why it is in a directory that is
// emptied at logout. It never outlives passwordsUnlockLifetimeMost.
type passwordsUnlockFile struct {
	Server     string    `json:"server"`
	AccountKey string    `json:"accountKey"`
	PrivateKey string    `json:"privateKey"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

var unlockFileUnsafeCharacters = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// errPasswordsNoRuntimeDirectory is why `unlock` does not keep the keys on a
// computer with no $XDG_RUNTIME_DIR.
var errPasswordsNoRuntimeDirectory = errors.New("this computer has no per-login runtime directory ($XDG_RUNTIME_DIR), and the keys unlock keeps are not encrypted, so they are not written to a disk; " +
	"each passwords command will ask for the passphrase instead, or pass --passphrase-stdin to read it from a script")

// passwordsUnlockPaths are where the opened keys of the profile this command
// talks to are kept, and the name of its server. The keys are kept only in the
// runtime directory, so path is empty when there is none. leftover is where an
// earlier version wrote them when it had no runtime directory, which is only
// ever removed.
func passwordsUnlockPaths(command *cli.Command) (path, leftover, server string, err error) {
	name, server, err := passwordsProfile(command)
	if err != nil {
		return "", "", "", err
	}
	configurationDirectory, err := passwordsConfigurationDirectory()
	if err != nil {
		return "", "", "", err
	}
	filename := passwords.UnlockFileName(name)
	if runtimeDirectory := passwordsRuntimeDirectory(); runtimeDirectory != "" {
		path = filepath.Join(runtimeDirectory, "teanode", filename)
	}
	return path, filepath.Join(configurationDirectory, filename), server, nil
}

// readPasswordsUnlock gives the keys kept by `unlock` and when they stop being
// kept. It gives nil when there are none, they have expired or are kept longer
// than they ever may be (the file is removed then) or they belong to another
// server. A file that others could have read or changed is an error, not
// something to use.
func readPasswordsUnlock(command *cli.Command) (*passwordsKeys, time.Time, error) {
	if err := refuseAgentCommand(); err != nil {
		return nil, time.Time{}, err
	}
	path, leftover, server, err := passwordsUnlockPaths(command)
	if err != nil {
		return nil, time.Time{}, err
	}
	// Keys an earlier build left in the configuration directory are on a disk
	// and in backups with no end time that anything enforces: delete them.
	_, _ = removeFile(leftover)
	if path == "" {
		return nil, time.Time{}, nil
	}
	content, err := readPrivateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	var kept passwordsUnlockFile
	now := time.Now()
	if err := json.Unmarshal(content, &kept); err != nil || kept.Server != server || !now.Before(kept.ExpiresAt) || kept.ExpiresAt.After(now.Add(passwordsUnlockLifetimeMost)) {
		_, _ = removeFile(path)
		return nil, time.Time{}, nil
	}
	accountKey, accountKeyErr := base64.StdEncoding.DecodeString(kept.AccountKey)
	privateKey, privateKeyErr := base64.StdEncoding.DecodeString(kept.PrivateKey)
	if accountKeyErr != nil || privateKeyErr != nil || len(accountKey) != passwords.KeyLength {
		_, _ = removeFile(path)
		return nil, time.Time{}, nil
	}
	keyPair, err := passwords.KeyPairFromPrivateKey(privateKey)
	if err != nil {
		_, _ = removeFile(path)
		return nil, time.Time{}, nil
	}
	return &passwordsKeys{AccountKey: accountKey, KeyPair: keyPair}, kept.ExpiresAt, nil
}

// writePasswordsUnlock keeps the opened keys until the lock time passes, but
// never more than passwordsUnlockLifetimeMost.
func writePasswordsUnlock(command *cli.Command, keys *passwordsKeys, lockAfter time.Duration) (time.Time, error) {
	if err := refuseAgentCommand(); err != nil {
		return time.Time{}, err
	}
	path, leftover, server, err := passwordsUnlockPaths(command)
	if err != nil {
		return time.Time{}, err
	}
	if path == "" {
		return time.Time{}, errPasswordsNoRuntimeDirectory
	}
	lockAfter = min(lockAfter, passwordsUnlockLifetimeMost)
	expiresAt := time.Now().Add(lockAfter).Truncate(time.Second)
	content, err := json.Marshal(passwordsUnlockFile{
		Server:     server,
		AccountKey: base64.StdEncoding.EncodeToString(keys.AccountKey),
		PrivateKey: base64.StdEncoding.EncodeToString(keys.KeyPair.PrivateKey()),
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		return time.Time{}, err
	}
	if err := writePrivateFile(path, append(content, '\n')); err != nil {
		return time.Time{}, err
	}
	// The one place the keys are kept; a copy an earlier version left in the
	// configuration directory is not kept at all.
	_, _ = removeFile(leftover)
	return expiresAt, nil
}

// removePasswordsUnlock forgets the kept keys, wherever they are, and says
// whether there were any.
func removePasswordsUnlock(command *cli.Command) (bool, error) {
	path, leftover, _, err := passwordsUnlockPaths(command)
	if err != nil {
		return false, err
	}
	wasUnlocked := false
	for _, each := range []string{path, leftover} {
		if each == "" {
			continue
		}
		wasRemoved, err := removeFile(each)
		if err != nil {
			return false, err
		}
		wasUnlocked = wasUnlocked || wasRemoved
	}
	return wasUnlocked, nil
}

// refuseAgentCommand is the refusal for a command the agent runs: the computer
// program marks every shell command and terminal it starts for the agent (see
// passwords.AgentCommandEnvironmentName), and a marked command does not open
// Passwords, read or write the file `unlock` keeps the keys in, or ask for the
// passphrase. This stops the obvious routes (`passwords show --reveal`,
// `passwords export`, `passwords copy`, reading the unlock file); a command
// the agent runs runs as the person and can unset the mark in a program of its
// own, so it is a guard and not a boundary.
func refuseAgentCommand() error {
	if passwords.IsAgentCommand() {
		return errors.New(passwords.AgentCommandRefusalWords)
	}
	return nil
}

// passwordsInput is where a passphrase, a recovery key or a password is read
// from when it is not asked for on a terminal: the standard input of the root
// command, which a test replaces.
func passwordsInput(command *cli.Command) *bufio.Reader {
	var input io.Reader = os.Stdin
	if root := command.Root(); root != nil && root.Reader != nil {
		input = root.Reader
	}
	return readerOf(input)
}

// readHiddenLine asks for a secret on the terminal without echoing it. With
// fromStandardInput it reads one line from standard input instead, for
// scripts, and keeps every character of it but the line ending: a passphrase
// may begin or end with a space. Without a terminal and without that it
// refuses, rather than read a secret from a pipe nobody meant it for.
func readHiddenLine(command *cli.Command, prompt string, fromStandardInput bool, standardInputFlag string) (string, error) {
	if fromStandardInput {
		line, err := passwordsInput(command).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("cannot read from standard input: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	descriptor := int(os.Stdin.Fd())
	if !term.IsTerminal(descriptor) {
		return "", usage("this needs a terminal to type on; pass --" + standardInputFlag + " to read it from standard input")
	}
	fmt.Fprint(os.Stderr, prompt)
	secret, err := term.ReadPassword(descriptor)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("cannot read from the terminal: %w", err)
	}
	return string(secret), nil
}

// passphraseFlag is offered by every command that asks for the passphrase. The
// passphrase is never an argument or an environment variable, which a process
// listing or a shell history would show.
func passphraseFlag() cli.Flag {
	return &cli.BoolFlag{Name: "passphrase-stdin", Usage: "read the passphrase from the first line of standard input, for scripts"}
}

// readPassphrase asks for the current passphrase.
func readPassphrase(command *cli.Command) (string, error) {
	passphrase, err := readHiddenLine(command, "Passwords passphrase: ", command.Bool("passphrase-stdin"), "passphrase-stdin")
	if err != nil {
		return "", err
	}
	if passphrase == "" {
		return "", usage("the passphrase is empty")
	}
	return passphrase, nil
}

// readRecoveryKey asks for the recovery key.
func readRecoveryKey(command *cli.Command) (string, error) {
	recoveryKey, err := readHiddenLine(command, "Recovery key: ", command.Bool("passphrase-stdin"), "passphrase-stdin")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(recoveryKey) == "" {
		return "", usage("the recovery key is empty")
	}
	return recoveryKey, nil
}

// readNewPassphrase asks for a passphrase to set, twice on a terminal so that
// a typo does not lock the person out, and once from standard input.
func readNewPassphrase(command *cli.Command, prompt string) (string, error) {
	fromStandardInput := command.Bool("passphrase-stdin")
	first, err := readHiddenLine(command, prompt, fromStandardInput, "passphrase-stdin")
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(first) < passwordsMinimumPassphraseLength {
		return "", usage(fmt.Sprintf("a passphrase has at least %d characters", passwordsMinimumPassphraseLength))
	}
	if !fromStandardInput {
		second, err := readHiddenLine(command, "Again: ", false, "passphrase-stdin")
		if err != nil {
			return "", err
		}
		if first != second {
			return "", usage("the two passphrases do not match")
		}
	}
	return first, nil
}

// passwordsSecret is a secret that proves the person may open Passwords: the
// passphrase, or the recovery key.
type passwordsSecret struct {
	Passphrase  string
	RecoveryKey string
}

// secretBytes are the 32 bytes the wrap key and the proof key are made from:
// the passphrase key, or the recovery key itself.
func (self passwordsSecret) secretBytes(setup *client.PasswordsSetupView) ([]byte, error) {
	if self.RecoveryKey != "" {
		return passwords.ParseRecoveryKey(self.RecoveryKey)
	}
	salt, err := passwords.DecodeCanonicalBase64(setup.PassphraseSalt)
	if err != nil {
		return nil, errors.New("the server's passphrase salt is not base64; the server may be misbehaving")
	}
	// The count comes from the server: a low one would make the passphrase
	// cheap to guess from the proof, a very high one would keep this computer
	// busy for hours.
	if err := passwords.CheckIterationCount(setup.PassphraseIterationCount); err != nil {
		return nil, fmt.Errorf("the server asked for %d passphrase iterations, which is not from %d to %d; the server may be misbehaving",
			setup.PassphraseIterationCount, passwords.MinimumIterationCount, passwords.MaximumIterationCount)
	}
	return passwords.DerivePassphraseKey(self.Passphrase, salt, setup.PassphraseIterationCount)
}

// proof is what the server is sent to show the person knows the secret.
func (self passwordsSecret) proof(setup *client.PasswordsSetupView) (proof string, wrapKey []byte, err error) {
	secret, err := self.secretBytes(setup)
	if err != nil {
		return "", nil, err
	}
	proofKey, err := passwords.ProofKey(secret)
	if err != nil {
		return "", nil, err
	}
	wrapKey, err = passwords.WrapKey(secret)
	if err != nil {
		return "", nil, err
	}
	return base64.StdEncoding.EncodeToString(proofKey), wrapKey, nil
}

// readSetUpPasswords is ReadPasswordsSetup, refusing a person who has not set
// Passwords up with the words that say how.
func readSetUpPasswords(ctx context.Context, connection *client.Client) (*client.PasswordsSetupView, error) {
	setup, err := client.ReadPasswordsSetup(ctx, connection)
	if err != nil {
		return nil, err
	}
	if setup == nil || !setup.IsSetUp {
		return nil, errors.New("you have not set Passwords up yet; 'teanode passwords setup' does it (or the Passwords page on the dashboard, or the iPhone app)")
	}
	return setup, nil
}

// unlockPasswords proves the secret to the server and opens the keys it sends
// back. A recovery key proves with its own proof; the server answers with the
// account key sealed by it.
func unlockPasswords(ctx context.Context, connection *client.Client, setup *client.PasswordsSetupView, secret passwordsSecret) (*passwordsKeys, error) {
	if err := refuseAgentCommand(); err != nil {
		return nil, err
	}
	proof, wrapKey, err := secret.proof(setup)
	if errors.Is(err, passwords.ErrInvalidRecoveryKey) {
		return nil, usage("that is not a recovery key; it is 13 groups of 4 letters and digits")
	}
	if err != nil {
		return nil, err
	}
	unlocked, err := client.UnlockPasswords(ctx, connection, proof, secret.RecoveryKey != "")
	if err != nil {
		return nil, err
	}
	sealedAccountKey, err := passwords.ParseSealed(unlocked.AccountKeyWrapped)
	if err != nil {
		return nil, fmt.Errorf("the account key the server sent is not readable: %w", err)
	}
	accountKey, err := passwords.Open(wrapKey, sealedAccountKey, passwords.AccountKeyData())
	if err != nil {
		return nil, errors.New("the server's answer did not open with that secret; the server may be misbehaving")
	}
	sealedPrivateKey, err := passwords.ParseSealed(unlocked.PrivateKeyByAccountKey)
	if err != nil {
		return nil, fmt.Errorf("the private key the server sent is not readable: %w", err)
	}
	privateKey, err := passwords.Open(accountKey, sealedPrivateKey, passwords.PrivateKeyData())
	if err != nil {
		return nil, errors.New("the private key the server sent does not open with your account key; the server may be misbehaving")
	}
	keyPair, err := passwords.KeyPairFromPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("the private key is not valid: %w", err)
	}
	if unlocked.PublicKey != "" && unlocked.PublicKey != base64.StdEncoding.EncodeToString(keyPair.PublicKey()) {
		return nil, errors.New("the public key the server sent is not the one your private key makes; the server may be misbehaving")
	}
	return &passwordsKeys{AccountKey: accountKey, KeyPair: keyPair}, nil
}

// passwordsSession is an unlocked Passwords on one connection: the keys, and
// who the person is, which an item shared with them is sealed for.
type passwordsSession struct {
	ctx        context.Context
	command    *cli.Command
	connection *client.Client
	keys       *passwordsKeys
	userID     string

	// known are the public keys of the people this computer has shared with or
	// received from, read when first needed.
	known *passwordsKnownKeys

	// checkedOwnerKeys remembers, for the length of this command, what was
	// decided about an owner's key, so that a changed key is asked about once.
	checkedOwnerKeys map[string]*ownerKeyDecision

	// isKnownKeysFaultTold is whether the file of known keys being unusable was
	// already told, so that it is told once.
	isKnownKeysFaultTold bool
	// automaticRekeyCount is how many re-keys nobody asked for this command has
	// made.
	automaticRekeyCount int
}

// openPasswords gives the keys `unlock` kept, or asks for the passphrase when
// there are none. What it asks for it does not keep: only `unlock` leaves the
// keys on disk.
func openPasswords(ctx context.Context, command *cli.Command, connection *client.Client) (*passwordsSession, error) {
	if err := refuseAgentCommand(); err != nil {
		return nil, err
	}
	keys, _, err := readPasswordsUnlock(command)
	if err != nil {
		return nil, err
	}
	if keys != nil && command.Bool("passphrase-stdin") {
		// A script that pipes the passphrase first and then a secret must get
		// the same result whether or not an earlier `unlock` left the keys: the
		// first line is the passphrase either way, so it is read and dropped
		// here, and the next line is not mistaken for it.
		if _, err := readHiddenLine(command, "", true, "passphrase-stdin"); err != nil {
			return nil, err
		}
	}
	if keys == nil {
		setup, err := readSetUpPasswords(ctx, connection)
		if err != nil {
			return nil, err
		}
		passphrase, err := readPassphrase(command)
		if err != nil {
			return nil, err
		}
		keys, err = unlockPasswords(ctx, connection, setup, passwordsSecret{Passphrase: passphrase})
		if err != nil {
			return nil, err
		}
	}
	return &passwordsSession{ctx: ctx, command: command, connection: connection, keys: keys}, nil
}

// currentUserID is the person's own user id, asked once.
func (self *passwordsSession) currentUserID() (string, error) {
	if self.userID != "" {
		return self.userID, nil
	}
	user, err := client.GetCurrentUser(self.ctx, self.connection)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", errors.New("the server does not say who you are")
	}
	self.userID = user.ID
	return self.userID, nil
}

// passwordsItem is an item opened: the fields in the clear and the item key.
// Exactly one of Login, Card and Note is set, the one Kind names.
type passwordsItem struct {
	View    *client.PasswordItemView
	Kind    passwords.ItemKind
	ItemKey []byte
	Login   *passwords.LoginFields
	Card    *passwords.CardFields
	Note    *passwords.NoteFields

	// AccessCheck is what opening found out about the access record sealed in
	// the fields of an item the person owns (see passwordsAccessCheck).
	AccessCheck passwordsAccessCheck
	// VerifiedAccess is the access record when the owner's devices signed it
	// and it is not an old one shown again, and nil otherwise. Everything that
	// decides who may have the item or the agent reads this, never Access.
	VerifiedAccess *passwords.PasswordAccess
	// HighestRecordVersionSeen is the highest version of the access record
	// this computer has seen with a valid signature, which the next signed
	// record counts up from.
	HighestRecordVersionSeen int
	// IsRemembered is whether this computer had written down a record of the item
	// before the command that opened it: from an earlier session, or from its own
	// earlier action. A re-key nobody asked for happens only for such an item.
	IsRemembered bool
	// SealedForUserIDs and HasMadeAgentCopy are what this computer itself did for
	// the item before the command that opened it (see passwordsKnownItem). A
	// re-key nobody asked for never goes beyond them.
	SealedForUserIDs []string
	HasMadeAgentCopy bool
}

// Name is the item's name.
func (self *passwordsItem) Name() string {
	switch {
	case self.Login != nil:
		return self.Login.Name
	case self.Card != nil:
		return self.Card.Name
	case self.Note != nil:
		return self.Note.Name
	}
	return ""
}

// Folder is the folder the item is in.
func (self *passwordsItem) Folder() string {
	switch {
	case self.Login != nil:
		return self.Login.Folder
	case self.Card != nil:
		return self.Card.Folder
	case self.Note != nil:
		return self.Note.Folder
	}
	return ""
}

// UserName is a login's user name, or a card's cardholder.
func (self *passwordsItem) UserName() string {
	switch {
	case self.Login != nil:
		return self.Login.UserName
	case self.Card != nil:
		return self.Card.CardholderName
	}
	return ""
}

// Sites are a login's sites, and nothing for the other kinds.
func (self *passwordsItem) Sites() []string {
	if self.Login != nil {
		return self.Login.Sites
	}
	return nil
}

// FirstSite is the first of a login's sites.
func (self *passwordsItem) FirstSite() string {
	if sites := self.Sites(); len(sites) > 0 {
		return sites[0]
	}
	return ""
}

// IsOwner is whether the person made the item, and so may change it.
func (self *passwordsItem) IsOwner() bool {
	return !self.View.IsSharedWithMe
}

// Access is the record sealed inside the fields as it is there: who may have
// the item and whether the agent may use it, according to whoever wrote it. It
// is not to be believed until VerifiedAccess says the owner's devices signed
// it. Nil is no record at all.
func (self *passwordsItem) Access() *passwords.PasswordAccess {
	switch {
	case self.Login != nil:
		return self.Login.Access
	case self.Card != nil:
		return self.Card.Access
	case self.Note != nil:
		return self.Note.Access
	}
	return nil
}

// setAccess puts a new access record in the fields, to be sealed with them.
func (self *passwordsItem) setAccess(access *passwords.PasswordAccess) {
	switch {
	case self.Login != nil:
		self.Login.Access = access
	case self.Card != nil:
		self.Card.Access = access
	case self.Note != nil:
		self.Note.Access = access
	}
}

// isUsableByAgentBySealedAccess is whether the owner's own signed record lets
// the agent use the item, which is the word of the owner's device and not the
// server's.
func (self *passwordsItem) isUsableByAgentBySealedAccess() bool {
	return self.VerifiedAccess != nil && self.VerifiedAccess.IsUsableByAgent
}

// Fields are the fields to seal, whichever the kind.
func (self *passwordsItem) Fields() any {
	switch {
	case self.Login != nil:
		return self.Login
	case self.Card != nil:
		return self.Card
	}
	return self.Note
}

// fieldsWithoutAccess are the fields without the record of who may have the
// item, for a file that leaves this computer: the record is for this server
// and these people, and means nothing in another password manager.
func (self *passwordsItem) fieldsWithoutAccess() any {
	switch {
	case self.Login != nil:
		fields := *self.Login
		fields.Access = nil
		return &fields
	case self.Card != nil:
		fields := *self.Card
		fields.Access = nil
		return &fields
	}
	fields := *self.Note
	fields.Access = nil
	return &fields
}

// duplicateKey is what makes two items the same one for an import: the kind,
// the name, the folder, the first site, the user name (without case) and the
// password or note text. Two logins that differ in any of them
// are kept both, so a changed password is not lost to the old one.
func (self *passwordsItem) duplicateKey() string {
	return duplicateKeyOf(self.Kind, self.Name(), self.Folder(), self.FirstSite(), self.Login, self.Note)
}

func duplicateKeyOf(kind passwords.ItemKind, name, folder, firstSite string, login *passwords.LoginFields, note *passwords.NoteFields) string {
	userName, secret := "", ""
	if login != nil {
		userName, secret = login.UserName, login.Password
	}
	if note != nil {
		secret = note.Text
	}
	// The key lives only in memory for one import, so the secret itself is
	// part of it rather than a digest of it.
	return strings.Join([]string{
		string(kind),
		strings.ToLower(strings.TrimSpace(name)),
		strings.ToLower(strings.TrimSpace(folder)),
		strings.TrimSuffix(strings.ToLower(strings.TrimSpace(firstSite)), "/"),
		strings.ToLower(strings.TrimSpace(userName)),
		secret,
	}, "\x00")
}

// openItem opens an item the server sent. A person's own item opens with the
// account key; one shared with them opens with their key pair.
func (self *passwordsSession) openItem(view *client.PasswordItemView) (*passwordsItem, error) {
	kind := passwords.ItemKind(view.ItemKind)
	if !kind.IsValid() {
		return nil, fmt.Errorf("item %s is of a kind this command does not know: %q", view.ID, view.ItemKind)
	}
	var itemKey []byte
	if view.IsSharedWithMe {
		userID, err := self.currentUserID()
		if err != nil {
			return nil, err
		}
		ownerPublicKey, err := self.checkedOwnerKey(view)
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", view.ID, err)
		}
		sealed, err := passwords.ParseSealedForKeyPair(view.ItemKey)
		if err != nil {
			return nil, fmt.Errorf("item %s: its key is not readable: %w", view.ID, err)
		}
		itemKey, err = self.keys.KeyPair.Open(sealed, ownerPublicKey, passwords.ShareData(view.ID, view.OwnerUserID, userID))
		if err != nil {
			return nil, fmt.Errorf("item %s: its key does not open: %w", view.ID, err)
		}
	} else {
		sealed, err := passwords.ParseSealed(view.ItemKey)
		if err != nil {
			return nil, fmt.Errorf("item %s: its key is not readable: %w", view.ID, err)
		}
		itemKey, err = passwords.Open(self.keys.AccountKey, sealed, passwords.ItemKeyData(view.ID))
		if err != nil {
			return nil, fmt.Errorf("item %s: its key does not open: %w", view.ID, err)
		}
	}
	sealedFields, err := passwords.ParseSealed(view.EncryptedFields)
	if err != nil {
		return nil, fmt.Errorf("item %s: its fields are not readable: %w", view.ID, err)
	}
	plain, err := passwords.Open(itemKey, sealedFields, passwords.ItemFieldsData(view.ID, kind))
	if err != nil {
		return nil, fmt.Errorf("item %s: its fields do not open: %w", view.ID, err)
	}
	if view.IsSharedWithMe {
		// The owner's key opened this item, so it is worth writing down now.
		self.recordOwnerKey(view)
	}
	item := &passwordsItem{View: view, Kind: kind, ItemKey: itemKey}
	if !view.IsSharedWithMe {
		self.checkSealedAccess(item, plain)
	}
	switch kind {
	case passwords.ItemKindLogin:
		item.Login = &passwords.LoginFields{}
		err = json.Unmarshal(plain, item.Login)
	case passwords.ItemKindCard:
		item.Card = &passwords.CardFields{}
		err = json.Unmarshal(plain, item.Card)
	case passwords.ItemKindNote:
		item.Note = &passwords.NoteFields{}
		err = json.Unmarshal(plain, item.Note)
	}
	if err != nil {
		return nil, fmt.Errorf("item %s: its fields are not JSON: %w", view.ID, err)
	}
	return item, nil
}

// loadItems opens every item the person can reach, the deleted ones too when
// asked. An item that does not open is counted, not fatal: it is told on
// standard error, so that the rest are still usable.
func (self *passwordsSession) loadItems(includeDeleted bool) ([]*passwordsItem, error) {
	list, err := client.ListPasswordItems(self.ctx, self.connection, nil)
	if err != nil {
		return nil, err
	}
	var items []*passwordsItem
	failedCount := 0
	for _, view := range list.Items {
		if view.DeletedAt != nil && !includeDeleted {
			continue
		}
		item, err := self.openItem(view)
		if err != nil {
			failedCount++
			fmt.Fprintf(os.Stderr, "note: %s\n", forTerminal(err.Error()))
			continue
		}
		items = append(items, item)
	}
	if failedCount > 0 {
		fmt.Fprintf(os.Stderr, "note: %s could not be opened and are left out\n", countWords(failedCount, "item"))
	}
	sort.SliceStable(items, func(first, second int) bool {
		return strings.ToLower(items[first].Name()) < strings.ToLower(items[second].Name())
	})
	return items, nil
}

// findItem picks the one item the person typed: its id, the start of its id,
// its name, or a word in its name, site or user name. More than one match is
// an error that lists them, because acting on the wrong item would be worse
// than asking again.
func findItem(items []*passwordsItem, typed string) (*passwordsItem, error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return nil, usage("say which item: its name, or its id from 'teanode passwords list'")
	}
	lowered := strings.ToLower(typed)
	var matches []*passwordsItem
	for _, item := range items {
		if item.View.ID == typed {
			return item, nil
		}
	}
	collect := func(isMatch func(*passwordsItem) bool) {
		if len(matches) > 0 {
			return
		}
		for _, item := range items {
			if isMatch(item) {
				matches = append(matches, item)
			}
		}
	}
	collect(func(item *passwordsItem) bool { return len(typed) >= 6 && strings.HasPrefix(item.View.ID, typed) })
	collect(func(item *passwordsItem) bool { return strings.ToLower(item.Name()) == lowered })
	collect(func(item *passwordsItem) bool {
		if strings.Contains(strings.ToLower(item.Name()), lowered) || strings.Contains(strings.ToLower(item.UserName()), lowered) {
			return true
		}
		for _, site := range item.Sites() {
			if strings.Contains(strings.ToLower(site), lowered) {
				return true
			}
		}
		return false
	})
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no item matches %q; 'teanode passwords list' shows them", typed)
	case 1:
		return matches[0], nil
	}
	var lines []string
	for index, item := range matches {
		if index == 5 {
			lines = append(lines, fmt.Sprintf("  and %d more", len(matches)-5))
			break
		}
		lines = append(lines, fmt.Sprintf("  %s  %s  %s", item.View.ID, item.Name(), item.UserName()))
	}
	return nil, usage(fmt.Sprintf("%q matches %d items; give the id of the one you mean:\n%s", typed, len(matches), strings.Join(lines, "\n")))
}

// sealFields seals an item's fields with its item key.
func sealFields(itemID string, kind passwords.ItemKind, fields any, itemKey []byte) (string, error) {
	plain, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sealed, err := passwords.Seal(itemKey, plain, passwords.ItemFieldsData(itemID, kind))
	if err != nil {
		return "", err
	}
	return sealed.String(), nil
}

// newItemID is a random version 4 UUID, which the device chooses for a new item
// because the encryption names it.
func newItemID() (string, error) {
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return "", err
	}
	identifier[6] = identifier[6]&0x0f | 0x40
	identifier[8] = identifier[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", identifier[0:4], identifier[4:6], identifier[6:8], identifier[8:10], identifier[10:16]), nil
}

// saveNewItem seals a new item with a new item key and saves it.
func (self *passwordsSession) saveNewItem(kind passwords.ItemKind, fields any) (*client.PasswordItemView, error) {
	itemID, err := newItemID()
	if err != nil {
		return nil, err
	}
	itemKey, err := passwords.NewKey()
	if err != nil {
		return nil, err
	}
	// A new item starts with a signed record that shares it with nobody, so
	// that "no record" can only mean a record somebody took away.
	access, err := passwords.SignAccess(self.keys.AccountKey, itemID, kind, nil, 0)
	if err != nil {
		return nil, err
	}
	setFieldsAccess(fields, access)
	encryptedFields, err := sealFields(itemID, kind, fields, itemKey)
	if err != nil {
		return nil, err
	}
	sealedItemKey, err := passwords.Seal(self.keys.AccountKey, itemKey, passwords.ItemKeyData(itemID))
	if err != nil {
		return nil, err
	}
	view, err := client.SavePasswordItem(self.ctx, self.connection, &client.SavePasswordItemInput{
		ID:              itemID,
		ItemKind:        string(kind),
		EncryptedFields: encryptedFields,
		ItemKey:         sealedItemKey.String(),
	})
	if err != nil {
		return nil, err
	}
	self.rememberRecord(itemID, access)
	return view, nil
}

// saveChangedItem seals the item's fields again with its own item key and
// saves them, based on the revision it was read at, so that a change made on
// another device in between is refused rather than overwritten.
func (self *passwordsSession) saveChangedItem(item *passwordsItem) (*client.PasswordItemView, error) {
	encryptedFields, err := sealFields(item.View.ID, item.Kind, item.Fields(), item.ItemKey)
	if err != nil {
		return nil, err
	}
	baseRevision := item.View.Revision
	return client.SavePasswordItem(self.ctx, self.connection, &client.SavePasswordItemInput{
		ID:              item.View.ID,
		ItemKind:        string(item.Kind),
		EncryptedFields: encryptedFields,
		ItemKey:         item.View.ItemKey,
		BaseRevision:    &baseRevision,
	})
}

// saveItemWithAccess saves the item's fields sealed with a new access record,
// signed with the owner's account key at one version more than the highest this
// computer has seen, based on the revision it was read at. It is the first step
// of widening access (sharing with somebody new, letting the agent use the
// item): the record is written down before the server is asked, so it is never
// narrower than what the server allows. A record that is already the item's
// signed one is not saved again.
func (self *passwordsSession) saveItemWithAccess(item *passwordsItem, access *passwords.PasswordAccess) error {
	signed, err := self.signedAccessFor(item, access)
	if err != nil {
		return err
	}
	if signed == item.VerifiedAccess {
		return nil
	}
	previous := item.Access()
	item.setAccess(signed)
	view, err := self.saveChangedItem(item)
	if err != nil {
		item.setAccess(previous)
		return err
	}
	item.View = view
	self.recordAccessWritten(item, signed)
	return nil
}

// Bounds of a generated password. The generator is the one in
// internal/passwords, which the dashboard's matches.
const (
	generatedPasswordLengthLeast = 8
	generatedPasswordLengthMost  = passwords.PasswordLengthMost
	generatedPasswordLengthUsual = passwords.PasswordLengthDefault
)

// generatePassword draws a password from the system's randomness: letters of
// both cases and digits, and symbols unless left out, with at least one of each
// kind.
func generatePassword(length int, isSymbolsIncluded bool) (string, error) {
	if length < generatedPasswordLengthLeast || length > generatedPasswordLengthMost {
		return "", usage(fmt.Sprintf("--length is between %d and %d", generatedPasswordLengthLeast, generatedPasswordLengthMost))
	}
	options := passwords.DefaultPasswordOptions()
	options.Length, options.ShouldIncludeSymbols = length, isSymbolsIncluded
	return passwords.GeneratePassword(options)
}

// clipboardTool is a program that puts text on the clipboard, with the
// arguments that copy what it is given on standard input and the ones that
// clear the clipboard. A program that can read the clipboard back is named by
// PasteProgram.
type clipboardTool struct {
	Program        string
	CopyArguments  []string
	ClearArguments []string
	PasteProgram   string
	PasteArguments []string
}

// clipboardTools are tried in order: Wayland, X11, macOS, Windows. A program
// with no way to clear is given an empty text to copy instead. Windows has no
// program here that reads the clipboard back.
var clipboardTools = []clipboardTool{
	{Program: "wl-copy", ClearArguments: []string{"--clear"}, PasteProgram: "wl-paste", PasteArguments: []string{"--no-newline"}},
	{Program: "xclip", CopyArguments: []string{"-selection", "clipboard"}, PasteProgram: "xclip", PasteArguments: []string{"-selection", "clipboard", "-out"}},
	{Program: "xsel", CopyArguments: []string{"--clipboard", "--input"}, ClearArguments: []string{"--clipboard", "--delete"}, PasteProgram: "xsel", PasteArguments: []string{"--clipboard", "--output"}},
	{Program: "pbcopy", PasteProgram: "pbpaste"},
	{Program: "clip.exe"},
}

// clipboardLookPath finds a program; a test replaces it.
var clipboardLookPath = exec.LookPath

// findClipboardTool is the first tool this computer has, and where it is.
func findClipboardTool() (*clipboardTool, string) {
	for index := range clipboardTools {
		if path, err := clipboardLookPath(clipboardTools[index].Program); err == nil {
			return &clipboardTools[index], path
		}
	}
	return nil, ""
}

// copyToClipboard puts the text on the clipboard with the first tool the
// computer has, and says which. The text goes in on standard input, never in
// an argument a process listing would show. It gives an empty name when no tool
// exists. An empty text clears the clipboard.
func copyToClipboard(ctx context.Context, text string) (string, error) {
	tool, path := findClipboardTool()
	if tool == nil {
		return "", nil
	}
	arguments := tool.CopyArguments
	input := strings.NewReader(text)
	if text == "" && tool.ClearArguments != nil {
		arguments, input = tool.ClearArguments, strings.NewReader("")
	}
	process := exec.CommandContext(ctx, path, arguments...)
	process.Stdin = input
	// No pipes: wl-copy and xclip leave a copy of themselves running to
	// hold the clipboard, and a pipe they inherit would never close.
	if err := process.Run(); err != nil {
		return tool.Program, fmt.Errorf("%s failed: %w", tool.Program, err)
	}
	return tool.Program, nil
}

// clearClipboardIfStillHolding clears the clipboard after a copy, unless the
// person has copied something else since: it reads the clipboard back and
// leaves it alone when it no longer holds the secret. With a tool that cannot
// be read back it clears, since leaving a password there is the worse
// mistake. It says whether it cleared.
func clearClipboardIfStillHolding(ctx context.Context, secret string) (bool, error) {
	tool, _ := findClipboardTool()
	if tool == nil {
		return false, nil
	}
	if tool.PasteProgram != "" {
		if pastePath, err := clipboardLookPath(tool.PasteProgram); err == nil {
			held, err := exec.CommandContext(ctx, pastePath, tool.PasteArguments...).Output()
			// A read that fails is an empty clipboard or one that holds
			// something that is not text (a picture): not the secret, so not
			// ours to clear.
			if err != nil || string(held) != secret {
				return false, nil
			}
		}
	}
	if _, err := copyToClipboard(ctx, ""); err != nil {
		return false, err
	}
	return true, nil
}
