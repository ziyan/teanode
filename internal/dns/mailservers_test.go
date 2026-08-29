package dns

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// A deployment that receives mail on one name is the common case, and asking
// it to configure a list would be ceremony. An empty list means the server
// itself.
func TestOneMailServerByDefault(t *testing.T) {
	t.Parallel()
	configuration := &config.Configuration{}
	configuration.Server.Name = "mail.example.com"

	hosts := configuration.MailServers()
	if len(hosts) != 1 || hosts[0] != "mail.example.com" {
		t.Errorf("MailServers() = %v, want just the server's own name", hosts)
	}
}

// The reason this exists: mail for these domains arrives at a pair of names,
// so each domain needs a pair of MX records rather than one naming the host
// the server calls itself.
func TestAPairOfMailServersIsAPairOfRecords(t *testing.T) {
	t.Parallel()
	configuration := &config.Configuration{}
	configuration.Server.Name = "mail.example.com"
	configuration.Server.MailServers = []string{"mx1.example.com", "mx2.example.com"}

	hosts := configuration.MailServers()
	if len(hosts) != 2 || hosts[0] != "mx1.example.com" || hosts[1] != "mx2.example.com" {
		t.Fatalf("MailServers() = %v, want the configured pair in order", hosts)
	}

	// The record set asks for one row per name, ten apart, in the order given.
	set := &RecordSet{}
	for index, host := range hosts {
		set.Records = append(set.Records, &Record{
			Type:     "MX",
			Name:     "example.com.",
			Priority: uint16(10 * (index + 1)),
			Expected: dnsName(host),
		})
	}
	if set.Records[0].Priority != 10 || set.Records[1].Priority != 20 {
		t.Errorf("priorities are %d and %d, want 10 and 20",
			set.Records[0].Priority, set.Records[1].Priority)
	}
}

// Blank entries are a configuration typo, not a request for an empty MX.
func TestBlankMailServersAreIgnored(t *testing.T) {
	t.Parallel()
	configuration := &config.Configuration{}
	configuration.Server.Name = "mail.example.com"
	configuration.Server.MailServers = []string{"  ", "mx1.example.com", ""}

	hosts := configuration.MailServers()
	if len(hosts) != 1 || hosts[0] != "mx1.example.com" {
		t.Errorf("MailServers() = %v, want the one real entry", hosts)
	}
}

// Mail arriving is a different question from being as redundant as intended.
// Publishing the first of two names is worth pointing out on its own row, and
// is not a reason to tell somebody their mail cannot reach them.
func TestOnePublishedNameOfTwoIsStillDeliverable(t *testing.T) {
	t.Parallel()
	set := &RecordSet{Records: []*Record{
		{Type: "MX", Priority: 10, Expected: "mx1.example.com.", Verified: true},
		{Type: "MX", Priority: 20, Expected: "mx2.example.com.", Verified: false},
	}}
	if !set.DeliverableTo() {
		t.Error("a domain publishing the first of two mail servers was reported undeliverable")
	}

	none := &RecordSet{Records: []*Record{
		{Type: "MX", Priority: 10, Expected: "mx1.example.com.", Verified: false},
		{Type: "MX", Priority: 20, Expected: "mx2.example.com.", Verified: false},
	}}
	if none.DeliverableTo() {
		t.Error("a domain publishing neither mail server was reported deliverable")
	}
}
