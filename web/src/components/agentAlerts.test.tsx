import { expect, it } from 'vitest'

import { AgentAlert, muteTargets } from './agentAlerts'

const alert = (muteChoices: AgentAlert['muteChoices']): AgentAlert => ({
  id: 'alert-1',
  alertText: 'Someone has been asking for sign-in codes.',
  sentAt: '2026-09-29T10:00:00Z',
  subjectKey: 'photo app sign-in codes',
  isUrgent: false,
  covered: [
    {
      mailId: 'mail-1',
      subject: 'Your code',
      fromAddress: 'no-reply@photos.example.com',
      candidateKind: 'burst',
      mailCategory: 'notification',
    },
  ],
  muteChoices,
})

// What a Mute offers is what the server would mute, in its order: the
// first is the default, the burst itself rather than the words the model
// chose for it.
it('offers what the server would mute, the default first', () => {
  const choices = [
    { muteScope: 'subjectKey' as const, muteTarget: 'no-reply@photos.example.com|your sign-in code is' },
    { muteScope: 'sender' as const, muteTarget: 'no-reply@photos.example.com' },
    { muteScope: 'domain' as const, muteTarget: 'photos.example.com' },
    { muteScope: 'kind' as const, muteTarget: 'burst' },
  ]
  expect(muteTargets(alert(choices))).toEqual(choices)
})

it('offers nothing when the server has nothing to offer', () => {
  expect(muteTargets(alert([]))).toEqual([])
})
