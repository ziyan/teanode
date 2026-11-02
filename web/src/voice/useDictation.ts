import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'

import { graphql, framedDrawer } from '../api'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { Dictation, type DictationPlace } from './dictation'

// READ_VOICE says whether the dashboard offers the microphone for dictation
// and for voice messages, and the rate the socket wants audio at.
export const READ_VOICE = `query { ReadAgentVoice { isVoiceAvailable isVoiceMessageAvailable sampleRate } }`

export type VoiceAvailability = {
  isDictationAvailable: boolean
  isVoiceMessageAvailable: boolean
  sampleRate: number
}

// useVoiceAvailability asks the server what voice it offers this person.
// The browser extension's drawer is another origin with a microphone
// permission of its own, so it is told nothing is offered; so is a disabled
// caller.
export function useVoiceAvailability(isEnabled = true): VoiceAvailability {
  const [availability, setAvailability] = useState<VoiceAvailability>({
    isDictationAvailable: false,
    isVoiceMessageAvailable: false,
    sampleRate: 0,
  })
  useEffect(() => {
    if (!isEnabled || framedDrawer) return
    let isCanceled = false
    graphql<{
      ReadAgentVoice: { isVoiceAvailable: boolean; isVoiceMessageAvailable?: boolean | null; sampleRate: number }
    }>(READ_VOICE)
      .then((response) => {
        if (isCanceled) return
        const voice = response.ReadAgentVoice
        setAvailability({
          isDictationAvailable: Boolean(voice.isVoiceAvailable) && voice.sampleRate > 0,
          isVoiceMessageAvailable: Boolean(voice.isVoiceMessageAvailable),
          sampleRate: voice.sampleRate,
        })
      })
      .catch(() => undefined)
    return () => {
      isCanceled = true
    }
  }, [isEnabled])
  return availability
}

// withWordsAt puts words into text where the cursor is, replacing what is
// selected, with a space between them and the words either side. It gives
// the text and where the cursor goes: after the words.
export function withWordsAt(
  text: string,
  selectionStart: number,
  selectionEnd: number,
  words: string,
): { text: string; caret: number } {
  const start = Math.max(0, Math.min(selectionStart, text.length))
  const end = Math.max(start, Math.min(selectionEnd, text.length))
  const before = text.slice(0, start)
  const after = text.slice(end)
  const leading = before !== '' && !/\s$/.test(before) ? ' ' : ''
  const trailing = after !== '' && !/^\s/.test(after) ? ' ' : ''
  const inserted = `${leading}${words}${trailing}`
  return { text: `${before}${inserted}${after}`, caret: before.length + leading.length + words.length }
}

export type DictationState = 'off' | 'starting' | 'listening'

// useDictation dictates into a textarea. Each finished utterance goes in at
// the cursor, with a space, through onInsert; nothing is sent. The hook
// stops itself when the box goes away (unmount) and hands its caller abort
// for the moments the box is sent, switched or closed.
export function useDictation({
  place,
  input,
  value,
  sampleRate,
  onInsert,
}: {
  place: DictationPlace
  input: RefObject<HTMLTextAreaElement | null>
  // The text in the box now.
  value: string
  sampleRate: number
  // The box takes new text; the hook then moves the cursor after the words.
  onInsert: (text: string) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [state, setState] = useState<DictationState>('off')
  const [captionText, setCaptionText] = useState('')
  const dictation = useRef<Dictation | null>(null)
  // Dictations stopped by the person that are still transcribing the words
  // they were speaking.
  const finishing = useRef(new Set<Dictation>())
  const latest = useRef({ value, onInsert })
  latest.current = { value, onInsert }

  const insert = useCallback(
    (words: string) => {
      const element = input.current
      const text = element ? element.value : latest.current.value
      const start = element ? element.selectionStart : text.length
      const end = element ? element.selectionEnd : text.length
      const next = withWordsAt(text, start, end, words)
      latest.current.onInsert(next.text)
      // The box is controlled: its value is the new text only after the
      // render, and setting the value moves the cursor to the end, so the
      // cursor is put after the words once it has.
      window.requestAnimationFrame(() => {
        const current = input.current
        if (current && current.value === next.text) current.setSelectionRange(next.caret, next.caret)
      })
    },
    [input],
  )

  const abort = useCallback(() => {
    for (const stopped of finishing.current) stopped.abort()
    finishing.current.clear()
    const running = dictation.current
    if (!running) return
    dictation.current = null
    running.abort()
    setState('off')
    setCaptionText('')
  }, [])

  const toggle = () => {
    const running = dictation.current
    if (running) {
      // The words being spoken still land, a moment later.
      dictation.current = null
      finishing.current.add(running)
      running.stop()
      setState('off')
      setCaptionText('')
      return
    }
    setState('starting')
    const session: Dictation = new Dictation(
      {
        onListening: () => {
          if (dictation.current === session) setState('listening')
        },
        onCaption: (words) => {
          if (dictation.current === session) setCaptionText(words)
        },
        onWords: insert,
        onProblem: (problemText) => toast.failed(problemText),
        onEnded: () => {
          finishing.current.delete(session)
          if (dictation.current !== session) return
          dictation.current = null
          setState('off')
          setCaptionText('')
        },
      },
      {
        microphoneRefused: t('agentDrawer.voiceMicrophoneRefused'),
        connectionLost: t('agentDrawer.voiceConnectionLost'),
      },
    )
    dictation.current = session
    void session.start(sampleRate, place).catch((caught) => {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
      session.abort()
    })
  }

  // Escape stops it, wherever the focus is.
  useEffect(() => {
    if (state === 'off') return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') abort()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [state, abort])

  // A microphone left listening behind a box that is gone is not
  // acceptable.
  useEffect(() => abort, [abort])

  return { state, captionText, toggle, abort }
}
