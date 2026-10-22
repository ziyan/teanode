package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The operations and fields of recording sharing are in the schema the server
// builds.
func TestRecordingShareDocumentsMatchTheBuiltSchema(test *testing.T) {
	schema := buildSchemaForValidation(test)
	for _, source := range []string{
		`mutation { ShareRecording(recordingId: "a", granteeUserId: "b", recordingShareLevel: "summary") { id recordingId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName recordingShareLevel createdAt } }`,
		`mutation { ShareRecording(recordingId: "a", granteeCircleId: "c", recordingShareLevel: "transcript") { id } }`,
		`mutation { UnshareRecording(recordingShareId: "a") }`,
		`query { ListRecordingShares(recordingId: "a") { id granteeUsername granteeCircleName recordingShareLevel } }`,
		`query { ListSharedRecordings(pageSize: 10) { recordingId ownerUserId ownerUsername ownerName recordingTitle createdAt recordedAt timelineDurationMS recordingSummary recordingShareLevel } }`,
		`query { ListSharedRecordings(ownerUserId: "a", beforeRecordingId: "b", pageSize: 10) { recordingId } }`,
		`query { ReadSharedRecording(recordingId: "a", passageOffset: 0, pageSize: 50) { recordingId speakerNames passageCount hasMorePassages passages { segmentStartMS segmentEndMS speakerName transcriptText isFiller } } }`,
	} {
		document, err := gqlparser.Parse(gqlparser.ParseParams{Source: source})
		if err != nil {
			test.Fatal(err)
		}
		if validated := graphql.ValidateDocument(&schema, document, nil); !validated.IsValid {
			test.Errorf("%s does not match the built schema: %v", source, validated.Errors)
		}
	}
}

// recordingShareWorld is a server with four people: alex owns a recording and
// is in a circle with sam, kim is in a circle of her own, and lee is in alex's
// circle but may not use the agent, which the recording operations require.
type recordingShareWorld struct {
	*avatarWorld
	alex, sam, kim, lee *models.User
	family              string
	recordingId         string
}

func newRecordingShareWorld(t *testing.T) *recordingShareWorld {
	t.Helper()
	world := &recordingShareWorld{avatarWorld: newAvatarWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		create := func(username string, permissions ...models.Permission) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username, Name: strings.ToUpper(username[:1]) + username[1:]})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: permissions})
			if err != nil {
				t.Fatalf("CreateRole: %s", err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
			return user
		}
		world.alex = create("recshare-alex", models.PermissionAgentUse, models.PermissionCircleUse)
		world.sam = create("recshare-sam", models.PermissionAgentUse, models.PermissionCircleUse)
		world.kim = create("recshare-kim", models.PermissionAgentUse, models.PermissionCircleUse)
		world.lee = create("recshare-lee", models.PermissionCircleUse)

		family, err := tx.CreateCircle(&models.Circle{Name: "Family", CircleKind: models.CircleKindFamily}, world.alex.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		world.family = family.ID
		for _, member := range []*models.User{world.sam, world.lee} {
			if _, err := tx.AddCircleMember(family.ID, member.ID, models.MemberRoleMember, world.alex.ID); err != nil {
				t.Fatalf("AddCircleMember: %s", err)
			}
		}
		if _, err := tx.CreateCircle(&models.Circle{Name: "Kim's", CircleKind: models.CircleKindFamily}, world.kim.ID); err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}

		recording, err := tx.CreateRecording(&models.Recording{UserID: world.alex.ID, ClientRecordingID: "share-source", RecordingTitle: "Standup", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			t.Fatalf("CreateRecording: %s", err)
		}
		world.recordingId = recording.ID
		if err := tx.StoreGeneratedRecordingSummary(world.alex.ID, recording.ID, "Standup", "Two items were agreed."); err != nil {
			t.Fatalf("StoreGeneratedRecordingSummary: %s", err)
		}
	})
	return world
}

func (self *recordingShareWorld) call(t *testing.T, user *models.User, query string) map[string]any {
	t.Helper()
	data, failures := self.ask(t, user, query)
	if len(failures) > 0 {
		t.Fatalf("%s: %v\n%s", user.Username, failures, query)
	}
	return data
}

// refused returns what a refusal said, and fails when there was none.
func (self *recordingShareWorld) refused(t *testing.T, user *models.User, query string) string {
	t.Helper()
	data, failures := self.ask(t, user, query)
	if len(failures) == 0 {
		t.Fatalf("%s was not refused: %v\n%s", user.Username, data, query)
	}
	return strings.Join(failures, "; ")
}

func (self *recordingShareWorld) share(t *testing.T, owner *models.User, grantee, recordingShareLevel string) map[string]any {
	t.Helper()
	data := self.call(t, owner, fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, %s, recordingShareLevel: %q) { id recordingShareLevel granteeUsername granteeCircleName } }`,
		self.recordingId, grantee, recordingShareLevel))
	return data["ShareRecording"].(map[string]any)
}

func (self *recordingShareWorld) read(t *testing.T, reader *models.User) map[string]any {
	t.Helper()
	data := self.call(t, reader, fmt.Sprintf(`query { ReadSharedRecording(recordingId: %q, passageOffset: 0, pageSize: 10) { recordingTitle recordingSummary recordingShareLevel ownerUsername passageCount passages { transcriptText } } }`, self.recordingId))
	return data["ReadSharedRecording"].(map[string]any)
}

// The owner shares at a level, the reader reads that level, the owner sees and
// takes back the share, and people who are not reached are told nothing.
func TestARecordingIsSharedAtALevelAndTakenBack(t *testing.T) {
	world := newRecordingShareWorld(t)
	readQuery := fmt.Sprintf(`query { ReadSharedRecording(recordingId: %q, passageOffset: 0, pageSize: 10) { recordingTitle } }`, world.recordingId)
	listQuery := `query { ListSharedRecordings(pageSize: 10) { recordingId recordingTitle recordingSummary recordingShareLevel ownerUsername } }`

	// Nothing is shared yet: sam does not know the recording is there.
	world.refused(t, world.sam, readQuery)
	if listed := world.call(t, world.sam, listQuery)["ListSharedRecordings"].([]any); len(listed) != 0 {
		t.Fatalf("before any share: %v", listed)
	}

	shared := world.share(t, world.alex, fmt.Sprintf(`granteeUserId: %q`, world.sam.ID), "summary")
	if shared["recordingShareLevel"] != "summary" || shared["granteeUsername"] != world.sam.Username {
		t.Fatalf("the share: %v", shared)
	}
	read := world.read(t, world.sam)
	if read["recordingTitle"] != "Standup" || read["recordingSummary"] != "Two items were agreed." || read["recordingShareLevel"] != "summary" ||
		read["ownerUsername"] != world.alex.Username || read["passageCount"] != float64(0) {
		t.Fatalf("sam reads the summary: %v", read)
	}
	listed := world.call(t, world.sam, listQuery)["ListSharedRecordings"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["recordingId"] != world.recordingId || listed[0].(map[string]any)["ownerUsername"] != world.alex.Username {
		t.Fatalf("sam lists it: %v", listed)
	}

	// Sharing again changes the level of the same share.
	again := world.share(t, world.alex, fmt.Sprintf(`granteeUserId: %q`, world.sam.ID), "transcript")
	if again["id"] != shared["id"] || again["recordingShareLevel"] != "transcript" {
		t.Fatalf("sharing again: %v", again)
	}
	if read := world.read(t, world.sam); read["recordingShareLevel"] != "transcript" {
		t.Fatalf("sam reads the transcript level: %v", read)
	}

	// The owner sees who it is shared with, and a reader cannot.
	owned := world.call(t, world.alex, fmt.Sprintf(`query { ListRecordingShares(recordingId: %q) { id granteeUsername recordingShareLevel } }`, world.recordingId))["ListRecordingShares"].([]any)
	if len(owned) != 1 || owned[0].(map[string]any)["granteeUsername"] != world.sam.Username {
		t.Fatalf("the owner's shares: %v", owned)
	}
	world.refused(t, world.sam, fmt.Sprintf(`query { ListRecordingShares(recordingId: %q) { id } }`, world.recordingId))
	world.refused(t, world.sam, fmt.Sprintf(`mutation { UnshareRecording(recordingShareId: %q) }`, shared["id"]))
	world.refused(t, world.sam, fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeCircleId: %q, recordingShareLevel: "summary") { id } }`, world.recordingId, world.family))

	// Outsiders reach nothing: kim shares no circle with alex.
	world.refused(t, world.kim, readQuery)
	world.refused(t, world.kim, fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeUserId: %q, recordingShareLevel: "summary") { id } }`, world.recordingId, world.kim.ID))

	// Taken back, sam reads nothing.
	world.call(t, world.alex, fmt.Sprintf(`mutation { UnshareRecording(recordingShareId: %q) }`, shared["id"]))
	world.refused(t, world.sam, readQuery)
}

// A circle's share reaches every member who may use the agent and no one else.
func TestARecordingSharedWithACircleReachesItsMembers(t *testing.T) {
	world := newRecordingShareWorld(t)
	shared := world.share(t, world.alex, fmt.Sprintf(`granteeCircleId: %q`, world.family), "summary")
	if shared["granteeCircleName"] != "Family" {
		t.Fatalf("the share: %v", shared)
	}
	if read := world.read(t, world.sam); read["recordingTitle"] != "Standup" {
		t.Fatalf("a member reads it: %v", read)
	}
	// Lee is in the circle but cannot use the agent, so recordings are not
	// his to read.
	world.refused(t, world.lee, fmt.Sprintf(`query { ReadSharedRecording(recordingId: %q, passageOffset: 0, pageSize: 10) { recordingTitle } }`, world.recordingId))
	world.refused(t, world.kim, fmt.Sprintf(`query { ReadSharedRecording(recordingId: %q, passageOffset: 0, pageSize: 10) { recordingTitle } }`, world.recordingId))
}

// A level that cannot be given, a grantee that is both or neither, a person
// who is the owner, and someone else's recording are refused, and a person
// the owner shares no circle with is answered as not found.
func TestARecordingShareIsRefusedWhenItCannotBeMade(t *testing.T) {
	world := newRecordingShareWorld(t)
	for name, query := range map[string]string{
		"audio level": fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeUserId: %q, recordingShareLevel: "audio") { id } }`, world.recordingId, world.sam.ID),
		"no level":    fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeUserId: %q, recordingShareLevel: "") { id } }`, world.recordingId, world.sam.ID),
		"both":        fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeUserId: %q, granteeCircleId: %q, recordingShareLevel: "summary") { id } }`, world.recordingId, world.sam.ID, world.family),
		"neither":     fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, recordingShareLevel: "summary") { id } }`, world.recordingId),
		"yourself":    fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeUserId: %q, recordingShareLevel: "summary") { id } }`, world.recordingId, world.alex.ID),
		"stranger":    fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeUserId: %q, recordingShareLevel: "summary") { id } }`, world.recordingId, world.kim.ID),
		"unknown":     fmt.Sprintf(`mutation { ShareRecording(recordingId: "nobody", granteeUserId: %q, recordingShareLevel: "summary") { id } }`, world.sam.ID),
	} {
		if failure := world.refused(t, world.alex, query); failure == "" {
			t.Errorf("%s: no refusal", name)
		}
	}
	// A person without the agent cannot share a recording either.
	world.refused(t, world.lee, fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, granteeUserId: %q, recordingShareLevel: "summary") { id } }`, world.recordingId, world.alex.ID))
}
