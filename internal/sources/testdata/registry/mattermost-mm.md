---
name: mattermost-mm
description: A Mattermost server's channels - their posts, threads and files - from the copy the mm command line tool keeps with mm archive, brought up to date at the start of every pass.
requires: [mm]

settings:
  - name: archive
    description: the directory mm archive keeps its copy in
    type: path
    default: "~/mattermost-archive"
  - name: sync
    description: bring the copy up to date with mm archive sync before reading it; off reads the copy as it is
    type: boolean
    default: true
  - name: channels
    description: which channels a sync reads - mine, public or all
    type: string
    pattern: "^(mine|public|all)$"
    default: mine
  - name: files
    description: which attachments a sync downloads - none, mine or all
    type: string
    pattern: "^(none|mine|all)$"
    default: mine
  - name: largestFileMB
    description: attachments larger than this many megabytes are not downloaded
    type: integer
    minimum: 1
    maximum: 1024
    default: 10

refresh:
  # Incremental: each channel is asked only for what is newer than the
  # last post the copy holds. Channels listed by mm archive exclude are
  # left alone.
  - when: "{{settings.sync}}"
    command: [mm, archive, sync, "{{settings.archive}}", --channels, "{{settings.channels}}", --files, "{{settings.files}}", --max-file-mb, "{{settings.largestFileMB}}"]

lookups:
  users: {file: "{{settings.archive}}/users.json", parse: json, key: "{{item.id}}", value: "{{item.username}}"}
  channels: {file: "{{settings.archive}}/channels.json", parse: json, key: "{{item.id}}"}
  # An attachment is kept as <file id>__<its name>.
  stored: {files: {in: "{{settings.archive}}/files", match: "*"}, key: "{{item.name | before '__'}}", value: "{{item.name}}"}

containers:
  - files: {in: "{{settings.archive}}", match: "posts/*/*.jsonl"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", team: "{{item.directory | after 'posts/'}}", channel: "{{item.stem}}"}

records:
  - file: "{{settings.archive}}/{{container.path}}"
    parse: jsonl
    # A channel that is mostly an integration talking to itself is left out.
    dropWhenMostly: {items: "{{item.type}} == slack_attachment", share: 0.8}
    skip: "{{item.type}} matches ^system_ || {{item.type}} == slack_attachment || {{item.message | empty}}"
    record:
      id: "{{item.id}}"
      kind: chat
      channel: "{{container.channel}}"
      thread: "{{item.root_id}}{{item.id | if item.reply_count | unless item.root_id}}"
      # In the computer's own zone, as the hour of a post is read.
      at: "{{item.create_at | epoch-ms | local-time}}"
      author: "{{lookup.users[item.user_id] | or 'somebody'}}"
      private: "{{lookup.channels[item.channel_id].type}} in [P, D, G]"
      text: "{{item.message}}"
    metadata:
      team: "{{container.team}}"
      purpose: "{{lookup.channels[item.channel_id].purpose}}"
    attachments:
      each: item.file_ids
      path: "{{settings.archive}}/files/{{lookup.stored[each]}}"
      name: "{{lookup.stored[each] | after '__'}}"
---

# Mattermost

Reads the channels the person is in on a Mattermost server from the copy `mm archive` keeps on their computer. Every pass first runs `mm archive sync`, which asks each channel only for posts newer than the copy holds, so a pass over a quiet server is quick; the first sync reads everything and can take hours, and a pass that runs out of time goes on from where it stopped.

The copy is the person's own: `mm archive search` reads it without the server, and `mm archive exclude` names the channels a sync leaves alone. Turning `sync` off reads the copy as it is, for a copy some other schedule keeps.

A post is a record; replies carry their thread, and TeaNode reads a thread as one conversation. System messages (joins, leaves, header changes) and integration posts are left out, and so is a channel whose posts are mostly an integration's. Files posted with a message are named where the archive keeps them; how large a file TeaNode reads is its own setting.

## Document identifiers

A channel is the file the archive keeps it in, `posts/<team>/<channel>.jsonl`; direct messages are under the team `direct` and group messages under `group`. A post is its 26-character identifier within it.

## Checked against the tool

Read against `mm` 0.5's archive layout: `posts/`, `users.json`, `channels.json` and `files/<file id>__<name>`.
