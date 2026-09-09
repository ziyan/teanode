import React from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import './style.css'
import { App } from './app'
import { initializeTheme } from './components/theme'
import { ToastProvider } from './components/toast'
import { TranslationProvider } from './i18n/i18n'
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
