package config

import (
	"reflect"
	"strings"
	"testing"
)

// notSecret names the fields whose names read like a secret but are not, so
// that the check below can be strict about everything else.
var notSecret = map[string]bool{
	// Paths to secrets, not the secrets themselves. Showing where a key is
	// kept is how an operator finds it.
	"TLS.PrivateKeyFile":               true,
	"TLS.CertificateFile":              true,
	"TLS.ACME.PrivateKeyFile":          true,
	"TLS.ACME.AccountKeyFile":          true,
	"TLS.ACME.CertificateFile":         true,
	"TLS.ACME.Route53.CredentialsFile": true,
	"Storage.S3.CredentialsFile":       true,
	"DKIM.PrivateKeyFile":              true,
	"Session.SessionKeyFile":           true,
	// An AWS access key identifier is not a credential on its own, and is
	// what an operator needs to see to know which account is in use.
	"TLS.ACME.Route53.AccessKeyID": true,
	"Storage.S3.AccessKeyID":       true,
}

// secretish matches a field name that probably holds a secret. It is
// deliberately over-eager: a false positive costs one line in notSecret, while
// a false negative is a leaked key.
func secretish(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range []string{"secret", "password", "privatekey", "accountkey", "hash", "key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// TestEverySecretIsTagged fails when a field that looks like a secret is
// neither tagged nor listed as an exception, so that adding one to the
// configuration cannot quietly skip redaction.
func TestEverySecretIsTagged(t *testing.T) {
	var walk func(reflect.Type, string)
	walk = func(structType reflect.Type, prefix string) {
		for index := 0; index < structType.NumField(); index++ {
			field := structType.Field(index)
			if !field.IsExported() {
				continue
			}
			path := strings.TrimPrefix(prefix+"."+field.Name, ".")

			fieldType := field.Type
			for fieldType.Kind() == reflect.Pointer || fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Map {
				t.Errorf("%s is a map; Redact cannot replace values inside one", path)
				continue
			}
			if fieldType.Kind() == reflect.Struct {
				walk(fieldType, path)
				continue
			}

			tagged := IsSecretField(field)
			if tagged && fieldType.Kind() != reflect.String {
				t.Errorf("%s is tagged secret but is %s; Redact only replaces strings", path, fieldType.Kind())
			}
			if secretish(field.Name) && !tagged && !notSecret[path] {
				t.Errorf("%s looks like a secret but is not tagged `secret:\"true\"`; tag it, or add it to notSecret with a reason", path)
			}
			if !secretish(field.Name) && tagged {
				continue
			}
		}
	}
	walk(reflect.TypeOf(Configuration{}), "")
}

func TestRedactReplacesSecretsAndLeavesTheOriginal(t *testing.T) {
	configuration := Default()
	configuration.Server.Secret = "the-server-secret"
	configuration.Session.Key = "the-session-key"
	configuration.Database.Password = "the-database-password"
	configuration.TLS.ACME.Route53.AccessKeyID = "AKIAEXAMPLE"
	configuration.TLS.ACME.Route53.SecretAccessKey = "the-aws-secret"
	configuration.Domains = []*Domain{{
		ID:     "domain",
		Domain: "example.com",
		DKIM:   DomainKey{Selector: "teanode1", PrivateKey: "the-signing-key"},
		Credentials: []*Credential{
			{ID: "credential", Key: "the-credential-key"},
		},
		Aliases: []*Alias{
			{ID: "alias", Kind: AliasKindMailServer, MailServer: &MailServer{Host: "smtp.example.net", Password: "the-relay-password"}},
		},
	}}
	configuration.Users = []*User{{
		Username:     "ziyan",
		PasswordHash: "the-hash",
	}}

	redacted, err := configuration.Redact()
	if err != nil {
		t.Fatalf("Redact: %s", err)
	}

	for name, value := range map[string]string{
		"server.secret":           redacted.Server.Secret,
		"session.key":             redacted.Session.Key,
		"database.password":       redacted.Database.Password,
		"route53.secretAccessKey": redacted.TLS.ACME.Route53.SecretAccessKey,
		"domain dkim.privateKey":  redacted.Domains[0].DKIM.PrivateKey,
		"credential.key":          redacted.Domains[0].Credentials[0].Key,
		"mailServer.password":     redacted.Domains[0].Aliases[0].MailServer.Password,
		"user.passwordHash":       redacted.Users[0].PasswordHash,
	} {
		if value != Redacted {
			t.Errorf("%s was not redacted: %q", name, value)
		}
	}

	// What is not a secret has to survive, or the redacted view is useless
	// for telling whether something is configured correctly.
	if redacted.TLS.ACME.Route53.AccessKeyID != "AKIAEXAMPLE" {
		t.Errorf("the access key identifier should not be redacted, got %q", redacted.TLS.ACME.Route53.AccessKeyID)
	}
	if redacted.Domains[0].DKIM.Selector != "teanode1" {
		t.Errorf("the selector should not be redacted, got %q", redacted.Domains[0].DKIM.Selector)
	}
	if redacted.Domains[0].Domain != "example.com" {
		t.Errorf("the domain name should not be redacted, got %q", redacted.Domains[0].Domain)
	}

	if configuration.Server.Secret != "the-server-secret" {
		t.Errorf("Redact changed the original: %q", configuration.Server.Secret)
	}
	if configuration.Domains[0].Credentials[0].Key != "the-credential-key" {
		t.Errorf("Redact changed the original credential key")
	}
}
