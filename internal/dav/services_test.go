package dav_test

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"golang.org/x/crypto/bcrypt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSyncWithoutMailboxObeysScopesAndServiceChanges(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "device-owner")
	password := "sync-selector123456-device-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		test.Fatal(err)
	}
	var credential *models.SyncCredential
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		role, err := transaction.CreateRole(&models.Role{Name: "collections", Permissions: []models.Permission{models.PermissionContactsUse, models.PermissionCalendarUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "collection-users", RoleIDs: []string{role.ID}, UserIDs: []string{userId}}); err != nil {
			test.Fatal(err)
		}
		credential, err = transaction.CreateSyncCredential(&models.SyncCredential{UserID: userId, CredentialName: "Device", Selector: "selector123456", PasswordHash: string(hash), HasContactsAccess: true})
		if err != nil {
			test.Fatal(err)
		}
	})
	configuration := config.Default()
	configuration.Services = config.AssistantServices()
	configuration.Services.Contacts.IsEnabled = new(true)
	configuration.Services.Calendar.IsEnabled = new(true)
	configuration.Listen.HTTPS = ""
	configuration.TLS.ACME.Enabled = false
	store := config.NewMemoryStore(configuration)
	defer func() { _ = store.Close() }()
	component, err := dav.New(database, store, nil)
	if err != nil {
		test.Fatal(err)
	}
	router := mux.NewRouter().StrictSlash(true)
	if err := component.AddRoutes(router); err != nil {
		test.Fatal(err)
	}
	ask := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("PROPFIND", path, strings.NewReader("<d:propfind xmlns:d=\"DAV:\"><d:allprop/></d:propfind>"))
		request.Header.Set("Content-Type", "application/xml")
		request.Header.Set("Depth", "1")
		request.RemoteAddr = "127.0.0.1:1234"
		request.SetBasicAuth("device-owner", password)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := ask(fmt.Sprintf("/dav/%s/contacts/", userId)); response.Code != 207 {
		test.Fatalf("contacts: %d %s", response.Code, response.Body.String())
	}
	if response := ask(fmt.Sprintf("/dav/%s/calendars/", userId)); response.Code != 403 {
		test.Fatalf("unscoped calendar: %d", response.Code)
	}
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Services.Contacts.IsEnabled = new(false)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	if response := ask(fmt.Sprintf("/dav/%s/contacts/", userId)); response.Code != 403 {
		test.Fatalf("disabled contacts: %d", response.Code)
	}
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Services.Contacts.IsEnabled = new(true)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	if response := ask(fmt.Sprintf("/dav/%s/contacts/", userId)); response.Code != 207 {
		test.Fatalf("re-enabled contacts: %d", response.Code)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteSyncCredential(userId, credential.ID); err != nil {
			test.Fatal(err)
		}
	})
	if response := ask(fmt.Sprintf("/dav/%s/contacts/", userId)); response.Code != 401 {
		test.Fatalf("revoked credential: %d", response.Code)
	}
	if dbtest.QueryString(test, database, "SELECT count(*)::text FROM mailbox") != "0" {
		test.Fatal("sync provisioned a mailbox")
	}
	if dbtest.QueryString(test, database, "SELECT count(*)::text FROM calendar") != "0" {
		test.Fatal("unscoped sync provisioned a calendar")
	}
}
