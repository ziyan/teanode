package apigraph

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// While a listing holds the revision lock, deleting a circle or an account that
// has something in Passwords waits, gives up and says Passwords is busy, in the
// sentence the other Passwords changes use; nothing changes, and the photo
// objects of the account are not deleted by a deletion that did not happen.
// Once the listing is done the same deletion goes through and removes them.
func TestDeletingACircleOrAnAccountWhilePasswordsIsBusySaysSo(test *testing.T) {
	world := newPasswordsWorld(test)
	inTransaction := func(steps func(tx db.Transaction)) {
		test.Helper()
		if err := world.database.Transaction(func(tx db.Transaction) error {
			steps(tx)
			return nil
		}); err != nil {
			test.Fatal(err)
		}
	}
	var previewId string
	inTransaction(func(tx db.Transaction) {
		if _, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: "00000000-0000-4000-8000-000000000001", OwnerUserID: world.ben.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: sealedOf("fields"), ItemKeyByAccountKey: sealedOf("key"),
		}, nil); err != nil {
			test.Fatal(err)
		}
		made, _, err := tx.CreatePhoto(&models.Photo{UserID: world.ben.ID, ClientPhotoID: "first", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine})
		if err != nil {
			test.Fatal(err)
		}
		previewId = models.PhotoPreviewFileID(made.ID, models.PhotoPreviewSmall)
	})
	if err := world.files.PutFile(test.Context(), previewId, []byte("preview")); err != nil {
		test.Fatal(err)
	}
	holding, release := make(chan struct{}), make(chan struct{})
	var holder sync.WaitGroup
	holder.Add(1)
	go func() {
		defer holder.Done()
		_ = world.database.Transaction(func(tx db.Transaction) error {
			_, err := tx.ListPasswordItems(world.cora.ID, 0)
			close(holding)
			<-release
			return err
		})
	}()
	<-holding

	deleteCircle := fmt.Sprintf(`mutation { DeleteCircle(circleId: %q) }`, world.home)
	deleteUser := fmt.Sprintf(`mutation { DeleteUser(userId: %q) }`, world.ben.ID)
	if refusal := world.refused(test, world.ada, deleteCircle); !strings.Contains(refusal, passwordsBusySentence) {
		test.Errorf("DeleteCircle said %q", refusal)
	}
	if refusal := world.refused(test, world.manager, deleteUser); !strings.Contains(refusal, passwordsBusySentence) {
		test.Errorf("DeleteUser said %q", refusal)
	}
	close(release)
	holder.Wait()
	if _, err := world.files.GetFile(test.Context(), previewId); err != nil {
		test.Errorf("the photo preview was deleted by a refused deletion of the account: %v", err)
	}
	if found, err := world.database.GetUser(world.ben.ID); err != nil || found == nil {
		test.Fatalf("the account is gone after the refusal: %v", err)
	}

	world.call(test, world.manager, deleteUser)
	if _, err := world.files.GetFile(test.Context(), previewId); err == nil {
		test.Error("the photo preview is still in the store after the account was deleted")
	}
}
