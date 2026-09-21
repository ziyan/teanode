// The options page: where the server is, and who the extension is signed in
// as. Signing in opens the server's own authorization page in the browser's
// web sign-in flow; the page hands the token to this extension's callback
// address, which nothing else can receive.

const serverInput = document.getElementById('server')
const saved = document.getElementById('saved')
const who = document.getElementById('who')
const status = document.getElementById('status')
const signIn = document.getElementById('signin')
const signOut = document.getElementById('signout')

async function serverOrigin() {
  const { server } = await chrome.storage.sync.get('server')
  return (server || '').replace(/\/+$/, '')
}

async function show() {
  const { username, token } = await chrome.storage.local.get(['username', 'token'])
  const origin = await serverOrigin()
  if (token && username) {
    who.textContent = `Signed in as ${username}` + (origin ? ` at ${origin}` : '')
    signIn.hidden = true
    signOut.hidden = false
  } else {
    who.textContent = 'Not signed in.'
    signIn.hidden = false
    signOut.hidden = true
  }
}

function nonce() {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

async function login() {
  status.textContent = ''
  status.className = 'status'
  const origin = await serverOrigin()
  if (!origin) {
    status.textContent = 'Enter the server address first.'
    status.className = 'status bad'
    return
  }
  const state = nonce()
  const redirect = chrome.identity.getRedirectURL('authorized')
  const query = new URLSearchParams({ state, name: 'extension', redirect })
  let landed
  try {
    landed = await chrome.identity.launchWebAuthFlow({ url: `${origin}/cli?${query.toString()}`, interactive: true })
  } catch (error) {
    status.textContent = 'Not signed in: ' + String(error && error.message ? error.message : error)
    status.className = 'status bad'
    return
  }
  const fragment = new URLSearchParams((landed || '').split('#')[1] || '')
  if (fragment.get('state') !== state || !fragment.get('token')) {
    status.textContent = 'Not signed in: the page did not answer this request.'
    status.className = 'status bad'
    return
  }
  await chrome.storage.local.set({ token: fragment.get('token'), tokenId: fragment.get('tokenId') || '', username: fragment.get('username') || '' })
  status.textContent = 'Signed in.'
  status.className = 'status good'
  await show()
}

async function logout() {
  const { token, tokenId } = await chrome.storage.local.get(['token', 'tokenId'])
  const origin = await serverOrigin()
  if (token && tokenId && origin) {
    // Best effort: the token is forgotten here whatever the server says.
    try {
      await fetch(`${origin}/api/v1/graphql`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ query: 'mutation ($tokenId: String!) { RevokeToken(tokenId: $tokenId) }', variables: { tokenId } }),
      })
    } catch {
      // The server was not reachable; the token expires on its own.
    }
  }
  await chrome.storage.local.remove(['token', 'tokenId', 'username'])
  status.textContent = 'Signed out.'
  status.className = 'status'
  await show()
}

serverOrigin().then((origin) => {
  serverInput.value = origin
})
document.getElementById('save').addEventListener('click', async () => {
  const server = serverInput.value.trim().replace(/\/+$/, '')
  await chrome.storage.sync.set({ server })
  saved.textContent = server ? 'Saved.' : 'Cleared.'
  await show()
})
signIn.addEventListener('click', () => void login())
signOut.addEventListener('click', () => void logout())
void show()
