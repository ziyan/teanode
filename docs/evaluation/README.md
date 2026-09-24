# The memory question set

A fixed set of questions, replayed through the agent's recall against a
real graph, to say in numbers whether the facts a question needs are the
facts the agent would have been given.

    teanode agent memory evaluate docs/evaluation/memory-questions.json

Each question is put through the same two steps a turn takes — the fused
search over pages and facts, then the choice of what fits the recall
budget — and nothing else. No model is asked anything, nothing in the
graph is marked as used, and the same graph gives the same answer twice.
That is what makes it cheap enough to run before and after a night, or
against a snapshot restored into a development database, and to compare
the two totals.

What it does not measure is the answer. Whether the model then used what
it was handed is a second evaluation, with a grader and a bill. This one
says whether it had the facts at all, which is the failure worth catching
first: an answer built from what the agent never recalled is wrong for
reasons no prompt can fix.

## The file

A JSON list. JSON has no comments, which is why the shape is written here
rather than at the top of the file.

```json
[
  {
    "id": "direct-01",
    "question": "what does Alice Chen do?",
    "kind": "direct",
    "expects": [{ "path": "people/alice-chen", "words": ["platform"] }],
    "forbids": []
  }
]
```

| Field | What it is |
| --- | --- |
| `id` | how the row and the failure are named; unique in the file |
| `question` | the words a person would type, sent to recall as typed |
| `kind` | one of `direct`, `paraphrase`, `changed`, `multihop`, `abstain` |
| `expects` | the facts recall has to carry for the question to be a hit |
| `forbids` | the facts it must not carry |

An entry of `expects` or `forbids` is a *claim*: a `path`, which is the
page the fact sits on, and `words`, all of which must appear in one
carried fact on that page. Words are compared without case, and every
word has to be in the *same* fact — two words from two different facts on
one page are not the fact the question was about. A claim with no `words`
is about the page alone: anything carried from it satisfies it, which is
how an abstain question says "nothing from here".

A question is a hit when every claim in `expects` was carried and no
claim in `forbids` was. The command prints a row per question and totals
per kind, exits non-zero when anything missed, and prints the same thing
as JSON with `--json`.

## The kinds

- **direct** — the question uses the words of the fact itself. If this
  misses, the search is broken.
- **paraphrase** — the same thing asked another way, with none of the
  fact's own words. This is what the embedding half of the search is for.
- **changed** — something that was corrected. `expects` is the statement
  that stands; `forbids` is the one it replaced. A graph that carries
  both has learned the correction without forgetting the mistake, and the
  model will pick one at random.
- **multihop** — needs facts from two pages, so it carries two claims on
  two paths. There is no hop along links in recall today; these questions
  are the measure of what that would be worth.
- **abstain** — there is nothing to carry and nothing should be. It
  catches memory that answers anyway: a question about a person the agent
  does not know that comes back full of the nearest person it does.

## The starter set is examples

`memory-questions.json` holds ten examples, two of each kind, about
invented people and projects. They are here to show the shape and to give
the command something to run; they will miss against any real graph,
because no graph has those pages.

Replace them. A question set is worth what its questions are worth, and
the ones that measure anything come from your own graph: things you have
actually asked the agent, corrections you actually made — a moved house,
a changed job, a project renamed — and people it should have nothing to
say about. Fifty is enough to see a change between two nights, ten of
each kind.

To write one, ask the graph what it holds (`teanode agent memory get
people/…`), pick the fact, then write the question the way you would have
asked it rather than the way the fact is worded.

## Grading the answers

Recall says whether the facts reached the model. Whether the answer came
out right is a second question, with a model and a bill:

    teanode agent memory answers <file> --from memory,sources,both

Each question is answered once for each source named, with the model a
conversation uses and no tools:

- **memory** — from the facts recall carries, as a turn gets them.
- **sources** — from the passages the document search finds, as the
  search tool returns them.
- **both** — from both, as a turn that searches has them.

The answer is then graded against the one the person gave, by the same
model, as one of `correct`, `partial`, `not_known`, `stale` (it gave the
answer that was true once), `invented` (it answered where the truth is
"not known", or contradicted the right answer) or `wrong`. A grader whose
answer cannot be read is `ungraded`, and counts as nothing rather than as
a wrong answer the model never gave. An answer of "not known" is not put
to the grader: it is `not_known` where the expected answer is "not known"
too, and `missed` everywhere else, because then what the model was given
did not hold the fact. A miss is recall's failure and a wrong answer is
the model's, and the two are worked on apart.

Each source's score counts a right answer, and a right "not known" to an
abstain question, as one, and a partial answer as a half. Comparing the
three says what the extracted memory is worth over the documents it was
read from. The answers and the grades are runs of kind `evaluate`: listed
and priced with the rest, and never written to a conversation or to the
graph.

A question takes part when it has an `expectedAnswer`, and a changed one
may say what used to be true:

```json
{
  "id": "changed-01",
  "question": "where does Alice Chen live?",
  "kind": "changed",
  "expects": [],
  "forbids": [],
  "expectedAnswer": "Lisbon, since March.",
  "outdatedAnswer": "Berlin."
}
```

An abstain question's `expectedAnswer` is `not known`.

The questions that measure anything are about your own life, so keep that
set out of any repository: `~/.config/teanode/evaluation/` is a good place
for it, readable by you alone.
