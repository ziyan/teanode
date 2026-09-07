package config

import (
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// MailServers returns the hosts every domain's MX records should name, in
// order of preference.
//
// Empty configuration means the server itself, which is what a single-host
// deployment wants and what the getting-started guide tells people to publish.
// Reading it through here rather than at each call site means the two cases
// are the same shape everywhere.
func (self *Configuration) MailServers() []string {
	hosts := make([]string, 0, len(self.Server.MailServers))
	for _, host := range self.Server.MailServers {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 && self.Server.Name != "" {
		return []string{self.Server.Name}
	}
	return hosts
}

// MailHostsFor returns the names this domain's mail arrives at, which is what
// its MX records must point at.
//
// The domain the server is named under uses the server's own names: they are
// already in its zone, and a second name for the same host there buys nothing.
//
// Every other domain gets one name of its own, "mx." in front of the domain.
// It could be told to point at the server's name instead, and used to be, but
// then every domain published the name of a different one — look up the MX of
// any of them and you have the set they belong to.
//
// The other domains are what decide which one owns the server's name, which
// is why they are passed in: the caller has them, and this package no longer
// holds them.
func (self *Configuration) MailHostsFor(domain *models.Domain, domains []*models.Domain) []string {
	if domain == nil || domain.Domain == "" {
		return self.MailServers()
	}

	// What the domain says, when it says anything. One deployment wants a
	// pair of names because that is what it has always published, another
	// wants one, and a third wants a particular domain to keep pointing at
	// the server's own name. None of those is wrong, so none of them is
	// derived.
	if configured := trimmedHosts(domain.MailServers); len(configured) > 0 {
		return configured
	}

	servers := self.MailServers()
	owned := make([]string, 0, len(servers))
	for _, host := range servers {
		if !ownsServerName(domain, host, domains) {
			return []string{"mx." + domain.Domain}
		}
		owned = append(owned, host)
	}
	if len(owned) == 0 {
		return []string{"mx." + domain.Domain}
	}
	return owned
}

// trimmedHosts drops the blanks and the spaces, so a list typed into a form
// with a trailing comma means what it looks like it means.
func trimmedHosts(hosts []string) []string {
	trimmed := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(host), "."))); host != "" {
			trimmed = append(trimmed, host)
		}
	}
	return trimmed
}

// MailHostFor is the one name to use where only one is wanted — naming the
// host a sender reached, in a header. The first, which is the one an MX record
// prefers.
func (self *Configuration) MailHostFor(domain *models.Domain, domains []*models.Domain) string {
	hosts := self.MailHostsFor(domain, domains)
	if len(hosts) == 0 {
		return self.Server.Name
	}
	return hosts[0]
}

// LinkHostFor is the name to write into an address this server puts in mail it
// sends: a picture in a template, and whatever else a recipient's program
// later fetches.
//
// The domain's own, when it has one. Otherwise the name its mail arrives at,
// which is the right guess and is sometimes wrong for a reason that has
// nothing to do with mail: the host it resolves to may answer HTTPS with
// something else. That is what LinkHost is for.
func (self *Configuration) LinkHostFor(domain *models.Domain, domains []*models.Domain) string {
	if domain != nil {
		if host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain.LinkHost), ".")); host != "" {
			return host
		}
	}
	return self.MailHostFor(domain, domains)
}

// ownsServerName reports whether this domain is the one a name belonging to
// the server sits under, and so the one whose zone that name is published in.
//
// When no configured domain owns it — the server is called mail.example.net
// while serving example.com — nobody would be shown the records for it at all,
// so every domain is treated as the owner and the Setup page carries the
// address as well.
func ownsServerName(domain *models.Domain, serverName string, domains []*models.Domain) bool {
	owner := ""
	for _, candidate := range domains {
		if candidate == nil || candidate.Domain == "" {
			continue
		}
		name := strings.ToLower(candidate.Domain)
		if strings.EqualFold(serverName, name) || strings.HasSuffix(strings.ToLower(serverName), "."+name) {
			// The longest match wins, so a server called mail.a.example.com
			// is owned by a.example.com rather than example.com when both are
			// configured.
			if len(name) > len(owner) {
				owner = name
			}
		}
	}
	if owner == "" {
		return true
	}
	return strings.EqualFold(owner, domain.Domain)
}

// SubmissionHost is the host a mail client should be told to connect to.
//
// The configured one when there is one, and the server's own name otherwise —
// which is right whenever the server is reachable at the name it announces,
// and that is most deployments.
func (self *Configuration) SubmissionHost() string {
	if host := strings.TrimSpace(self.SMTP.Submission.Host); host != "" {
		return host
	}
	return self.Server.Name
}

// SubmissionPort is the port a mail client should be told to connect to.
//
// The configured one when there is one, and otherwise the port this server
// listens on — which is wrong exactly when something forwards a different one,
// which is what the setting is for.
func (self *Configuration) SubmissionPort() string {
	if port := self.SMTP.Submission.Port; port != 0 {
		return strconv.Itoa(int(port))
	}
	return portOf(self.Listen.SMTPOutgoing)
}

// portOf extracts the port from a listen address such as ":587" or
// "127.0.0.1:10587".
func portOf(address string) string {
	if index := strings.LastIndex(address, ":"); index >= 0 {
		return address[index+1:]
	}
	return address
}
