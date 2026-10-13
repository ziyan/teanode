package apigraph

import (
	"testing"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Every document the command line and the tool send is valid against the schema the server builds,
// including the title source, the summary, the owner flag and the optional title.
func TestRecordingClientDocumentsMatchTheBuiltSchema(test *testing.T) {
	schema := buildSchemaForValidation(test)
	for operationName, source := range client.RecordingDocuments {
		document, err := gqlparser.Parse(gqlparser.ParseParams{Source: source})
		if err != nil {
			test.Fatalf("%s does not parse: %s", operationName, err)
		}
		if validated := graphql.ValidateDocument(&schema, document, nil); !validated.IsValid {
			test.Errorf("%s does not match the built schema: %v", operationName, validated.Errors)
		}
	}
	for _, source := range []string{
		`{ ReadRecording(recordingId: "a") { recording { recordingTitleSource recordingSummary recordedAt } } }`,
		`{ ReadRecordingTranscriptTimeline(recordingId: "a", segmentOffset: 0, pageSize: 1) { segments { isAccountOwner } } }`,
		`{ ReadRecordingSpeakers(recordingId: "a") { speakerIdentities { isAccountOwner } } }`,
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

// Changing only the date leaves the title and who wrote it alone; an empty title clears it.
func TestUpdateRecordingMetadataWithoutATitleKeepsTheTitle(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "metadata-api-owner")
	resolver := &graph{database: database}
	var recordingId string
	update := func(arguments UpdateRecordingMetadataArguments) *models.Recording {
		var updated *models.Recording
		if err := database.Transaction(func(transaction db.Transaction) error {
			owner, err := transaction.GetUser(ownerId)
			if err != nil {
				return err
			}
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}), transaction)
			if recordingId == "" {
				created, err := resolver.CreateRecording(ctx, CreateRecordingArguments{ClientRecordingID: "metadata-api", RecordingOrigin: models.RecordingOriginCapture})
				if err != nil {
					return err
				}
				recordingId = created.ID
				if created.RecordingTitle != "" || created.RecordingTitleSource != models.RecordingTitleSourceNone {
					test.Fatalf("a recording needs no title to be created: %+v", created)
				}
			}
			current, err := transaction.GetRecording(ownerId, recordingId)
			if err != nil {
				return err
			}
			arguments.RecordingID, arguments.ExpectedSourceGeneration = recordingId, current.SourceGeneration
			updated, err = resolver.UpdateRecordingMetadata(ctx, arguments)
			return err
		}); err != nil {
			test.Fatal(err)
		}
		return updated
	}
	typedTitle := "Typed title"
	if updated := update(UpdateRecordingMetadataArguments{RecordingTitle: &typedTitle}); updated.RecordingTitleSource != models.RecordingTitleSourceUser {
		test.Fatalf("a typed title is a user title: %+v", updated)
	}
	if updated := update(UpdateRecordingMetadataArguments{}); updated.RecordingTitle != "Typed title" || updated.RecordingTitleSource != models.RecordingTitleSourceUser {
		test.Fatalf("an absent title changed the title: %+v", updated)
	}
	emptyTitle := ""
	if updated := update(UpdateRecordingMetadataArguments{RecordingTitle: &emptyTitle}); updated.RecordingTitle != "" || updated.RecordingTitleSource != models.RecordingTitleSourceNone {
		test.Fatalf("an empty title did not clear it: %+v", updated)
	}
}
