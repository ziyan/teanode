// The server address the person types in the options page. The token the
// extension signs in with goes to that address on every call, and the agent
// reaches the browser through it, so the address must be one that keeps the
// token from being read on the way: https, or plain http only to this
// computer (localhost, 127.0.0.1, [::1] and names ending in .localhost).

const LOOPBACK_HOSTS = new Set(['localhost', '127.0.0.1', '[::1]'])

// serverAddressProblem is a sentence saying why the address cannot be used,
// or an empty text when it can.
export function serverAddressProblem(address: string): string {
  let parsed: URL
  try {
    parsed = new URL(address.trim())
  } catch {
    return 'The server address must start with https://.'
  }
  if (parsed.protocol === 'https:') return ''
  if (parsed.protocol !== 'http:') return 'The server address must start with https://.'
  const host = parsed.hostname.toLowerCase()
  if (LOOPBACK_HOSTS.has(host) || host.endsWith('.localhost')) return ''
  return 'The server address must start with https://. Plain http:// would send your sign-in token in the clear; it is allowed only for localhost.'
}
