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
  const playing = useRef<{ context: AudioContext; source?: AudioBufferSourceNode } | null>(null)

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
      void playing.current?.context.close().catch(() => undefined)
    }
  }, [])
  if (!voices) return null

  const heard = speechVoice || voices.serverSpeechVoice
  // play says the sample through Web Audio: the dashboard lets media
  // elements load only from this server, which a blob address is not, and
  // Safari on a phone plays only from an audio context made in the tap
  // itself, so it is made before anything is waited on.
  const play = async () => {
    void playing.current?.context.close().catch(() => undefined)
    const context = new AudioContext()
    void context.resume().catch(() => undefined)
    playing.current = { context }
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
      const buffer = wavBuffer(context, await response.arrayBuffer())
      if (playing.current?.context !== context) return
      const source = context.createBufferSource()
      source.buffer = buffer
      source.connect(context.destination)
      source.onended = () => {
        void context.close().catch(() => undefined)
        setPlaying(false)
      }
      playing.current = { context, source }
      source.start()
    } catch (caught) {
      void context.close().catch(() => undefined)
      setPlaying(false)
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  return (
    <div className="settings-subform">
      <h4>{t('agent.speechVoice')}</h4>
      <p className="muted">{t('agent.speechVoiceHint')}</p>
      <div className="form-narrow">
        <div className="speech-voice-row">
          <label>
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
          <button
            type="button"
            className="button-quiet speech-voice-listen"
            disabled={isPlaying}
            onClick={() => void play()}
          >
            <SpeakerIcon size={14} /> {isPlaying ? t('agent.speechVoicePlaying') : t('agent.speechVoiceListen')}
          </button>
        </div>
      </div>
    </div>
  )
}

// wavBuffer reads the server's sample, a WAV file of mono 16-bit PCM, into
// an audio buffer, at the rate its header says.
export function wavBuffer(context: BaseAudioContext, wav: ArrayBuffer): AudioBuffer {
  const view = new DataView(wav)
  if (wav.byteLength < 44 || view.getUint32(0, false) !== 0x52494646) throw new Error('not a WAV file')
  const sampleRate = view.getUint32(24, true)
  const sampleCount = Math.floor((wav.byteLength - 44) / 2)
  const buffer = context.createBuffer(1, Math.max(1, sampleCount), sampleRate)
  const channel = buffer.getChannelData(0)
  for (let index = 0; index < sampleCount; index++) channel[index] = view.getInt16(44 + index * 2, true) / 32768
  return buffer
}
