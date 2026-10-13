import { graphql } from '../api'

// Fields a server older than this dashboard does not have yet. A query that
// names one is refused whole, so the page asks with them first and, when the
// server says it does not know one, asks again without and remembers.
const OPTIONAL_FIELD_PATTERN = /\b(recordingSummary|recordingTitleSource|isAccountOwner)\b/
const OPTIONAL_FIELD_TOKENS = / (recordingSummary|recordingTitleSource|isAccountOwner)(?=[ }])/g
let isServerMissingOptionalFields = false

export const withoutOptionalFields = (document: string) => document.replace(OPTIONAL_FIELD_TOKENS, '')

export async function graphqlWithOptionalFields<T>(
  document: string,
  variables: Record<string, unknown> = {},
  signal?: AbortSignal,
): Promise<T> {
  if (isServerMissingOptionalFields) return graphql<T>(withoutOptionalFields(document), variables, signal)
  try {
    return await graphql<T>(document, variables, signal)
  } catch (caught) {
    if (
      !document.trimStart().startsWith('query') ||
      !(caught instanceof Error) ||
      !OPTIONAL_FIELD_PATTERN.test(caught.message)
    )
      throw caught
    isServerMissingOptionalFields = true
    return graphql<T>(withoutOptionalFields(document), variables, signal)
  }
}

export const resetOptionalFieldsForTest = () => {
  isServerMissingOptionalFields = false
}
