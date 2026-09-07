package dns

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

// mailHost is a name this domain's MX records point at.
type mailHost struct {
	// Name is what the MX record names, fully qualified.
	Name string

	// Aliases are the server's own names for the same host, when this domain
	// publishes its own. They are what an installation set up before this
	// points at, and still count as correct.
	Aliases []string
}

// mailHostsFor works out the names a domain's mail should be addressed to.
//
// A domain that owns the server's name uses it: a server called
// mail.example.com serving example.com has nothing to gain from a second name
// for the same host.
//
// Every other domain gets its own. Pointing twenty-five domains at
// mx1.example.com works and is what this used to ask for, but it publishes, in
// each of those domains, the name of a different one — so anybody who looks up
// the MX of one learns the set they belong to. A domain publishing
// mx1.<its own name> gives that away to nobody, and the address behind it is
// the same.
//
// The cost is real and worth stating: the address is then written down once
// per domain rather than once, so a server that changes address has one record
// per domain to update instead of one. That is the same trade the DKIM records
// make, and it is the reason this is derived from the server's names rather
// than invented — mx1 stays mx1, so what to change is obvious.
func mailHostsFor(configuration *config.Configuration, domain *models.Domain, domains []*models.Domain) []mailHost {
	// The names themselves are the configuration's answer, so the panel and
	// the rest of the server cannot disagree about what a domain's mail
	// arrives at. What is added here is only what checking needs: the
	// server's own names still count as reaching it, which is what an
	// installation set up before this points at.
	servers := configuration.MailServers()
	names := configuration.MailHostsFor(domain, domains)

	hosts := make([]mailHost, 0, len(names))
	for _, name := range names {
		host := mailHost{Name: dnsName(name)}
		if !slices.Contains(servers, name) {
			for _, server := range servers {
				host.Aliases = append(host.Aliases, dnsName(server))
			}
		}
		hosts = append(hosts, host)
	}
	return hosts
}

// namesThisServer reports whether an MX target is one this server answers on,
// under either the domain's own name or the server's.
func (self *mailHost) namesThisServer(target string) bool {
	if strings.EqualFold(target, self.Name) {
		return true
	}
	for _, alias := range self.Aliases {
		if alias != "" && strings.EqualFold(target, alias) {
			return true
		}
	}
	return false
}

// Record is one DNS record a domain has to publish, together with what is
// actually published today. The dashboard shows these so an operator can see
// what is missing without running dig.
type Record struct {
	// Type is the record type: MX, CNAME, A or TXT.
	Type string `json:"type"`

	// Name is the fully qualified name the record goes at.
	Name string `json:"name"`

	// Expected is the value that should be published. For TXT records with a
	// long value, such as a DKIM key, this is the whole value.
	Expected string `json:"expected"`

	// Found is what is published now, empty when nothing is.
	Found []string `json:"found,omitempty"`

	// Verified is true when what is published satisfies what is expected.
	Verified bool `json:"verified"`

	// Priority is the preference to publish an MX record at, and is zero for
	// every other type. Lower is tried first.
	Priority uint16 `json:"priority,omitempty"`

	// Optional marks a record worth publishing that nothing breaks without.
	// An AAAA is the case this exists for: a host reachable over IPv4 alone is
	// a complete, working mail server, and colouring its missing AAAA the same
	// red as a missing MX teaches an operator to ignore the colour.
	Optional bool `json:"optional,omitempty"`

	// Purpose says, in one sentence, what breaks without this record.
	Purpose string `json:"purpose"`
}

// RecordSet is every record one domain needs, and whether mail can flow.
type RecordSet struct {
	Domain string `json:"domain"`

	// Records in the order an operator should create them.
	Records []*Record `json:"records"`

	// CheckedAt is when this was last resolved.
	CheckedAt time.Time `json:"checkedAt"`

	// Error is set when the check itself failed, as distinct from a record
	// being absent.
	Error string `json:"error,omitempty"`
}

// Verified reports whether every record is published correctly.
// An optional record that is not published does not make the set incomplete.
// A domain reachable over IPv4 alone is correctly configured, and saying
// otherwise trains the reader to ignore the answer.
func (self *RecordSet) Verified() bool {
	for _, record := range self.Records {
		if !record.Verified && !record.Optional {
			return false
		}
	}
	return len(self.Records) > 0
}

// DeliverableTo reports whether mail can actually reach this server for the
// domain, which needs only an MX record. The rest affect whether that mail is
// trusted, not whether it arrives.
//
// Any one of them is enough. A deployment that asks for a pair of names and
// has published only the first is less redundant than it means to be, and mail
// still arrives, so this is not the place to complain about it — the row for
// the missing name already says so.
func (self *RecordSet) DeliverableTo() bool {
	for _, record := range self.Records {
		if record.Type == "MX" && record.Verified {
			return true
		}
	}
	return false
}

// resolveDomainRecords works out what this domain should publish and checks
// what it actually publishes.
//
// The expectations come from the configuration: the MX must name this server,
// the bounce subdomain must resolve to it, and the DKIM and DMARC records must
// exist. Unlike the hosted service this grew out of, the DKIM record is a TXT
// holding the public key rather than a CNAME, because this server holds the
// key itself.
func (self *verifier) resolveDomainRecords(ctx context.Context, configuration *config.Configuration, domain *models.Domain, domains []*models.Domain) *RecordSet {
	start := time.Now()

	recordSet := &RecordSet{
		Domain:    domain.Domain,
		CheckedAt: start,
	}

	serverName := strings.TrimSuffix(configuration.Server.Name, ".")
	external := self.ExternalAddresses(ctx)

	// Whatever the MX names has to resolve to this server before anything
	// else matters, so those addresses come first.
	//
	// They follow the mail servers rather than server.name, because those are
	// two different things once a deployment reaches mail on a pair of names:
	// the MX names mx1 and mx2, and it is those that must resolve. Asking for
	// an address on server.name instead described a requirement that had
	// stopped being true.
	//
	// One pair per name this domain's mail is addressed to, and those names
	// are this domain's own — so every row on this page is a record the reader
	// can go and create. It used to list the server's names, which meant
	// twenty-four pages out of twenty-five carried two rows that were somebody
	// else's to publish.
	hosts := mailHostsFor(configuration, domain, domains)
	for _, host := range hosts {
		// Only for a name in this domain's own zone. A domain configured to
		// point at a name somebody else owns — the server's own, most likely
		// — is not the place to ask for that name's address record, because
		// the reader of this page cannot publish it.
		if !domain.InThisDomain(host.Name) {
			continue
		}
		recordSet.Records = append(recordSet.Records,
			self.serverAddressRecords(ctx, strings.TrimSuffix(host.Name, "."), external)...)
	}

	// MX: without at least one of these, no mail arrives at all.
	//
	// One row per configured mail server rather than one row for the set,
	// because every other record here is one thing to publish and a reader
	// should not have to treat this one differently. A deployment that reaches
	// mail on a pair of names gets a pair of rows, each ticked on its own.
	published, resolveErr := []string(nil), error(nil)
	if _, records, err := self.resolveMx(ctx, domain.Domain); err != nil {
		recordSet.Error = err.Error()
		resolveErr = err
	} else {
		published = records
	}
	for index, host := range hosts {
		mailServer := &Record{
			Type: "MX",
			Name: dnsName(domain.Domain),
			// Ten apart, in the order configured. The numbers themselves carry
			// no meaning beyond their order, and leaving room between them is
			// the convention.
			Priority: uint16(10 * (index + 1)),
			Expected: host.Name,
			Purpose:  "directs mail for this domain to this server; without it no mail arrives",
		}
		if resolveErr == nil {
			mailServer.Found = published
			mailServer.Verified = self.mxReachesHere(ctx, published, host, external)
		}
		recordSet.Records = append(recordSet.Records, mailServer)
	}

	// The bounce and report subdomain has to receive mail, because it is the
	// domain part of the signed return path on outbound mail and where this
	// domain's DMARC reports are sent.
	//
	// Its own MX, naming the same hosts as the apex. It used to be a CNAME to
	// the server's name, which worked and told anybody who asked which server
	// this was — the same thing the MX records used to do. An installation
	// that still publishes that CNAME stays correct without changing anything:
	// an MX lookup follows it and finds the same hosts.
	//
	// Nothing is asked for when the name is already one of the mail hosts, or
	// the server itself. Both already resolve here, and a record pointing a
	// name at itself is a loop.
	subdomainName := dnsName(domain.Hostname())
	isMailHost := false
	for _, host := range hosts {
		if strings.EqualFold(subdomainName, host.Name) {
			isMailHost = true
		}
	}
	if !isMailHost && !strings.EqualFold(subdomainName, dnsName(serverName)) {
		subdomainPublished, subdomainErr := []string(nil), error(nil)
		if _, records, err := self.resolveMx(ctx, subdomainName); err != nil {
			subdomainErr = err
		} else {
			subdomainPublished = records
		}

		for index, host := range hosts {
			subdomain := &Record{
				Type:     "MX",
				Name:     subdomainName,
				Priority: uint16(10 * (index + 1)),
				Expected: host.Name,
				Purpose:  "where bounces and DMARC reports for this domain come back to",
			}
			if subdomainErr == nil {
				subdomain.Found = subdomainPublished
				subdomain.Verified = self.mxReachesHere(ctx, subdomainPublished, host, external)
			}
			if !subdomain.Verified {
				// The older shape: an alias to the server, or an address
				// record pointing at the same host. Both still deliver here.
				if target, err := self.resolveCname(ctx, subdomainName); err == nil && target != "" {
					if len(subdomain.Found) == 0 {
						subdomain.Found = []string{target}
					}
					subdomain.Verified = strings.EqualFold(target, dnsName(serverName))
				}
			}
			if !subdomain.Verified {
				if addresses, err := self.resolveAddresses(ctx, subdomainName); err == nil && len(addresses) > 0 {
					serverAddresses, err := self.resolveAddresses(ctx, serverName)
					if err == nil && sameAddress(addresses, serverAddresses) {
						subdomain.Found = addresses
						subdomain.Verified = true
					} else if len(subdomain.Found) == 0 {
						subdomain.Found = addresses
					}
				}
			}
			recordSet.Records = append(recordSet.Records, subdomain)
		}
	}

	// SPF, at the return path rather than at the domain itself.
	//
	// This is the record that says which addresses may send for this domain,
	// and receivers evaluate it against the envelope sender — which on
	// everything this server sends is the signed return path under the bounce
	// name, not the address in the From header. So the record belongs there,
	// and a perfectly good one at the domain itself does nothing for it.
	//
	// Nothing is asked for at the domain itself. The apex is where other
	// senders' records live — a hosted mailing list, a transactional provider
	// — and a "-all" published here on their behalf would refuse mail this
	// server knows nothing about.
	//
	// There was no row for this at all until an alias that happened to carry
	// one was removed, and nothing anywhere said the sending address had
	// stopped being authorised.
	spfName := dnsName(domain.Hostname())
	sender := &Record{
		Type:     "TXT",
		Name:     spfName,
		Expected: expectedSpf(external),
		Purpose:  "says this server may send mail for this domain; without it, what you send is unauthenticated",
	}
	if configuration.SMTP.SOCKS5Proxy != "" || configuration.SMTP.Relay.Host != "" {
		// The server cannot see the address its mail leaves from when
		// something else carries it, and guessing would be worse than saying
		// so: the address below is where mail arrives, which is not
		// necessarily where it leaves.
		sender.Purpose = "says which addresses may send mail for this domain — the ones your outgoing mail leaves from, which with a proxy or a relay are not this server's own"
	}
	if records, err := self.resolveTxt(ctx, spfName); err == nil {
		sender.Found = records
		for _, record := range records {
			if authorisesSending(record) {
				sender.Verified = true
				break
			}
		}
	}
	recordSet.Records = append(recordSet.Records, sender)

	// DKIM. The expected value is the domain's actual public key, so it can be
	// copied straight into a DNS provider's form rather than being fetched
	// with a separate command.
	if domain.DKIM.Selector != "" {
		name := dnsName(models.DomainKeyName(domain.DKIM.Selector, domain.Domain))
		domainKey := &Record{
			Type:    "TXT",
			Name:    name,
			Purpose: "lets receiving servers verify the signature on mail sent from this domain",
		}

		expected, err := domain.DKIM.PublicKeyRecord()
		if err != nil {
			domainKey.Expected = "(this domain has no usable signing key)"
		} else {
			domainKey.Expected = expected
		}

		if records, err := self.resolveTxt(ctx, name); err == nil {
			domainKey.Found = records
			for _, record := range records {
				if !publishesDkimKey(record) {
					continue
				}
				// Any published key counts as a working record, but only the
				// matching one actually verifies this server's signatures, so
				// a stale key from a previous setup is worth pointing out.
				if expected != "" && !sameDkimKey(record, expected) {
					domainKey.Purpose = "a DKIM key is published here, but not this server's — signatures from here will fail until it is replaced"
					continue
				}
				domainKey.Verified = true
				break
			}
		}
		recordSet.Records = append(recordSet.Records, domainKey)
	}

	// DMARC tells receivers what to do when authentication fails, and is where
	// aggregate reports are sent.
	dmarcName := dnsName("_dmarc." + domain.Domain)
	dmarc := &Record{
		Type:     "TXT",
		Name:     dmarcName,
		Expected: fmt.Sprintf("v=DMARC1; p=none; rua=mailto:%s", reportAddress(configuration, domain)),
		Purpose:  "tells receiving servers what to do with mail that fails authentication, and where to send reports",
	}
	if records, err := self.resolveTxt(ctx, dmarcName); err == nil {
		dmarc.Found = records
		for _, record := range records {
			if strings.HasPrefix(strings.TrimSpace(record), "v=DMARC1") {
				dmarc.Verified = true
				break
			}
		}
	}
	recordSet.Records = append(recordSet.Records, dmarc)

	log.Debugf("took %s to check the records for %q", time.Since(start), domain.Domain)
	return recordSet
}

// dnsName returns a fully qualified name, with the trailing dot that DNS
// answers carry, so comparisons do not depend on how it was written.
func dnsName(name string) string {
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return ""
	}
	return name + "."
}

func sameAddress(first, second []string) bool {
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	present := make(map[string]bool, len(second))
	for _, address := range second {
		present[address] = true
	}
	for _, address := range first {
		if present[address] {
			return true
		}
	}
	return false
}

// publishesDkimKey reports whether a TXT record actually carries a usable
// public key.
//
// An empty "p=" is not an absent tag: RFC 6376 section 3.6.1 says it means the
// key has been revoked. Treating that as verified would tell an operator their
// DKIM was fine while every signature they send fails.
// sameDkimKey compares the key material of two records, ignoring the other
// tags and any whitespace a DNS provider may have introduced.
func sameDkimKey(first, second string) bool {
	return dkimKeyMaterial(first) == dkimKeyMaterial(second)
}

func dkimKeyMaterial(record string) string {
	for _, tag := range strings.Split(record, ";") {
		tag = strings.TrimSpace(tag)
		if !strings.HasPrefix(tag, "p=") {
			continue
		}
		return strings.Join(strings.Fields(strings.TrimPrefix(tag, "p=")), "")
	}
	return ""
}

func publishesDkimKey(record string) bool {
	published := false
	for _, tag := range strings.Split(record, ";") {
		tag = strings.TrimSpace(tag)
		switch {
		case strings.HasPrefix(tag, "v="):
			// RFC 6376 section 3.6.1 makes v= RECOMMENDED rather than
			// required, with a default of DKIM1 — so a record without it is a
			// DKIM key record and every verifier treats it as one. Insisting
			// on it reported working keys as missing, which is worse than
			// useless: it asks somebody to fix DNS that was already right.
			//
			// A v= that says something else is a different kind of record,
			// and "v=spf1 -all" is the one that turns up at the wrong name.
			if strings.TrimSpace(strings.TrimPrefix(tag, "v=")) != "DKIM1" {
				return false
			}
		case strings.HasPrefix(tag, "p="):
			published = strings.TrimSpace(strings.TrimPrefix(tag, "p=")) != ""
		}
	}
	return published
}

// serverAddressRecords describes the A and AAAA records for the server's own
// name. They come first because everything else depends on them: the MX record
// names this host, so mail cannot arrive until it resolves.
//
// The expected value is this server's external address, discovered by asking
// something outside what address it sees. That is a question the server cannot
// answer alone — the address on its interface is usually private — and it is
// the single thing an operator most needs told, because it is the one value
// they cannot look up anywhere.
func (self *verifier) serverAddressRecords(ctx context.Context, host string, external ExternalAddresses) []*Record {
	name := dnsName(host)
	published, _ := self.resolveAddresses(ctx, host)

	var records []*Record
	for _, family := range []struct {
		recordType string
		expected   string
		matches    func(string) bool
	}{
		{"A", external.IPv4, func(value string) bool { return net.ParseIP(value).To4() != nil }},
		{"AAAA", external.IPv6, func(value string) bool {
			address := net.ParseIP(value)
			return address != nil && address.To4() == nil
		}},
	} {
		// A server with no IPv6 needs no AAAA record, and inventing one to
		// complain about would be noise.
		if family.expected == "" {
			continue
		}

		record := &Record{
			Type:     family.recordType,
			Name:     name,
			Expected: family.expected,
			Optional: family.recordType == "AAAA",
			Purpose:  "points a name in this domain's MX record at this server; without it no mail arrives",
		}
		if record.Optional {
			record.Purpose = "lets senders reach this server over IPv6, which is worth having and not required"
		}
		for _, value := range published {
			if !family.matches(value) {
				continue
			}
			record.Found = append(record.Found, value)
			if value == family.expected {
				record.Verified = true
			}
		}
		records = append(records, record)
	}

	if len(records) == 0 {
		// Nothing is known about the address, so say that rather than
		// silently omitting the most important record on the page.
		records = append(records, &Record{
			Type:     "A",
			Name:     name,
			Expected: external.Suggestion(),
			Purpose:  "points a name in this domain's MX record at this server; without it no mail arrives",
			Found:    published,
			Verified: len(published) > 0,
		})
	}
	return records
}

// reportAddress is where a domain's DMARC aggregate reports are sent.
//
// Signed, because that is the only kind this server accepts: a recipient
// starting "rua-" is checked against the server secret before it is taken, and
// anything else at that name is an unknown address and refused. This asked for
// a plain "rua@" for as long as it has existed, which is an address the server
// rejects — so an operator who followed the dashboard exactly got no reports
// and nothing said why. The live deployment worked only because its records
// were written by hand, by the hosted service this grew out of, which signed
// them.
//
// The identifier is derived from the domain rather than being the domain's
// own, because a signed address has to carry a ULID and a domain identifier is
// usually its name — "example.com" is not one, and an address carrying it is
// refused by the very check that is supposed to accept it. Nothing reads the
// identifier on a report anyway: which domain a report is about comes from the
// report, not from the address it arrived at.
//
// Signing needs the server secret; with none — a configuration being checked
// before the first start — the plain form is shown rather than nothing, and it
// is corrected the moment there is a secret.
func reportAddress(configuration *config.Configuration, domain *models.Domain) string {
	secret := configuration.Secret()
	if len(secret) == 0 {
		return "rua@" + domain.Hostname()
	}
	signed, err := mailparse.SignAddress("rua", security.DerivedULID(secret, "rua:"+domain.ID), domain.Hostname(), secret)
	if err != nil {
		return "rua@" + domain.Hostname()
	}
	return signed
}

// mxReachesHere reports whether what is published at a name delivers to this
// server.
//
// By name first, which covers both the domain's own and the server's, and only
// then by address — because a name this code did not choose still reaches the
// right place if it resolves to the right address, and telling somebody their
// working MX is wrong because it is spelled differently is worse than one
// extra lookup.
func (self *verifier) mxReachesHere(ctx context.Context, published []string, host mailHost, external ExternalAddresses) bool {
	for _, target := range published {
		if host.namesThisServer(target) {
			return true
		}
	}

	var expected []string
	if external.IPv4 != "" {
		expected = append(expected, external.IPv4)
	}
	if external.IPv6 != "" {
		expected = append(expected, external.IPv6)
	}
	if len(expected) == 0 {
		return false
	}
	for _, target := range published {
		if addresses, err := self.resolveAddresses(ctx, strings.Trim(target, ".")); err == nil {
			if sameAddress(addresses, expected) {
				return true
			}
		}
	}
	return false
}

// expectedSpf is the record to publish when this server sends its own mail
// directly, which is the case it can answer for.
func expectedSpf(external ExternalAddresses) string {
	mechanisms := []string{"v=spf1"}
	if external.IPv4 != "" {
		mechanisms = append(mechanisms, "ip4:"+external.IPv4)
	}
	if external.IPv6 != "" {
		mechanisms = append(mechanisms, "ip6:"+external.IPv6)
	}
	// Everything else refused, because this name exists only as the return
	// path of mail this server sends: nothing else has any business using it.
	return strings.Join(append(mechanisms, "-all"), " ")
}

// authorisesSending reports whether an SPF record permits anything at all.
//
// Not whether it permits this server, which cannot be decided here: an
// "include:" or an "a" is a question for a resolver, and the address mail
// actually leaves from is not one this server knows when a proxy or a relay
// carries it. What it does catch is the two ways this record goes wrong — not
// published, and published as a blanket refusal. "v=spf1 -all" is the right
// record for a domain that sends nothing and the wrong one for a domain whose
// return path this is; it is also what a domain gets by default from more than
// one registrar, so it arrives without anybody choosing it.
func authorisesSending(record string) bool {
	fields := strings.Fields(strings.TrimSpace(record))
	if len(fields) == 0 || !strings.EqualFold(fields[0], "v=spf1") {
		return false
	}
	for _, field := range fields[1:] {
		// The trailing all, whatever its qualifier, is what happens when
		// nothing else matched — it is never what authorises a sender.
		if strings.EqualFold(strings.TrimLeft(field, "+-~?"), "all") {
			continue
		}
		if strings.HasPrefix(field, "-") || strings.HasPrefix(field, "~") || strings.HasPrefix(field, "?") {
			// An explicit refusal of something is not permission for it.
			continue
		}
		return true
	}
	return false
}
