import { graphql } from '../api'
import { useQuery } from './useQuery'

// Whether any of the person's computers reports herdr, asked by the rail
// to decide whether to show the Coding row. On its own, apart from
// herdrSessions, because the rail is in the first download and must not bring
// the Coding page into it. Asks only for the computers' names, once.

const HERDR_PRESENCE = `query { ListAgentHerdrSessions { computerNames } }`

// useHerdrPresence says whether a computer reports herdr: null while it is
// being asked, and false when it is not asked or the server cannot say.
export function useHerdrPresence(isAsked: boolean): boolean | null {
  const { data, error } = useQuery(
    async () =>
      isAsked
        ? { ...(await graphql<{ ListAgentHerdrSessions: { computerNames: string[] } }>(HERDR_PRESENCE)), isAsked: true }
        : { ListAgentHerdrSessions: { computerNames: [] as string[] }, isAsked: false },
    [isAsked],
    { refresh: false },
  )
  if (!isAsked || error) return false
  if (!data?.isAsked) return null
  return data.ListAgentHerdrSessions.computerNames.length > 0
}
