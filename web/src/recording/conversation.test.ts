import { expect, it } from 'vitest'
import { distinctSpeakerCount, groupConversation } from './conversation'
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
