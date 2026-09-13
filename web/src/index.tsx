import React from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import './style.css'
import { App } from './app'
import { initializeTheme } from './components/theme'
import { ToastProvider } from './components/toast'
import { TranslationProvider, detectLanguage, loadCatalog } from './i18n/i18n'
import { startRipples } from './ripple'

// The page already did this inline, before the bundle was fetched. Doing it
// again keeps the two from drifting if the storage key ever changes.
initializeTheme()

// Presses leave a mark, everywhere rather than on the few controls somebody
// remembered to decorate. Listens on the document, so a control added later
// gets it without knowing about it.
startRipples()

const container = document.getElementById('teanode')
if (!container) {
  throw new Error('the page is missing its root element')
}

// The words first, then the page. English is in the bundle; the other two are
// a file of their own, and drawing before it arrives would show a reader of
// Chinese a screen of English and correct it a moment later. It is one small
// fetch in parallel with nothing, because nothing can be drawn without it.
void loadCatalog(detectLanguage()).then(() => {
  createRoot(container).render(
    <React.StrictMode>
      <BrowserRouter>
        <TranslationProvider>
          <ToastProvider>
            <App />
          </ToastProvider>
        </TranslationProvider>
      </BrowserRouter>
    </React.StrictMode>,
  )
})
