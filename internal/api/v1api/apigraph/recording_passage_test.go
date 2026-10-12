package apigraph

import (
	"errors"
	"testing"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingTranscriptPassageOffsetSchemaAndAuthorization(test *testing.T) {
	schema := buildSchemaForValidation(test)
	document, err := gqlparser.Parse(gqlparser.ParseParams{Source: `query ($recordingTranscriptRunId:String!,$passageStartMS:Int64!,$passageEndMS:Int64!){ReadRecordingTranscriptPassageOffset(recordingTranscriptRunId:$recordingTranscriptRunId,passageStartMS:$passageStartMS,passageEndMS:$passageEndMS)}`})
	if err != nil {
		test.Fatal(err)
	}
	validated := graphql.ValidateDocument(&schema, document, nil)
	if !validated.IsValid {
		test.Fatal("passage lookup client document does not match built schema", validated.Errors)
	}
	resolver := &graph{}
	if _, err := resolver.ReadRecordingTranscriptPassageOffset(test.Context(), ReadRecordingTranscriptPassageOffsetArguments{}); !errors.Is(err, api.ErrNotLoggedIn) {
		test.Fatal("anonymous citation lookup was not refused", err)
	}
	principal := &api.Principal{User: &models.User{ID: "synthetic-passage-owner"}, Permissions: models.NewEffectivePermissions(nil)}
	if _, err := resolver.ReadRecordingTranscriptPassageOffset(api.ContextWithPrincipal(test.Context(), principal), ReadRecordingTranscriptPassageOffsetArguments{}); !errors.Is(err, api.ErrNotFound) {
		test.Fatal("citation lookup without agent permission was not refused", err)
	}
}

func TestRecordingResumeLearningSchemaAndAuthorization(test *testing.T) {
	schema := buildSchemaForValidation(test)
	document, err := gqlparser.Parse(gqlparser.ParseParams{Source: `mutation($recordingId:String!,$expectedSourceGeneration:Int64!){ResumeRecordingLearning(recordingId:$recordingId,expectedSourceGeneration:$expectedSourceGeneration){sourceGeneration hasAgent hasStoppedReading pendingDocumentCount knowledgeSource{id enabled}}}`})
	if err != nil {
		test.Fatal(err)
	}
	validated := graphql.ValidateDocument(&schema, document, nil)
	if !validated.IsValid {
		test.Fatal("resume learning document does not match built schema", validated.Errors)
	}
	resolver := &graph{}
	if _, err := resolver.ResumeRecordingLearning(test.Context(), ResumeRecordingLearningArguments{}); !errors.Is(err, api.ErrNotLoggedIn) {
		test.Fatal("anonymous resume was not refused", err)
	}
	principal := &api.Principal{User: &models.User{ID: "synthetic-learning-owner"}, Permissions: models.NewEffectivePermissions(nil)}
	if _, err := resolver.ResumeRecordingLearning(api.ContextWithPrincipal(test.Context(), principal), ResumeRecordingLearningArguments{}); !errors.Is(err, api.ErrNotFound) {
		test.Fatal("resume without agent permission was not refused", err)
	}
}
