import { expect, it } from 'vitest'

import { AgentAlert, muteTargets } from './agentAlerts'

const alert = (covered: AgentAlert['covered']): AgentAlert => ({
  id: 'alert-1',
  alertText: 'Someone has been asking for sign-in codes.',
  sentAt: '2026-09-29T10:00:00Z',
  subjectKey: 'photo app sign-in codes',
  isUrgent: false,
  covered,
})

// What a Mute offers is what the server would mute: the subject, and the
// sender, its domain and its kind from the first message.
it('offers the subject, the sender, the domain and the kind', () => {
  const targets = muteTargets(
    alert([
      {
        mailId: 'mail-1',
        subject: 'Your code',
        fromAddress: 'no-reply@photos.example.com',
        candidateKind: 'burst',
        mailCategory: 'notification',
      },
    ]),
  )
  expect(targets).toEqual([
    { muteScope: 'subjectKey', muteTarget: 'photo app sign-in codes' },
    { muteScope: 'sender', muteTarget: 'no-reply@photos.example.com' },
    { muteScope: 'domain', muteTarget: 'photos.example.com' },
    { muteScope: 'kind', muteTarget: 'burst' },
  ])
})

it('offers only the subject when the messages are gone', () => {
  expect(muteTargets(alert([]))).toEqual([{ muteScope: 'subjectKey', muteTarget: 'photo app sign-in codes' }])
})
