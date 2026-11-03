import { expect, it } from 'vitest'

import { callOf } from './chatCallFixtures'
import { ChatCallStore, goingCallOf, ringingCalls } from './chatCallStore'

it('keeps the latest of a call, and tells the calls ringing the person apart from the rest', () => {
  const store = new ChatCallStore()
  store.apply(callOf())
  store.apply(callOf({ id: 'call-two', conversationId: 'other', askerRingState: 'answered', isAskerOn: true }))

  expect(ringingCalls(store.getSnapshot()).map((call) => call.id)).toEqual(['call-one'])
  expect(goingCallOf(store.getSnapshot(), 'other')?.id).toBe('call-two')

  store.apply(callOf({ isGoing: false, endReason: 'missed', endedAt: '2030-01-01T10:01:00Z' }))
  expect(ringingCalls(store.getSnapshot())).toEqual([])
  expect(goingCallOf(store.getSnapshot(), 'chat-conversation')).toBeNull()
})

it('forgets a call that ListGoingChatCalls no longer lists, unless an event came after the read began', () => {
  const store = new ChatCallStore()
  store.apply(callOf())
  store.apply(callOf({ id: 'call-two', conversationId: 'other' }))

  const markedAt = store.writeMark()
  // An event for the second call arrives while the read is on its way.
  store.apply(callOf({ id: 'call-two', conversationId: 'other', askerRingState: 'answered', isAskerOn: true }))
  store.settleGoing([], markedAt)

  expect(store.getSnapshot().map((call) => call.id)).toEqual(['call-two'])
})

it('forgets the going call of a chat that ReadGoingChatCall says has none, unless an event came after', () => {
  const store = new ChatCallStore()
  store.apply(callOf())

  const markedAt = store.writeMark()
  store.settleConversation('chat-conversation', null, markedAt)
  expect(goingCallOf(store.getSnapshot(), 'chat-conversation')).toBeNull()

  // A call that started while a read was on its way is not undone by it.
  const secondMark = store.writeMark()
  store.apply(callOf({ id: 'call-three' }))
  store.settleConversation('chat-conversation', null, secondMark)
  expect(goingCallOf(store.getSnapshot(), 'chat-conversation')?.id).toBe('call-three')
})

it('tells its listeners of each change, and a new snapshot each time', () => {
  const store = new ChatCallStore()
  let told = 0
  const stop = store.subscribe(() => (told += 1))
  const before = store.getSnapshot()

  store.apply(callOf())
  expect(told).toBe(1)
  expect(store.getSnapshot()).not.toBe(before)

  stop()
  store.apply(callOf({ id: 'call-two' }))
  expect(told).toBe(1)
})
