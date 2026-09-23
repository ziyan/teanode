---
name: mattermost-mm
description: A Mattermost server's channels - their posts, threads and files - read with the mm command line tool, signed in as the person.
requires: [mm]

settings:
  - name: teams
    description: the teams whose channels are read, by name; empty reads every team the account is in
    type: array
    items: {type: string, pattern: "^[a-z0-9][a-z0-9_-]*$"}
    default: []
  - name: exclude
    description: channels left unread, by name
    type: array
    items: {type: string, pattern: "^[a-z0-9][a-z0-9_-]*$"}
    default: []
  - name: bots
    description: read what bots and integrations post, as well as people
    type: boolean
    default: false

containers:
  - id: teams
    command: [mm, team, list, --json]
    parse: {json: {items: "."}}
    paging: none
    skip: "!({{settings.teams | empty}} || {{item.name}} in {{settings.teams}})"
    name: "teams/{{item.name}}"
    fields: {team: "{{item.name}}"}
    only: parents           # a team is listed to be walked into, not read

  - over: teams
    command: [mm, channel, list, --team, "{{parent.team}}", --json]
    parse: {json: {items: "."}}
    paging: none
    skip: "{{item.name}} in {{settings.exclude}}"
    name: "posts/{{parent.team}}/{{item.name}}.jsonl"
    fields: {channel: "{{item.id}}", channelName: "{{item.name}}", title: "{{item.display_name}}", private: "{{item.type}} != O", lastPost: "{{item.last_post_at | epoch-ms}}"}

records:
  # Only what changed since the last complete pass over the channel, so a
  # post this pass does not see is kept rather than deleted.
  - command: [mm, post, list, "{{container.channel}}", --since, "{{pass.since}}", --full-id, --json]
    parse: {json: {items: "posts.*"}}
    paging: none
    since: {first: "1970-01-01T00:00:00Z", unchangedWhen: "{{container.lastPost}} <= {{pass.since}}"}
    skip: "{{item.type}} != '' || (!{{settings.bots}} && {{item.props.from_bot}} == true)"
    record:
      id: "{{item.id}}"
      kind: chat
      channel: "{{container.channelName}}"
      thread: "{{item.root_id | or item.id}}"
      at: "{{item.create_at | epoch-ms}}"
      modifiedAt: "{{item.update_at | epoch-ms}}"
      author: "{{response.users[item.user_id].username}}"
      private: "{{container.private}}"
      text: "{{item.message}}"
    attachments:
      each: item.file_ids
      command: [mm, file, download, "{{each}}", --output, "{{output}}"]
      version: "{{each}}"
      maxBytes: 26214400
---

# Mattermost

Reads the channels the person is in on a Mattermost server, through `mm`, which is signed in on their computer as them. A post is a record; replies carry their thread, and TeaNode reads a thread as one conversation.

A pass reads only what was posted or edited since the last complete pass over each channel, and a channel with nothing new is not asked at all (`unchangedWhen`), so a pass over a quiet server is a few seconds of listing. The first pass reads everything.

System messages (joins, leaves, header changes) are left out, and so, unless the setting says otherwise, are posts by bots and integrations, which in a busy server outnumber people.

Files posted with a message are fetched once each, by their identifier, up to 25 MB; pictures and videos are described later by TeaNode itself.

## Document identifiers

A channel is the file `posts/<team>/<channel>.jsonl` and a post is its 26-character identifier within it: the names the records script this replaces used, so a source switched to this type keeps what it has read.

## Checked against the tool

`mm team list --json`, `mm channel list --json` and the shape of `mm post list --json` (an `order` list, a `posts` map by identifier, a `users` map) were read from `mm` 0.5. Not yet checked: that `--since` with no count returns every post since then rather than a first page, that `users` in the answer maps a post's `user_id` to a name, and the arguments of `mm file download`.
