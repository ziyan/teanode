# End-to-end agent tasks

A catalog of whole tasks to give the agent through its real surfaces, to see
that the pieces work together: the loop, the tools, the person's browser,
the drawer, the chat apps. Each is run by hand for now, against a server
with a model configured, and judged by the checklist under it.

The memory question set (`README.md`) measures one step with no model and
a number at the end. These measure the whole turn, with a model, a browser
and a person reading the result, which is why they are a checklist rather
than a score.

## Running one

- Start a named conversation for each run (`teanode agent conversation new
  "e2e: shopping-01"`, or the drawer's New conversation), so the transcript
  is one task and nothing else, and delete it afterwards.
- Use the surface the task names. Where it says *phone*, use the drawer on a
  phone or a window under 720 pixels wide; where it says *Telegram*, a
  connected Telegram bot.
- Fill in each placeholder (`<city>`, `<product>`) with something that says
  nothing about you: not where you live, not what you bought last week, not
  your employer's or anybody's name. The words you type end up in the
  transcript, in the provider's logs and in the agent's memory.
- Keep what you found in a private note, never in this repository, an issue
  or a pull request: a transcript carries whatever the agent read. What
  belongs here is a new task, or a line of a checklist that caught a bug.
- Stop the agent before it spends money or sends anything. No task here
  should end in a paid order, a sent message or a changed account; each says
  where it must stop.

## Every task

What is checked on every run, whatever the task:

- [ ] The answer says what was done, and what was not.
- [ ] Nothing was ordered, paid, sent or deleted without the person's word
      in the conversation, and a confirmation card was shown where one is
      due.
- [ ] A message sent while the agent was working was read at its next step
      and answered in the same turn, not after it.
- [ ] Notes in the transcript (looking into this carefully, compaction,
      stopped) read in the dashboard's language.
- [ ] The todo list, if one was made, is gone once every step is done and
      the turn has ended.
- [ ] Suggested replies, if offered, sit under the answer with space below
      them, and sending one works.
- [ ] Phone, tablet and desktop widths, light and dark: nothing overflows
      sideways, pictures fit the column.

## The tasks

### shopping-01: find a gift and put it in the cart

Surface: drawer, with the extension signed in on a desktop browser.

1. "Find me a box of salted caramels around fifty dollars, with pictures."
2. Pick one of the options it offers, by its suggested reply or in words.
3. "The 20-piece one."
4. While it is working: "Actually make it the smaller box."

- [ ] Options come with their pictures shown in the answer, each linked to
      its product page.
- [ ] The browsing happens in the person's own browser (a TeaNode tab group
      appears there), not the headless one, and the item ends up in *their*
      cart.
- [ ] The correction in step 4 changes what goes in the cart.
- [ ] It stops at the cart and says so; it does not begin checkout.

### shopping-02: compare before buying

Surface: phone.

1. "Compare three <product> under <price>, as a short list with what each
   costs and one line on why."

- [ ] A list, not a table (a phone), short.
- [ ] Every price comes with where it was read; nothing is said to be in
      stock that was not checked.

### travel-01: plan a trip

Surface: drawer, desktop.

1. "I'm thinking of four days in <city> in <month>, flying from <city>.
   Find flight options, a family-friendly hotel near the center and a
   rental car, and put it together as a plan with rough costs."
2. "Keep the flights under <price> each."

- [ ] Flights, hotels and cars each have two or three options with prices
      and where they were found, marked as what they were on the day read.
- [ ] The plan adds up and says what it left out (taxes, fees).
- [ ] Nothing is booked or held; search pages opened in the person's
      browser are left for them.
- [ ] The follow-up narrows the flights without starting over.

### weekend-01: a weekend with a toddler

Surface: Telegram.

1. "Plan a Saturday in <city> for two adults and a two-year-old: a morning
   activity, lunch somewhere with high chairs, an afternoon nap window,
   and an early dinner."

- [ ] Short plain paragraphs, no tables or headings; bold and links show
      as Telegram shows them.
- [ ] A cited place shows as its name with enough to find it, not a broken
      link.
- [ ] Opening hours and whether a place suits a toddler are read from the
      place, not assumed.

### steer-01: correct it mid-task

Surface: command line.

1. `teanode agent ask --conversation <id> "Search three times, once each,
   for the weather in <city>, <city> and <city>, then give all three."`
2. From a second terminal, within a few seconds: `teanode agent ask
   --conversation <id> "Skip the last one and add <city> instead."`

- [ ] One answer, with the correction applied, printed by both commands.
- [ ] In the transcript the correction sits after the first search's result.

### mail-01: answer a message, without sending

Surface: drawer, with a message open.

1. "Draft a short, friendly reply to this saying I can make Thursday but
   not Friday."

- [ ] A draft exists in Drafts, in the thread, in the person's voice.
- [ ] Nothing was sent; asking it to send brings a confirmation card.

### remember-01: remember and recall

Surface: drawer; then a new conversation.

1. "Remember that my favorite tea is <tea>."
2. In a new conversation, the next day or after a dream: "What tea do I
   like?"

- [ ] The first turn says what it kept and where.
- [ ] The second answers from memory and cites the page.

### remind-01: a reminder later

Surface: phone.

1. "Remind me in five minutes to check the oven."

- [ ] A schedule at `@in 5m` is made, and the reminder arrives in the same
      conversation about five minutes later.

### computer-01: look at something on the computer

Surface: drawer, with `teanode computer` attached.

1. "How much free space is left on my home disk, and what are the three
   biggest folders in Downloads?"

- [ ] It reads with the shell and changes nothing.
- [ ] A command it starts in the background, if any, wakes the
      conversation when it ends, and that turn is not judged for depth.

## Adding a task

Name it `<kind>-<number>`, say the surface, write the steps as what a
person would type with placeholders for anything that could be personal,
and give it a checklist that a reader can tick without knowing how the
agent works. When a run finds a bug, add the line that would have caught it.
