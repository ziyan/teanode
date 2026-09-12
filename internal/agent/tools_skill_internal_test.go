package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
)

// secretRun is as much of a run as resolving a skill's secrets needs:
// whose agent it is, and what the operator configured.
type secretRun struct {
	tools.Run
	agent         *models.Agent
	configuration *config.Configuration
}

func (self *secretRun) Agent() *models.Agent                 { return self.agent }
func (self *secretRun) Configuration() *config.Configuration { return self.configuration }

// A skill with one key of the operator's and one of each person's own:
// the first tool uses the person's, the second the operator's.
const twoScopes = "---\nname: camera\ndescription: cameras\nsecrets:\n" +
	"  - key: CAMERA_TOKEN\n    scope: person\n    description: your own token\n" +
	"  - key: DIRECTORY_KEY\n    description: the shared directory key\n" +
	"tools:\n" +
	"  - name: camera_mine\n    description: mine\n    type: http\n    url: \"https://example.com/mine\"\n" +
	"    headers: {X-Token: \"{{secret:CAMERA_TOKEN}}\"}\n    result: json\n    parameters: {type: object, properties: {}}\n" +
	"  - name: camera_list\n    description: list\n    type: http\n    url: \"https://example.com/list\"\n" +
	"    headers: {X-Key: \"{{secret:DIRECTORY_KEY}}\"}\n    result: json\n    parameters: {type: object, properties: {}}\n---\n"

func skillFixture(t *testing.T) (*Agent, *secretRun, *skills.Skill, db.Database, func()) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	configuration := config.Default()
	configuration.Server.Secret = "a secret long enough to seal things with"
	configuration.Agent.SkillSecrets = []config.AgentSkillSecret{
		{Skill: "camera", Key: "DIRECTORY_KEY", Value: "the-directory-key"},
		// An operator may write a person-scoped key here by mistake. It
		// must be ignored, not quietly shared with everybody.
		{Skill: "camera", Key: "CAMERA_TOKEN", Value: "somebody-elses-token"},
	}
	worker := New(&Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test"})
	run := &secretRun{configuration: configuration}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	skill, err := skills.Parse([]byte(twoScopes))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	return worker, run, skill, database, closeDatabase
}

// A key the skill declares as the person's own is never filled in from
// the operator's list, and the tool that needs it refuses by name until
// that person sets it.
func TestAPersonsKeyIsNeverTakenFromTheOperatorsList(t *testing.T) {
	worker, run, skill, database, closeDatabase := skillFixture(t)
	defer closeDatabase()
	ctx := context.Background()

	_, err := worker.skillSecrets(ctx, run, skill, "camera_mine")
	if err == nil || !strings.Contains(err.Error(), "CAMERA_TOKEN") {
		t.Fatalf("it should refuse, naming the key: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "somebody-elses-token") {
		t.Fatal("a refusal must not carry a value")
	}

	// The skill's other tool wants only the operator's key, and works.
	filled, err := worker.skillSecrets(ctx, run, skill, "camera_list")
	if err != nil {
		t.Fatalf("a tool wanting only the operator's key runs: %v", err)
	}
	if filled["DIRECTORY_KEY"] != "the-directory-key" {
		t.Fatalf("the operator's key is filled in: %v", filled["DIRECTORY_KEY"])
	}
	if filled["CAMERA_TOKEN"] != "" {
		t.Fatal("a person-scoped key must not come from the operator's list")
	}

	// Once that person sets their own, their tool runs with it.
	sealed, err := worker.SealSecret("alices-own-token")
	if err != nil {
		t.Fatalf("SealSecret: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.PutAgentSkillSecret(&models.AgentSkillSecret{
			AgentID: run.agent.ID, Skill: "camera", Key: "CAMERA_TOKEN", Value: sealed,
		}); err != nil {
			t.Fatalf("PutAgentSkillSecret: %s", err)
		}
	})
	if filled, err = worker.skillSecrets(ctx, run, skill, "camera_mine"); err != nil {
		t.Fatalf("with the value set it runs: %v", err)
	}
	if filled["CAMERA_TOKEN"] != "alices-own-token" {
		t.Fatalf("their own value is used, not the operator's: %q", filled["CAMERA_TOKEN"])
	}
	if sealed == "alices-own-token" || strings.Contains(sealed, "alices-own-token") {
		t.Fatal("the value is sealed at rest")
	}
}

// One person's sealed value is theirs: another agent asking for the same
// key of the same skill is told to set their own.
func TestOnePersonsValueIsNotAnothers(t *testing.T) {
	worker, run, skill, database, closeDatabase := skillFixture(t)
	defer closeDatabase()
	ctx := context.Background()

	sealed, err := worker.SealSecret("alices-own-token")
	if err != nil {
		t.Fatalf("SealSecret: %s", err)
	}
	stranger := &secretRun{configuration: run.configuration}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.PutAgentSkillSecret(&models.AgentSkillSecret{
			AgentID: run.agent.ID, Skill: "camera", Key: "CAMERA_TOKEN", Value: sealed,
		}); err != nil {
			t.Fatalf("PutAgentSkillSecret: %s", err)
		}
		owner, err := tx.CreateUser(&models.User{Username: "bertie"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if stranger.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	if _, err := worker.skillSecrets(ctx, stranger, skill, "camera_mine"); err == nil || !strings.Contains(err.Error(), "CAMERA_TOKEN") {
		t.Fatalf("somebody else's value is not theirs to use: %v", err)
	}
}

// Nobody to ask is not somebody to guess for: a run with no agent cannot
// resolve a key that belongs to a person.
func TestAPersonsKeyNeedsAPerson(t *testing.T) {
	worker, run, skill, _, closeDatabase := skillFixture(t)
	defer closeDatabase()
	nobody := &secretRun{configuration: run.configuration}
	if _, err := worker.skillSecrets(context.Background(), nobody, skill, "camera_mine"); err == nil ||
		!strings.Contains(err.Error(), "nobody to ask") {
		t.Fatalf("want a refusal about there being nobody to ask, got %v", err)
	}
}
