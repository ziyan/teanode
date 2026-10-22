package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A recording and a finance account of ada's, for the Sharing tests of those
// two kinds. The recording is called "Standup"; the account is "Everyday
// Checking" with the mask 0001.
func (self *sharingWorld) seedRecordingAndFinance(test *testing.T) (recordingId, financeAccountId string) {
	test.Helper()
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		recording, err := tx.CreateRecording(&models.Recording{
			UserID: self.ada.ID, ClientRecordingID: "sharing-source", RecordingTitle: "Standup", RecordingOrigin: models.RecordingOriginAudioImport,
		})
		if err != nil {
			test.Fatalf("CreateRecording: %s", err)
		}
		recordingId = recording.ID
	})
	financeAccountId, _ = seedFinance(test, self.database, self.ada)
	return recordingId, financeAccountId
}

func (self *sharingWorld) shareRecording(test *testing.T, recordingId, grantee, level string) map[string]any {
	test.Helper()
	return self.call(test, self.ada, fmt.Sprintf(`mutation { ShareRecording(recordingId: %q, %s, recordingShareLevel: %q) { id } }`,
		recordingId, grantee, level))["ShareRecording"].(map[string]any)
}

// shareFinance shares an account as the person's own tap, which the session
// of the test world is.
func (self *sharingWorld) shareFinance(test *testing.T, financeAccountId string, granteeUserId, granteeCircleId any, level string) map[string]any {
	test.Helper()
	return self.run(test, self.ada, shareFinanceQuery, map[string]any{
		"financeAccountId": financeAccountId, "granteeUserId": granteeUserId, "granteeCircleId": granteeCircleId, "financeShareLevel": level,
	})["ShareFinanceAccount"].(map[string]any)
}

// Recordings and finance accounts are kinds of the list on both sides, with
// what they are called, the level, and whether they came by name or through a
// circle.
func TestListSharingShowsRecordingsAndFinanceAccounts(test *testing.T) {
	world := newSharingWorld(test)
	recordingId, financeAccountId := world.seedRecordingAndFinance(test)
	world.shareRecording(test, recordingId, userGrantee(world.ben), "transcript")
	world.shareFinance(test, financeAccountId, world.ben.ID, nil, "spending")
	world.shareFinance(test, financeAccountId, nil, world.home, "balance")

	made, _ := world.sharing(test, world.ada)
	if fmt.Sprint(kindsOf(made)) != "[recording finance finance]" {
		test.Fatalf("ada shares %v", kindsOf(made))
	}
	if row := rowOfKind(test, made, "recording"); row["subjectName"] != "Standup" || row["subjectId"] != recordingId ||
		row["shareLevel"] != "transcript" || row["granteeUsername"] != world.ben.Username {
		test.Errorf("the recording row is %v", row)
	}
	for _, row := range made {
		if row["shareKind"] != "finance" {
			continue
		}
		wantLevel, wantCircle := "spending", ""
		if row["granteeCircleId"] != nil {
			wantLevel, wantCircle = "balance", "Home"
		}
		if row["subjectName"] != "Everyday Checking" || row["subjectId"] != financeAccountId || row["shareLevel"] != wantLevel ||
			row["granteeCircleName"] != wantCircle {
			test.Errorf("a finance row is %v", row)
		}
	}

	_, received := world.sharing(test, world.ben)
	if fmt.Sprint(kindsOf(received)) != "[recording finance]" {
		test.Fatalf("ben receives %v", kindsOf(received))
	}
	if row := rowOfKind(test, received, "recording"); row["subjectName"] != "Standup" || row["shareLevel"] != "transcript" ||
		row["ownerUsername"] != world.ada.Username || row["isDirect"] != true || row["canDecline"] != true {
		test.Errorf("ben's recording row is %v", row)
	}
	// Reached by name at spending and through the circle at balance, the
	// account is one row at the higher level, the way the finance page shows it.
	if row := rowOfKind(test, received, "finance"); row["subjectName"] != "Everyday Checking" || row["shareLevel"] != "spending" ||
		row["ownerUsername"] != world.ada.Username || row["isDirect"] != true || row["canDecline"] != true {
		test.Errorf("ben's finance row is %v", row)
	}
	maskRows := world.call(test, world.ben, `query { ListSharing { sharesReceived { shareKind subjectMask } } }`)["ListSharing"].(map[string]any)["sharesReceived"].([]any)
	if maskRow := maskRows[1].(map[string]any); maskRow["shareKind"] != "finance" || maskRow["subjectMask"] != "0001" {
		test.Errorf("the account's mask is in %v", maskRow)
	}

	// Cora is in the circle, so she receives what it was shared with and
	// cannot decline it. Her list carries the account at its balance level.
	world.join(test, world.ada, world.home, world.cora)
	_, received = world.sharing(test, world.cora)
	row := rowOfKind(test, received, "finance")
	if row["shareLevel"] != "balance" || row["isDirect"] != false || row["canDecline"] != false || row["granteeCircleName"] != "Home" {
		test.Errorf("cora's finance row is %v", row)
	}
	if fmt.Sprint(kindsOf(received)) != "[finance]" {
		test.Errorf("cora receives %v", kindsOf(received))
	}
}

// What others share of finance is not the agent's to read, so the person's
// agent is shown neither side of it, and cannot decline it; recordings it can.
func TestListSharingLeavesFinanceOutForTheAgent(test *testing.T) {
	world := newSharingWorld(test)
	recordingId, financeAccountId := world.seedRecordingAndFinance(test)
	world.shareRecording(test, recordingId, userGrantee(world.ben), "summary")
	shared := world.shareFinance(test, financeAccountId, world.ben.ID, nil, "balance")

	for _, user := range []*models.User{world.ada, world.ben} {
		operations := &agentOperations{graph: world.graph, user: user, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{
			models.PermissionAgentUse, models.PermissionCircleUse}}}
		var result struct {
			ListSharing struct {
				SharesMade     []map[string]any `json:"sharesMade"`
				SharesReceived []map[string]any `json:"sharesReceived"`
			} `json:"ListSharing"`
		}
		if err := operations.Execute(test.Context(), listSharingQuery, nil, &result); err != nil {
			test.Fatalf("the agent of %s listing: %s", user.Username, err)
		}
		listed := append(append([]map[string]any{}, result.ListSharing.SharesMade...), result.ListSharing.SharesReceived...)
		if got := kindsOf(listed); fmt.Sprint(got) != "[recording]" {
			test.Errorf("the agent of %s sees %v", user.Username, got)
		}
	}
	benAgent := &agentOperations{graph: world.graph, user: world.ben, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{
		models.PermissionAgentUse, models.PermissionCircleUse}}}
	var declined map[string]any
	err := benAgent.Execute(test.Context(), `mutation ($shareKind: String!, $shareId: String!) { DeclineShare(shareKind: $shareKind, shareId: $shareId) }`,
		map[string]any{"shareKind": "finance", "shareId": shared["id"]}, &declined)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
		test.Errorf("the agent declined a finance share: %v %v", declined, err)
	}
	if _, received := world.sharing(test, world.ben); fmt.Sprint(kindsOf(received)) != "[recording finance]" {
		test.Errorf("ben receives %v", kindsOf(received))
	}
}

// A recording or a finance account shared with a person by name can be
// declined by that person alone; one through a circle cannot.
func TestOnlyTheGranteeCanDeclineARecordingOrFinanceShareMadeByName(test *testing.T) {
	world := newSharingWorld(test)
	recordingId, financeAccountId := world.seedRecordingAndFinance(test)
	world.shareRecording(test, recordingId, userGrantee(world.ben), "summary")
	world.shareRecording(test, recordingId, fmt.Sprintf(`granteeCircleId: %q`, world.home), "summary")
	world.shareFinance(test, financeAccountId, world.ben.ID, nil, "transactions")
	declineQuery := func(row map[string]any) string {
		return fmt.Sprintf(`mutation { DeclineShare(shareKind: %q, shareId: %q) }`, row["shareKind"], row["shareId"])
	}

	_, received := world.sharing(test, world.ben)
	recordingRow, financeRow := rowOfKind(test, received, "recording"), rowOfKind(test, received, "finance")
	// The owner and somebody it was not shared with cannot decline either.
	for _, row := range []map[string]any{recordingRow, financeRow} {
		for _, user := range []*models.User{world.ada, world.cora} {
			world.refused(test, user, declineQuery(row))
		}
	}
	world.call(test, world.ben, declineQuery(recordingRow))
	world.call(test, world.ben, declineQuery(financeRow))
	// Declined once, it is gone for a second try.
	world.refused(test, world.ben, declineQuery(recordingRow))
	world.refused(test, world.ben, declineQuery(financeRow))

	made, _ := world.sharing(test, world.ada)
	if fmt.Sprint(kindsOf(made)) != "[recording]" || made[0]["granteeCircleName"] != "Home" {
		test.Errorf("ada still shares %v", made)
	}
	// What the circle shared reaches ben through it, and cannot be declined.
	_, received = world.sharing(test, world.ben)
	if fmt.Sprint(kindsOf(received)) != "[recording]" || received[0]["isDirect"] != false || received[0]["canDecline"] != false {
		test.Fatalf("ben receives %v", received)
	}
	world.refused(test, world.ben, declineQuery(received[0]))
	if data, failures := world.ask(test, world.ben, `query { ListSharedFinanceAccounts { financeAccountId } }`); len(failures) > 0 ||
		len(data["ListSharedFinanceAccounts"].([]any)) != 0 {
		test.Errorf("ben still reaches the account: %v %v", data, failures)
	}
}

// A recording or a finance account shared, or shared at a higher level, is
// told to the person it reaches, without the account's name; narrowing it and
// sharing it again tell nobody, and neither does the sharer's own share.
func TestARecordingOrFinanceShareIsToldToWhoItReaches(test *testing.T) {
	world := newSharingWorld(test)
	recordingId, financeAccountId := world.seedRecordingAndFinance(test)
	benNotes := func() []string { return world.notesOf(test, world.ben, models.NoteShareReceived) }

	world.shareRecording(test, recordingId, userGrantee(world.ben), "summary")
	if got := benNotes(); fmt.Sprint(got) != "[Share-ada shared the recording Standup with you (title and summary)]" {
		test.Fatalf("a recording share told ben %q", got)
	}
	world.shareRecording(test, recordingId, userGrantee(world.ben), "summary")
	if got := benNotes(); len(got) != 1 {
		test.Fatalf("the same recording share told ben again: %q", got)
	}
	world.shareRecording(test, recordingId, userGrantee(world.ben), "transcript")
	if got := benNotes(); len(got) != 2 || got[1] != "Share-ada shared the recording Standup with you (summary and transcript)" {
		test.Fatalf("widening a recording share told ben %q", got)
	}
	world.shareRecording(test, recordingId, userGrantee(world.ben), "summary")
	if got := benNotes(); len(got) != 2 {
		test.Fatalf("narrowing a recording share told ben %q", got)
	}

	world.shareFinance(test, financeAccountId, world.ben.ID, nil, "balance")
	got := benNotes()
	if len(got) != 3 || got[2] != "Share-ada shared a finance account with you (balance only)" {
		test.Fatalf("a finance share told ben %q", got)
	}
	if strings.Contains(got[2], "Checking") || strings.Contains(got[2], "0001") {
		test.Errorf("the note names the account: %q", got[2])
	}
	world.shareFinance(test, financeAccountId, world.ben.ID, nil, "balance")
	world.shareFinance(test, financeAccountId, world.ben.ID, nil, "transactions")
	if got := benNotes(); len(got) != 4 || got[3] != "Share-ada shared a finance account with you (balance, spending and transactions)" {
		test.Fatalf("widening a finance share told ben %q", got)
	}

	// A share to the circle tells its other members, never the sharer, and
	// the people it does not reach are told nothing.
	world.shareFinance(test, financeAccountId, nil, world.home, "spending")
	if got := benNotes(); len(got) != 5 || got[4] != "Share-ada shared a finance account with you (balance and spending)" {
		test.Errorf("a finance share to the circle told ben %q", got)
	}
	for _, user := range []*models.User{world.ada, world.cora} {
		if got := world.notesOf(test, user, models.NoteShareReceived); len(got) != 0 {
			test.Errorf("%s was told %q", user.Username, got)
		}
	}
}

// Both kinds are among the kinds a circle's owner is told a joiner can now
// see, in the order of the Sharing page.
func TestOwnersAreToldAJoinerSeesRecordingsAndFinance(test *testing.T) {
	world := newSharingWorld(test)
	recordingId, financeAccountId := world.seedRecordingAndFinance(test)
	world.call(test, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeCircleId: %q, shareLevel: "current") { id } }`, world.home))
	world.shareFinance(test, financeAccountId, nil, world.home, "balance")
	world.shareRecording(test, recordingId, fmt.Sprintf(`granteeCircleId: %q`, world.home), "summary")

	world.join(test, world.ada, world.home, world.cora)
	got := world.notesOf(test, world.ada, models.NoteShareReached)
	if fmt.Sprint(got) != "[Share-cora joined Home and can now see your location, recordings and finance]" {
		test.Fatalf("ada was told %q", got)
	}
}

// Recordings and finance accounts are listed to a person who may use the
// agent, which is the permission their own operations ask for, and to nobody
// else; finance is also left out for the person's agent.
func TestRecordingsAndFinanceAreListedOnlyToWhoMayUseThem(test *testing.T) {
	world := newSharingWorld(test)
	withAgent := &api.Principal{Permissions: &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionCircleUse, models.PermissionAgentUse}}}
	withoutAgent := &api.Principal{Permissions: &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionCircleUse}}}
	for _, shareKind := range []models.ShareKind{models.ShareKindRecording, models.ShareKindFinance} {
		for _, isReceived := range []bool{false, true} {
			if !world.graph.canListShareKind(test.Context(), withAgent, shareKind, isReceived) {
				test.Errorf("%s (received %v) is left out for a person who may use the agent", shareKind, isReceived)
			}
			if world.graph.canListShareKind(test.Context(), withoutAgent, shareKind, isReceived) {
				test.Errorf("%s (received %v) is listed to a person who may not use the agent", shareKind, isReceived)
			}
		}
	}
}

// An account reached by name at a lower level than a circle gives is one row
// at the higher level, and the row is the share made by name, so that the
// person can decline it. Declining it leaves the circle's share.
func TestAFinanceAccountSharedByNameBelowTheCircleCanBeDeclined(test *testing.T) {
	world := newSharingWorld(test)
	_, financeAccountId := world.seedRecordingAndFinance(test)
	direct := world.shareFinance(test, financeAccountId, world.ben.ID, nil, "balance")
	world.shareFinance(test, financeAccountId, nil, world.home, "spending")

	_, received := world.sharing(test, world.ben)
	row := rowOfKind(test, received, "finance")
	if row["shareLevel"] != "spending" || row["isDirect"] != true || row["canDecline"] != true || row["shareId"] != direct["id"] {
		test.Fatalf("ben's finance row is %v, want the share by name (%v) at spending", row, direct["id"])
	}
	world.call(test, world.ben, fmt.Sprintf(`mutation { DeclineShare(shareKind: "finance", shareId: %q) }`, row["shareId"]))

	_, received = world.sharing(test, world.ben)
	row = rowOfKind(test, received, "finance")
	if row["shareLevel"] != "spending" || row["isDirect"] != false || row["canDecline"] != false || row["granteeCircleName"] != "Home" {
		test.Errorf("after declining, ben's finance row is %v", row)
	}
}
