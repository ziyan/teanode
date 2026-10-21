package passwords

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetectCSVFormat(test *testing.T) {
	cases := []struct {
		header []string
		want   CSVFormat
	}{
		{[]string{"name", "url", "username", "password", "note"}, CSVFormatChrome},
		{[]string{"name", "url", "username", "password"}, CSVFormatChrome},
		{[]string{"Title", "URL", "Username", "Password", "Notes", "OTPAuth"}, CSVFormatApple},
		{[]string{"folder", "favorite", "type", "name", "notes", "fields", "reprompt", "login_uri", "login_username", "login_password", "login_totp"}, CSVFormatBitwarden},
		{[]string{byteOrderMark + "Name", "URL", "Username", "Password"}, CSVFormatChrome},
	}
	for _, testCase := range cases {
		got, err := DetectCSVFormat(testCase.header)
		if err != nil || got != testCase.want {
			test.Errorf("%v = %q, %v, want %q", testCase.header, got, err, testCase.want)
		}
	}
	if _, err := DetectCSVFormat([]string{"a", "b"}); err == nil {
		test.Error("an unknown header was accepted")
	}
}

func TestParseCSVChrome(test *testing.T) {
	file := "name,url,username,password,note\n" +
		"example.com,https://example.com/login,ada@example.com,\"pass,word\",\"line one\nline two\"\n" +
		",https://www.example.net,grace,pw2,\n" +
		"\n" +
		"Kept,,ada,,\n"
	csvImport, err := ParseCSV(strings.NewReader(file))
	if err != nil {
		test.Fatal(err)
	}
	if csvImport.Format != CSVFormatChrome || len(csvImport.Items) != 3 {
		test.Fatalf("import = %+v", csvImport)
	}
	want := &LoginFields{
		Name:     "example.com",
		UserName: "ada@example.com",
		Password: "pass,word",
		Sites:    []string{"https://example.com/login"},
		Notes:    "line one\nline two",
	}
	if !reflect.DeepEqual(csvImport.Items[0].Login, want) {
		test.Errorf("first = %+v", csvImport.Items[0].Login)
	}
	// A row with no name is named after its site's host.
	if csvImport.Items[1].Login.Name != "www.example.net" {
		test.Errorf("second = %+v", csvImport.Items[1].Login)
	}
	if csvImport.Items[2].Login.Name != "Kept" || len(csvImport.Items[2].Login.Sites) != 0 {
		test.Errorf("third = %+v", csvImport.Items[2].Login)
	}
}

func TestParseCSVApple(test *testing.T) {
	file := byteOrderMark + "Title,URL,Username,Password,Notes,OTPAuth\n" +
		"Example,https://example.com,ada,pw,,otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP\n" +
		"Plain,example.org,grace,pw2,remember,JBSWY3DP EHPK3PXP\n" +
		"Odd,example.net,alan,pw3,,steam://abc\n"
	csvImport, err := ParseCSV(strings.NewReader(file))
	if err != nil {
		test.Fatal(err)
	}
	if csvImport.Format != CSVFormatApple || len(csvImport.Items) != 3 {
		test.Fatalf("import = %+v", csvImport)
	}
	first := csvImport.Items[0].Login
	if first.Name != "Example" || first.OneTimeCodeURI != "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP" {
		test.Errorf("first = %+v", first)
	}
	// A bare secret gets an address made around it.
	second := csvImport.Items[1].Login
	if second.OneTimeCodeURI != "otpauth://totp/Plain?secret=JBSWY3DPEHPK3PXP" || second.Notes != "remember" || second.Sites[0] != "https://example.org" {
		test.Errorf("second = %+v", second)
	}
	// A code this importer cannot use is kept as text.
	third := csvImport.Items[2].Login
	if third.OneTimeCodeURI != "" || third.Notes != "One-time code: steam://abc" {
		test.Errorf("third = %+v", third)
	}
}

func TestParseCSVBitwarden(test *testing.T) {
	file := "folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,login_password,login_totp\n" +
		"Work,1,login,Intranet,some notes,\"pin: 1234\",0,\"https://intranet.example.com\nhttps://sso.example.com\",ada,pw,otpauth://totp/Intranet?secret=JBSWY3DPEHPK3PXP\n" +
		",0,note,Router,\"The router is in the hall.\",,0,,,,\n" +
		",0,card,Visa,,,0,,,,\n" +
		",0,identity,Me,,,0,,,,\n"
	csvImport, err := ParseCSV(strings.NewReader(file))
	if err != nil {
		test.Fatal(err)
	}
	if csvImport.Format != CSVFormatBitwarden || len(csvImport.Items) != 2 {
		test.Fatalf("import = %+v", csvImport)
	}
	want := &LoginFields{
		Name:           "Intranet",
		UserName:       "ada",
		Password:       "pw",
		Sites:          []string{"https://intranet.example.com", "https://sso.example.com"},
		OneTimeCodeURI: "otpauth://totp/Intranet?secret=JBSWY3DPEHPK3PXP",
		Notes:          "some notes\npin: 1234",
		Folder:         "Work",
	}
	if !reflect.DeepEqual(csvImport.Items[0].Login, want) {
		test.Errorf("login = %+v", csvImport.Items[0].Login)
	}
	note := csvImport.Items[1]
	if note.ItemKind != ItemKindNote || note.Note.Name != "Router" || note.Note.Text != "The router is in the hall." {
		test.Errorf("note = %+v", note)
	}
	if !reflect.DeepEqual(csvImport.SkippedItemNames, []string{"Visa", "Me"}) {
		test.Errorf("skipped = %v", csvImport.SkippedItemNames)
	}
}

func TestParseCSVRefuses(test *testing.T) {
	if _, err := ParseCSV(strings.NewReader("")); err == nil {
		test.Error("an empty file was accepted")
	}
	if _, err := ParseCSV(strings.NewReader("a,b\n1,2\n")); err == nil {
		test.Error("an unknown file was accepted")
	}
}
