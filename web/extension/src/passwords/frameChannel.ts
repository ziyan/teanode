// The messages between a website's content script and the extension's frames
// drawn over it. They travel on a MessageChannel, not on the window: the
// content script makes the channel, posts one end of it to the frame once the
// frame has loaded (this "connect" message is the only message that crosses
// the page's window, and it is addressed to the extension's origin), and from
// then on both sides talk through the channel's ports, which nothing else on
// the page can reach or hear.
export const FRAME_MESSAGE_CHANNEL = 'teanode-passwords-frame'
export const FRAME_CONNECT_KIND = 'connect'

// originOfAddress is the origin messages to the address are posted to. A
// browser that does not give an extension address an origin of its own (the
// answer is the text "null") is read as the scheme and host.
export function originOfAddress(address: string): string {
  const url = new URL(address)
  return url.origin === 'null' ? `${url.protocol}//${url.host}` : url.origin
}

// frameOriginOf is the origin the document of an extension page's frame has,
// which is where the connect message is addressed: the extension's own origin,
// by its fixed id. The frame's address is not asked, because with
// use_dynamic_url in the manifest chrome.runtime.getURL names a dynamic id
// that the document inside is not at, and a message addressed to the dynamic
// origin is dropped without a word. (The manifest does not use it: in Chrome
// 154 the debugger is detached from a tab, with "target_closed", the moment
// such a frame opens in it, which ends a take-over's stream and the agent's
// own debugging session.) Where there is no id, the origin of the address is
// the best there is.
export function frameOriginOf(address: string): string {
  const id = typeof chrome === 'undefined' ? '' : chrome.runtime?.id
  return typeof id === 'string' && id !== '' ? `chrome-extension://${id}` : originOfAddress(address)
}
