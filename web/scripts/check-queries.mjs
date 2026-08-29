// Validates every GraphQL query in the dashboard against the running server.
//
// The queries are template literals typed by hand, so nothing checks them
// until somebody opens the page they are on. That is how a query declaring
// $domainId as String, where the schema wants String!, shipped and sat there:
// the only screen that used it was behind a login that was itself broken.
//
// Needs a server to talk to, so it is not part of the build. Run it against a
// development server:
//
//     TEANODE_URL=http://127.0.0.1:8833 node scripts/check-queries.mjs

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const source = join(here, '..', 'src')
const url = (process.env.TEANODE_URL ?? 'http://127.0.0.1:8833').replace(/\/$/, '')

function files(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name)
    return statSync(path).isDirectory() ? files(path) : path.endsWith('.ts') || path.endsWith('.tsx') ? [path] : []
  })
}

// Comments are stripped first. A prose comment that happens to quote a URL in
// backticks is not a query, and treating it as one fails the check for a
// reason that has nothing to do with the schema.
function withoutComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1')
}

// Every backtick string that looks like an operation. Interpolation is allowed
// only for whole selections, which is how SESSION_FIELDS is used; the ${...} is
// replaced with a placeholder selection so the parse still succeeds.
function operations(text) {
  const found = []
  for (const match of withoutComments(text).matchAll(/`([^`]*?(?:query|mutation|\{)[^`]*?)`/gs)) {
    const body = match[1]
    if (!/\b(query|mutation)\b|^\s*\{/.test(body)) {
      continue
    }
    if (!body.includes('{')) {
      continue
    }
    found.push(body.replace(/\$\{[^}]*\}/g, '{ __typename }'))
  }
  return found
}

let checked = 0
let failures = 0

for (const file of files(source)) {
  for (const operation of operations(readFileSync(file, 'utf8'))) {
    // Ask the server to validate without running it. A query with unmet
    // variables still fails validation for the right reasons — an unknown
    // field or a mistyped variable — which is what is being looked for.
    const response = await fetch(`${url}/api/v1/graphql`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: operation, variables: {} }),
    })
    const body = await response.json().catch(() => ({}))
    checked += 1

    for (const error of body.errors ?? []) {
      const message = error.message ?? ''
      // Errors from executing it — not logged in, missing a required value —
      // mean the query itself parsed and type checked, which is all this is
      // asking about.
      if (/not logged in|not found|invalid arguments|must be provided|was not provided/i.test(message)) {
        continue
      }
      console.error(`${file}:\n  ${message}\n  in: ${operation.trim().split('\n')[0]}`)
      failures += 1
    }
  }
}

if (failures > 0) {
  console.error(`\n${failures} problem(s) across ${checked} operations.`)
  process.exit(1)
}
console.log(`${checked} GraphQL operations check out against ${url}`)
