# The agent's face is drawn from a description, and moves with its run

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

The agent has a name and a voice and no face. The drawer's header shows a
fixed spark icon, the iPhone's agent bubble shows nothing, and people are
shown by their initial. Once several people and several agents are in one
chat, a reader has to tell them apart at a glance, and see which agent is
busy.

The obvious way to give an agent a face is to generate a picture with an
image model. Nothing in `internal/llm` generates images today; it would
need a provider the operator configured, the picture could not be
animated, and every regeneration would be a different creature.

The runs already say what the agent is doing: `asked`, `text`,
`tool_call`, `confirmation`, `question`, `done`, `error`, and the iPhone's
Live Activity already reduces them to waiting, working, done and failed
(`activityStateOf` in `internal/agent/live_activity.go`).

## Decision

An agent's **face** is a small description, not a picture: a creature from
a fixed set drawn for this program, its colors from the dashboard's
palette, an accessory and an expression. The dashboard and the iPhone app
draw it as vector shapes from one shared set of drawings, so it is sharp
at every size and the same everywhere.

A model writes the first description from the agent's name and
instructions, with structured output against the description's schema, on
the model the operator already configured. The person can describe it in
words ("an owl with glasses"), which the model turns into a new
description, or pick each part by hand. Without a model, a face is picked
from the agent's id.

The face moves with the agent's **activity state**: `idle`, `thinking`
(asked, nothing said yet), `speaking` (an answer streaming), `working` (a
tool call), `waiting` (a card or a question), `failed`, `dreaming` (a dream
running), and `away` (the agent is off). The owner's surfaces add which
kind of tool it is using; everybody else sees only the state. Motion stops
for people who asked their device to reduce it, and the state is then a
word beside the face.

People get a picture they upload, cropped square and stripped of its
metadata, with their initial on a color taken from their id as the
fallback.

## Consequences

`agent.face` (a JSON column) and `user.avatar_*` columns; one set of
drawings checked into the repository and copied into the iPhone app, with
a test that the two copies are equal.

A face is a cartoon, by design. A person who wants a photograph of
something for their agent can upload one instead, and it is shown still,
with the state as a ring around it.

Generating a picture with an image model stays possible later, as a
portrait drawn from the same description, and does not replace the face
that moves.
