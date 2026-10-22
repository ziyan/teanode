package access

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// appPasswordTransaction is a mailbox with the given app passwords and
// nothing else a sign-in looks at.
type appPasswordTransaction struct {
	db.Transaction
	appPasswords []*models.MailboxAppPassword
}

func (self *appPasswordTransaction) GetDomainByName(name string) (*models.Domain, error) {
	alias := &models.Alias{Kind: models.AliasKindMailbox, Pattern: models.PatternForLocalPart("ana"), MailboxID: "mailbox-1"}
	return &models.Domain{Domain: name, Aliases: []*models.Alias{alias}}, nil
}

func (self *appPasswordTransaction) GetMailbox(string) (*models.Mailbox, error) {
	return &models.Mailbox{ID: "mailbox-1", UserID: "user-1"}, nil
}

func (self *appPasswordTransaction) GetUser(string) (*models.User, error) {
	return &models.User{ID: "user-1"}, nil
}

func (self *appPasswordTransaction) EffectivePermissions(string) (*models.EffectivePermissions, error) {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}}), nil
}

func (self *appPasswordTransaction) ListAppPasswords(string) ([]*models.MailboxAppPassword, error) {
	return self.appPasswords, nil
}

func (self *appPasswordTransaction) GetAppPasswordBySelector(string, string) (*models.MailboxAppPassword, error) {
	return nil, nil
}

func (self *appPasswordTransaction) TouchAppPassword(string, time.Time) error { return nil }

func newAppPassword(t *testing.T, identifier, selector, password string) *models.MailboxAppPassword {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &models.MailboxAppPassword{ID: identifier, MailboxID: "mailbox-1", Selector: selector, PasswordHash: string(hash)}
}

// A password says which password it is.
//
// The username a mail program sends is the mailbox's address, which is the
// same for every device, so a mailbox with twenty devices had twenty
// passwords behind one name and a sign-in tried each in turn -- a hash
// apiece, and a hash is slow on purpose. The tag in front of a password names
// its own row, so a sign-in is one lookup and one hash whether the password
// is right or wrong.
func TestAPasswordSaysWhichPasswordItIs(t *testing.T) {
	t.Parallel()

	if got := selectorOf("abcdef-11111-22222-33333-44444"); got != "abcdef" {
		t.Fatalf("the tag is the first group: %q", got)
	}
	// A password made before tags existed has four groups of five, so its
	// first group is the wrong length and it takes the old route.
	if got := selectorOf("aaaaa-bbbbb-ccccc-ddddd"); got != "" {
		t.Fatalf("an older password carries no tag: %q", got)
	}
	for _, wrong := range []string{"", "abcdef", "ABCDEF-11111", "ab!def-11111", "abcdefg-11111"} {
		if got := selectorOf(wrong); got != "" {
			t.Fatalf("%q is not a tag: %q", wrong, got)
		}
	}
	// The tag is drawn from the alphabet the password is, which leaves out
	// the characters that are misread off a screen.
	for _, letter := range "01lIO" {
		if strings.ContainsRune(appPasswordAlphabet, letter) {
			t.Fatalf("%q is easy to misread and is not in the alphabet", letter)
		}
	}
}

// A guess with no tag in it can only mean a password made before tags
// existed. A mailbox whose passwords are all tagged has none, so the guess
// costs one hash, as it does for an address with no mailbox. Otherwise a
// refusal takes longer the more devices a mailbox has, and one sign-in
// attempt is charged for all of those hashes.
func TestAnUntaggedGuessAtATaggedMailboxCostsOneHash(t *testing.T) {
	t.Parallel()
	transaction := &appPasswordTransaction{}
	for _, selector := range []string{"aaaaaa", "bbbbbb", "cccccc", "dddddd", "eeeeee"} {
		transaction.appPasswords = append(transaction.appPasswords, newAppPassword(t, selector, selector, "the-right-one"))
	}

	for _, guess := range []string{"x", "zzzzz-11111-22222-33333"} {
		_, _, tried, err := AuthenticateAppPasswordCounting(transaction, "ana@example.com", guess)
		if err != ErrInvalidAppPassword {
			t.Fatalf("%q: expected the refusal, got %v", guess, err)
		}
		if tried != 1 {
			t.Errorf("%q: the refusal spent %d hashes, expected 1", guess, tried)
		}
	}
}

// A mailbox that still holds a password made before tags existed is tried the
// old way, but only against such passwords: the tagged ones cannot match an
// untagged guess.
func TestAnUntaggedGuessIsOnlyTriedAgainstUntaggedPasswords(t *testing.T) {
	t.Parallel()
	transaction := &appPasswordTransaction{appPasswords: []*models.MailboxAppPassword{
		newAppPassword(t, "tagged-1", "aaaaaa", "first"),
		newAppPassword(t, "tagged-2", "bbbbbb", "second"),
		newAppPassword(t, "older", "", "aaaaa-bbbbb-ccccc-ddddd"),
	}}

	_, _, tried, err := AuthenticateAppPasswordCounting(transaction, "ana@example.com", "wrong")
	if err != ErrInvalidAppPassword || tried != 1 {
		t.Errorf("a wrong guess: expected the refusal after 1 hash, got %v after %d", err, tried)
	}

	_, appPassword, tried, err := AuthenticateAppPasswordCounting(transaction, "ana@example.com", "aaaaa-bbbbb-ccccc-ddddd")
	if err != nil || appPassword == nil || appPassword.ID != "older" {
		t.Fatalf("the older password was not accepted: %v", err)
	}
	if tried != 1 {
		t.Errorf("the older password took %d hashes, expected 1", tried)
	}
}
