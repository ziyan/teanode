// The options page: where the server is, and who the extension is signed in
// as. Signing in opens the server's own authorization page in the browser's
// web sign-in flow; the page hands the token to this extension's callback
// address, which nothing else can receive.

import { serverAddressProblem } from './serverAddress'

const serverInput = document.getElementById('server')
const saved = document.getElementById('saved')
const who = document.getElementById('who')
const status = document.getElementById('status')
const signIn = document.getElementById('signin')
const signOut = document.getElementById('signout')

async function serverOrigin() {
  const { server } = await chrome.storage.local.get('server')
  return (server || '').replace(/\/+$/, '')
}

async function show() {
  const { username, token } = await chrome.storage.local.get(['username', 'token'])
  const origin = await serverOrigin()
  // An address saved before the rule against plain http is not used by the
  // extension: say so here instead of leaving the person to wonder why
  // nothing connects.
  const addressProblem = origin ? serverAddressProblem(origin) : ''
  if (addressProblem) {
    saved.textContent = `The saved server address is not used. ${addressProblem}`
    saved.className = 'status bad'
  }
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
  const addressProblem = serverAddressProblem(origin)
  if (addressProblem) {
    status.textContent = addressProblem
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
  // The opened Passwords keys go with the sign-in: they live only in the
  // session storage, and are dropped here as well as by the service worker.
  await chrome.storage.session.remove('passwordsKeys')
  const { token, tokenId } = await chrome.storage.local.get(['token', 'tokenId'])
  const origin = await serverOrigin()
  // The token is sent to the server only if the address is one the extension
  // may send it to (https, or plain http to this computer). Whatever the
  // server says, or whether it is asked at all, the token is forgotten here.
  let isServerTold = false
  if (token && tokenId && origin && !serverAddressProblem(origin)) {
    try {
      const response = await fetch(`${origin}/api/v1/graphql`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ query: 'mutation ($tokenId: String!) { RevokeToken(tokenId: $tokenId) }', variables: { tokenId } }),
      })
      isServerTold = response.ok
    } catch {
      // The server was not reachable; the token expires on its own.
    }
  }
  await chrome.storage.local.remove(['token', 'tokenId', 'username'])
  const isTokenKnownToServer = Boolean(token && tokenId)
  if (isTokenKnownToServer && !isServerTold) {
    status.textContent = 'Signed out here. The server could not be told to forget this sign-in, so remove it on the dashboard if you want it gone now.'
    status.className = 'status bad'
  } else {
    status.textContent = 'Signed out.'
    status.className = 'status'
  }
  await show()
}

// The sites the agent ran script on, which the service worker keeps in
// chrome.storage.local under this key (as a list of origins). The section
// stays hidden while there are none.
const SCRIPTED_ORIGINS_KEY = 'scriptedOrigins'
const scriptedSection = document.getElementById('scripted')
const scriptedList = document.getElementById('scripted-origins')
const scriptedStatus = document.getElementById('scripted-status')

function scriptedOriginsOf(stored) {
  if (Array.isArray(stored)) return stored.filter((origin) => typeof origin === 'string' && origin !== '')
  if (typeof stored === 'object' && stored !== null) return Object.keys(stored)
  return []
}

async function showScriptedOrigins() {
  const stored = (await chrome.storage.local.get(SCRIPTED_ORIGINS_KEY))[SCRIPTED_ORIGINS_KEY]
  const origins = scriptedOriginsOf(stored)
  scriptedList.replaceChildren(
    ...origins.map((origin) => {
      const item = document.createElement('li')
      item.textContent = origin
      return item
    }),
  )
  scriptedSection.hidden = origins.length === 0
}

document.getElementById('scripted-clear').addEventListener('click', async () => {
  scriptedStatus.textContent = ''
  scriptedStatus.className = 'status'
  try {
    const answer = await chrome.runtime.sendMessage({ type: 'passwords:clear-scripted-origins' })
    if (answer && typeof answer.error === 'string') throw new Error(answer.error)
    scriptedStatus.textContent = 'The agent can fill on these sites again.'
    scriptedStatus.className = 'status good'
  } catch (error) {
    scriptedStatus.textContent = String(error && error.message ? error.message : error)
    scriptedStatus.className = 'status bad'
  }
  await showScriptedOrigins()
})
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && SCRIPTED_ORIGINS_KEY in changes) void showScriptedOrigins()
})

serverOrigin().then((origin) => {
  serverInput.value = origin
})
document.getElementById('save').addEventListener('click', async () => {
  const server = serverInput.value.trim().replace(/\/+$/, '')
  const addressProblem = server ? serverAddressProblem(server) : ''
  if (addressProblem) {
    saved.textContent = addressProblem
    saved.className = 'status bad'
    return
  }
  await chrome.storage.local.set({ server })
  saved.textContent = server ? 'Saved.' : 'Cleared.'
  saved.className = 'status'
  await show()
})
document.getElementById('passwords').addEventListener('click', () => {
  chrome.runtime.sendMessage({ type: 'passwords:open-popup-window' })
})
signIn.addEventListener('click', () => void login())
signOut.addEventListener('click', () => void logout())
void show()
void showScriptedOrigins()
