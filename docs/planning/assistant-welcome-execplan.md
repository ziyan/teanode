# One page from a new account to a first conversation

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows the ExecPlan rules the author keeps in their own notes: self-contained, prose first, every milestone verifiable on its own.


## Purpose / Big Picture

Somebody who installs TeaNode with the assistant compose file (`deploy/docker-compose.assistant.yml`) and opens the dashboard creates the first account, and is then dropped on Manage → Server → Setup: a page with a rail of other places, eight tabs, a choice between two presets, five checkboxes, and a list of four steps that each send them to a different page. To get a first answer they must, in order: add a model provider on Manage → Agents → Models and save it, press Test, choose a Default model and save again, tick Enabled on Manage → Agents → General and save a third time, come back and restart the server, and turn on their own agent. A recorded run of exactly that took two minutes of brisk clicking by somebody who already knew the way; a newcomer does not know where to go next.

After this change, the person who creates the first account lands on one page with nothing else on it: "Start your assistant". It asks one question, which model service to use, with a field for its API key (or, for a model server of their own such as Ollama or vLLM, its address), and has one button. Pressing it checks the key, chooses a sensible model, turns the assistant on for the server, turns on that person's own agent, restarts the server, and opens the conversation. Progress is shown on the same page while it happens. Mail, calendars, contacts and every model setting stay where they are under Manage, for later. The same operation is reachable from the command line as `teanode services assistant`, because every capability in this project is reachable from the dashboard and the command line alike.

To see it working: start a fresh server with the assistant compose file, open it, create the account, choose OpenAI, paste a key, press Start my assistant, and within about thirty seconds the chat drawer opens with the agent's greeting in it.


## Progress

- [x] (2026-10-16) Plan written after the user watched the onboarding recording and found it dizzying.
- [x] (2026-10-16) Milestone 1: `SetUpAssistant` and `chooseAssistantModels` in `internal/api/v1api/apigraph/assistant_setup.go`, tested against a fake model server; a refused key is reported as `AssistantKeyRefused`.
- [x] (2026-10-16) Milestone 2: `web/src/pages/welcome.tsx`, drawn at `/welcome` as one card; `homePath` sends a new operator there; `hasModelProvider` in the session.
- [x] (2026-10-16) Milestone 3: `teanode services assistant`; the quick start (three languages) and `docs/reference/deployment.md` rewritten to one step.
- [x] (2026-10-16) Audited in headless Chrome on fresh sandboxes at 1280 and 390 wide, light and dark, English, Chinese and Japanese; fixed what it found (below).
- [ ] A recorded run from a fresh sandbox.


## Surprises & Discoveries

- A restart cannot be avoided cheaply. `internal/cmd/server/run.go` builds the model registry (`agentRegistry`) and the agent worker (`agentWorker`) once, at startup, and only when `agent.enabled` is true; when it is false nothing downstream of the agent is built at all, and many components are handed the worker then. `startupOnly` in the same file lists `agent.enabled`, `agent.providers` and `agent.models` as read once. Making them live would mean building the agent late and rewiring everything that holds it. So the restart stays, but the person never asks for it: the page does.
- The audit at phone width found the greeting missing: a page that has just loaded asks for the main conversation from several places at once, and each request found none and made one (four main conversations, created within 30 milliseconds). The greeting went into one the drawer did not show. `MainAgentConversation` in `internal/db/database_insight.go` now finds or makes it with the agent's row locked; a test that holds eight transactions open together made eight without the lock and makes one with it.
- The greeting came a minute late: the agent looks for whom to speak to first once a minute, and the first look after the restart came before the reloaded page reported that the person was there. A person coming into view now makes the next tick look again (`hasArrival` in `internal/agent/presence.go`). From creating the account to the agent's greeting now takes about thirty seconds, most of it the model.
- Turning on a person's own agent does not need the worker. `UpdateAgent` in `internal/api/v1api/apigraph/agent.go` checks only `config.Current().Agent.Enabled`, which a configuration update changes at once, so the person's agent can be turned on before the restart.


## Decision Log

- Decision: one GraphQL mutation does the whole setup, and the page then restarts the server through the existing `RestartServer` mutation and waits for it. Rationale: the restart is the step most likely to fail (a server not run under a supervisor cannot restart itself), and the page needs to show it and say what to do if it fails; a mutation that restarted the server under its own reply could not report anything. Date: 2026-10-16.
- Decision: the person who sets the assistant up gets their own agent turned on by it. Each person still turns on their own agent everywhere else, which is deliberate (an agent reads what its person can), but somebody pressing "Start my assistant" has answered that question. Date: 2026-10-16.
- Decision: the model is chosen for them, from what the key can reach, by a short preference list per service: for OpenAI `gpt-5-mini`, then `gpt-4.1-mini`, then `gpt-4o-mini`; for Anthropic the newest `sonnet`, then the newest `haiku`; for Gemini the newest `flash`; for a model server of their own, the first model that is not an embedding model. An embedding model is chosen the same way when the service has one (`text-embedding-3-small` for OpenAI, a name containing `embedding` for Gemini, a name containing `embed` otherwise). Rationale: picking out of a hundred and forty names is the step a newcomer cannot do well, and the page says which model it chose and where to change it. Date: 2026-10-16.
- Decision: on OpenAI the default is the newest `gpt-*-sol` and the newest `gpt-*-luna` does the cheap background work (`agent.models.fast`), falling back to `gpt-5-mini` alone where the key reaches neither. Rationale: the operator asked for the better models. Luna is not the quick model for typed turns here: on a new server, with nothing remembered, a typed turn started on it called nine tools, saved an instruction nobody gave, and spent the whole daily token budget without answering, where sol answered the same question in one call. Date: 2026-10-16.
- Decision: the welcome page offers OpenAI, Anthropic, Gemini and "a model server of your own". Signing in with a ChatGPT plan is a separate flow (`docs/planning/chatgpt-signin-execplan.md`) and stays on Manage → Agents → Models, linked from the page. Date: 2026-10-16.
- Decision: the first account lands on the welcome page while setup is not complete and the assistant has no model; the setup page stays where it is for everything else (mail, calendars, the presets). A mail server, chosen at install, still lands on the setup page, since its next step is a domain, not a model. Date: 2026-10-16.


## Outcomes & Retrospective

From a fresh compose install, a new operator now creates the account, sees one card asking for a model service and its key, presses one button, and is in a conversation the agent opens with a greeting, in about thirty seconds and without visiting another page. Before, the same took three saves on three pages, a restart, and a second switch for the person's own agent. The restart is still there, behind the progress lines; removing it would mean building the agent while the server runs (see the first discovery). A ChatGPT plan still signs in on Manage → Agents → Models.


## Context and Orientation

TeaNode is one Go program (`cmd/teanode-server`) with a dashboard written in React (`web/`) compiled into it, and a command line client (`cmd/teanode`). The dashboard and the command line both talk to the server through GraphQL at `/api/v1/graphql`; the server's GraphQL operations are methods on the `graph` type in `internal/api/v1api/apigraph/`, discovered by name (a method `UpdateServices(ctx, UpdateServicesArguments)` becomes the mutation `UpdateServices`), with arguments described by a struct whose fields carry `json` tags.

The configuration lives in the database and is read with `self.config.Current()` and changed with `self.config.Update(func(configuration *config.Configuration) error { ... })`. The parts this plan touches are `configuration.Agent.Enabled` (whether the server offers agents at all), `configuration.Agent.Providers` (a list of `config.AgentProvider`, each with a `Name`, a `Kind` such as `openai`, `anthropic` or `gemini`, an optional `BaseURL` and an `APIKey`), `configuration.Agent.Models` (which model does which work; `Default` and `Embedding` are written as `provider:model`, such as `openai:gpt-5-mini`), and `configuration.Services.HasCompletedSetup` (whether the first operator has finished setting the server up; while it is false the dashboard sends them to setup when they sign in).

`listProviderModels` in `internal/api/v1api/apigraph/settings_agent.go` builds a provider from a `config.AgentProvider` and lists the models it offers; the Test button on Manage → Agents → Models uses it, and so will this plan, to check a key and to choose a model. `RestartServer` in `internal/api/v1api/apigraph/server.go` asks the process to restart under its supervisor (Docker's restart policy, or systemd). `web/src/components/serverRestart.ts` has `restartServer()`, which calls it and waits until the server answers with a new start time.

In the dashboard, `web/src/app.tsx` draws a few single-purpose pages without the rail, as one card on a plain background (`<div className="auth-page">`): the first-account form, sign-in, `/connect` and `/cli`. `homePath` in `web/src/services.ts` decides where `/` goes; today an operator whose setup is not complete goes to `/manage/server/setup`. The chat drawer is opened from a page with `openAgentDrawer()` in `web/src/api.ts`, and notices an agent turned on when the `AGENT_TURNED_EVENT` event from `web/src/agentPreferences.ts` is sent.

Translations are in `web/src/i18n/en.ts`, `zh.ts` and `ja.ts`, which must have the same keys (a test checks). The command line's service commands are in `internal/cmd/services.go`, using `internal/client/services.go`.


## Plan of Work

Milestone 1 adds the server operation. In a new file `internal/api/v1api/apigraph/assistant_setup.go`, add `SetUpAssistantArguments` with `Kind string`, `APIKey *string`, `BaseURL *string` and `Model *string` (an optional choice that overrides the automatic one), and a result `AssistantSetup` with `Provider string`, `DefaultModel string`, `EmbeddingModel string`, `IsAgentOn bool` and `NeedsRestart bool`. The method `SetUpAssistant(ctx, arguments)` requires the `server:manage` permission, refuses a kind outside openai, anthropic and gemini (a model server of one's own is kind openai with a base URL), builds a `config.AgentProvider` named after the kind (`openai`, `anthropic`, `gemini`, or `local` for one's own server), lists its models with `listProviderModels` and returns the provider's own error, worded for a person, if the key is refused. It then chooses the models with a pure function `chooseAssistantModels(kind string, isOwnServer bool, names []string) (defaultModel, embeddingModel string)` in the same file, updates the configuration in one `config.Update` (replacing a provider of the same name, setting `Models.Default` and, if one was chosen and none is set, `Models.Embedding`, setting `Agent.Enabled`, and, unless mail routing is chosen, `Services.HasCompletedSetup`), turns on the caller's own agent by calling the existing `UpdateAgent` with `Enabled` true, and reports whether `agent.enabled`, `agent.providers` or `agent.models` now differ from what the process started with (the restarter's pending list). Tests in `assistant_setup_test.go` cover `chooseAssistantModels` for each kind with realistic lists, and the mutation against an `httptest` server that answers `/v1/models` like OpenAI does, checking the configuration it leaves and that a refused key changes nothing.

Milestone 2 adds the page. `web/src/pages/welcome.tsx` exports `WelcomePage`: a heading, one sentence, four choices drawn as a segmented row (OpenAI, Anthropic, Gemini, Your own server), the field the choice needs (API key, or address and an optional key), and the button. While it works it shows three lines, each ticked as it finishes: "Checking the key", "Starting the assistant" (the restart, through `restartServer()`), "Opening your conversation"; then it sends `AGENT_TURNED_EVENT`, navigates to `/` (which now goes to the person's home), and opens the chat drawer with `openAgentDrawer()`. If the key is refused, the error appears under the field and nothing else changes. If the restart is unavailable it says to restart the container (`docker compose restart`) and offers a button to check again. `app.tsx` draws `/welcome` the way it draws `/connect`. `homePath` sends an operator to `/welcome` while setup is not complete, the assistant has no default model, and mail routing is not chosen; this needs `hasModelProvider` in the session's service capabilities, added to `ServiceCapabilities` beside `hasCompletedSetup`. A link under the card, "Set up mail and more instead", goes to the setup page.

Milestone 3 adds `teanode services assistant --kind openai --api-key-file FILE [--base-url URL] [--model NAME] [--restart]` in `internal/cmd/services.go` with a client method in `internal/client/services.go`, rewrites step 3 of the quick start (in the site's own repository, `data/docs/{en,zh,ja}/quick-start.markdown`) and the assistant section of `docs/reference/deployment.md` to one paragraph, and records a run from a fresh sandbox.


## Concrete Steps

From the repository root, after each milestone:

    gofmt -l internal/ cmd/            # prints nothing
    go vet -mod=vendor ./internal/api/... ./internal/cmd/...
    TEANODE_TEST_DATABASE_HOST=<postgres address> go test -mod=vendor ./internal/api/v1api/apigraph/ -run 'Assistant' -count=1
    cd web && npx tsc --noEmit -p . && npx eslint src --max-warnings 0 && npx vitest run

A PostgreSQL for the tests is `docker run -d --name dbtest -e POSTGRES_DB=teanode -e POSTGRES_USER=teanode -e POSTGRES_PASSWORD=teanode -e POSTGRES_HOST_AUTH_METHOD=trust pgvector/pgvector:pg17`, addressed by the container's IP address.


## Validation and Acceptance

On a fresh server started with `docker compose -f docker-compose.assistant.yml up -d`, opening http://127.0.0.1:8080/ and creating the account shows the Start your assistant card and nothing else. Choosing OpenAI, pasting a working key and pressing the button shows the three lines ticking, and the chat drawer opens with the agent's greeting, without the person visiting any other page. Pasting a wrong key shows the provider's refusal under the field. `teanode services assistant --kind openai --api-key-file key.txt --restart` does the same from a terminal and prints the chosen models. The go tests named above fail before the change (the mutation does not exist) and pass after.


## Idempotence and Recovery

The operation can be run again: it replaces the provider of the same name and sets the same fields. Choosing another service later adds a second provider and makes it the default; the first stays and can be removed on Manage → Agents → Models. Nothing is deleted.


## Interfaces and Dependencies

In `internal/api/v1api/apigraph/assistant_setup.go`:

    type SetUpAssistantArguments struct {
        Kind    string  `json:"kind"`
        APIKey  *string `json:"apiKey" graphapi:"nullable"`
        BaseURL *string `json:"baseUrl" graphapi:"nullable"`
        Model   *string `json:"model" graphapi:"nullable"`
    }
    type AssistantSetup struct {
        Provider       string `json:"provider"`
        DefaultModel   string `json:"defaultModel"`
        EmbeddingModel string `json:"embeddingModel"`
        IsAgentOn      bool   `json:"isAgentOn"`
        NeedsRestart   bool   `json:"needsRestart"`
    }
    func (self *graph) SetUpAssistant(ctx context.Context, arguments SetUpAssistantArguments) (*AssistantSetup, error)
    func chooseAssistantModels(kind string, isOwnServer bool, names []string) (defaultModel string, embeddingModel string)

In `web/src/pages/welcome.tsx`: `export function WelcomePage()`. In `internal/client/services.go`: `func (self *Client) SetUpAssistant(ctx context.Context, variables map[string]any) (*AssistantSetup, error)`.
