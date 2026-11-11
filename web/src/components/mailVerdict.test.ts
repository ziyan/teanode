import { describe, expect, it } from 'vitest'

import { Mail } from '../api'
import { verdictOf } from './common'

const t = (key: string, values?: Record<string, string | number>) =>
  key + (values ? ' ' + Object.values(values).join(' ') : '')

const mailWith = (results: Mail['authenticationResults']) => ({ authenticationResults: results }) as Mail

describe('verdictOf', () => {
  // Forwarding rewrites the envelope sender and can break the original
  // signature; once DMARC is satisfied neither is a failure.
  it('does not call mail that passed DMARC failed for a broken signature', () => {
    const verdict = verdictOf(
      mailWith({ spf: { result: 'pass' }, dkims: [{ result: 'fail' }], dmarc: { result: 'pass' } }),
      t,
    )
    expect(verdict?.tone).toBe('good')
  })

  it('takes a trusted forwarder at its word', () => {
    const verdict = verdictOf(
      mailWith({
        spf: { result: 'pass' },
        dkims: [{ result: 'fail' }],
        dmarc: { result: 'fail', passedAtForwarderDomain: 'forwarder.example.com' },
      }),
      t,
    )
    expect(verdict?.tone).toBe('good')
    expect(verdict?.detail).toContain('mailbox.dmarcPassedAtForwarder forwarder.example.com')
  })

  it('still calls a DMARC failure a failure', () => {
    const verdict = verdictOf(mailWith({ spf: { result: 'pass' }, dmarc: { result: 'fail' } }), t)
    expect(verdict?.tone).toBe('bad')
  })

  it('still calls only broken signatures a failure without DMARC', () => {
    const verdict = verdictOf(mailWith({ spf: { result: 'pass' }, dkims: [{ result: 'fail' }] }), t)
    expect(verdict?.tone).toBe('bad')
  })
})
