// Installed in the page's own world at document_start (manifest content_scripts
// with "world": "MAIN"). See passkeyOverride.ts.

import { installPasskeyOverride } from './passkeyOverride'

installPasskeyOverride(window)
