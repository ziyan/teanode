import { graphql } from '../api'

// The people the signed-in person shares a circle with, as the server lists
// them. Contacts link to one of them ("In your circles as..."), and Photos,
// Circles and Location open each other's pages through that link.

export type CirclePerson = {
  userId: string
  username: string
  name: string
  avatarUrl: string | null
  avatarColorIndex: number
}

// The fields of a person, for a query that nests one (a contact's account).
export const CIRCLE_PERSON_FIELDS = 'userId username name avatarUrl avatarColorIndex'

const LIST_CIRCLE_PEOPLE = `query { ListCirclePeople { ${CIRCLE_PERSON_FIELDS} } }`

export async function listCirclePeople(signal?: AbortSignal): Promise<CirclePerson[]> {
  const answer = await graphql<{ ListCirclePeople: CirclePerson[] | null }>(LIST_CIRCLE_PEOPLE, {}, signal)
  return answer.ListCirclePeople ?? []
}

// Where the photos of one person in the circles are: the library, filtered to
// the contact the caller linked to that account.
export function accountPhotosAddress(username: string): string {
  return `/photos?account=${encodeURIComponent(username)}`
}
