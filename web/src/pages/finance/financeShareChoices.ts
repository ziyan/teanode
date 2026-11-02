import { graphql } from '../../api'
import { personLabel } from '../../components/personLabel'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'

// Who a finance share can be with, found through circles: the people the
// person shares a circle with, and their circles. Shared by sharing one
// account and sharing all of one's finance, which pick the same way.

export type Person = { userId: string; username: string; name: string }
export type CircleChoice = { id: string; name: string; members: { userId: string }[] }
export type ShareChoices = { people: Person[]; circles: CircleChoice[] }

const LIST_PEOPLE = `query { ListCirclePeople { userId username name } }`
const LIST_CIRCLES = `query { ListCircles { id name members { userId } } }`

// useShareChoices reads the people and circles to offer. A person without
// circle:use gets none and no failure.
export function useShareChoices(canUseCircles: boolean) {
  return useQuery(
    async (): Promise<ShareChoices> => {
      if (!canUseCircles) return { people: [], circles: [] }
      const [people, circles] = await Promise.all([
        graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE),
        graphql<{ ListCircles: CircleChoice[] }>(LIST_CIRCLES),
      ])
      return { people: people.ListCirclePeople, circles: circles.ListCircles }
    },
    [canUseCircles],
    { refresh: false },
  )
}

// useGranteeOptions is the choices of a "Share with" menu: people, then
// circles, a circle saying how many people it is so two circles with one name
// can be told apart, as they are on the command line.
export function useGranteeOptions(people: Person[], circles: CircleChoice[]) {
  const { t, plural } = useTranslation()
  return [
    ...people.map((person) => ({
      value: `user:${person.userId}`,
      label: personLabel(person),
      group: t('finance.sharePeople'),
    })),
    ...circles.map((circle) => ({
      value: `circle:${circle.id}`,
      label: t('finance.shareCircleOption', {
        name: circle.name,
        people: plural(
          circle.members.length,
          { one: 'circles.memberCountOne', other: 'circles.memberCount' },
          { count: circle.members.length },
        ),
      }),
      group: t('finance.shareCircles'),
    })),
  ]
}
