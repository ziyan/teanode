package configdb

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// TestRoundTrip is what holds the two mappings together.
//
// A field written by one and not read by the other is a setting that silently
// resets the next time anything is saved — the operator changes a domain, and
// the spam threshold they set last month goes back to the default. Nothing
// about that is visible in a diff of either function on its own.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	original := config.Default()
	original.Server.Name = "mail.primary.test"
	// Raw bytes, because that is what a server secret is: 32 from crypto/rand,
	// so roughly one in eight contains a zero byte and most are not valid
	// UTF-8. An encoding that quietly repairs those — encoding/json turns an
	// invalid byte into the replacement character — would invalidate every
	// SMTP password on the server, and nothing would say so.
	original.Server.Secret = string([]byte{0x00, 0x41, 0xff, 0xfe, 0x42, 0x00, 0x80})
	original.Server.LogDirectory = "/var/lib/teanode/mail"
	original.Session.Key = string([]byte{0xff, 0x00, 0x01, 0x7f, 0xc3})
	original.SMTP.SOCKS5Proxy = "127.0.0.1:1081"
	original.SMTP.RequireReverseDNS = false
	original.TLS.Hosts = []string{"mail.primary.test", "mx1.primary.test"}
	original.TLS.ACME.Enabled = false
	original.TLS.CertificateFile = "teanode.crt"
	original.TLS.PrivateKeyFile = "teanode.key"
	original.Storage.S3.Enabled = true
	original.Storage.S3.Endpoint = "http://minio:9000"
	original.Storage.S3.Bucket = "teanode"
	original.Storage.S3.PathStyle = true
	original.Antivirus.Enabled = true
	original.GeoIP.Enabled = false

	original.Domains = []*config.Domain{{
		ID:                       "primary.test",
		Domain:                   "primary.test",
		Subdomain:                "mail",
		Comment:                  "the first one",
		SpamFilterScoreThreshold: 7.5,
		// Not a real key, and deliberately not shaped like one either: the
		// secret scanner reads PEM headers in tracked files as a leak, and it
		// is right to, so a fixture should not teach anyone to write one.
		DKIM: config.DomainKey{Selector: "teanode1", PrivateKey: "a-private-key-that-is-not-parsed-here"},
		Aliases: []*config.Alias{
			{ID: "alias-1", Pattern: "^hello$", Comment: "a note", Kind: config.AliasKindEmail, Email: "you@example.net"},
			{ID: "alias-2", Pattern: "", Kind: config.AliasKindMailServer, Disabled: true,
				MailServer: &config.MailServer{Host: "smtp.example.net", Port: 25, Username: "relay", Password: "secret"}},
			{ID: "alias-3", Pattern: "^hook$", Kind: config.AliasKindWebhook, Webhook: "https://example.net/hook"},
		},
		Credentials: []*config.Credential{
			{ID: "credential-1", Key: "a-key", Comment: "laptop", Alias: "noreply"},
			{ID: "credential-2", Key: "another-key", Disabled: true},
		},
	}}

	original.Users = []*config.User{{
		Username:     "ziyan",
		PasswordHash: "$2a$12$" + strings.Repeat("x", 53),
		Email:        "ziyan@example.net",
	}}

	rows, err := ToRows(original, 7)
	if err != nil {
		t.Fatalf("ToRows: %s", err)
	}
	if rows.Version != 7 {
		t.Errorf("the version should be carried through, got %d", rows.Version)
	}

	returned, err := FromRows(rows)
	if err != nil {
		t.Fatalf("FromRows: %s", err)
	}

	// Compared field by field rather than with DeepEqual on the whole thing,
	// because the configuration carries an unexported lookup index that is
	// rebuilt on demand and is not part of what is stored.
	if !reflect.DeepEqual(original.Server, returned.Server) {
		t.Errorf("server settings differ:\n got %+v\nwant %+v", returned.Server, original.Server)
	}
	// Called out separately, because a difference here is the expensive one
	// and "server settings differ" buries it in a struct dump.
	if returned.Server.Secret != original.Server.Secret {
		t.Errorf("the server secret came back as %q, want %q; every SMTP password derives from this",
			returned.Server.Secret, original.Server.Secret)
	}
	if returned.Session.Key != original.Session.Key {
		t.Errorf("the session key came back as %q, want %q; everybody would be logged out",
			returned.Session.Key, original.Session.Key)
	}
	for name, pair := range map[string][2]any{
		"listen":    {original.Listen, returned.Listen},
		"tls":       {original.TLS, returned.TLS},
		"smtp":      {original.SMTP, returned.SMTP},
		"dkim":      {original.DKIM, returned.DKIM},
		"session":   {original.Session, returned.Session},
		"dns":       {original.DNS, returned.DNS},
		"antivirus": {original.Antivirus, returned.Antivirus},
		"antispam":  {original.Antispam, returned.Antispam},
		"geoip":     {original.GeoIP, returned.GeoIP},
		"storage":   {original.Storage, returned.Storage},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s settings differ:\n got %+v\nwant %+v", name, pair[1], pair[0])
		}
	}

	if !reflect.DeepEqual(original.Domains, returned.Domains) {
		t.Errorf("domains differ:\n got %+v\nwant %+v", returned.Domains[0], original.Domains[0])
	}

	if len(returned.Users) != 1 {
		t.Fatalf("expected one account, got %d", len(returned.Users))
	}
	user := returned.Users[0]
	if user.Username != "ziyan" || user.PasswordHash != original.Users[0].PasswordHash || user.Email != "ziyan@example.net" {
		t.Errorf("the account differs: %+v", user)
	}
}

// TestEverySectionIsStored fails when a section is added to the configuration
// and not to the mapping — which would otherwise be a setting that is
// configurable, appears to save, and is gone after a restart.
func TestEverySectionIsStored(t *testing.T) {
	t.Parallel()

	// The lists are stored as their own tables rather than as settings.
	asTables := map[string]bool{"Domains": true, "Users": true}

	// The connection to the database cannot be stored in the database. It is
	// bootstrap, and comes from the environment.
	fromEnvironment := map[string]bool{"Database": true}

	rows, err := ToRows(config.Default(), 0)
	if err != nil {
		t.Fatalf("ToRows: %s", err)
	}

	configurationType := reflect.TypeOf(config.Configuration{})
	for index := 0; index < configurationType.NumField(); index++ {
		field := configurationType.Field(index)
		if !field.IsExported() || asTables[field.Name] || fromEnvironment[field.Name] {
			continue
		}
		key := strings.ToLower(field.Name)
		if _, ok := rows.Settings[key]; !ok {
			t.Errorf("the %q section is not stored; add it to the section map in rows.go, "+
				"or to asTables here if it has a table of its own", field.Name)
		}
	}
}

// The signing key is the one thing in the configuration that is both a
// private key and a row of its own, so it is the one thing that can be
// protected from a copy of a single table. It leaves this package encrypted.
func TestTheSigningKeyIsEncryptedInTheRow(t *testing.T) {
	t.Parallel()

	original := config.Default()
	original.Server.Secret = "a-server-secret-long-enough-to-derive-from"

	first, err := config.GenerateDomainKey("teanode")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}
	second, err := config.GenerateDomainKey("teanode")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}
	original.Domains = []*config.Domain{
		{ID: "one.test", Domain: "one.test", DKIM: first,
			TLS: config.DomainCertificate{Certificate: "-----BEGIN CERTIFICATE-----\nnot really\n", PrivateKey: "the private half"}},
		{ID: "two.test", Domain: "two.test", DKIM: second},
	}

	rows, err := ToRows(original, 1)
	if err != nil {
		t.Fatalf("ToRows: %s", err)
	}
	for _, row := range rows.Domains {
		if strings.Contains(row.DKIMPrivateKey, "PRIVATE KEY") {
			t.Errorf("the row for %s holds the key as plaintext", row.Domain)
		}
		if row.CertificatePrivateKey != "" && !secretbox.Sealed(row.CertificatePrivateKey) {
			t.Errorf("the certificate key of %s is not sealed: %.20q", row.Domain, row.CertificatePrivateKey)
		}
		// The certificate itself is public — it is handed to everyone who
		// connects — so it is stored as it stands and stays readable.
		if row.Domain == "one.test" && !strings.Contains(row.Certificate, "BEGIN CERTIFICATE") {
			t.Errorf("the certificate of %s was mangled: %.30q", row.Domain, row.Certificate)
		}
		if !secretbox.Sealed(row.DKIMPrivateKey) {
			t.Errorf("the row for %s is not sealed: %.20q", row.Domain, row.DKIMPrivateKey)
		}
		// The selector is not a secret and stays readable: it is what the
		// operator is told to publish, and a support query that shows it
		// gives nothing away.
		if row.DKIMSelector != "teanode" {
			t.Errorf("the selector of %s was mangled: %q", row.Domain, row.DKIMSelector)
		}
	}

	returned, err := FromRows(rows)
	if err != nil {
		t.Fatalf("FromRows: %s", err)
	}
	if got := returned.FindDomain("one.test").DKIM.PrivateKey; got != first.PrivateKey {
		t.Error("the signing key did not survive being encrypted and read back")
	}
	if got := returned.FindDomain("two.test").DKIM.PrivateKey; got != second.PrivateKey {
		t.Error("the second signing key did not survive being encrypted and read back")
	}
	if got := returned.FindDomain("one.test").TLS.PrivateKey; got != "the private half" {
		t.Errorf("the certificate key did not survive the round trip: %q", got)
	}

	// Sealed under a key derived from the server secret, so another server's
	// copy of the row is of no use.
	rows.Settings[settingServer] = strings.Replace(rows.Settings[settingServer], original.Server.Secret, "a-different-server-secret-entirely", 1)
	if _, err := FromRows(rows); err == nil {
		t.Error("a row sealed under one secret opened under another")
	}
}

// A database written before the column was encrypted holds the key as it
// stands, and has to keep working: an upgrade that could not read the keys
// would be an upgrade that stops signing every domain's mail. The next save
// seals it.
func TestAPlaintextSigningKeyIsStillRead(t *testing.T) {
	t.Parallel()

	stored := config.Default()
	stored.Server.Secret = "a-server-secret-long-enough-to-derive-from"
	generated, err := config.GenerateDomainKey("teanode")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}
	stored.Domains = []*config.Domain{{ID: "one.test", Domain: "one.test", DKIM: generated}}

	rows, err := ToRows(stored, 1)
	if err != nil {
		t.Fatalf("ToRows: %s", err)
	}
	// What an older release left behind.
	rows.Domains[0].DKIMPrivateKey = generated.PrivateKey

	returned, err := FromRows(rows)
	if err != nil {
		t.Fatalf("FromRows on a plaintext key: %s", err)
	}
	if returned.FindDomain("one.test").DKIM.PrivateKey != generated.PrivateKey {
		t.Fatal("the plaintext key was not read back")
	}

	resaved, err := ToRows(returned, 2)
	if err != nil {
		t.Fatalf("ToRows: %s", err)
	}
	if !secretbox.Sealed(resaved.Domains[0].DKIMPrivateKey) {
		t.Error("saving again left the key in plaintext, so a column would never convert")
	}
}

// An installation whose secret has not been generated yet — the first save of
// a brand new server, before config.EnsureSecrets runs — still has to save.
// It is sealed on the save straight after.
func TestASigningKeyIsStoredWithoutASecretYet(t *testing.T) {
	t.Parallel()

	fresh := config.Default()
	fresh.Server.Secret = ""
	generated, err := config.GenerateDomainKey("teanode")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}
	fresh.Domains = []*config.Domain{{ID: "one.test", Domain: "one.test", DKIM: generated}}

	rows, err := ToRows(fresh, 1)
	if err != nil {
		t.Fatalf("ToRows without a secret: %s", err)
	}
	if rows.Domains[0].DKIMPrivateKey != generated.PrivateKey {
		t.Error("the key was altered when there was no secret to seal it with")
	}
	if _, err := FromRows(rows); err != nil {
		t.Fatalf("FromRows without a secret: %s", err)
	}
}
