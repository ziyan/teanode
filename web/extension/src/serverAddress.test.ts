import { describe, expect, it } from 'vitest'
import { serverAddressProblem } from './serverAddress'

describe('serverAddressProblem', () => {
  it('accepts https, and plain http only to this computer', () => {
    for (const address of [
      'https://mail.example.test',
      'https://mail.example.test:8443/',
      'HTTPS://Mail.Example.test',
      'http://localhost',
      'http://localhost:8080',
      'http://127.0.0.1:8080',
      'http://[::1]:8080',
      'http://app.localhost:3000',
    ]) {
      expect(serverAddressProblem(address), address).toBe('')
    }
  })

  it('refuses plain http to any other host, including names that only look local', () => {
    for (const address of [
      'http://mail.example.test',
      'http://192.0.2.7:8080',
      'http://localhost.example.test',
      'http://localhost@example.test',
      'http://example.test/localhost',
      'http://127.0.0.1.example.test',
      'http://[2001:db8::1]',
    ]) {
      expect(serverAddressProblem(address), address).toMatch(/must start with https:\/\//)
    }
  })

  it('refuses what is not an http or https address', () => {
    for (const address of [
      '',
      'mail.example.test',
      'ftp://mail.example.test',
      'javascript:alert(1)',
      'localhost:8080',
    ]) {
      expect(serverAddressProblem(address), address).toMatch(/must start with https:\/\//)
    }
  })
})
