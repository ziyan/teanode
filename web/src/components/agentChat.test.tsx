import { expect, it } from 'vitest'

import {
  addressableAgentsOf,
  addressedIn,
  agentSpeakerName,
  agentsMatching,
  chatLineOf,
  isChatMessage,
  linesWithSaid,
  linesWithStreamedText,
  linesWithoutStreaming,
  isStreamingKey,
  mentionAt,
  readersByMessage,
  startsRun,
  streamingKeyOf,
  withMention,
  type ChatLine,
  type ChatMember,
  type ChatSummary,
} from './agentChat'

function member(userId: string, readThroughMessageId: string | null): ChatMember {
  return {
    userId,
    username: userId,
    name: userId,
    avatarUrl: null,
    avatarColorIndex: 0,
    memberRole: 'member',
    isDeletedAccount: false,
    isRemoved: false,
    readThroughMessageId,
    isAgentReading: false,
    isAgentAskableByOthers: false,
    agentId: '',
    agentName: '',
    isAgentActive: false,
  }
}

function said(messageId: string, authorUserId: string): ChatLine {
  return {
    kind: 'chat',
    key: messageId,
    messageId,
    text: messageId,
    authorKind: 'person',
    authorUserId,
    authorName: authorUserId,
    authorOwnerName: '',
    authorAvatarUrl: null,
    authorAvatarColorIndex: null,
    authorAgentId: '',
    isMine: authorUserId === 'person-a',
    editedAt: null,
    isDeleted: false,
  }
}

const GROUP = [said('01', 'person-a'), said('02', 'person-b'), said('03', 'person-c'), said('04', 'person-a')]

it('puts each member under the newest message by somebody else that they have read', () => {
  const readers = readersByMessage(GROUP, [member('person-b', '03'), member('person-c', '04')], 'person-a')
  // Person B read through 03, and 02 is their own: the newest of anybody
  // else's is Person C's 03. Person C read through 04, which is Person A's.
  expect(readers.get('03')?.map((reader) => reader.userId)).toEqual(['person-b'])
  expect(readers.get('04')?.map((reader) => reader.userId)).toEqual(['person-c'])
  expect(readers.get('02')).toBeUndefined()
})

it('puts a member under none when everything they have read is their own', () => {
  const readers = readersByMessage(GROUP, [member('person-b', '02')], 'person-a')
  // 01 is Person A's, so Person B is under it, and not under their own 02.
  expect(readers.get('01')?.map((reader) => reader.userId)).toEqual(['person-b'])
  expect(readers.get('02')).toBeUndefined()

  const onlyOwn = readersByMessage([said('02', 'person-b')], [member('person-b', '02')], 'person-a')
  expect(onlyOwn.size).toBe(0)
})

it('leaves out the signed-in person, members who left and members who have read nothing', () => {
  const left = { ...member('person-c', '04'), isRemoved: true }
  const readers = readersByMessage(GROUP, [member('person-a', '04'), left, member('person-b', null)], 'person-a')
  expect(readers.size).toBe(0)
})

function room(): ChatSummary {
  const withAgent = (userId: string, agentName: string, overrides: Partial<ChatMember> = {}): ChatMember => ({
    ...member(userId, null),
    agentId: `agent-${userId}`,
    agentName,
    isAgentActive: true,
    ...overrides,
  })
  return {
    chatId: 'chat',
    chatKind: 'group',
    circleId: null,
    title: 'Trip',
    isCircleChat: false,
    unreadCount: 0,
    isMuted: false,
    mutedUntil: null,
    isRemoved: false,
    members: [
      withAgent('person-b', 'Benny', { isAgentAskableByOthers: true }),
      withAgent('person-a', 'Annie'),
      withAgent('person-c', 'Cyrus'),
      withAgent('person-d', 'Dee', { isAgentAskableByOthers: true, isAgentActive: false }),
      withAgent('person-e', 'Evie', { isAgentAskableByOthers: true, isRemoved: true }),
      member('person-f', null),
    ],
  }
}

it('offers the person’s own agent first and then the agents their owners let the others ask', () => {
  const agents = addressableAgentsOf(room(), 'person-a', '(deleted)')
  expect(agents.map((agent) => [agent.name, agent.isOwn])).toEqual([
    ['Annie', true],
    ['Benny', false],
  ])
})

it('finds an @ at the caret only where it starts a word', () => {
  expect(mentionAt('@', 1)).toEqual({ start: 0, query: '' })
  expect(mentionAt('hello @Be', 9)).toEqual({ start: 6, query: 'Be' })
  expect(mentionAt('hello @Be and more', 9)).toEqual({ start: 6, query: 'Be' })
  // An address, or an @ already followed by a space, is not a mention.
  expect(mentionAt('mail me@example.com', 19)).toBeNull()
  expect(mentionAt('hello @Benny and', 16)).toBeNull()
  expect(mentionAt('no at sign', 10)).toBeNull()
})

it('narrows the agents by the letters after the @, in any case', () => {
  const agents = addressableAgentsOf(room(), 'person-a', '(deleted)')
  expect(agentsMatching(agents, '').map((agent) => agent.name)).toEqual(['Annie', 'Benny'])
  expect(agentsMatching(agents, 'be').map((agent) => agent.name)).toEqual(['Benny'])
  expect(agentsMatching(agents, 'x')).toEqual([])
})

it('puts the chosen name where the @ was and the caret after it', () => {
  const [annie] = addressableAgentsOf(room(), 'person-a', '(deleted)')
  expect(withMention('hi @An there', { start: 3, query: 'An' }, 6, annie)).toEqual({
    text: 'hi @Annie  there',
    caret: 10,
  })
})

it('addresses the agents chosen whose names are still in the words, and the agent replied to', () => {
  const [annie, benny] = addressableAgentsOf(room(), 'person-a', '(deleted)')
  expect(addressedIn('@Annie and @Benny, hello', [annie, benny])).toEqual(['agent-person-a', 'agent-person-b'])
  // Taken out of the words, it is taken out of the message.
  expect(addressedIn('@Benny, hello', [annie, benny])).toEqual(['agent-person-b'])
  // A name that only begins with it is another word.
  expect(addressedIn('@Anniefied', [annie])).toEqual([])
  // Typed by hand without choosing it addresses nothing.
  expect(addressedIn('@Annie', [])).toEqual([])
  // A reply to an agent's message is for that agent, once.
  expect(addressedIn('thanks', [], 'agent-person-b')).toEqual(['agent-person-b'])
  expect(addressedIn('@Benny thanks', [benny], 'agent-person-b')).toEqual(['agent-person-b'])
})

it('is not a message of the chat when it is a row of the agent’s own work', () => {
  const base = { id: '01', role: 'assistant', content: '', authorKind: 'agent' }
  expect(isChatMessage({ ...base, content: 'Thursday at 3.' })).toBe(true)
  expect(isChatMessage({ ...base, isAgentWork: true })).toBe(false)
  expect(isChatMessage({ ...base, toolCalls: [{ id: 'call' }] })).toBe(false)
  expect(isChatMessage({ ...base, content: 'Let me look.', toolCalls: [{ id: 'call' }] })).toBe(true)
  expect(isChatMessage({ ...base, role: 'tool' })).toBe(false)
})

it('takes the line of a turn that was stopped off the screen, and leaves the rest', () => {
  const run = { runId: 'run-1', name: 'Robin', authorAgentId: 'agent-robin' }
  let lines: ChatLine[] = [said('01', 'person-b')]
  lines = linesWithStreamedText(lines, { ...run, text: 'Thurs' })
  lines = linesWithStreamedText(lines, { ...run, runId: 'run-2', text: 'x' })
  expect(isStreamingKey(streamingKeyOf('run-1'))).toBe(true)
  expect(isStreamingKey('01')).toBe(false)
  const remaining = linesWithoutStreaming(lines, streamingKeyOf('run-1'))
  expect(remaining.map((line) => line.key)).toEqual([lines[0].key, streamingKeyOf('run-2')])
})

it('adds the words of an answer to the line for its run, and the stored message takes its place', () => {
  const run = { runId: 'run-1', name: 'Robin', authorAgentId: 'agent-robin' }
  let lines: ChatLine[] = [said('01', 'person-b')]
  lines = linesWithStreamedText(lines, { ...run, text: 'Thurs' })
  lines = linesWithStreamedText(lines, { ...run, text: 'day' })
  expect(lines).toHaveLength(2)
  expect(lines[1]).toMatchObject({
    key: streamingKeyOf('run-1'),
    text: 'Thursday',
    isStreaming: true,
    authorKind: 'agent',
  })
  // Another run is another line.
  expect(linesWithStreamedText(lines, { ...run, runId: 'run-2', text: 'x' })).toHaveLength(3)

  const stored = {
    id: '02',
    role: 'assistant',
    content: 'Thursday at 3.',
    authorKind: 'agent',
    authorAgentId: 'agent-robin',
    authorName: 'Robin',
    agentRunId: 'run-1',
  }
  const replaced = linesWithSaid(lines, stored)
  expect(replaced).toHaveLength(2)
  expect(replaced[1]).toMatchObject({ messageId: '02', text: 'Thursday at 3.' })
  expect((replaced[1] as ChatLine).isStreaming).toBeUndefined()
  // With no line for its run, it is added.
  expect(linesWithSaid([said('01', 'person-b')], stored)).toHaveLength(2)
})

it('starts a new run after a date divider even for the same person, and not within the same day', () => {
  const at = (messageId: string, authorUserId: string, moment: Date): ChatLine => ({
    ...said(messageId, authorUserId),
    at: moment.toISOString(),
  })
  // Two minutes apart, either side of midnight in the reader's own zone.
  const lateEvening = new Date(2030, 0, 2, 23, 59)
  const justAfterMidnight = new Date(2030, 0, 3, 0, 1)
  const before = at('01', 'person-b', lateEvening)
  expect(startsRun(at('02', 'person-b', justAfterMidnight), before)).toBe(true)

  // The same two minutes in the middle of a day stay one run.
  const noon = new Date(2030, 0, 2, 12, 0)
  const soonAfter = new Date(2030, 0, 2, 12, 2)
  expect(startsRun(at('04', 'person-b', soonAfter), at('03', 'person-b', noon))).toBe(false)

  // A different author starts one either way, and the first line of all does.
  expect(startsRun(at('05', 'person-c', soonAfter), at('03', 'person-b', noon))).toBe(true)
  expect(startsRun(at('06', 'person-b', noon), undefined)).toBe(true)
})

// The words of the dashboard's catalog, for a test that has no provider.
const words = (key: string, values?: Record<string, unknown>) =>
  key === 'chat.agentOf' ? `${values?.owner}'s agent` : key

it('names an agent by its name and whose it is, and by whose it is alone while it is called Agent', () => {
  expect(agentSpeakerName('Bertie', 'Ada', words)).toBe("Bertie · Ada's agent")
  // Most agents keep the default name, which would only repeat the word.
  expect(agentSpeakerName('Agent', 'Ada', words)).toBe("Ada's agent")
  expect(agentSpeakerName('Agent', 'Ben', words)).toBe("Ben's agent")
  expect(agentSpeakerName('', 'Ben', words)).toBe("Ben's agent")
  // An agent whose owner is not known is called by its own name.
  expect(agentSpeakerName('Bertie', '', words)).toBe('Bertie')
  expect(agentSpeakerName(' Bertie ', '  ', words)).toBe('Bertie')
})

it('keeps the owner’s name on the line an agent’s message is drawn as, and on the words still being written', () => {
  const stored = {
    id: '02',
    role: 'assistant',
    content: 'Thursday at 3.',
    authorKind: 'agent',
    authorAgentId: 'agent-robin',
    authorName: 'Agent',
    authorOwnerName: 'Ada',
  }
  expect(chatLineOf(stored)).toMatchObject({ authorName: 'Agent', authorOwnerName: 'Ada' })
  const streamed = linesWithStreamedText([], {
    runId: 'run-1',
    text: 'Thurs',
    name: 'Agent',
    authorOwnerName: 'Ada',
    authorAgentId: 'agent-robin',
  })
  expect(streamed[0]).toMatchObject({ authorName: 'Agent', authorOwnerName: 'Ada', isStreaming: true })
  // A message from before the owner was recorded has none.
  expect(chatLineOf({ ...stored, authorOwnerName: undefined }).authorOwnerName).toBe('')
})
