package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A question file imported twice lists its questions once, as confirmed;
// one without an answer is refused; a question is changed and dropped from
// the page, and a question of somebody else's is not theirs to change.
func TestMemoryCheckQuestionsAreImportedListedAndChanged(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var person, stranger *models.User
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "checked"}); err != nil {
			t.Fatal(err)
		}
		if stranger, err = tx.CreateUser(&models.User{Username: "stranger"}); err != nil {
			t.Fatal(err)
		}
		for _, owner := range []*models.User{person, stranger} {
			if _, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
				t.Fatal(err)
			}
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{}}
	as := func(user *models.User, do func(ctx context.Context)) {
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			do(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx))
		})
	}
	file := ImportAgentEvaluationQuestionsArguments{Questions: []ImportedEvaluationQuestion{
		{QuestionKind: "direct", QuestionText: "What is the cat called?", ExpectedAnswer: "Pepper."},
		{QuestionKind: "changed", QuestionText: "Which team do you play for?", ExpectedAnswer: "The Otters.", OutdatedAnswer: "The Herons."},
		{QuestionKind: "abstain", QuestionText: "What is your blood type?", ExpectedAnswer: "not known"},
	}}
	for round, expectedCount := range []int{3, 0} {
		as(person, func(ctx context.Context) {
			addedCount, err := resolver.ImportAgentEvaluationQuestions(ctx, file)
			if err != nil || addedCount != expectedCount {
				t.Fatalf("round %d: %d added, not %d: %v", round, addedCount, expectedCount, err)
			}
		})
	}
	as(person, func(ctx context.Context) {
		if _, err := resolver.ImportAgentEvaluationQuestions(ctx, ImportAgentEvaluationQuestionsArguments{Questions: []ImportedEvaluationQuestion{{QuestionText: "What is missing?"}}}); err == nil {
			t.Fatal("a confirmed question needs its answer")
		}
	})
	var questionId string
	as(person, func(ctx context.Context) {
		questions, err := resolver.ListAgentEvaluationQuestions(ctx, ListAgentEvaluationQuestionsArguments{QuestionStates: []string{"confirmed"}})
		if err != nil || len(questions) != 3 {
			t.Fatalf("three confirmed questions: %v %v", questions, err)
		}
		questionId = questions[0].ID
		answer := "Pepper the Second."
		updated, err := resolver.UpdateAgentEvaluationQuestion(ctx, UpdateAgentEvaluationQuestionArguments{QuestionID: questionId, ExpectedAnswer: &answer})
		if err != nil || updated.ExpectedAnswer != answer {
			t.Fatalf("the answer is changed: %+v %v", updated, err)
		}
		dropped := "dropped"
		if updated, err = resolver.UpdateAgentEvaluationQuestion(ctx, UpdateAgentEvaluationQuestionArguments{QuestionID: questionId, QuestionState: &dropped}); err != nil || updated.QuestionState != models.EvaluationQuestionDropped {
			t.Fatalf("the question is dropped: %+v %v", updated, err)
		}
	})
	as(stranger, func(ctx context.Context) {
		dropped := "confirmed"
		if _, err := resolver.UpdateAgentEvaluationQuestion(ctx, UpdateAgentEvaluationQuestionArguments{QuestionID: questionId, QuestionState: &dropped}); err == nil {
			t.Fatal("somebody else's question is not found")
		}
		if questions, _ := resolver.ListAgentEvaluationQuestions(ctx, ListAgentEvaluationQuestionsArguments{}); len(questions) != 0 {
			t.Fatalf("nobody else's questions are listed: %v", questions)
		}
	})
}
