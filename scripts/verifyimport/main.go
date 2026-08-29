// Command verifyimport compares a configuration file produced by
// "teanode config import" against the database it came from, field by field.
//
// It exists to answer one question before an existing deployment is replaced:
// did the migration lose anything? A dropped alias silently stops forwarding
// somebody's mail, and a changed credential key silently invalidates an SMTP
// password, so neither can be checked by eye across twenty-five domains.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"

	_ "github.com/lib/pq"

	"github.com/ziyan/teanode/internal/config"
)

func main() {
	filename := flag.String("config", "", "the configuration file to verify")
	dataSource := flag.String("database", "host=127.0.0.1 port=25432 user=teanode password=teanode dbname=teanode sslmode=disable", "the legacy database")
	flag.Parse()

	configuration, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot load %s: %s\n", *filename, err)
		os.Exit(1)
	}

	database, err := sql.Open("postgres", *dataSource)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open the database: %s\n", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close() }()

	var problems []string
	problems = append(problems, compareDomains(database, configuration)...)
	problems = append(problems, compareAliases(database, configuration)...)
	problems = append(problems, compareCredentials(database, configuration)...)
	problems = append(problems, compareStoredMail(database, configuration)...)

	if len(problems) > 0 {
		fmt.Printf("\n%d PROBLEMS\n", len(problems))
		sort.Strings(problems)
		for _, problem := range problems {
			fmt.Println("  -", problem)
		}
		os.Exit(1)
	}
	fmt.Println("\nLOSSLESS: every field in the database is represented in the configuration file")
}

func compareDomains(database *sql.DB, configuration *config.Configuration) []string {
	rows, err := database.Query(`SELECT id, domain, COALESCE(subdomain,''), COALESCE(spam_filter_score_threshold,0) FROM domain`)
	if err != nil {
		return []string{fmt.Sprintf("cannot read domains: %s", err)}
	}
	defer func() { _ = rows.Close() }()

	var problems []string
	var count int
	for rows.Next() {
		var id, name, subdomain string
		var threshold float64
		if err := rows.Scan(&id, &name, &subdomain, &threshold); err != nil {
			return append(problems, fmt.Sprintf("cannot read a domain: %s", err))
		}
		count++

		domain := configuration.FindDomainByID(id)
		if domain == nil {
			problems = append(problems, fmt.Sprintf("domain %q (%s) is missing from the configuration", name, id))
			continue
		}
		if domain.Domain != name {
			problems = append(problems, fmt.Sprintf("domain %s: name is %q, database says %q", id, domain.Domain, name))
		}
		if domain.Subdomain != subdomain {
			problems = append(problems, fmt.Sprintf("domain %q: subdomain is %q, database says %q", name, domain.Subdomain, subdomain))
		}
		if domain.SpamFilterScoreThreshold != threshold {
			problems = append(problems, fmt.Sprintf("domain %q: spam threshold is %v, database says %v", name, domain.SpamFilterScoreThreshold, threshold))
		}
	}
	fmt.Printf("domains:     %d in the database, %d in the configuration\n", count, len(configuration.Domains))
	return problems
}

func compareAliases(database *sql.DB, configuration *config.Configuration) []string {
	rows, err := database.Query(`
		SELECT id, domain_id, pattern, COALESCE(kind,''), COALESCE(email,''), COALESCE(webhook,''),
		       COALESCE(mail_server_host,''), COALESCE(mail_server_port,0),
		       COALESCE(mail_server_username,''), COALESCE(mail_server_password,''),
		       disabled_at IS NOT NULL
		FROM alias`)
	if err != nil {
		return []string{fmt.Sprintf("cannot read aliases: %s", err)}
	}
	defer func() { _ = rows.Close() }()

	var problems []string
	var count, configured int
	for rows.Next() {
		var id, domainId, pattern, kind, email, webhook, host, username, password string
		var port int
		var disabled bool
		if err := rows.Scan(&id, &domainId, &pattern, &kind, &email, &webhook, &host, &port, &username, &password, &disabled); err != nil {
			return append(problems, fmt.Sprintf("cannot read an alias: %s", err))
		}
		count++

		alias := configuration.FindAliasByID(id)
		if alias == nil {
			problems = append(problems, fmt.Sprintf("alias %q (%s) is missing from the configuration; mail matching it would stop being forwarded", pattern, id))
			continue
		}
		if alias.Pattern != pattern {
			problems = append(problems, fmt.Sprintf("alias %s: pattern is %q, database says %q", id, alias.Pattern, pattern))
		}
		if string(alias.Kind) != kind {
			problems = append(problems, fmt.Sprintf("alias %q: kind is %q, database says %q", pattern, alias.Kind, kind))
		}
		if kind == "email" && alias.Email != email {
			problems = append(problems, fmt.Sprintf("alias %q: forwards to %q, database says %q", pattern, alias.Email, email))
		}
		if kind == "webhook" && alias.Webhook != webhook {
			problems = append(problems, fmt.Sprintf("alias %q: webhook differs", pattern))
		}
		if kind == "mailServer" {
			if alias.MailServer == nil {
				problems = append(problems, fmt.Sprintf("alias %q: no mail server in the configuration, database says %s:%d", pattern, host, port))
			} else {
				if alias.MailServer.Host != host || int(alias.MailServer.Port) != port {
					problems = append(problems, fmt.Sprintf("alias %q: relays to %s:%d, database says %s:%d", pattern, alias.MailServer.Host, alias.MailServer.Port, host, port))
				}
				if alias.MailServer.Username != username || alias.MailServer.Password != password {
					problems = append(problems, fmt.Sprintf("alias %q: relay credentials differ, so it would fail to authenticate", pattern))
				}
			}
		}
		if alias.Disabled != disabled {
			problems = append(problems, fmt.Sprintf("alias %q: disabled is %v, database says %v", pattern, alias.Disabled, disabled))
		}

		// The domain it belongs to has to be the same one.
		var found bool
		if domain := configuration.FindDomainByID(domainId); domain != nil {
			for _, candidate := range domain.Aliases {
				if candidate.ID == id {
					found = true
					break
				}
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("alias %q is under a different domain than the database says", pattern))
		}
	}
	for _, domain := range configuration.Domains {
		configured += len(domain.Aliases)
	}
	fmt.Printf("aliases:     %d in the database, %d in the configuration\n", count, configured)
	return problems
}

func compareCredentials(database *sql.DB, configuration *config.Configuration) []string {
	rows, err := database.Query(`SELECT id, domain_id, key, COALESCE(alias,''), disabled_at IS NOT NULL FROM credential`)
	if err != nil {
		return []string{fmt.Sprintf("cannot read credentials: %s", err)}
	}
	defer func() { _ = rows.Close() }()

	var problems []string
	var count, configured int
	for rows.Next() {
		var id, domainId, key, alias string
		var disabled bool
		if err := rows.Scan(&id, &domainId, &key, &alias, &disabled); err != nil {
			return append(problems, fmt.Sprintf("cannot read a credential: %s", err))
		}
		count++

		domain, credential := configuration.FindCredential(id)
		if credential == nil {
			problems = append(problems, fmt.Sprintf("credential %s is missing from the configuration; whatever uses it could no longer send", id))
			continue
		}
		// The key is what the SMTP password derives from. If it changed, every
		// client holding that password stops working.
		if credential.Key != key {
			problems = append(problems, fmt.Sprintf("credential %s: key differs, so its SMTP password would change", id))
		}
		if credential.Alias != alias {
			problems = append(problems, fmt.Sprintf("credential %s: alias restriction is %q, database says %q", id, credential.Alias, alias))
		}
		if credential.Disabled != disabled {
			problems = append(problems, fmt.Sprintf("credential %s: disabled is %v, database says %v", id, credential.Disabled, disabled))
		}
		if domain == nil || domain.ID != domainId {
			problems = append(problems, fmt.Sprintf("credential %s is under a different domain than the database says", id))
		}
	}
	for _, domain := range configuration.Domains {
		configured += len(domain.Credentials)
	}
	fmt.Printf("credentials: %d in the database, %d in the configuration\n", count, configured)
	return problems
}

// compareStoredMail checks the other direction: every domain, alias and
// credential that stored mail still points at has to exist in the
// configuration, or the dashboard would show that mail as orphaned.
func compareStoredMail(database *sql.DB, configuration *config.Configuration) []string {
	var problems []string

	checks := []struct {
		what  string
		query string
		find  func(string) bool
	}{
		{"mail", `SELECT DISTINCT domain_id FROM mail WHERE domain_id <> ''`,
			func(id string) bool { return configuration.FindDomainByID(id) != nil }},
		{"deliveries", `SELECT DISTINCT alias_id FROM delivery WHERE alias_id IS NOT NULL AND alias_id <> ''`,
			func(id string) bool { return configuration.FindAliasByID(id) != nil }},
		{"mail sent with a credential", `SELECT DISTINCT credential_id FROM mail WHERE credential_id IS NOT NULL AND credential_id <> ''`,
			func(id string) bool { _, credential := configuration.FindCredential(id); return credential != nil }},
		{"reports", `SELECT DISTINCT domain_id FROM report WHERE domain_id <> ''`,
			func(id string) bool { return configuration.FindDomainByID(id) != nil }},
	}

	for _, check := range checks {
		rows, err := database.Query(check.query)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cannot read %s: %s", check.what, err))
			continue
		}
		var total, dangling int
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				continue
			}
			total++
			if !check.find(id) {
				dangling++
				problems = append(problems, fmt.Sprintf("%s reference %s, which is not in the configuration", check.what, id))
			}
		}
		_ = rows.Close()
		fmt.Printf("%-28s %d referenced, %d dangling\n", check.what+":", total, dangling)
	}
	return problems
}
