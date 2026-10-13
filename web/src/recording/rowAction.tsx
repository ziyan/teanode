import type { ReactNode } from 'react'
import { Tooltip } from '../components/tooltip'

export function RecordingRowAction({
  label,
  icon,
  disabled,
  isDestructive = false,
  onClick,
}: {
  label: string
  icon: ReactNode
  disabled?: boolean
  isDestructive?: boolean
  onClick: () => void
}) {
  return (
    <Tooltip label={label}>
      <button
        type="button"
        className={isDestructive ? 'icon-action danger' : 'icon-action'}
        aria-label={label}
        disabled={disabled}
        onClick={onClick}
      >
        {icon}
      </button>
    </Tooltip>
  )
}
