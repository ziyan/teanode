# A background command wakes the conversation that started it

- Status: accepted
- Date: 2026-09-23
- Deciders: the-owner

## Context

The shell tool ran a command and waited for it, at most ten minutes, and
killed it when that time was up. A build, a test suite or a download that
took longer was thrown away when the wait ran out, and the model's usual
answer to that was to run it again with a longer timeout. A wait for
something to happen, such as a loop that checks a condition and sleeps, held
the whole turn while it waited. And a command that ended after the turn was
over had nobody to tell.

## Decision

The program on the computer keeps commands running in the background: one
the agent starts that way, and one still running when its call's wait runs
out, which goes on instead of being killed. It keeps the first and the last
of each stream, answers requests to read, list and stop them, and says when
one ends without being asked.

The program holds them, not the connection, and it is the only record of
them. Each is started with an origin the server wrote: the agent and the
conversation that started it, and whether that turn had somebody present.
The ending carries the origin back, and the program says it again after every
reconnect until the server acknowledges it. The server keeps nothing about
them in its database.

An ending wakes the conversation named in its origin: a turn begins with a
message marked `[background command]`, saying how the command ended and
carrying the end of its output fenced as untrusted data. The turn is not
headless. It carries on from a turn the person took, the way a turn of
theirs that runs on after they look away is still theirs, so it has the
same tools, and its confirmation cards are shown in the drawer and wait for
an answer as any card does.

Three things bound it. A turn with nobody present never leaves a command
running: past its wait the command is killed as before, and it cannot start
one in the background. An ending from such a turn, if there ever is one,
wakes nothing. And a conversation takes at most twenty woken turns before the
person writes there again; after that, endings are written into the
transcript as a note and wake nothing.

## Consequences

A slow command no longer costs the work it had done, and the model can start
several at once and end its turn instead of waiting, because it will be told.
This is also the answer to tool calls in one round running one after the
other: the loop still runs them in order, and what is slow goes in the
background.

A turn can start with nobody watching and use the person's computer. That is
new: until now only the night could. It is bounded by the rules above, not by
a card, and the twenty-turn limit is what stops a loop that starts a command
that wakes a turn that starts another.

A computer whose program predates this keeps the old behavior. The hello
names the feature; the server asks for background commands only from a
program that names it, and the protocol version is unchanged, so an update of
either side does not force the other.

Endings are acknowledged when the woken turn is over, not when it starts. A
server that stops in between hears the ending again and wakes the
conversation twice, which is better than not at all.

Computers are attached to one instance. An ending reaches the instance that
holds the connection, and the woken turn runs there, like every other use of
the computer.

A background command stops when the program on the computer stops, and after
twenty-four hours in any case. Ended ones stay readable for a day.
