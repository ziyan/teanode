# Working in this repository

Entry point for anyone — human or coding agent — making a change here. Read
`CONTRIBUTING.md` for the conventions; this file is about orientation.

## What this program is

A mail server that receives mail over SMTP for domains you configure,
authenticates it (SPF, DKIM, DMARC, ARC), optionally scans it for viruses and
spam, and forwards it somewhere else — another address, an HTTP webhook, or
another mail server. It also relays authenticated outbound mail from your own
devices.

It is **not** a mailbox server. There is no IMAP and no long-term inbox. Mail
is stored only so the dashboard can show it and so a failed delivery can be
retried.

## Where things are

    main.go                 CLI entry point (urfave/cli v3)
    cmd/                    one file per subcommand: run, config, dkim,
                            credential, password, version
    internal/
      config/               the configuration: types, validation, the Store
                            interface. Everything an operator can set
      configdb/             config.Store backed by PostgreSQL, with a version
                            row so several instances can write safely
      bootstrap/            the environment: how to reach the database, which
                            instance this is, and what to create on a first run
      mx/                   the mail path. Start at exchange.go HandleEnvelope
      db/                   PostgreSQL via GORM: mail, deliveries, DMARC
                            reports, usage counters, templates
      api/                  GraphQL over the config store and the database
      web/                  HTTP server and middlewares
      dns/                  advisory DNS record checking for configured domains
      mailer/               template rendering and transactional send
      models/               structs shared across packages
      util/                 protocol implementations: smtpd, smtpc, dkim, spf,
                            dmarc, arc, mailparse, autoacme, clamav, spamc
    web/                    dashboard source (React, TypeScript, webpack)
    docs/                   see docs/decisions/ for why things are as they are

## How a message flows

1. `internal/util/smtpd` speaks SMTP, optionally does STARTTLS and `AUTH
   PLAIN`, and produces a `mailparse.Envelope`.
2. `mx.exchange.HandleEnvelope` picks one of four paths from the envelope: a
   signed `dsn+`, `rua+` or `ruf+` recipient means a bounce or a DMARC report;
   a credential means outbound submission; otherwise it is inbound mail.
3. Inbound: find the domain in the configuration, run the authentication checks
   in parallel, prepend `Received` and `Authentication-Results`, store the
   mail, match the recipient's local part against that domain's aliases, and
   create one delivery per match.
4. Delivery: sign a bounce return path, add ARC headers, connect out, and on
   failure schedule a retry on a fixed backoff ladder.

Reading those four files in order — `exchange.go`, `exchange_incoming.go`,
`exchange_utils.go`, `exchange_delivery.go` — explains most of the system.

## Ground rules

The invariants in `CONTRIBUTING.md` are the ones that bite. The short version:

- Every migration needs reverse SQL.
- Configuration identifiers never change once generated.
- No cloud client is constructed when its integration is disabled.
- The ACME challenge route comes before authentication.
- Unit tests never touch the network.
- Never commit a secret or a real email address.

## Current state

This repository is being restructured from a hosted service into a
self-hostable open-source server. The plan, its progress, and the surprises
found along the way are in
`docs/planning/active/20260818-open-source-restructure.md`. Read it before
starting anything substantial — it says what is already done, what is
deliberately deferred, and which decisions are settled.
