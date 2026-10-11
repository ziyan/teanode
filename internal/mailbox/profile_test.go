package mailbox_test

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/mailbox"
)

// readPlist reads a property list back into maps, slices, strings, ints and
// bools, as a device would.
func readPlist(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var read func(start xml.StartElement) any
	read = func(start xml.StartElement) any {
		switch start.Name.Local {
		case "dict":
			dictionary := map[string]any{}
			key := ""
			for {
				token, err := decoder.Token()
				if err != nil {
					t.Fatalf("the profile does not parse: %s", err)
				}
				switch typed := token.(type) {
				case xml.StartElement:
					if typed.Name.Local == "key" {
						var text string
						if err := decoder.DecodeElement(&text, &typed); err != nil {
							t.Fatal(err)
						}
						key = text
					} else {
						dictionary[key] = read(typed)
					}
				case xml.EndElement:
					return dictionary
				}
			}
		case "array":
			var items []any
			for {
				token, err := decoder.Token()
				if err != nil {
					t.Fatal(err)
				}
				switch typed := token.(type) {
				case xml.StartElement:
					items = append(items, read(typed))
				case xml.EndElement:
					return items
				}
			}
		case "true", "false":
			if err := decoder.Skip(); err != nil {
				t.Fatal(err)
			}
			return start.Name.Local == "true"
		default:
			var text string
			if err := decoder.DecodeElement(&text, &start); err != nil {
				t.Fatal(err)
			}
			if start.Name.Local == "integer" {
				number, err := strconv.Atoi(text)
				if err != nil {
					t.Fatal(err)
				}
				return number
			}
			return text
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("no dictionary in the profile: %s", err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "dict" {
			return read(start).(map[string]any)
		}
	}
}

func TestASetupProfileHasMailCalendarsAndContacts(t *testing.T) {
	profile := &mailbox.SetupProfile{
		DeviceName: "Phone", MailboxName: "Robin & Co", Address: "robin@example.com", Password: strings.Join([]string{"abc234", "defgh", "jkmnp", "qrstu", "vwxyz"}, "-"),
		IMAPHost: "mail.example.com", IMAPPort: 993, SubmissionHost: "mail.example.com", SubmissionPort: 587,
		DAVHost: "mail.example.com:8443", PrincipalPath: "/dav/u1/", Identifier: "com.teanode.profile.m1",
	}
	data, err := profile.Plist()
	if err != nil {
		t.Fatal(err)
	}
	read := readPlist(t, data)
	if read["PayloadType"] != "Configuration" || read["PayloadIdentifier"] != "com.teanode.profile.m1" {
		t.Fatalf("not a configuration profile: %v", read)
	}
	payloads := read["PayloadContent"].([]any)
	byType := map[string]map[string]any{}
	uuids := map[string]bool{read["PayloadUUID"].(string): true}
	for _, payload := range payloads {
		fields := payload.(map[string]any)
		byType[fields["PayloadType"].(string)] = fields
		uuids[fields["PayloadUUID"].(string)] = true
	}
	if len(uuids) != 4 {
		t.Fatalf("every payload needs its own UUID: %v", uuids)
	}
	mail := byType["com.apple.mail.managed"]
	if mail["IncomingMailServerHostName"] != "mail.example.com" || mail["IncomingMailServerPortNumber"] != 993 ||
		mail["OutgoingMailServerPortNumber"] != 587 || mail["IncomingPassword"] != profile.Password ||
		mail["EmailAddress"] != "robin@example.com" || mail["EmailAccountName"] != "Robin & Co" {
		t.Fatalf("the mail payload is wrong: %v", mail)
	}
	calendar := byType["com.apple.caldav.account"]
	if calendar["CalDAVHostName"] != "mail.example.com" || calendar["CalDAVPort"] != 8443 ||
		calendar["CalDAVPrincipalURL"] != "https://mail.example.com:8443/dav/u1/" || calendar["CalDAVUsername"] != "robin@example.com" {
		t.Fatalf("the calendar payload is wrong: %v", calendar)
	}
	contacts := byType["com.apple.carddav.account"]
	if contacts["CardDAVPort"] != 8443 || contacts["CardDAVPassword"] != profile.Password {
		t.Fatalf("the contacts payload is wrong: %v", contacts)
	}
	if !strings.Contains(string(data), "Robin &amp; Co") {
		t.Fatal("text must be escaped")
	}
}

func TestWithoutADAVHostTheProfileIsMailOnly(t *testing.T) {
	profile := &mailbox.SetupProfile{
		Address: "robin@example.com", Password: "secret", IMAPHost: "mail.example.com", IMAPPort: 143,
		SubmissionHost: "mail.example.com", SubmissionPort: 587, Identifier: "com.teanode.profile.m1",
	}
	data, err := profile.Plist()
	if err != nil {
		t.Fatal(err)
	}
	payloads := readPlist(t, data)["PayloadContent"].([]any)
	if len(payloads) != 1 || payloads[0].(map[string]any)["PayloadType"] != "com.apple.mail.managed" {
		t.Fatalf("only mail belongs in it: %v", payloads)
	}
}
