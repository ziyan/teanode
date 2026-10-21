import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const manifest = JSON.parse(readFileSync(resolve(__dirname, '../manifest.json'), 'utf8'))

describe('the manifest', () => {
  it('does not give the frames a dynamic address', () => {
    // With use_dynamic_url, Chrome 154 detaches the debugger from a tab the
    // moment one of the extension's frames opens in it, which ends a take-over's
    // stream and the agent's own debugging session.
    for (const resource of manifest.web_accessible_resources) {
      expect(resource.use_dynamic_url).toBeUndefined()
    }
  })
})
