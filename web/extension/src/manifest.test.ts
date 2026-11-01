import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const manifest = JSON.parse(readFileSync(resolve(__dirname, '../manifest.json'), 'utf8'))

describe('the manifest', () => {
  it("opens the button as a dropdown and the chat in Chrome's side panel", () => {
    // With a default_popup Chrome never fires action.onClicked, so the chat
    // is opened from a button in the dropdown, with chrome.sidePanel.open.
    expect(manifest.action.default_popup).toBe('passwords.html')
    expect(manifest.permissions).toContain('sidePanel')
    expect(manifest.side_panel.default_path).toBe('sidepanel.html')
  })

  it('needs a Chrome that has the side panel, and opens it from a click', () => {
    // The dropdown opens the side panel with chrome.sidePanel.open, which an
    // older Chrome does not have.
    expect(Number(manifest.minimum_chrome_version)).toBeGreaterThanOrEqual(127)
  })

  it('lets no website load a page of the extension', () => {
    // The agent used to put its panel and a suggestion bar inside the sites
    // the person visits; nothing of the extension is framed there any more.
    for (const resource of manifest.web_accessible_resources ?? []) {
      expect(resource.resources).not.toContain('suggestion.html')
      expect(resource.resources).not.toContain('bar.html')
      expect(resource.use_dynamic_url).toBeUndefined()
    }
  })
})
