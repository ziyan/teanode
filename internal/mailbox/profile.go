package mailbox

import (
	"bytes"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// SetupProfile is what an iPhone or a Mac needs to read a mailbox in its own
// Mail, Calendar, Contacts and Reminders apps. Collection payloads may use an
// account-scoped credential separately from the mailbox app password.
//
// The profile carries that password in the clear, so it is made for one
// download and never stored.
type SetupProfile struct {
	IsSyncOnly        bool
	IsCalendarEnabled *bool
	IsContactsEnabled *bool

	// What the device is called, as the app password is named.
	DeviceName string

	// What the mailbox is called, and the address it signs in and sends as.
	MailboxName string
	Address     string
	Password    string

	// Where mail is read, on the port with TLS from the first byte when
	// there is one, and where it is sent.
	IMAPHost       string
	IMAPPort       int
	SubmissionHost string
	SubmissionPort int

	// DAVHost is the name, with its port when it is not 443, the calendars
	// and the address book are found at; empty leaves them out.
	DAVHost string

	// An explicit pair keeps collection credentials out of the mail payload.
	// Leaving both empty preserves the existing mailbox profile contract.
	DAVUsername string
	DAVPassword string

	// PrincipalPath is the person's own address on the DAV server, which a
	// client otherwise finds by asking.
	PrincipalPath string

	// Identifier names the profile, so one made again for the same mailbox
	// replaces the old one when installed rather than sitting beside it.
	Identifier string
}

// Plist is the profile as Apple's property list, the .mobileconfig a
// device installs.
func (self *SetupProfile) Plist() ([]byte, error) {
	if self.Address == "" || self.Password == "" || (!self.IsSyncOnly && (self.IMAPHost == "" || self.SubmissionHost == "")) {
		return nil, fmt.Errorf("a setup profile needs an address, a password and the mail hosts")
	}
	if self.Identifier == "" {
		return nil, fmt.Errorf("a setup profile needs an identifier")
	}
	if (self.DAVUsername == "") != (self.DAVPassword == "") {
		return nil, fmt.Errorf("collection sync needs both its username and password")
	}
	davUsername, davPassword := self.DAVUsername, self.DAVPassword
	if davUsername == "" {
		davUsername, davPassword = self.Address, self.Password
	}
	name := strings.TrimSpace(self.MailboxName)
	if name == "" {
		name = self.Address
	}
	payloads := []plistDictionary{{
		{"PayloadType", "com.apple.mail.managed"},
		{"PayloadVersion", 1},
		{"PayloadIdentifier", self.Identifier + ".mail"},
		{"PayloadUUID", newUUID()},
		{"PayloadDisplayName", "Mail: " + self.Address},
		{"EmailAccountDescription", name},
		{"EmailAccountName", name},
		{"EmailAccountType", "EmailTypeIMAP"},
		{"EmailAddress", self.Address},
		{"IncomingMailServerAuthentication", "EmailAuthPassword"},
		{"IncomingMailServerHostName", self.IMAPHost},
		{"IncomingMailServerPortNumber", self.IMAPPort},
		{"IncomingMailServerUseSSL", true},
		{"IncomingMailServerUsername", self.Address},
		{"IncomingPassword", self.Password},
		{"OutgoingMailServerAuthentication", "EmailAuthPassword"},
		{"OutgoingMailServerHostName", self.SubmissionHost},
		{"OutgoingMailServerPortNumber", self.SubmissionPort},
		{"OutgoingMailServerUseSSL", true},
		{"OutgoingMailServerUsername", self.Address},
		{"OutgoingPassword", self.Password},
		{"OutgoingPasswordSameAsIncomingPassword", true},
	}}
	if self.DAVHost != "" {
		host, port := splitHostPort(self.DAVHost, 443)
		principal := "https://" + self.DAVHost + self.PrincipalPath
		payloads = append(payloads,
			plistDictionary{
				{"PayloadType", "com.apple.caldav.account"},
				{"PayloadVersion", 1},
				{"PayloadIdentifier", self.Identifier + ".calendar"},
				{"PayloadUUID", newUUID()},
				{"PayloadDisplayName", "Calendar: " + self.Address},
				{"CalDAVAccountDescription", name},
				{"CalDAVHostName", host},
				{"CalDAVPort", port},
				{"CalDAVPrincipalURL", principal},
				{"CalDAVUseSSL", true},
				{"CalDAVUsername", davUsername},
				{"CalDAVPassword", davPassword},
			},
			plistDictionary{
				{"PayloadType", "com.apple.carddav.account"},
				{"PayloadVersion", 1},
				{"PayloadIdentifier", self.Identifier + ".contacts"},
				{"PayloadUUID", newUUID()},
				{"PayloadDisplayName", "Contacts: " + self.Address},
				{"CardDAVAccountDescription", name},
				{"CardDAVHostName", host},
				{"CardDAVPort", port},
				{"CardDAVPrincipalURL", principal},
				{"CardDAVUseSSL", true},
				{"CardDAVUsername", davUsername},
				{"CardDAVPassword", davPassword},
			},
		)
	}
	filtered := make([]plistDictionary, 0, len(payloads))
	for _, payload := range payloads {
		payloadType := payload[0].value.(string)
		if self.IsSyncOnly && payloadType == "com.apple.mail.managed" {
			continue
		}
		if payloadType == "com.apple.caldav.account" && self.IsCalendarEnabled != nil && !*self.IsCalendarEnabled {
			continue
		}
		if payloadType == "com.apple.carddav.account" && self.IsContactsEnabled != nil && !*self.IsContactsEnabled {
			continue
		}
		filtered = append(filtered, payload)
	}
	payloads = filtered
	if len(payloads) == 0 {
		return nil, fmt.Errorf("a setup profile needs at least one enabled service")
	}
	description := "Adds " + self.Address + " to Mail"
	if self.IsSyncOnly {
		description = "Sets up collection sync for " + self.Address
	}
	if self.DAVHost != "" {
		description += ", with the selected calendar and contact sync services"
	}
	if self.DeviceName != "" {
		description += ", on " + self.DeviceName
	}
	profile := plistDictionary{
		{"PayloadType", "Configuration"},
		{"PayloadVersion", 1},
		{"PayloadIdentifier", self.Identifier},
		{"PayloadUUID", newUUID()},
		{"PayloadDisplayName", "TeaNode: " + self.Address},
		{"PayloadDescription", description + "."},
		{"PayloadOrganization", "TeaNode"},
		{"PayloadRemovalDisallowed", false},
		{"PayloadContent", payloads},
	}
	var written bytes.Buffer
	written.WriteString(xml.Header)
	written.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	written.WriteString(`<plist version="1.0">` + "\n")
	if err := writePlistValue(&written, profile, 0); err != nil {
		return nil, err
	}
	written.WriteString("</plist>\n")
	return written.Bytes(), nil
}

// plistEntry is one key of a dictionary, in the order it is written.
type plistEntry struct {
	key   string
	value any
}

type plistDictionary []plistEntry

func writePlistValue(written *bytes.Buffer, value any, depth int) error {
	indent := strings.Repeat("\t", depth)
	switch typed := value.(type) {
	case plistDictionary:
		written.WriteString(indent + "<dict>\n")
		for _, entry := range typed {
			written.WriteString(indent + "\t<key>")
			if err := xml.EscapeText(written, []byte(entry.key)); err != nil {
				return err
			}
			written.WriteString("</key>\n")
			if err := writePlistValue(written, entry.value, depth+1); err != nil {
				return err
			}
		}
		written.WriteString(indent + "</dict>\n")
	case []plistDictionary:
		written.WriteString(indent + "<array>\n")
		for _, item := range typed {
			if err := writePlistValue(written, item, depth+1); err != nil {
				return err
			}
		}
		written.WriteString(indent + "</array>\n")
	case string:
		written.WriteString(indent + "<string>")
		if err := xml.EscapeText(written, []byte(typed)); err != nil {
			return err
		}
		written.WriteString("</string>\n")
	case int:
		written.WriteString(indent + "<integer>" + strconv.Itoa(typed) + "</integer>\n")
	case bool:
		if typed {
			written.WriteString(indent + "<true/>\n")
		} else {
			written.WriteString(indent + "<false/>\n")
		}
	default:
		return fmt.Errorf("a setup profile cannot hold a %T", value)
	}
	return nil
}

// splitHostPort is a host and its port, or the fallback port when it names
// none.
func splitHostPort(authority string, fallback int) (string, int) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		return authority, fallback
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return host, fallback
	}
	return host, number
}

// newUUID is a random version 4 UUID, which every payload needs.
func newUUID() string {
	var random [16]byte
	_, _ = rand.Read(random[:])
	random[6] = random[6]&0x0f | 0x40
	random[8] = random[8]&0x3f | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", random[0:4], random[4:6], random[6:8], random[8:10], random[10:16])
}
