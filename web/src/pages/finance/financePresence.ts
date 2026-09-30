import { graphql } from '../../api'
import { useQuery } from '../../components/useQuery'
import type { FinanceProvider } from './financeApi'

// Whether a person has finance at all, asked by the rail, the Finance page
// and the agent page's Finance tab. On its own, apart from financeApi and
// the sections, because the rail is in the first download and must not bring
// the finance pages into it.

const FINANCE_PRESENCE = `query { FinanceProviders { providerKind } FinanceSources { id } }`

const AGENT_STATE = `query { ReadAgent { agent { id } allowed { enabled } } }`

export type FinancePresence = { isShown: boolean | null; hasSources: boolean }

// useFinancePresence says whether finance is shown: when the operator
// offers a provider to link through, or when the person already has finance
// sources, which stay theirs to see and delete after a provider stops being
// offered. Null while it is being asked. A server without the finance
// operations answers with an error, and then there is no finance.
export function useFinancePresence(isAgentOn: boolean): FinancePresence {
  const { data, error } = useQuery(
    async () =>
      isAgentOn
        ? {
            ...(await graphql<{ FinanceProviders: FinanceProvider[]; FinanceSources: { id: string }[] }>(
              FINANCE_PRESENCE,
            )),
            isAskedForAgent: true,
          }
        : { FinanceProviders: [], FinanceSources: [], isAskedForAgent: false },
    [isAgentOn],
    { refresh: false },
  )
  // Undecided until the question has been answered for the agent that is
  // on. The query's data outlives a change of its inputs, and on the render
  // after the agent loaded it still held the empty answer given while the
  // agent was loading, with nothing marked as loading yet; read as "no
  // finance", it sent a link to the Finance tab to the first tab.
  if (error && isAgentOn) return { isShown: false, hasSources: false }
  if (!isAgentOn || !data?.isAskedForAgent) return { isShown: null, hasSources: false }
  const hasSources = (data?.FinanceSources.length ?? 0) > 0
  return { isShown: (data?.FinanceProviders.length ?? 0) > 0 || hasSources, hasSources }
}

// useAgentFinancePresence is useFinancePresence for a place that has not
// read the agent already: it asks first whether the person's agent is on,
// then whether there is finance. isAsked false asks nothing and answers
// null, for the rail while it shows something other than the account.
export function useAgentFinancePresence(isAsked = true): FinancePresence {
  const agent = useQuery(
    async () =>
      isAsked
        ? {
            ...(await graphql<{ ReadAgent: { agent?: { id: string } | null; allowed: { enabled: boolean } } }>(
              AGENT_STATE,
            )),
            isAsked: true,
          }
        : null,
    [isAsked],
    { refresh: false },
  )
  // The same care as above: an answer left over from before the question
  // was asked is not an answer.
  const view = agent.data?.isAsked ? agent.data.ReadAgent : null
  const isAgentOn = Boolean(view?.allowed.enabled && view.agent)
  const presence = useFinancePresence(isAgentOn)
  if (!isAsked) return { isShown: null, hasSources: false }
  if (agent.error) return { isShown: false, hasSources: false }
  if (!view) return { isShown: null, hasSources: false }
  if (!isAgentOn) return { isShown: false, hasSources: false }
  return presence
}
