import { graphql } from '../api'

// The people and circles an album can be shared with or made for. The server
// answers only the ones the person shares a circle with, and answers as not
// found for anybody else, so these are the whole of the choices. They are the
// same two queries the calendar and the pages of Knowledge ask.

export interface CirclePerson {
  userId: string
  username: string
  name: string
  avatarUrl: string | null
  avatarColorIndex: number
}

export interface CircleChoice {
  id: string
  name: string
}

export async function listCirclePeople(signal?: AbortSignal): Promise<CirclePerson[]> {
  const answer = await graphql<{ ListCirclePeople: CirclePerson[] }>(
    'query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }',
    {},
    signal,
  )
  return answer.ListCirclePeople
}

export async function listCircleChoices(signal?: AbortSignal): Promise<CircleChoice[]> {
  const answer = await graphql<{ ListCircles: CircleChoice[] }>('query { ListCircles { id name } }', {}, signal)
  return answer.ListCircles
}

// The name to call a person by: the name they gave, else their username.
export const circlePersonName = (person: { name: string; username: string }) => person.name || person.username
