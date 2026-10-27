import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../../api'
import { uploadFiles } from '../../upload'
import { callsOf, renderShopping, shoppingItem, shoppingList } from './shoppingTestSupport'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))

beforeEach(() => {
  vi.mocked(subscribe).mockImplementation((() => () => undefined) as never)
  vi.mocked(graphql).mockImplementation((async (document: string) => {
    if (document.includes('ListShoppingLists')) return { ListShoppingLists: [shoppingList()] }
    if (document.includes('GetShoppingList(')) {
      return {
        GetShoppingList: {
          totalCount: 1,
          shoppingList: shoppingList(),
          shoppingItems: [shoppingItem()],
          shoppingTrips: [],
        },
      }
    }
    if (document.includes('PreviewShoppingImport')) {
      return {
        PreviewShoppingImport: {
          shoppingItems: [
            {
              itemDescription: 'Tea',
              itemDetails: '',
              neededQuantity: '1',
              quantityUnit: '',
              aisleName: '',
              storeName: '',
              isStoreBrand: false,
              mergedShoppingItemId: '',
              mergedShoppingItemState: '',
              mergedNeededQuantity: '',
            },
          ],
        },
      }
    }
    return {}
  }) as never)
  let uploadCount = 0
  vi.mocked(uploadFiles).mockImplementation((() => ({
    promise: Promise.resolve({ attachments: [{ id: `attachment-${++uploadCount}` }] }),
    cancel: () => undefined,
  })) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

it('uploads a receipt as an attachment and has it read, saying the matches come when it is read', async () => {
  renderShopping('/shopping/lists/list-1')
  await screen.findByText('Peas')

  const file = new File(['receipt'], 'receipt.jpg', { type: 'image/jpeg' })
  const input = document.querySelector('input[type="file"][accept="image/*,application/pdf"]') as HTMLInputElement
  fireEvent.change(input, { target: { files: [file] } })

  await waitFor(() => expect(callsOf('ReadReceipt(')).toHaveLength(1))
  expect(vi.mocked(uploadFiles).mock.calls[0].slice(0, 3)).toEqual(['POST', '/api/v1/agent/attachments', [file]])
  expect(callsOf('ReadReceipt(')[0][1]).toEqual({ agentAttachmentId: 'attachment-1' })
  expect(await screen.findByText('Reading the receipt; matched items are checked off when it is read.')).toBeTruthy()
})

it('refuses a receipt that is too large without uploading it', async () => {
  renderShopping('/shopping/lists/list-1')
  await screen.findByText('Peas')

  const file = new File(['receipt'], 'huge.pdf', { type: 'application/pdf' })
  Object.defineProperty(file, 'size', { value: 11 << 20 })
  const input = document.querySelector('input[type="file"][accept="image/*,application/pdf"]') as HTMLInputElement
  fireEvent.change(input, { target: { files: [file] } })

  expect(await screen.findByText('huge.pdf is larger than 10 MB.')).toBeTruthy()
  expect(vi.mocked(uploadFiles)).not.toHaveBeenCalled()
  expect(callsOf('ReadReceipt(')).toHaveLength(0)
})

it('reads pictures of a list sent as attachments, and sends each once however often it is read', async () => {
  renderShopping('/shopping/lists/list-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Import' }))

  const picture = new File(['list'], 'list.png', { type: 'image/png' })
  const input = document.querySelector('input[type="file"][multiple]') as HTMLInputElement
  fireEvent.change(input, { target: { files: [picture] } })
  const chosen = screen.getByText('list.png')
  expect(
    within(chosen.closest('.shopping-import-file') as HTMLElement).getByRole('button', { name: 'Remove list.png' }),
  ).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'Read the list' }))
  await screen.findByText('Tea')
  expect(callsOf('PreviewShoppingImport')[0][1]).toEqual({
    shoppingListId: 'list-1',
    importText: null,
    agentAttachmentIds: ['attachment-1'],
  })

  // Back, and read again: the same picture is not sent twice.
  fireEvent.click(screen.getByRole('button', { name: 'Back' }))
  fireEvent.click(screen.getByRole('button', { name: 'Read the list' }))
  await waitFor(() => expect(callsOf('PreviewShoppingImport')).toHaveLength(2))
  expect(vi.mocked(uploadFiles)).toHaveBeenCalledTimes(1)
  expect(callsOf('PreviewShoppingImport')[1][1]?.agentAttachmentIds).toEqual(['attachment-1'])
})
