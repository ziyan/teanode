// The extension keeps the token it signs in with and the server address it
// signs in to in chrome.storage.local and, for Passwords, the public keys it
// has pinned. By default every part of the extension can read those areas,
// including its content scripts, which run inside web pages. A content script
// is the part of the extension nearest to a hostile page, so the two areas are
// closed to everything but the extension's own pages and its service worker.
// (chrome.storage.session is closed to content scripts already.) What a
// content script needs from them it asks the service worker for.
//
// The setting does not last, so it is made each time the service worker
// starts. A browser without it, or one that refuses, is left as it is.

export async function restrictStorageToTrustedContexts(): Promise<void> {
  for (const areaName of ['local', 'sync'] as const) {
    try {
      await chrome.storage[areaName].setAccessLevel({ accessLevel: 'TRUSTED_CONTEXTS' })
    } catch {
      // Not available in this browser.
    }
  }
}
