import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import type { ShoppingList } from './shoppingApi'
import { ShoppingShareDialog } from './shoppingShareDialog'
import { callsOf, renderInProviders, shoppingList } from './shoppingTestSupport'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function serve() {
  vi.mocked(graphql).mockImplementation((async (document: string) => {
    if (document.includes('ListShoppingListShares')) {
      return {
        ListShoppingListShares: [
          {
            id: 'share-1',
            shoppingListId: 'list-1',
            granteeUserId: 'person-b',
            granteeCircleId: '',
            granteeName: 'Sam',
            granteeUsername: 'sam',
            granteeCircleName: '',
          },
        ],
      }
    }
    if (document.includes('ListCirclePeople')) {
      return {
        ListCirclePeople: [{ userId: 'person-c', username: 'kim', name: 'Kim', avatarUrl: null, avatarColorIndex: 0 }],
      }
    }
    if (document.includes('ListCircles')) return { ListCircles: [{ id: 'circle-1', name: 'Family' }] }
    return {}
  }) as never)
}

function open(list: ShoppingList = shoppingList({ isShared: true })) {
  const onChanged = vi.fn()
  renderInProviders(<ShoppingShareDialog shoppingList={list} onChanged={onChanged} onClose={() => undefined} />)
  return { onChanged }
}

it('lists who the list is shared with and shares it with a person from the picker', async () => {
  serve()
  const { onChanged } = open()

  expect(await screen.findByText('Sam')).toBeTruthy()
  const picker = (await screen.findByLabelText('Share with')) as HTMLSelectElement
  await waitFor(() => expect(picker.options.length).toBeGreaterThan(1))
  fireEvent.change(picker, { target: { value: 'user:person-c' } })
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))

  await waitFor(() => expect(callsOf('ShareShoppingList')).toHaveLength(1))
  expect(callsOf('ShareShoppingList')[0][1]).toEqual({ shoppingListId: 'list-1', granteeUserId: 'person-c' })
  await waitFor(() => expect(onChanged).toHaveBeenCalled())
})

it('shares with a whole circle', async () => {
  serve()
  open()

  const picker = (await screen.findByLabelText('Share with')) as HTMLSelectElement
  await waitFor(() => expect(picker.options.length).toBeGreaterThan(1))
  expect(screen.getByRole('option', { name: 'Everyone in Family' })).toBeTruthy()
  fireEvent.change(picker, { target: { value: 'circle:circle-1' } })
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))

  await waitFor(() => expect(callsOf('ShareShoppingList')).toHaveLength(1))
  expect(callsOf('ShareShoppingList')[0][1]).toEqual({ shoppingListId: 'list-1', granteeCircleId: 'circle-1' })
})

it('stops sharing with the trash icon, after asking', async () => {
  serve()
  open()

  fireEvent.click(await screen.findByRole('button', { name: 'Stop sharing with Sam' }))
  expect(callsOf('UnshareShoppingList')).toHaveLength(0)
  expect(screen.getByText('Stop sharing with Sam?')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing' }))

  await waitFor(() => expect(callsOf('UnshareShoppingList')).toHaveLength(1))
  expect(callsOf('UnshareShoppingList')[0][1]).toEqual({ shoppingListShareId: 'share-1' })
})

it('lets the caller choose their own store reminder for the list', async () => {
  serve()
  open()

  const reminder = (await screen.findByRole('checkbox', {
    name: 'Remind me at a store about what this list needs there',
  })) as HTMLInputElement
  expect(reminder.checked).toBe(true)
  fireEvent.click(reminder)

  await waitFor(() => expect(callsOf('SetShoppingListReminder')).toHaveLength(1))
  expect(callsOf('SetShoppingListReminder')[0][1]).toEqual({ shoppingListId: 'list-1', isReminded: false })
})

it('gives the people it is shared with the reminder and no sharing', async () => {
  serve()
  open(shoppingList({ isOwner: false, isReminded: false }))

  const reminder = (await screen.findByRole('checkbox', {
    name: 'Remind me at a store about what this list needs there',
  })) as HTMLInputElement
  expect(reminder.checked).toBe(false)
  expect(screen.queryByLabelText('Share with')).toBeNull()
  expect(callsOf('ListShoppingListShares')).toHaveLength(0)
  expect(callsOf('ListCirclePeople')).toHaveLength(0)
})
