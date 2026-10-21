import { describe, expect, it } from 'vitest'
import { CAPTURE_LIFETIME_MILLISECONDS, CapturedLogins } from './capturedLogins'

function make() {
  let now = 1_000
  const captured = new CapturedLogins(() => now)
  return { captured, advance: (milliseconds: number) => (now += milliseconds) }
}

const page = 'https://example.com/sign-in'

describe('CapturedLogins', () => {
  it('keeps what was typed for two minutes and then forgets it', () => {
    const { captured, advance } = make()
    captured.capture(5, { pageUrl: page, userName: 'ada', password: 'hunter2', isNewPassword: false })
    advance(CAPTURE_LIFETIME_MILLISECONDS - 1)
    expect(captured.peek(5)?.password).toBe('hunter2')
    advance(1)
    expect(captured.peek(5)).toBeNull()
  })

  it('keeps one login for each tab', () => {
    const { captured } = make()
    captured.capture(1, { pageUrl: page, userName: 'ada', password: 'one', isNewPassword: false })
    captured.capture(2, { pageUrl: page, userName: 'grace', password: 'two', isNewPassword: false })
    expect(captured.peek(1)?.userName).toBe('ada')
    expect(captured.peek(2)?.userName).toBe('grace')
    captured.forget(1)
    expect(captured.peek(1)).toBeNull()
    expect(captured.peek(2)).not.toBeNull()
  })

  it('takes the user name from the first step of a sign-in on the same host', () => {
    const { captured } = make()
    captured.capture(1, { pageUrl: page, userName: 'ada', password: '', isNewPassword: false })
    captured.capture(1, {
      pageUrl: 'https://www.example.com/password',
      userName: '',
      password: 'hunter2',
      isNewPassword: false,
    })
    expect(captured.peek(1)).toMatchObject({ userName: 'ada', password: 'hunter2' })
  })

  it('does not take a user name from another host', () => {
    const { captured } = make()
    captured.capture(1, { pageUrl: page, userName: 'ada', password: '', isNewPassword: false })
    captured.capture(1, {
      pageUrl: 'https://other.test/password',
      userName: '',
      password: 'hunter2',
      isNewPassword: false,
    })
    expect(captured.peek(1)?.userName).toBe('')
  })

  it('does not let a user name alone replace a password already captured', () => {
    const { captured } = make()
    captured.capture(1, { pageUrl: page, userName: 'ada', password: 'hunter2', isNewPassword: false })
    captured.capture(1, { pageUrl: page, userName: 'ada', password: '', isNewPassword: false })
    expect(captured.peek(1)?.password).toBe('hunter2')
  })

  it('offers a capture once', () => {
    const { captured } = make()
    captured.capture(1, { pageUrl: page, userName: 'ada', password: 'hunter2', isNewPassword: false })
    expect(captured.peek(1)?.hasBeenOffered).toBe(false)
    captured.markOffered(1)
    expect(captured.peek(1)?.hasBeenOffered).toBe(true)
  })

  it('forgets everything on lock', () => {
    const { captured } = make()
    captured.capture(1, { pageUrl: page, userName: 'ada', password: 'hunter2', isNewPassword: false })
    captured.forgetAll()
    expect(captured.peek(1)).toBeNull()
  })
})
