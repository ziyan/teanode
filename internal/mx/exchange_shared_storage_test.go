package mx

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// This integration fixture exercises independently constructed S3 clients. No
// local spool or shared in-process storage implementation can satisfy the read.
func TestSharedSubmissionAcceptanceAcrossInstancesAndObjectStoreFailure(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var mutex sync.Mutex
	objects := make(map[string][]byte)
	isUnavailable := true
	putCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		if isUnavailable {
			// Non-retryable so the failure gate does not wait through SDK backoff.
			response.Header().Set("Content-Type", "application/xml")
			response.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(response, `<Error><Code>AccessDenied</Code><Message>Fixture unavailable</Message></Error>`)
			return
		}
		switch request.Method {
		case http.MethodPut:
			content, err := io.ReadAll(request.Body)
			if err != nil {
				test.Error(err)
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			objects[request.URL.Path] = content
			putCount++
			response.Header().Set("ETag", `"fixture"`)
		case http.MethodGet:
			content, found := objects[request.URL.Path]
			if !found {
				response.Header().Set("Content-Type", "application/xml")
				response.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(response, `<Error><Code>NoSuchKey</Code></Error>`)
				return
			}
			_, _ = response.Write(content)
		default:
			test.Errorf("unexpected object-store operation: %s %s", request.Method, request.URL.Path)
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	openSpool := func() storage.Storage {
		spool, err := storage.Open(&storage.Settings{Mode: "shared", S3: &storage.S3Settings{Bucket: "fixture", Region: "us-east-1", AccessKeyID: "fixture-access", SecretAccessKey: "fixture-secret", Endpoint: server.URL}})
		if err != nil {
			test.Fatal(err)
		}
		test.Cleanup(func() { _ = spool.Close() })
		return spool
	}
	firstInstance, mailbox := submissionExchange(test, database, openSpool())
	firstInstance.deliveryWake = make(chan struct{}, 1)
	secondDatabase, err := db.Open(&db.Settings{Host: os.Getenv("TEANODE_TEST_DATABASE_HOST"), Port: 5432, User: "teanode", Password: "teanode", DBName: dbtest.QueryString(test, database, "SELECT current_database()"), BackendID: "test2"})
	if err != nil {
		test.Fatal(err)
	}
	defer func() {
		if err := secondDatabase.Close(); err != nil {
			test.Error(err)
		}
	}()
	secondInstance := &exchange{database: secondDatabase, storage: openSpool(), config: firstInstance.config, settings: firstInstance.settings, directory: directory{source: firstInstance.directory.source}}
	makeEnvelope := func() *mailparse.Envelope {
		return &mailparse.Envelope{ID: "shared-envelope", MailboxID: mailbox.ID, Sender: "sender@example.com", Recipients: []string{"recipient@example.net"}, IP: net.IPv4(127, 0, 0, 1), ReceivedAt: time.Now(), Headers: []string{"From: sender@example.com\r\n", "To: recipient@example.net\r\n", "Message-ID: <shared@example.com>\r\n", "Content-Type: text/plain\r\n"}, Body: []byte("Shared fixture body\r\n"), Size: 128}
	}
	// Even if the outer request commits, failed mandatory persistence must
	// leave neither accepted mail, a Sent item nor a queued delivery.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		accepted, err := firstInstance.AcceptSubmission(test.Context(), transaction, makeEnvelope())
		if err == nil || accepted != nil {
			test.Fatalf("object-store failure accepted mail: %+v, %v", accepted, err)
		}
	})
	for _, table := range []string{"mail", "mailbox_item", "delivery"} {
		if count := dbtest.QueryString(test, database, "SELECT count(*)::text FROM "+table); count != "0" {
			test.Fatalf("failed acceptance retained %s rows in %s", count, table)
		}
	}
	if len(firstInstance.deliveryWake) != 0 {
		test.Fatal("failed acceptance woke delivery")
	}
	mutex.Lock()
	isUnavailable = false
	mutex.Unlock()
	var accepted *models.Mail
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		accepted, err = firstInstance.AcceptSubmission(test.Context(), transaction, makeEnvelope())
		if err != nil || accepted == nil {
			test.Fatalf("acceptance after storage recovery: %+v, %v", accepted, err)
		}
	})
	// The receiving instance has never seen the envelope or its MIME body.
	dbtest.RunTransactionOn(test, secondInstance.database, func(transaction db.Transaction) {
		stored, err := transaction.GetMail(accepted.ID, nil)
		if err != nil || stored == nil {
			test.Fatalf("second instance metadata: %+v, %v", stored, err)
		}
		headers, body, err := secondInstance.storage.Get(context.Background(), stored.ID)
		if err != nil || len(headers) == 0 || string(body) != "Shared fixture body\r\n" {
			test.Fatalf("second instance bytes: %q, %v", body, err)
		}
	})
	mutex.Lock()
	defer mutex.Unlock()
	if putCount != 1 || len(objects) != 1 || len(firstInstance.deliveryWake) != 1 {
		test.Fatalf("accepted objects=%d, writes=%d, wakes=%d", len(objects), putCount, len(firstInstance.deliveryWake))
	}
}
