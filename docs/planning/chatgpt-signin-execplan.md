# Signing in with a ChatGPT plan from the dashboard

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

An operator who pays for a ChatGPT plan should be able to run the agent on that plan instead of buying API credits, without editing configuration by hand. The server already speaks to the plan: the `openai-codex` provider kind trades a refresh token for access tokens and sends Responses requests to the plan's own address. What it lacks is a way in. Today the refresh token comes from `teanode agent signin` on the operator's own machine, which prints it for pasting into the configuration. Saving providers from the dashboard then drops that token. And a token the service rotates is lost at the next restart.

After this change, the operator opens Settings, Agent, Providers and adds a provider of kind `openai-codex`. They press "Sign in with ChatGPT" and are shown a one-time code and a link to OpenAI's device page. They sign in there on any device and enter the code. The dashboard then says which account and plan it signed in to. From then on the provider's model can be assigned to any work except embeddings, and a rotated refresh token is written back to the configuration so the sign-in survives restarts.

## Progress

- [x] (2026-09-26) Settings keep a signed-in provider's refresh token and account through a save from the dashboard, show whether it is signed in, offer the kind, and list its models.
- [x] (2026-09-26) A rotated refresh token is written back to the configuration, and a changed one is adopted by the running provider without a restart.
- [x] (2026-09-26) Device-code sign-in in `internal/llm` (request a code, wait for it, redeem it).
- [x] (2026-09-26) GraphQL mutations to begin and finish a sign-in for a named provider, needing `server:manage`.
- [x] (2026-09-26) Dashboard: the kind in the provider dialog, a sign-in button showing the code and link, the row saying who is signed in.
- [x] (2026-09-26) Docs: configuration, a note on embeddings.
- [x] (2026-09-26) End to end on the development server with a real plan: signed in with a code through the mutations, restarted to load the new provider, the server wrote back the first rotated refresh token a minute later, and a turn on `chatgpt:gpt-5.5` was answered.
- [ ] The dialog itself, clicked through in a browser (the browser session was stuck at a narrow window; the mutations it calls were exercised directly).

## Surprises & Discoveries

- Observation: provider settings are read only at startup (`internal/cmd/server/run.go`, `startupOnly`), so any change to `agent.providers`, including a new refresh token, asks for a restart.
  Evidence: `"agent.providers": configuration.Agent.Providers` in the startup-only list.
- Observation: the Codex command line has a device-code sign-in for machines without a browser, against the same client.
  Evidence: the binary names `/api/accounts/deviceauth/usercode`, `/deviceauth/token`, `/deviceauth/callback` and `/codex/device`, and says "device code login is not enabled" when the account has not allowed it.

- Observation: the plan's endpoint refuses `max_output_tokens`, which every turn has sent since turns were bounded by effort, so no turn on the plan was answered.
  Evidence: "the provider answered 400: Unsupported parameter: max_output_tokens". Only the keyed Responses endpoint is sent the limit now.

## Decision Log

- Decision: the dashboard signs in with the device-code flow, not the browser redirect.
  Rationale: the redirect is registered to `http://localhost:1455`, which only works on the machine running the browser; a server is somewhere else. The device flow needs nothing but a code typed on OpenAI's page, from any device.
  Date/Author: 2026-09-26, agent.
- Decision: the refresh token is excluded from the startup-only comparison, and the running provider adopts a changed one when the configuration changes.
  Rationale: a service that rotates the token on every refresh would otherwise ask for a restart every hour, and a sign-in to an existing provider would not take effect until one.
  Date/Author: 2026-09-26, agent.
- Decision: a sign-in waiting for its code is held in the memory of the instance that began it.
  Rationale: it lives fifteen minutes and holds nothing worth keeping; a multi-instance deployment whose dashboard requests land on another instance is told to start again.
  Date/Author: 2026-09-26, agent.

## Outcomes & Retrospective

An operator can sign a provider in to a ChatGPT plan without editing configuration, and the sign-in survives dashboard saves, rotations and restarts. Adding a new provider still waits for a restart, as every provider does; a new sign-in to an existing one does not. The plan offers one model and no embeddings.

## Context and Orientation

`internal/llm/codex.go` is the provider for the plan: `newCodex(baseUrl, refreshToken, account, client)`, requests to `https://chatgpt.com/backend-api/codex/responses`, one model (`gpt-5.5`). `internal/llm/signin.go` holds a refresh token and trades it for access tokens (`signIn.token`), calling `rotated` when the service answers with a new refresh token. `internal/llm/signin_flow.go` is the browser sign-in used by `teanode agent signin` (`internal/cmd/agent_signin.go`). `internal/llm/registry.go` builds one client per enabled provider at startup (`Open`).

The configuration lives in the database, one YAML row per section (`internal/config/dbstore.go`), changed through `store.Update(func(*config.Configuration) error)`. `internal/config/agent.go` declares `AgentProvider` with `APIKey`, `RefreshToken`, `Account`. The dashboard's settings API is `internal/api/v1api/apigraph/settings_agent.go`: `AgentProviderSettings` is a provider as shown, the providers list is replaced whole on save, and `listProviderModels` builds a keyed client to list models. The dashboard page is `web/src/pages/settings/agentSettings.tsx` (`ProvidersSection`, `ProviderDialog`).

## Plan of Work

First the settings. In `settings_agent.go`, add `HasRefreshToken`, `Account` to `AgentProviderSettings`; when the providers list is replaced, keep each provider's `RefreshToken` and `Account` the way the API key is kept; offer `openai-codex` in `Kinds`; and list a signed-in provider's models through `llm.NewSignedInProvider`.

Then the token's life. `Registry` gains `AdoptRefreshToken(provider, token string)`, which hands a changed token to that provider's `signIn` (a no-op when it is the one held). The server subscribes to configuration changes and calls it for each signed-in provider, and gives the registry a callback that writes a rotated token back with `store.Update`. `startupOnly` compares providers with refresh tokens blanked.

Then the sign-in. `internal/llm/signin_device.go`: `BeginDeviceSignIn(ctx, kind)` asks for a code and returns `DeviceSignIn{UserCode, VerificationAddress, ExpiresAt}`; `(*DeviceSignIn).Wait(ctx)` polls at the interval the service gives until the code is used, then redeems the authorization code and returns a `SignInResult`. In the API, `BeginAgentProviderSignIn(provider)` starts one and keeps it by a random id; `FinishAgentProviderSignIn(id)` waits up to thirty seconds and either answers "still waiting" or saves the token and account to that provider and answers with the account and plan.

Then the page: for the kind `openai-codex`, the dialog hides the key and base address and shows the sign-in; the row says "signed in, <plan>" or "not signed in".

## Concrete Steps

From the repository root: `make test`, `make lint-ci`, `make web && make build`, then the deploy recipe in `docs/reference/deployment.md`.

## Validation and Acceptance

On the development server, add a provider `chatgpt` of kind `openai-codex`, sign in with the device code, restart when asked, assign `chatgpt:gpt-5.5` to the fast work, and ask the agent something: the answer comes back and the usage shows the model. Save the providers again from the dashboard and confirm the provider still answers. Unit tests: the settings keep the token through a save; the device flow against a fake service (pending, then success; expired); adopting a token drops the held access token.

## Idempotence and Recovery

A sign-in can be started again at any time; the last one to finish wins. Removing the provider removes the token.

## Interfaces and Dependencies

In `internal/llm/signin_device.go`:

    type DeviceSignIn struct { UserCode, VerificationAddress string; ExpiresAt time.Time }
    func BeginDeviceSignIn(ctx context.Context, kind string) (*DeviceSignIn, error)
    func (self *DeviceSignIn) Wait(ctx context.Context) (*SignInResult, error)  // ErrSignInPending when ctx ends first

In `internal/llm/registry.go`:

    func (self *Registry) AdoptRefreshToken(provider, refreshToken string)
