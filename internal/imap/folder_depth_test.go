package imap

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A transaction with only the two calls CREATE makes. The embedded interface
// is nil, so anything else the command touched would panic and fail the test.
type folderCountingTransaction struct {
	db.Transaction
	folders   []*models.MailboxFolder
	listCalls int
}

func (self *folderCountingTransaction) ListFolders(mailboxId string, options *db.FolderOptions) ([]*models.MailboxFolder, error) {
	self.listCalls++
	return append([]*models.MailboxFolder(nil), self.folders...), nil
}

func (self *folderCountingTransaction) CreateFolder(folder *models.MailboxFolder) (*models.MailboxFolder, error) {
	created := *folder
	created.ID = "folder-" + strconv.Itoa(len(self.folders)+1)
	self.folders = append(self.folders, &created)
	return &created, nil
}

type folderCountingDatabase struct {
	db.Database
	transaction *folderCountingTransaction
}

func (self *folderCountingDatabase) Transaction(function func(db.Transaction) error) error {
	return function(self.transaction)
}

// CREATE lists the folders once for each part of the name, so a name of
// thousands of parts, which fits in a command, made the server read the whole
// mailbox thousands of times. A name is limited to the depth the tree is read
// to.
func TestCreateRefusesAPathPastTheDepthTheTreeIsReadTo(t *testing.T) {
	t.Parallel()

	newSession := func() (*session, *folderCountingTransaction) {
		transaction := &folderCountingTransaction{}
		return &session{
			settings: &Settings{Database: &folderCountingDatabase{transaction: transaction}},
			mailbox:  &models.Mailbox{ID: "mailbox-1"},
			canWrite: true, checkedAt: time.Now(),
		}, transaction
	}
	deep := func(parts int) string { return strings.TrimSuffix(strings.Repeat("a/", parts), "/") }

	allowed, transaction := newSession()
	if err := allowed.Create(deep(maximumFolderDepth), nil); err != nil {
		t.Fatalf("a name at the limit should be created: %s", err)
	}
	if len(transaction.folders) != maximumFolderDepth {
		t.Errorf("created %d folders, want %d", len(transaction.folders), maximumFolderDepth)
	}

	refused, transaction := newSession()
	err := refused.Create(deep(maximumFolderDepth+1), nil)
	var imapError *goimap.Error
	if !errors.As(err, &imapError) || imapError.Code != goimap.ResponseCodeCannot {
		t.Fatalf("a name past the limit should be refused with CANNOT, got %v", err)
	}
	if transaction.listCalls != 0 || len(transaction.folders) != 0 {
		t.Errorf("a refused name should cost nothing: %d listings, %d folders", transaction.listCalls, len(transaction.folders))
	}

	huge, transaction := newSession()
	if err := huge.Create(deep(20000), nil); err == nil || transaction.listCalls != 0 {
		t.Errorf("a name of twenty thousand parts: err %v, %d listings", err, transaction.listCalls)
	}
}
