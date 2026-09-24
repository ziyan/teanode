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
  // A tool called, or a question asked, by a program over MCP.
  'mcp',
]
