import { useState } from 'react'
import { FormDialog } from '../../components/dialog'
import { useTranslation } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
import { browserOffsetMinutes, formatOffset, localFieldValue, takenAtFromField } from '../../photos/takenAt'

// ChangeDateDialog corrects the date a photo was taken: a date and a time as
// the clock showed them where it was taken. The zone is the photo's own offset
// when it has one, since the time it was taken is a time of that place, and the
// browser's when it has none. The date is sent as RFC 3339 with that offset,
// which the server keeps as the offset the photo is shown at.
export function ChangeDateDialog({
  photo,
  isBusy,
  error,
  onSubmit,
  onClose,
}: {
  photo: Photo
  isBusy?: boolean
  error?: string | null
  onSubmit: (takenAt: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  // A photo with no date of its own is placed where it arrived.
  const instant = photo.takenAt ?? photo.createdAt
  const ownOffsetMinutes = photo.takenAtOffsetMinutes
  const [fieldValue, setFieldValue] = useState(() =>
    localFieldValue(instant, ownOffsetMinutes ?? browserOffsetMinutes(new Date(instant))),
  )
  // The browser's offset is the one at the time that was typed, so a date
  // across a change of clocks gets the offset it had then.
  const typedMoment = new Date(fieldValue)
  const offsetMinutes =
    ownOffsetMinutes ?? (Number.isNaN(typedMoment.getTime()) ? 0 : browserOffsetMinutes(typedMoment))
  const takenAt = takenAtFromField(fieldValue, offsetMinutes)
  return (
    <FormDialog
      title={t('photos.changeDateTitle')}
      submitLabel={t('common.save')}
      busy={isBusy}
      error={error}
      canSubmit={takenAt !== null}
      onSubmit={() => takenAt && onSubmit(takenAt)}
      onClose={onClose}
    >
      <p className="muted">{t('photos.changeDateHint')}</p>
      <label>
        <span>{t('photos.changeDateField')}</span>
        <input type="datetime-local" value={fieldValue} onChange={(event) => setFieldValue(event.target.value)} />
      </label>
      <p className="muted field-hint">
        {t(ownOffsetMinutes === null ? 'photos.changeDateZoneBrowser' : 'photos.changeDateZonePhoto', {
          zone: `UTC${formatOffset(offsetMinutes)}`,
        })}
      </p>
    </FormDialog>
  )
}
