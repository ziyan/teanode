package passwords

import (
	"errors"
	"reflect"
	"testing"
)

func TestParsePassEntryLoginInDomainDirectory(test *testing.T) {
	item, err := ParsePassEntry("web/example.com/ada@example.com", "correct horse\nurl: example.com/login\nnote line one\nnote line two\n")
	if err != nil {
		test.Fatal(err)
	}
	want := &LoginFields{
		Name:     "example.com",
		UserName: "ada@example.com",
		Password: "correct horse",
		Sites:    []string{"https://example.com/login", "https://example.com"},
		Notes:    "note line one\nnote line two",
		Folder:   "web",
	}
	if item.ItemKind != ItemKindLogin || !reflect.DeepEqual(item.Login, want) {
		test.Fatalf("item = %+v, login = %+v", item, item.Login)
	}
}

func TestParsePassEntryFields(test *testing.T) {
	text := "hunter2\n" +
		"Login: ada\n" +
		"EMAIL: ada@example.com\n" +
		"Website: https://example.com\n" +
		"url: https://www.example.com/sign-in\n" +
		"otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP&issuer=Example\n" +
		"security question: first pet\n" +
		"See https://example.com/help for more\n"
	item, err := ParsePassEntry("work/intranet.gpg", text)
	if err != nil {
		test.Fatal(err)
	}
	login := item.Login
	if login.Password != "hunter2" || login.UserName != "ada" {
		test.Errorf("password = %q, user name = %q", login.Password, login.UserName)
	}
	if want := []string{"https://example.com", "https://www.example.com/sign-in"}; !reflect.DeepEqual(login.Sites, want) {
		test.Errorf("sites = %v", login.Sites)
	}
	if login.OneTimeCodeURI != "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP&issuer=Example" {
		test.Errorf("one-time code = %q", login.OneTimeCodeURI)
	}
	// The second user name line and the lines that are not fields stay as notes.
	if want := "EMAIL: ada@example.com\nsecurity question: first pet\nSee https://example.com/help for more"; login.Notes != want {
		test.Errorf("notes = %q", login.Notes)
	}
	if login.Name != "intranet" || login.Folder != "work" {
		test.Errorf("name = %q, folder = %q", login.Name, login.Folder)
	}
}

func TestParsePassEntryDomainNameIsASite(test *testing.T) {
	item, err := ParsePassEntry("social/example.org", "pa55word\nusername: ada\n")
	if err != nil {
		test.Fatal(err)
	}
	want := &LoginFields{Name: "example.org", UserName: "ada", Password: "pa55word", Sites: []string{"https://example.org"}, Folder: "social"}
	if !reflect.DeepEqual(item.Login, want) {
		test.Fatalf("login = %+v", item.Login)
	}
}

func TestParsePassEntryExplicitUserNameWinsOverPath(test *testing.T) {
	item, err := ParsePassEntry("web/example.com/ada", "pw\nuser: ada.l\n")
	if err != nil {
		test.Fatal(err)
	}
	if item.Login.UserName != "ada.l" || item.Login.Name != "example.com" {
		test.Fatalf("login = %+v", item.Login)
	}
}

func TestParsePassEntryPlainName(test *testing.T) {
	item, err := ParsePassEntry("personal/home wifi", "pw\r\nnotes\r\n")
	if err != nil {
		test.Fatal(err)
	}
	if item.Login.Name != "home wifi" || item.Login.Folder != "personal" || item.Login.Password != "pw" || item.Login.Notes != "notes" || len(item.Login.Sites) != 0 {
		test.Fatalf("login = %+v", item.Login)
	}
}

func TestParsePassEntryDeepFolder(test *testing.T) {
	item, err := ParsePassEntry("a/b/c/d", "pw")
	if err != nil {
		test.Fatal(err)
	}
	if item.Login.Name != "d" || item.Login.Folder != "a/b/c" {
		test.Fatalf("login = %+v", item.Login)
	}
}

func TestParsePassEntryWithoutPasswordIsANote(test *testing.T) {
	text := "\nWiFi is on the router sticker.\nurl is not a field here without a colon\n"
	item, err := ParsePassEntry("notes/router", text)
	if err != nil {
		test.Fatal(err)
	}
	if item.ItemKind != ItemKindNote || item.Login != nil {
		test.Fatalf("item = %+v", item)
	}
	want := &NoteFields{Name: "router", Text: "\nWiFi is on the router sticker.\nurl is not a field here without a colon", Folder: "notes"}
	if !reflect.DeepEqual(item.Note, want) {
		test.Fatalf("note = %+v", item.Note)
	}
}

func TestParsePassEntryOneTimeCodeOnly(test *testing.T) {
	item, err := ParsePassEntry("otp/example.com", "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP\n")
	if err != nil {
		test.Fatal(err)
	}
	if item.ItemKind != ItemKindLogin || item.Login.Password != "" || item.Login.OneTimeCodeURI == "" {
		test.Fatalf("item = %+v, login = %+v", item, item.Login)
	}
}

func TestParsePassEntryFieldFirstLineIsNotThePassword(test *testing.T) {
	item, err := ParsePassEntry("web/example.net", "user: ada\nurl: https://example.net/in\n")
	if err != nil {
		test.Fatal(err)
	}
	if item.ItemKind != ItemKindLogin || item.Login.Password != "" || item.Login.UserName != "ada" {
		test.Fatalf("login = %+v", item.Login)
	}
}

func TestParsePassEntryBadOneTimeCodeStaysANote(test *testing.T) {
	item, err := ParsePassEntry("web/example.net", "pw\notpauth://totp/x\n")
	if err != nil {
		test.Fatal(err)
	}
	if item.Login.OneTimeCodeURI != "" || item.Login.Notes != "otpauth://totp/x" {
		test.Fatalf("login = %+v", item.Login)
	}
}

func TestParsePassEntryRefuses(test *testing.T) {
	if _, err := ParsePassEntry("web/empty", " \n\n"); !errors.Is(err, ErrEmptyEntry) {
		test.Errorf("empty text: %v", err)
	}
	if _, err := ParsePassEntry("/", "pw"); err == nil {
		test.Error("an entry with no path was accepted")
	}
}
