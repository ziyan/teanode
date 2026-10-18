import { PhotoBackupGuide } from '../../components/photoBackupGuide'
import { UploadIcon } from '../../components/icons'
import { CopyButton } from '../../components/settingsList'
import { Trans, useTranslation } from '../../i18n/i18n'

// Where the command line's reference says how a folder of photos is uploaded.
const COMMAND_LINE_GUIDE_URL = 'https://github.com/ziyan/teanode/blob/main/docs/reference/command-line.md#teanode-photo'

// FirstPhotos is what an empty library says below its one line: the three ways
// photos get in, each with the button that starts it. The iPhone comes first,
// since it is what the library is for; the upload from this computer is the
// one that needs no setup; the command line is for a folder too big to click
// through. Shown only on the library itself, never on an album or a search
// that found nothing.
export function FirstPhotos({ onChooseFiles }: { onChooseFiles: () => void }) {
  const { t } = useTranslation()
  const commands = [
    { command: `teanode auth login --url ${window.location.origin}`, label: t('photos.copySignInCommand') },
    { command: 'teanode photo upload ~/Pictures', label: t('photos.copyUploadCommand') },
  ]
  return (
    <>
      <PhotoBackupGuide canConnect />
      <section className="card photo-way">
        <h3>{t('photos.wayComputerTitle')}</h3>
        <p className="muted">{t('photos.wayComputer')}</p>
        <div className="page-actions">
          <button type="button" onClick={onChooseFiles}>
            <UploadIcon size={16} /> {t('photos.upload')}
          </button>
        </div>
      </section>
      <section className="card photo-way">
        <h3>{t('photos.wayCommandLineTitle')}</h3>
        <p className="muted">
          <Trans
            k="photos.wayCommandLine"
            nodes={{
              guide: (
                <a href={COMMAND_LINE_GUIDE_URL} target="_blank" rel="noreferrer">
                  {t('photos.wayCommandLineGuide')}
                </a>
              ),
            }}
          />
        </p>
        {commands.map(({ command, label }) => (
          <div className="photo-way-command" key={command}>
            <code>{command}</code>
            <CopyButton value={command} label={label} />
          </div>
        ))}
      </section>
    </>
  )
}
