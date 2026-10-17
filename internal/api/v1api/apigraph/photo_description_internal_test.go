package apigraph

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// withAssistant gives the world's graph an agent worker with a model for the
// bulk work, which is never called: asking for a description only queues one.
func (self *photoWorld) withAssistant() {
	self.test.Helper()
	configuration := self.config.Current()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", BaseURL: "http://127.0.0.1:1", APIKey: "k"}}
	configuration.Agent.Models.Default = "p:thinker"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		self.test.Fatal(err)
	}
	worker := agent.New(&agent.Settings{Database: self.database, Registry: registry, Configuration: self.config.Current, Instance: "test", Tick: time.Hour})
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return nil, nil })
	self.graph.settings = &api.Settings{Agent: worker}
}

func (self *photoWorld) giveAgent(userId string, isEnabled bool) *models.Agent {
	self.test.Helper()
	var created *models.Agent
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		if created, err = transaction.CreateAgent(&models.Agent{UserID: userId, Enabled: isEnabled}); err != nil {
			self.test.Fatal(err)
		}
	})
	return created
}

func TestDescribingAPhotoIsRefusedWithoutTheAssistantOnAndLeavesADescribedPhotoAlone(test *testing.T) {
	world := newPhotoWorld(test)
	allowed := []models.Permission{models.PermissionPhotosUse, models.PermissionAgentUse}
	noAgentId := world.person("describe-no-agent", allowed...)
	offId := world.person("describe-agent-off", allowed...)
	world.giveAgent(offId, false)
	noPermissionId := world.person("describe-no-permission", models.PermissionPhotosUse)
	world.giveAgent(noPermissionId, true)
	world.withAssistant()

	for name, userId := range map[string]string{"no agent": noAgentId, "agent off": offId, "no permission to use it": noPermissionId} {
		photo := world.storePhoto(userId, "to-describe", "a.jpg", []byte("a original"), nil)
		world.ask(userId, []models.Permission{models.PermissionPhotosUse, models.PermissionAgentUse}, func(ctx context.Context) {
			if name == "no permission to use it" {
				ctx = api.ContextWithPrincipal(ctx, &api.Principal{User: api.ContextPrincipal(ctx).User, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionPhotosUse}})})
			}
			if _, err := world.graph.DescribePhoto(ctx, DescribePhotoArguments{PhotoID: photo.ID}); err == nil || !strings.Contains(err.Error(), "Describing photos needs the assistant to be on.") {
				test.Fatalf("%s: %v", name, err)
			}
			library, err := world.graph.ReadPhotoLibrary(ctx)
			if err != nil || library.IsDescriptionAvailable || library.DescriptionUnavailableMessage != "Describing photos needs the assistant to be on." {
				test.Fatalf("%s: %+v %v", name, library, err)
			}
			// Nothing was queued, and the photo is not marked.
			found, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photo.ID})
			if err != nil || found.Photo.IsDescriptionRequested {
				test.Fatalf("%s: %+v %v", name, found, err)
			}
		})
	}

	// A photo that has a description is answered as it is, with no assistant
	// needed, unless a new description is asked for.
	described := world.storePhoto(noAgentId, "described", "b.jpg", []byte("b original"), nil)
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoDescription(noAgentId, described.ID, &db.PhotoDescription{PhotoDescription: "A red lighthouse.", ModelName: "p:thinker"}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(noAgentId, allowed, func(ctx context.Context) {
		answered, err := world.graph.DescribePhoto(ctx, DescribePhotoArguments{PhotoID: described.ID})
		if err != nil || answered.PhotoDescription != "A red lighthouse." || answered.DescribedAt == nil || answered.IsDescriptionRequested {
			test.Fatalf("%+v %v", answered, err)
		}
		if _, err := world.graph.DescribePhoto(ctx, DescribePhotoArguments{PhotoID: described.ID, IsRedescribed: new(true)}); err == nil {
			test.Fatal("a new description was asked for with no assistant")
		}
	})
}

func TestDescribingAPhotoQueuesOneJobAndSearchFindsItByItsDescription(test *testing.T) {
	world := newPhotoWorld(test)
	allowed := []models.Permission{models.PermissionPhotosUse, models.PermissionAgentUse}
	ownerId := world.person("describe-owner", allowed...)
	otherId := world.person("describe-other", allowed...)
	assistant := world.giveAgent(ownerId, true)
	world.withAssistant()
	lighthouse := world.storePhoto(ownerId, "lighthouse", "IMG_1.jpg", []byte("one"), nil)
	world.storePhoto(ownerId, "plain", "IMG_2.jpg", []byte("two"), nil)
	foreign := world.storePhoto(otherId, "foreign", "IMG_3.jpg", []byte("three"), nil)
	world.ask(ownerId, allowed, func(ctx context.Context) {
		library, err := world.graph.ReadPhotoLibrary(ctx)
		if err != nil || !library.IsDescriptionAvailable || library.DescriptionUnavailableMessage != "" {
			test.Fatalf("%+v %v", library, err)
		}
		first, err := world.graph.DescribePhoto(ctx, DescribePhotoArguments{PhotoID: lighthouse.ID})
		if err != nil || !first.IsDescriptionRequested || first.DescribedAt != nil {
			test.Fatalf("it answers without waiting, with the request pending: %+v %v", first, err)
		}
		if _, err := world.graph.DescribePhoto(ctx, DescribePhotoArguments{PhotoID: lighthouse.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := world.graph.DescribePhoto(ctx, DescribePhotoArguments{PhotoID: foreign.ID}); err == nil {
			test.Fatal("a photo of somebody else's was described")
		}
		jobs, err := world.graph.transaction(ctx).ListAgentJobs(&db.AgentJobFilter{AgentID: assistant.ID, Kinds: []models.AgentJobKind{models.AgentJobDescribePhoto}, SubjectID: lighthouse.ID}, nil)
		if err != nil || len(jobs) != 1 {
			test.Fatalf("one job however often it is asked: %d %v", len(jobs), err)
		}
		if undescribed, err := world.graph.CountUndescribedPhotos(ctx, ListPhotosArguments{SearchText: "ignored words"}); err != nil || undescribed != 2 {
			test.Fatalf("two photos have no description: %d %v", undescribed, err)
		}
	})

	// The job's result, as the handler stores it.
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoDescription(ownerId, lighthouse.ID, &db.PhotoDescription{PhotoDescription: "A red lighthouse by the sea.", VisibleText: "PIER 9", ModelName: "p:thinker"}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(ownerId, allowed, func(ctx context.Context) {
		found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{SearchText: "lighthouse"})
		if err != nil || len(found) != 1 || found[0].ID != lighthouse.ID || found[0].PhotoDescription != "A red lighthouse by the sea." || found[0].DescriptionVisibleText != "PIER 9" || found[0].IsDescriptionRequested {
			test.Fatalf("%+v %v", found, err)
		}
		if count, err := world.graph.CountPhotos(ctx, ListPhotosArguments{SearchText: "pier"}); err != nil || count != 1 {
			test.Fatalf("%d %v", count, err)
		}
		if undescribed, err := world.graph.CountUndescribedPhotos(ctx, ListPhotosArguments{}); err != nil || undescribed != 1 {
			test.Fatalf("%d %v", undescribed, err)
		}
		if found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{SearchText: "lighthouse", IsFavorite: new(true)}); err != nil || len(found) != 0 {
			test.Fatalf("the filters narrow a search: %v %v", found, err)
		}
	})
	world.ask(otherId, allowed, func(ctx context.Context) {
		if found, err := world.graph.ListPhotos(ctx, ListPhotosArguments{SearchText: "lighthouse"}); err != nil || len(found) != 0 {
			test.Fatalf("a search found somebody else's description: %v %v", found, err)
		}
	})
}
