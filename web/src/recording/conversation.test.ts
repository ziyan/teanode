import { expect, it } from 'vitest'
import { distinctSpeakerCount, groupConversation, liveRangeOfPage, temporarySpeakerNumber } from './conversation'
import { type TranscriptSegment } from './processing'

const segmentFixture = (overrides: Partial<TranscriptSegment>): TranscriptSegment =>
  ({
    id: 'segment',
    localSpeakerId: 'local-one',
    segmentStartMS: 0,
    segmentEndMS: 1000,
    hasUserConfirmedSpeaker: false,
    effectiveSpeakerIdentityId: '',
    effectiveSpeakerName: '',
    ...overrides,
  }) as TranscriptSegment

it('puts consecutive passages by one speaker in one bubble with the first start time', () => {
  const groups = groupConversation([
    segmentFixture({ id: 'a1', segmentStartMS: 1000 }),
    segmentFixture({ id: 'a2', segmentStartMS: 5000 }),
    segmentFixture({ id: 'b1', localSpeakerId: 'local-two', segmentStartMS: 9000 }),
    segmentFixture({ id: 'a3', segmentStartMS: 12000 }),
  ])
  expect(groups.map((group) => group.segments.map((segment) => segment.id))).toEqual([['a1', 'a2'], ['b1'], ['a3']])
  expect(groups.map((group) => group.groupStartMS)).toEqual([1000, 9000, 12000])
  expect(groups.map((group) => group.speakerLetter)).toEqual(['A', 'B', 'A'])
  expect(distinctSpeakerCount([segmentFixture({}), segmentFixture({ localSpeakerId: 'local-two' })])).toBe(2)
})

it('names confirmed speakers by identity and marks only the account owner', () => {
  const confirmed = {
    hasUserConfirmedSpeaker: true,
    effectiveSpeakerIdentityId: 'identity-one',
    effectiveSpeakerName: 'Fixture Person',
  }
  const groups = groupConversation([
    segmentFixture({ id: 'x1', localSpeakerId: 'local-one', ...confirmed, isAccountOwner: true }),
    segmentFixture({ id: 'x2', localSpeakerId: 'local-two', ...confirmed, isAccountOwner: true }),
    segmentFixture({
      id: 'y1',
      localSpeakerId: 'local-three',
      hasUserConfirmedSpeaker: true,
      effectiveSpeakerIdentityId: 'identity-two',
      effectiveSpeakerName: 'Other Person',
    }),
    segmentFixture({ id: 'z1', localSpeakerId: 'local-four', isAccountOwner: true }),
  ])
  expect(groups).toHaveLength(3)
  expect(groups[0].segments).toHaveLength(2)
  expect(groups.map((group) => group.isAccountOwner)).toEqual([true, false, false])
  expect(groups.map((group) => group.speakerName)).toEqual(['Fixture Person', 'Other Person', null])
})

it('treats a speaker recognized by a voice sample as that identity', () => {
  const groups = groupConversation([
    segmentFixture({
      id: 'r1',
      localSpeakerId: 'Fixture Owner',
      isRecognizedSpeaker: true,
      effectiveSpeakerIdentityId: 'identity-owner',
      effectiveSpeakerName: 'Fixture Owner',
      isAccountOwner: true,
    }),
  ])
  expect(groups[0].isAccountOwner).toBe(true)
  expect(groups[0].speakerName).toBe('Fixture Owner')
})

const liveFixture = (utteranceId: string, utteranceStartMS: number, transcriptText = `Words of ${utteranceId}`) => ({
  utteranceId,
  utteranceStartMS,
  utteranceEndMS: utteranceStartMS + 1000,
  transcriptText,
})

it('draws words heard live as one neutral bubble after the passages that cover the rest', () => {
  const groups = groupConversation(
    [segmentFixture({ id: 'a1', segmentStartMS: 1000 })],
    [liveFixture('u1', 30000), liveFixture('u2', 33000)],
  )
  expect(groups.map((group) => group.isLive)).toEqual([false, true])
  expect(groups[1].liveUtterances.map((utterance) => utterance.utteranceId)).toEqual(['u1', 'u2'])
  expect(groups[1]).toMatchObject({ speakerName: null, isAccountOwner: false, segments: [], groupStartMS: 30000 })
})

it('places live words among the passages by when they began', () => {
  const groups = groupConversation(
    [segmentFixture({ id: 'late', segmentStartMS: 70000 })],
    [liveFixture('early', 5000)],
  )
  expect(groups.map((group) => group.isLive)).toEqual([true, false])
})

it('does not put live words into the bubble of a speaker', () => {
  const groups = groupConversation(
    [segmentFixture({ id: 'a1', segmentStartMS: 1000 }), segmentFixture({ id: 'a2', segmentStartMS: 50000 })],
    [liveFixture('between', 20000)],
  )
  expect(groups.map((group) => group.segments.map((segment) => segment.id))).toEqual([['a1'], [], ['a2']])
})

it('draws only the live words that belong to the page shown', () => {
  const page = [segmentFixture({ id: 'p1', segmentStartMS: 100000, segmentEndMS: 110000 })]
  const live = [liveFixture('before', 5000), liveFixture('inside', 105000), liveFixture('after', 500000)]
  const idsOf = (groups: ReturnType<typeof groupConversation>) =>
    groups.flatMap((group) => group.liveUtterances.map((utterance) => utterance.utteranceId))
  expect(idsOf(groupConversation(page, live, liveRangeOfPage(page, true, false)))).toEqual(['before', 'inside'])
  expect(idsOf(groupConversation(page, live, liveRangeOfPage(page, false, false)))).toEqual(['inside'])
  expect(idsOf(groupConversation(page, live, liveRangeOfPage(page, false, true)))).toEqual(['inside', 'after'])
})

it('groups a speaker nobody has named yet by the temporary label the server gave it', () => {
  const groups = groupConversation([
    segmentFixture({ id: 't1', localSpeakerId: 'A', temporarySpeakerLabel: 'Speaker 1' }),
    segmentFixture({ id: 't2', localSpeakerId: 'B', temporarySpeakerLabel: 'Speaker 1' }),
    segmentFixture({ id: 't3', localSpeakerId: 'A', temporarySpeakerLabel: 'Speaker 2' }),
  ])
  expect(groups.map((group) => group.segments.map((segment) => segment.id))).toEqual([['t1', 't2'], ['t3']])
  expect(groups.map((group) => group.temporarySpeakerLabel)).toEqual(['Speaker 1', 'Speaker 2'])
  expect(
    distinctSpeakerCount([
      segmentFixture({ localSpeakerId: 'A', temporarySpeakerLabel: 'Speaker 1' }),
      segmentFixture({ localSpeakerId: 'B', temporarySpeakerLabel: 'Speaker 1' }),
    ]),
  ).toBe(1)
  expect(temporarySpeakerNumber('Speaker 12')).toBe('12')
  expect(temporarySpeakerNumber('Someone')).toBeNull()
})

it('prefers the name of a confirmed speaker to the temporary label', () => {
  const groups = groupConversation([
    segmentFixture({
      id: 'n1',
      hasUserConfirmedSpeaker: true,
      effectiveSpeakerIdentityId: 'identity-one',
      effectiveSpeakerName: 'Fixture Person',
      temporarySpeakerLabel: 'Speaker 1',
    }),
  ])
  expect(groups[0]).toMatchObject({ speakerName: 'Fixture Person', temporarySpeakerLabel: '' })
})
