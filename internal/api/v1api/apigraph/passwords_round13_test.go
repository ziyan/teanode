package apigraph

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// holdPasswordsLocks keeps the locks every change of Passwords takes in a
// transaction of its own, until the returned function is called.
func (self *passwordsWorld) holdPasswordsLocks(test *testing.T) (release func()) {
	test.Helper()
	holding, released := make(chan struct{}), make(chan struct{})
	var finished sync.WaitGroup
	finished.Add(1)
	go func() {
		defer finished.Done()
		_ = self.database.Transaction(func(tx db.Transaction) error {
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOfNumber(self.ben, 900), OwnerUserID: self.ben.ID, ItemKind: models.PasswordItemKindNote,
				EncryptedFields: sealedOf("held"), ItemKeyByAccountKey: sealedOf("held key"),
			}, nil)
			close(holding)
			<-released
			return err
		})
	}()
	<-holding
	return func() {
		close(released)
		finished.Wait()
	}
}

// A save, a delete and the rest that give up on the locks answer "Passwords is
// busy", as a re-share does, and not a server fault about a failed commit.
func TestAChangeBehindAHeldLockAnswersThatPasswordsIsBusy(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	saved := world.save(test, ada, "login")
	itemId := saved["id"].(string)
	release := world.holdPasswordsLocks(test)
	defer release()

	var waiters sync.WaitGroup
	var mutex sync.Mutex
	messages := map[string]string{}
	for name, document := range map[string]struct {
		query     string
		variables map[string]any
	}{
		"save":   {savePasswordItemQuery, map[string]any{"input": ada.itemInput("note", nil)}},
		"delete": {deletePasswordItemQuery, map[string]any{"id": itemId}},
		"purge":  {purgePasswordItemQuery, map[string]any{"id": itemId}},
		"let the agent use it": {passwordAgentUseQuery,
			map[string]any{"input": map[string]any{"itemId": itemId, "isUsableByAgent": false}}},
		"erase": {erasePasswordsQuery, nil},
	} {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			_, failures := world.callWith(test, world.ada, document.query, document.variables)
			mutex.Lock()
			defer mutex.Unlock()
			messages[name] = strings.Join(failures, "; ")
		}()
	}
	waiters.Wait()
	for name, message := range messages {
		if !strings.Contains(message, "Passwords is busy with other changes. Try again in a moment.") {
			test.Errorf("%s behind a held lock was answered %q", name, message)
		}
		if strings.Contains(message, "rollback") {
			test.Errorf("%s behind a held lock failed its commit: %q", name, message)
		}
	}
}

// A document of several changes behind a held lock waits for the locks once.
func TestADocumentBehindAHeldLockWaitsOnce(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	saved := world.save(test, ada, "login")
	release := world.holdPasswordsLocks(test)
	defer release()

	var selections []string
	variables := map[string]any{}
	declarations := []string{}
	for index := 0; index < 3; index++ {
		selections = append(selections, fmt.Sprintf(`rekey%d: RekeyPasswordItem(input: $input%d) { id }`, index, index))
		declarations = append(declarations, fmt.Sprintf("$input%d: PasswordItemRekeyInput!", index))
		variables[fmt.Sprintf("input%d", index)] = map[string]any{
			"id": saved["id"], "baseRevision": saved["revision"], "encryptedFields": sealedOf("again"),
			"itemKey": sealedOf("again key"), "shareKeys": []any{},
		}
	}
	document := `mutation (` + strings.Join(declarations, ", ") + `) { ` + strings.Join(selections, " ") + ` }`
	started := time.Now()
	_, failures := world.callWith(test, world.ada, document, variables)
	took := time.Since(started)
	if took < 4*time.Second || took > 8*time.Second {
		test.Errorf("three re-keys behind a held lock took %s, want about five seconds", took)
	}
	if len(failures) != 3 {
		test.Fatalf("three re-keys behind a held lock were answered %q", failures)
	}
	for _, failure := range failures {
		if !strings.Contains(failure, "Passwords is busy") {
			test.Errorf("a re-key was answered %q", failure)
		}
	}
}
