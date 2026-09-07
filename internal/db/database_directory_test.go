package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A domain, its aliases and its credentials are rows of their own: written
// one at a time, read back whole, and the identifiers stored mail points at
// never change under an edit.
func TestDomainRowsRoundTrip(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	if err := database.SetSecret([]byte("a-secret-long-enough-to-seal-with")); err != nil {
		t.Fatalf("SetSecret: %s", err)
	}

	key, err := models.GenerateDomainKey("teanode1")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}

	var domainId, aliasId, credentialId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		domain, err := tx.CreateDomain(&models.Domain{ID: "second.example", Domain: "second.example", Subdomain: "mail", DKIM: key})
		if err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		domainId = domain.ID
		if _, err := tx.CreateDomain(&models.Domain{Domain: "Second.Example"}); !errors.Is(err, db.ErrAlreadyExists) {
			t.Errorf("a duplicate domain name was accepted: %v", err)
		}
		alias, err := tx.CreateAlias(&models.Alias{DomainID: domainId, Pattern: "^support$", Kind: models.AliasKindEmail, Email: "support@example.net"})
		if err != nil {
			t.Fatalf("CreateAlias: %s", err)
		}
		aliasId = alias.ID
		if _, err := tx.CreateAlias(&models.Alias{DomainID: domainId, Pattern: "", Kind: models.AliasKindNull}); err != nil {
			t.Fatalf("CreateAlias (catch-all): %s", err)
		}
		credential, err := tx.CreateCredential(&models.Credential{DomainID: domainId, Key: "0123456789abcdef", Comment: "laptop"})
		if err != nil {
			t.Fatalf("CreateCredential: %s", err)
		}
		credentialId = credential.ID
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		domain, err := tx.GetDomainByName("SECOND.EXAMPLE")
		if err != nil || domain == nil {
			t.Fatalf("GetDomainByName: %v, %v", domain, err)
		}
		if domain.ID != domainId || domain.DKIM.PrivateKey != key.PrivateKey || domain.DKIM.Selector != "teanode1" {
			t.Errorf("the domain did not round trip: %+v", domain)
		}
		if len(domain.Aliases) != 2 || domain.Aliases[0].ID != aliasId || domain.Aliases[1].Position != 1 {
			t.Errorf("the aliases did not round trip in order: %+v", domain.Aliases)
		}
		if len(domain.Credentials) != 1 || domain.Credentials[0].ID != credentialId || domain.Credentials[0].Key != "0123456789abcdef" {
			t.Errorf("the credential did not round trip: %+v", domain.Credentials)
		}

		// Editing keeps the identifier, because deliveries already stored
		// point at it.
		updated, err := tx.UpdateAlias(aliasId, func(alias *models.Alias) error {
			alias.Pattern = "^help$"
			return nil
		})
		if err != nil {
			t.Fatalf("UpdateAlias: %s", err)
		}
		if updated.ID != aliasId || updated.Pattern != "^help$" || updated.DomainID != domainId {
			t.Errorf("the edit changed more than the pattern: %+v", updated)
		}
		// A rejected change leaves the row alone.
		if _, err := tx.UpdateAlias(aliasId, func(alias *models.Alias) error {
			alias.Pattern = "^["
			return nil
		}); err == nil {
			t.Error("an invalid pattern was accepted")
		}
		again, err := tx.GetAlias(aliasId)
		if err != nil || again == nil || again.Pattern != "^help$" {
			t.Errorf("a rejected change modified the row: %+v, %v", again, err)
		}
	})

	// Deleting the domain takes its aliases and credentials with it.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.DeleteDomain(domainId); err != nil {
			t.Fatalf("DeleteDomain: %s", err)
		}
		if alias, err := tx.GetAlias(aliasId); err != nil || alias != nil {
			t.Errorf("the alias survived its domain: %+v, %v", alias, err)
		}
		if credential, err := tx.GetCredential(credentialId); err != nil || credential != nil {
			t.Errorf("the credential survived its domain: %+v, %v", credential, err)
		}
	})
}

// The signing key is sealed at rest with the server secret, and opens with
// it: the column must not hold the key in the clear.
func TestTheSigningKeyIsSealedAtRest(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	if err := database.SetSecret([]byte("a-secret-long-enough-to-seal-with")); err != nil {
		t.Fatalf("SetSecret: %s", err)
	}
	key, err := models.GenerateDomainKey("teanode1")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.CreateDomain(&models.Domain{Domain: "sealed.example", DKIM: key}); err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
	})
	stored := dbtest.QueryString(t, database, `SELECT "dkim_private_key" FROM "domain" WHERE "domain" = 'sealed.example'`)
	if strings.Contains(stored, "PRIVATE KEY") {
		t.Error("the signing key is stored in the clear")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		domain, err := tx.GetDomainByName("sealed.example")
		if err != nil || domain == nil || domain.DKIM.PrivateKey != key.PrivateKey {
			t.Errorf("the sealed key did not open: %v", err)
		}
	})
}

// Who may do what: a user in a group holds the group's roles over the group's
// domains, additively across groups, and nothing attaches to a user directly.
func TestEffectivePermissionsComeFromGroups(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var userId, operatorId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, name := range []string{"one.test", "two.test"} {
			if _, err := tx.CreateDomain(&models.Domain{ID: name, Domain: name}); err != nil {
				t.Fatalf("CreateDomain: %s", err)
			}
		}
		user, err := tx.CreateUser(&models.User{Username: "member"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		userId = user.ID
		if _, err := tx.CreateRole(&models.Role{Name: "Bad", Permissions: []models.Permission{"carrier:pigeon"}}); err == nil {
			t.Error("a permission the code does not know was granted")
		}
		operator, err := tx.CreateRole(&models.Role{Name: "Operator", Permissions: []models.Permission{models.PermissionMailAudit, models.PermissionDomainManage, models.PermissionQueueManage}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		operatorId = operator.ID
		auditor, err := tx.CreateRole(&models.Role{Name: "Auditor", Permissions: []models.Permission{models.PermissionMailAuditAll}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "Team One", UserIDs: []string{userId}, RoleIDs: []string{operator.ID}, DomainIDs: []string{"one.test"}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "Auditors", UserIDs: []string{userId}, RoleIDs: []string{auditor.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		// A group the user is not in grants nothing, however much it holds.
		if _, err := tx.CreateGroup(&models.Group{Name: "Others", RoleIDs: []string{operator.ID}, DomainIDs: []string{"two.test"}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
	})

	// A row naming a permission the code has forgotten — one a newer release
	// wrote, or an older one — is ignored rather than fatal.
	dbtest.Exec(t, database, `INSERT INTO "role_permission" ("role_id", "permission_key") VALUES ('`+operatorId+`', 'carrier:pigeon')`)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		reread, err := tx.GetRole(operatorId)
		if err != nil || reread == nil || len(reread.Permissions) != 3 {
			t.Errorf("an unknown permission row was not ignored: %+v, %v", reread, err)
		}
		permissions, err := tx.EffectivePermissions(userId)
		if err != nil {
			t.Fatalf("EffectivePermissions: %s", err)
		}
		if !permissions.Has(models.PermissionQueueManage) {
			t.Error("a server permission held through a group is missing")
		}
		if !permissions.HasOverDomain(models.PermissionDomainManage, "one.test") || permissions.HasOverDomain(models.PermissionDomainManage, "two.test") {
			t.Errorf("a domain permission reaches the wrong domains: %+v", permissions)
		}
		if !permissions.HasOverDomain(models.PermissionMailAudit, "two.test") {
			t.Error("the all-domains permission from the second group does not reach a domain the first group lacks")
		}
		if permissions.Has(models.PermissionRoleManage) {
			t.Error("a permission nobody granted is held")
		}
		user, err := tx.GetUser(userId)
		if err != nil || user == nil || len(user.GroupIDs) != 2 {
			t.Errorf("the user's groups did not load: %+v, %v", user, err)
		}
	})

	// Leaving the group takes the reach with it.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		group, err := tx.GetGroupByName("team one")
		if err != nil || group == nil {
			t.Fatalf("GetGroupByName: %v, %v", group, err)
		}
		if _, err := tx.UpdateGroup(group.ID, func(group *models.Group) error {
			group.UserIDs = nil
			return nil
		}); err != nil {
			t.Fatalf("UpdateGroup: %s", err)
		}
		permissions, err := tx.EffectivePermissions(userId)
		if err != nil {
			t.Fatalf("EffectivePermissions: %s", err)
		}
		if permissions.Has(models.PermissionQueueManage) || permissions.HasOverDomain(models.PermissionDomainManage, "one.test") {
			t.Error("a permission survived leaving the group that granted it")
		}
	})
}

// Every administrative change writes one audit row in the same transaction,
// naming the actor the transaction was opened for, with secrets removed.
func TestAdministrativeChangesAreAudited(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	ctx := db.ContextWithAuditPrincipal(context.Background(), db.AuditPrincipal{ActorKind: models.AuditActorUser, UserID: "actor", SourceIP: "203.0.113.9"})
	var userId string
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		user, err := tx.CreateUser(&models.User{Username: "audited", PasswordHash: "$2a$12$notarealhashbutthecolumnisnotnull....................."})
		if err != nil {
			return err
		}
		userId = user.ID
		// A change that changes nothing writes nothing.
		if _, err := tx.UpdateUser(userId, func(*models.User) error { return nil }); err != nil {
			return err
		}
		_, err = tx.UpdateUser(userId, func(user *models.User) error {
			user.Name = "Somebody"
			return nil
		})
		return err
	}); err != nil {
		t.Fatalf("transaction: %s", err)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: "user", ResourceID: userId})
		if err != nil {
			t.Fatalf("ListAuditEvents: %s", err)
		}
		if len(events) != 2 {
			t.Fatalf("got %d audit rows, want a create and an update: %+v", len(events), events)
		}
		update, create := events[0], events[1]
		if create.Action != models.AuditActionCreate || create.ActorUserID != "actor" || create.SourceIP != "203.0.113.9" || create.Before != nil {
			t.Errorf("the create row is wrong: %+v", create)
		}
		if update.Action != models.AuditActionUpdate || !strings.Contains(string(update.After), "Somebody") {
			t.Errorf("the update row is wrong: %+v", update)
		}
		for _, event := range events {
			if strings.Contains(string(event.After), "notarealhash") || strings.Contains(string(event.Before), "notarealhash") {
				t.Error("the password hash reached the audit log")
			}
		}
		count, err := tx.CountAuditEvents(&db.AuditOptions{ActorUserID: "actor"})
		if err != nil || count != 2 {
			t.Errorf("CountAuditEvents = %d, %v", count, err)
		}
	})
}
