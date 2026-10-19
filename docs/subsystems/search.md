# Search across everything

One search that looks for a person's words in nine places at once: their chats,
mail, contacts, photos, memory, documents, recordings, places and finance. The
dashboard's search box, `teanode search` and the agent's `search` tool are the
same GraphQL operation, `SearchEverything(words, perAreaLimit, areas)`.

It adds no index and asks no model. Each area is the search that area already
has, run by its words alone, and what it finds is a few results with a way to
open them. Calendar events, reminders and todos are not searched: they have no
text search to reuse, and events live in CalDAV objects.

## The areas

The order is fixed, whatever order they finish in. Each is asked for one more
result than will be shown, which is how a group says it has more without
counting.

| Area | Searched with | Opens by |
| --- | --- | --- |
| `chats` | `SearchAgentConversations`: the person's conversations with their agent, by title, summary and what was said | the conversation |
| `mail` | `ListMailboxThreads` over each of the person's mailboxes, all of their folders, notes left out | the mail item |
| `contacts` | `ListContacts` over each address book: name, organization, address, number | the contact |
| `photos` | `ListPhotos` with the words and no meaning vector, so no embedding model is called | the photo |
| `memory` | `SearchAgentGraphFrom`: pages first, then facts, a page once | the page path |
| `documents` | the knowledge search (`indexed.Search`) with no embedding model, a document once however many passages matched | the document |
| `recordings` | `ListRecordings`: title, summary, transcript | the recording |
| `places` | `ListPlaces`: the person's own places by name and address | the place |
| `finance` | `ListFinanceTransactions`: description and merchant, copies of a transaction left out | the transaction |

The chats between people are not in `chats`: listing them first makes the rows
they are listed from, and a search writes nothing.

## Who may search what

Each area is searched under the same check as the area's own page, by calling
the same helper (`requireConversationPerson`, `requireMailbox`,
`requireAddressBookPerson`, `requirePhotosPerson`, `requireAgentPerson`,
`requireLocationPerson`, and `requirePermission` for recordings; finance also
needs a finance provider offered). The helpers are in `internal/api/v1api/apigraph`,
so the areas are built there (`search.go`), and `internal/search` knows nothing
of access: it runs the searchers it is given. One place decides each area.

An area the person may not use (the permission, the service off, no agent that
is on) is left out as if it had not been asked for, because a group that lists
what they cannot use tells them it exists. It is not in `failedAreas`. The
agent's tool is held to the agent's permissions the same way, so a turn limited
to photos searches photos only.

## Time and failure

Every area runs in its own goroutine, in a transaction of its own (the request's
single transaction cannot be shared by queries running side by side, which is
why `SearchEverything` is in `isModelBackedQuery`: it asks no model, but it
holds no transaction across its work either). Each has three seconds. The
transaction is bound to that time, so a query out of time is cancelled, and
`Everything` stops waiting at three seconds even for an area that ignores it.

An area that errors, panics or runs out of time goes in `failedAreas` and the
rest are kept. A search that cannot run at all (an area that does not exist,
the caller gone) is an error.

## What comes back

`Results` is the words, `Areas` (only areas with results, in the fixed order,
each with its `Results` and `HasMore`) and `FailedAreas`. A `Result` is its
area, a `TargetID` that opens it where it lives, a title, a detail line (a
snippet of 200 characters at most, a sender, an amount) and when it happened.
Five results an area by default, 20 at most. `HasMore` says the area's own
search finds more; `teanode search` prints the command for it and the agent's
answer names the area's own tool.

The words are matched as the areas match them: a `%` or `_` in them means
itself (`SearchAgentConversations` escapes them like the other searches do).
Words alone find a name or a phrase, not a paraphrase.
