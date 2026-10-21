import { Component, ErrorInfo, ReactNode } from 'react'
import { useLocation } from 'react-router-dom'

import { useTranslation } from '../i18n/i18n'

// A page that throws while drawing would otherwise unmount the whole
// dashboard and leave a blank window. The boundary keeps the rest of the
// dashboard and shows a message with a button that reloads the page. It
// clears itself when the address changes, so going to another page works
// without a reload.
//
// It prints nothing of the error to the page: the text can hold something
// that was decrypted on this device (Passwords shows item values), and the
// console has it for whoever debugs.

type BoundaryProps = {
  // resetKey changes when the boundary should try drawing its children again.
  resetKey: string
  children: ReactNode
}

type BoundaryState = { hasFailed: boolean }

class Boundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { hasFailed: false }

  static getDerivedStateFromError(): BoundaryState {
    return { hasFailed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('A page failed to draw', error, info.componentStack)
  }

  componentDidUpdate(previous: BoundaryProps) {
    if (this.state.hasFailed && previous.resetKey !== this.props.resetKey) {
      this.setState({ hasFailed: false })
    }
  }

  render() {
    return this.state.hasFailed ? <ErrorBoundaryMessage /> : this.props.children
  }
}

function ErrorBoundaryMessage() {
  const { t } = useTranslation()
  return (
    <div className="card" role="alert">
      <h2>{t('errorBoundary.title')}</h2>
      <p className="muted">{t('errorBoundary.body')}</p>
      <div className="page-actions">
        <button type="button" className="primary" onClick={() => window.location.reload()}>
          {t('errorBoundary.reload')}
        </button>
      </div>
    </div>
  )
}

// RouteErrorBoundary wraps the routes: a page that fails shows the message
// instead of a blank window, and moving to another page tries again.
export function RouteErrorBoundary({ children }: { children: ReactNode }) {
  const location = useLocation()
  return <Boundary resetKey={location.pathname}>{children}</Boundary>
}
