package apigraph

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// takeRolePermissions leaves a role granting only what is given, as an
// operator editing the role would.
func takeRolePermissions(test *testing.T, database db.Database, roleName string, remaining ...models.Permission) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		role, err := tx.GetRoleByName(roleName)
		if err != nil || role == nil {
			test.Fatalf("GetRoleByName: %v %v", role, err)
		}
		if _, err := tx.UpdateRole(role.ID, func(current *models.Role) error {
			current.Permissions = remaining
			return nil
		}); err != nil {
			test.Fatalf("UpdateRole: %s", err)
		}
	})
}

// endAfterLosingThePermission has ada lose every permission but circle:use
// after she shared, and checks that the Sharing page still lists the share
// (as the only one she made) and that the stop mutation built by stopQuery
// from its identifier ends it.
func (self *sharingWorld) endAfterLosingThePermission(test *testing.T, shareKind string, stopQuery func(shareId string) string) {
	test.Helper()
	takeRolePermissions(test, self.database, "Sharing role of "+self.ada.Username, models.PermissionCircleUse)
	made, _ := self.sharing(test, self.ada)
	if fmt.Sprint(kindsOf(made)) != fmt.Sprintf("[%s]", shareKind) {
		test.Fatalf("ada shares %v", kindsOf(made))
	}
	self.call(test, self.ada, stopQuery(made[0]["shareId"].(string)))
	if made, _ := self.sharing(test, self.ada); len(made) != 0 {
		test.Errorf("ada still shares %v", kindsOf(made))
	}
}

func TestAPersonWhoLostLocationUseEndsTheirLocationShare(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	world.endAfterLosingThePermission(test, "location", func(shareId string) string {
		return fmt.Sprintf(`mutation { UnshareLocation(shareId: %q) }`, shareId)
	})
}

// Taking a share back is open to somebody without health:use, and sharing
// more is not.
func TestAPersonWhoLostHealthUseEndsTheirHealthShare(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: ["activity", "sleep"]) { id } }`, world.ben.ID))
	takeRolePermissions(test, world.database, "Sharing role of "+world.ada.Username, models.PermissionCircleUse)
	world.refused(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: ["activity", "sleep", "heart"]) { id } }`, world.ben.ID))
	world.refused(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: ["activity"]) { id } }`, world.cora.ID))
	made, _ := world.sharing(test, world.ada)
	if fmt.Sprint(kindsOf(made)) != "[health]" {
		test.Fatalf("ada shares %v", kindsOf(made))
	}
	world.call(test, world.ada, fmt.Sprintf(`mutation { SetHealthShare(granteeUserId: %q, healthCategories: []) { id } }`, world.ben.ID))
	if made, _ := world.sharing(test, world.ada); len(made) != 0 {
		test.Errorf("ada still shares %v", kindsOf(made))
	}
}

func TestAPersonWhoLostPhotosUseEndsTheirAlbumShare(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { SharePhotoAlbum(photoAlbumId: %q, granteeUserId: %q, shareLevel: "view") { id } }`,
		world.albumID, world.ben.ID))
	world.endAfterLosingThePermission(test, "photo_album", func(shareId string) string {
		return fmt.Sprintf(`mutation { RemovePhotoAlbumShare(photoAlbumShareId: %q) }`, shareId)
	})
}

func TestAPersonWhoLostAgentUseEndsTheirPageShare(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/kim", granteeUserId: %q) { id } }`, world.ben.ID))
	world.endAfterLosingThePermission(test, "memory_page", func(shareId string) string {
		return fmt.Sprintf(`mutation { UnshareAgentPage(shareId: %q) }`, shareId)
	})
}

func TestAPersonWhoLostAgentUseEndsTheirSkillShare(test *testing.T) {
	world := newSharingWorld(test)
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if _, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{SkillName: "groceries", ToolNames: []string{"order"}, GranteeUserID: world.ben.ID}); err != nil {
			test.Fatalf("ShareSkill: %s", err)
		}
	})
	world.endAfterLosingThePermission(test, "skill", func(shareId string) string {
		return fmt.Sprintf(`mutation { UnshareSkill(shareId: %q) }`, shareId)
	})
}

// A share is still only its owner's to end, whatever the owner may use.
func TestEndingAShareStillNeedsToBeItsOwner(test *testing.T) {
	world := newSharingWorld(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/kim", granteeUserId: %q) { id } }`, world.ben.ID))
	made, _ := world.sharing(test, world.ada)
	takeRolePermissions(test, world.database, "Sharing role of "+world.cora.Username, models.PermissionCircleUse)
	for _, row := range made {
		stop := map[string]string{
			"location":    `mutation { UnshareLocation(shareId: %q) }`,
			"memory_page": `mutation { UnshareAgentPage(shareId: %q) }`,
		}[row["shareKind"].(string)]
		world.refused(test, world.cora, fmt.Sprintf(stop, row["shareId"]))
	}
	if made, _ := world.sharing(test, world.ada); len(made) != 2 {
		test.Errorf("ada shares %v", kindsOf(made))
	}
}

func TestARecordingOwnerWhoLostAgentUseEndsTheShare(t *testing.T) {
	world := newRecordingShareWorld(t)
	shared := world.share(t, world.alex, fmt.Sprintf(`granteeUserId: %q`, world.sam.ID), "summary")
	takeRolePermissions(t, world.database, world.alex.Username+" role", models.PermissionCircleUse)
	world.refused(t, world.sam, fmt.Sprintf(`query { ReadSharedRecording(recordingId: %q, passageOffset: 0, pageSize: 10) { recordingTitle } }`, world.recordingId))
	world.refused(t, world.sam, fmt.Sprintf(`mutation { UnshareRecording(recordingShareId: %q) }`, shared["id"]))
	world.call(t, world.alex, fmt.Sprintf(`mutation { UnshareRecording(recordingShareId: %q) }`, shared["id"]))
	world.refused(t, world.alex, fmt.Sprintf(`mutation { UnshareRecording(recordingShareId: %q) }`, shared["id"]))
}

func TestAFinanceOwnerWhoLostAgentUseEndsTheShare(t *testing.T) {
	world := newFinanceShareWorld(t)
	share := world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "spending")
	takeRolePermissions(t, world.database, world.ada.Username+" agent role", models.PermissionCircleUse)
	if _, failures := world.callWithCredential(t, world.ben, models.CredentialSession, unshareFinanceQuery, map[string]any{"shareId": share["id"]}); len(failures) == 0 {
		t.Error("a reader ended the owner's share")
	}
	if data := world.run(t, world.ada, unshareFinanceQuery, map[string]any{"shareId": share["id"]}); data["UnshareFinanceAccount"] != true {
		t.Fatalf("the owner ends the share: %v", data)
	}
}
