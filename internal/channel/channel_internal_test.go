package channel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// stoppingBot is a bot whose connection fails for good as soon as it runs.
type stoppingBot struct{}

func (stoppingBot) Name() string { return "stopping_bot" }

func (stoppingBot) Run(context.Context, func(context.Context, *Incoming, Chat)) error {
	return errors.New("the connection failed for good")
}

func (stoppingBot) ChatFor(string) (Chat, error) { return nil, errors.New("no chat") }

// A bot that stops on its own takes its relay with it, while the manager
// runs on: the next tick starts the bot again with a relay of its own, and
// one left behind would send the same turns twice.
func TestBotThatStopsEndsItsRelay(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	configuration := config.Default()
	configuration.Server.Secret = strings.Repeat("not-a-secret-", 4)
	worker := agent.New(&agent.Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	sealed, err := worker.SealSecret("TOKEN")
	if err != nil {
		t.Fatalf("SealSecret: %s", err)
	}
	manager := New(&Settings{
		Worker: worker, Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test",
		Openers: map[models.AgentChannelKind]Opener{models.AgentChannelTelegram: func(context.Context, string) (Bot, error) {
			return stoppingBot{}, nil
		}},
		RelayEvery: 50 * time.Millisecond,
	})
	defer manager.cancel()

	channel := &models.AgentChannel{ID: "channel-1", AgentID: "agent-1", Kind: models.AgentChannelTelegram, Token: sealed, Enabled: true}
	if err := manager.run(manager.ctx, channel); err == nil {
		t.Fatal("the bot's failure is what run answers")
	}
	if manager.ctx.Err() != nil {
		t.Fatal("the manager runs on")
	}
	ended := make(chan struct{})
	go func() {
		manager.wait.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the relay outlived its bot")
	}
}
