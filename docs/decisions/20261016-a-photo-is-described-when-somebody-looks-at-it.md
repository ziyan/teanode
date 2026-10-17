# A photo is described when somebody looks at it, and the description is kept

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

Finding "the picture of the receipt from the hardware store" needs words
about what is in a picture. A language model that can see pictures can
write them, at a price per picture. Describing a whole library up front
costs as much as the library is large, for pictures nobody will ask about.

## Decision

A photo is described only when somebody works with it: the agent looks at
it, or the person asks for a description in the viewer. The description,
the words the model read in the picture, and the model that wrote them are
kept on the photo, embedded like other text, and searched from then on. The
agent's scan model, the cheap one it uses for work nobody waits on, writes it.

Before a photo is described it is still found by everything the server
reads without a model: when and where it was taken, the place's name, the
camera, its albums, and the people whose faces are in it.

## Consequences

A search can miss a photo that nobody has described. The agent is told so,
and narrows by date, place, album or person first, then looks at the
candidates, which describes them for next time.

Describing needs the agent to be on, since the model and its spending are
the agent's.
