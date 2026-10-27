import { expect, it } from 'vitest'

import { conversationOfPath, photoOfPath } from './journalOpen'

// Which dashboard paths open in place: a conversation in the drawer, a photo
// in the lightbox. Anything else is an ordinary link.

it('finds the conversation a path of the chats opens, with none for the main one', () => {
  expect(conversationOfPath('/chat?conversation=conv-1&message=m-2')).toBe('conv-1')
  expect(conversationOfPath('/agent?conversation=conv%201')).toBe('conv 1')
  expect(conversationOfPath('/chat?message=m-2')).toBe('')
  expect(conversationOfPath('/calendar?conversation=conv-1')).toBeNull()
  expect(conversationOfPath('/photos/photo-1')).toBeNull()
})

it('finds the photo a path of Photos opens', () => {
  expect(photoOfPath('/photos/photo-1')).toBe('photo-1')
  expect(photoOfPath('/photos/photo%201')).toBe('photo 1')
  expect(photoOfPath('/photos')).toBeNull()
  expect(photoOfPath('/photos/albums/album-1')).toBeNull()
  expect(photoOfPath('/knowledge/photos/a')).toBeNull()
})
