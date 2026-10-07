import type { Key } from './i18n/i18n'

// The kinds a run can be, for a filter over runs: a page holds one page of
// runs, so the kinds present on it are not the kinds there are. Every model
// call is a run of one of these kinds.
export const RUN_KINDS = [
  'triage',
  'reply',
  'research',
  'summarize',
  'remember',
  'ingest',
  'dream',
  'schedule',
  'extract',
  'draft',
  'describe',
  'compact',
  // A question from a memory evaluation, answered and graded.
  'evaluate',
  // A survey: one run per page it asked, and one that combined them.
  'survey',
  // Work a turn handed to a run of its own.
  'subagent',
  // A tool called, or a question asked, by a program over MCP.
  'mcp',
  'goal',
  'background',
  'speak_first',
  'alert',
  'categorize',
  'statement_import',
  // A message in a mailbox this server does not host, read through a
  // skill and sorted.
  'watch',
]

// runKindLabel is a run's kind in the reader's words. A kind this dashboard
// has no words for, one a newer server added, is shown as the server names
// it: t() gives undefined for a key the catalog lacks.
export function runKindLabel(t: (key: Key) => string, kind: string): string {
  const label: string | undefined = t(`agent.runKinds.${kind}` as Key)
  return label || kind
}
