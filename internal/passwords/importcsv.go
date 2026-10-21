package passwords

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// byteOrderMark starts some exports (Excel adds it), and would otherwise stick
// to the first column name.
const byteOrderMark = "\xef\xbb\xbf"

// CSVFormat is the password manager a CSV export came from.
type CSVFormat string

// The formats, named by their header row.
const (
	// CSVFormatChrome is name,url,username,password,note (older files have no
	// note column).
	CSVFormatChrome CSVFormat = "chrome"
	// CSVFormatApple is Title,URL,Username,Password,Notes,OTPAuth (older files
	// have no OTPAuth column).
	CSVFormatApple CSVFormat = "apple"
	// CSVFormatBitwarden is folder,favorite,type,name,notes,fields,reprompt,
	// login_uri,login_username,login_password,login_totp.
	CSVFormatBitwarden CSVFormat = "bitwarden"
)

// CSVImport is what a CSV export held.
type CSVImport struct {
	Format CSVFormat
	Items  []ImportedItem
	// SkippedItemNames are the names of rows the importer does not take, such
	// as a Bitwarden card or identity.
	SkippedItemNames []string
}

// DetectCSVFormat tells which manager's export has this header row, whatever
// the case and order of the columns.
func DetectCSVFormat(header []string) (CSVFormat, error) {
	columns := columnIndexes(header)
	hasColumns := func(names ...string) bool {
		for _, name := range names {
			if _, isPresent := columns[name]; !isPresent {
				return false
			}
		}
		return true
	}
	switch {
	case hasColumns("login_uri", "login_username", "login_password", "type", "name"):
		return CSVFormatBitwarden, nil
	case hasColumns("title", "url", "username", "password"):
		return CSVFormatApple, nil
	case hasColumns("name", "url", "username", "password"):
		return CSVFormatChrome, nil
	}
	return "", errors.New("passwords: that CSV file is not a Chrome, Apple Passwords or Bitwarden export")
}

// columnIndexes maps each lower-cased column name of a header row to its
// position.
func columnIndexes(header []string) map[string]int {
	columns := make(map[string]int, len(header))
	for index, name := range header {
		name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, byteOrderMark)))
		if _, isPresent := columns[name]; !isPresent {
			columns[name] = index
		}
	}
	return columns
}

// ParseCSV reads an export from Chrome, Apple Passwords or Bitwarden, found by
// its header row. Rows that are entirely empty are skipped.
func ParseCSV(reader io.Reader) (*CSVImport, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("passwords: read CSV: %w", err)
	}
	content = bytes.TrimPrefix(content, []byte(byteOrderMark))
	csvReader := csv.NewReader(bytes.NewReader(content))
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true
	rows, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("passwords: read CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("passwords: the CSV file is empty")
	}
	format, err := DetectCSVFormat(rows[0])
	if err != nil {
		return nil, err
	}
	columns := columnIndexes(rows[0])

	csvImport := &CSVImport{Format: format}
	for _, row := range rows[1:] {
		// cell is a column of this row as written, or empty when the file has
		// no such column or the row is short.
		cell := func(name string) string {
			index, isPresent := columns[name]
			if !isPresent || index >= len(row) {
				return ""
			}
			return row[index]
		}
		if isRowEmpty(row) {
			continue
		}
		switch format {
		case CSVFormatChrome:
			csvImport.Items = append(csvImport.Items, loginFromCSV(csvLogin{
				name:     cell("name"),
				sites:    []string{cell("url")},
				userName: cell("username"),
				password: cell("password"),
				notes:    cell("note"),
			}))
		case CSVFormatApple:
			csvImport.Items = append(csvImport.Items, loginFromCSV(csvLogin{
				name:        cell("title"),
				sites:       []string{cell("url")},
				userName:    cell("username"),
				password:    cell("password"),
				notes:       cell("notes"),
				oneTimeCode: cell("otpauth"),
			}))
		case CSVFormatBitwarden:
			name := strings.TrimSpace(cell("name"))
			switch strings.ToLower(strings.TrimSpace(cell("type"))) {
			case "", "login":
				notes := strings.TrimSpace(cell("notes"))
				if customFields := strings.TrimSpace(cell("fields")); customFields != "" {
					notes = strings.TrimSpace(notes + "\n" + customFields)
				}
				csvImport.Items = append(csvImport.Items, loginFromCSV(csvLogin{
					name:        name,
					sites:       strings.Split(cell("login_uri"), "\n"),
					userName:    cell("login_username"),
					password:    cell("login_password"),
					notes:       notes,
					oneTimeCode: cell("login_totp"),
					folder:      cell("folder"),
				}))
			case "note":
				csvImport.Items = append(csvImport.Items, ImportedItem{
					ItemKind: ItemKindNote,
					Note:     &NoteFields{Name: name, Text: cell("notes"), Folder: strings.TrimSpace(cell("folder"))},
				})
			default:
				csvImport.SkippedItemNames = append(csvImport.SkippedItemNames, name)
			}
		}
	}
	return csvImport, nil
}

func isRowEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// csvLogin is the columns of a login row, whatever the format called them.
type csvLogin struct {
	name        string
	sites       []string
	userName    string
	password    string
	notes       string
	oneTimeCode string
	folder      string
}

func loginFromCSV(row csvLogin) ImportedItem {
	login := &LoginFields{
		Name:     strings.TrimSpace(row.name),
		UserName: strings.TrimSpace(row.userName),
		Password: row.password,
		Notes:    strings.TrimSpace(row.notes),
		Folder:   strings.TrimSpace(row.folder),
	}
	for _, site := range row.sites {
		if strings.TrimSpace(site) != "" {
			login.Sites = appendSite(login.Sites, site)
		}
	}
	if login.Name == "" {
		login.Name = siteHost(login.Sites)
	}
	if login.Name == "" {
		login.Name = login.UserName
	}
	if oneTimeCode := strings.TrimSpace(row.oneTimeCode); oneTimeCode != "" {
		login.OneTimeCodeURI = oneTimeCodeURI(oneTimeCode, login.Name)
		if login.OneTimeCodeURI == "" {
			// Not a code this importer can use (a Steam code, say): keep the
			// text rather than lose it.
			login.Notes = strings.TrimSpace(login.Notes + "\nOne-time code: " + oneTimeCode)
		}
	}
	return ImportedItem{ItemKind: ItemKindLogin, Login: login}
}

// oneTimeCodeURI is the otpauth address for what a CSV column holds: the
// address itself, or a bare base32 secret, which gets an address made around
// it. It is empty for anything else.
func oneTimeCodeURI(text, name string) string {
	address, err := OneTimeCodeURIFromText(text, name)
	if err != nil {
		return ""
	}
	return address
}

// OneTimeCodeURIFromText is the otpauth address for what a person typed or an
// export held: the address itself, or a bare base32 secret, which gets an
// address made around it with the name and the secret escaped. It is empty for
// empty text and an error for anything that is not a usable address.
func OneTimeCodeURIFromText(text, name string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if !strings.HasPrefix(strings.ToLower(text), "otpauth://") {
		text = "otpauth://totp/" + url.PathEscape(strings.TrimSpace(name)) + "?secret=" + url.QueryEscape(strings.ReplaceAll(text, " ", ""))
	}
	if _, err := ParseOneTimeCodeURI(text); err != nil {
		return "", err
	}
	return text, nil
}

// siteHost is the host name of the first site that has one, to name a login
// that came without a name.
func siteHost(sites []string) string {
	for _, site := range sites {
		if parsed, err := url.Parse(site); err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	return ""
}
