import { useEffect, useRef, useState } from 'react'
import { authorization, graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { Select } from './select'
import { useToast } from './toast'
import { SpeakerIcon } from './icons'

const READ_SPEECH_VOICES = `query { ReadAgentVoice { isVoiceAvailable speechVoice speechVoices serverSpeechVoice } }`

// SpeechVoiceChoice is the voice a person's answers are read aloud in on a
// voice call: one of the provider's, or the server's, with a few words in
// the one picked to hear it before deciding. Shown only where voice is on.
export function SpeechVoiceChoice({
  speechVoice,
  busy,
  onSave,
}: {
  speechVoice: string
  busy: boolean
  onSave: (variables: Record<string, unknown>, done: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [voices, setVoices] = useState<{ speechVoices: string[]; serverSpeechVoice: string } | null>(null)
  const [isPlaying, setPlaying] = useState(false)
  const playing = useRef<HTMLAudioElement | null>(null)

  useEffect(() => {
    let cancelled = false
    graphql<{
      ReadAgentVoice: { isVoiceAvailable: boolean; speechVoices: string[]; serverSpeechVoice: string }
    }>(READ_SPEECH_VOICES)
      .then((response) => {
        if (!cancelled && response.ReadAgentVoice.isVoiceAvailable) setVoices(response.ReadAgentVoice)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
      playing.current?.pause()
    }
  }, [])
  if (!voices) return null

  const heard = speechVoice || voices.serverSpeechVoice
  const play = async () => {
    playing.current?.pause()
    setPlaying(true)
    try {
      const response = await fetch(`/api/v1/agent/voice/sample?speechVoice=${encodeURIComponent(heard)}`, {
        headers: authorization(),
        credentials: 'same-origin',
      })
      if (!response.ok) {
        const refusal = (await response.json().catch(() => ({}))) as { error?: string }
        throw new Error(refusal.error ?? response.statusText)
      }
      const address = URL.createObjectURL(await response.blob())
      const audio = new Audio(address)
      playing.current = audio
      audio.onended = () => {
        URL.revokeObjectURL(address)
        setPlaying(false)
      }
      await audio.play()
    } catch (caught) {
      setPlaying(false)
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  return (
    <div className="settings-subform">
      <h4>{t('agent.speechVoice')}</h4>
      <p className="muted">{t('agent.speechVoiceHint')}</p>
      <div className="form-narrow">
        <div className="row speech-voice-row">
          <label>
            <span>{t('agent.speechVoice')}</span>
            <Select
              block
              value={speechVoice}
              disabled={busy}
              label={t('agent.speechVoice')}
              options={[
                { value: '', label: t('agent.speechVoiceServer', { speechVoice: voices.serverSpeechVoice }) },
                ...voices.speechVoices.map((choice) => ({ value: choice, label: choice })),
              ]}
              onChange={(value) => void onSave({ speechVoice: value }, t('agent.saved'))}
            />
          </label>
          <button type="button" className="button" disabled={isPlaying} onClick={() => void play()}>
            <SpeakerIcon size={14} /> {isPlaying ? t('agent.speechVoicePlaying') : t('agent.speechVoiceListen')}
          </button>
        </div>
      </div>
    </div>
  )
}
