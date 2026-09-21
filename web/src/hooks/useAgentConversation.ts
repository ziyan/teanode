import { useCallback, useEffect, useRef, useState } from 'react'

interface ConversationSnapshot {
  conversation: { id: string }
}

// Own each read until it settles, including transports that ignore abort.
// A reconnect may refresh the selected conversation, but cannot select another.
export function useAgentConversation<Snapshot extends ConversationSnapshot>(
  initialConversationId: string,
  readSnapshot: (conversationId: string, signal: AbortSignal) => Promise<Snapshot>,
  applySnapshot: (snapshot: Snapshot, requestedAt: number) => void,
) {
  const [conversationId, setConversationId] = useState(initialConversationId)
  const selectedConversation = useRef(initialConversationId)
  const displayedConversation = useRef(initialConversationId)
  const currentRead = useRef<Promise<void> | null>(null)
  const readController = useRef<AbortController | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [readFailure, setReadFailure] = useState<unknown>(null)

  const cancelRead = useCallback(() => {
    readController.current?.abort()
    readController.current = null
    currentRead.current = null
  }, [])

  useEffect(() => cancelRead, [cancelRead])

  const adoptConversation = useCallback(
    (conversationId: string) => {
      cancelRead()
      selectedConversation.current = conversationId
      displayedConversation.current = conversationId
      setConversationId(conversationId)
      setIsLoading(false)
      setReadFailure(null)
    },
    [cancelRead],
  )

  const readConversation = useCallback(
    (conversationId: string, isSelection = false): Promise<void> => {
      if (!isSelection && conversationId !== selectedConversation.current) return Promise.resolve()
      cancelRead()
      selectedConversation.current = conversationId
      const controller = new AbortController()
      readController.current = controller
      const requestedAt = Date.now()
      setIsLoading(true)
      setReadFailure(null)
      const isCurrent = () => readController.current === controller && !controller.signal.aborted
      const reading = Promise.resolve()
        .then(() => readSnapshot(conversationId, controller.signal))
        .then((snapshot) => {
          if (!isCurrent()) return
          selectedConversation.current = snapshot.conversation.id
          displayedConversation.current = snapshot.conversation.id
          setConversationId(snapshot.conversation.id)
          applySnapshot(snapshot, requestedAt)
        })
        .catch((caught: unknown) => {
          if (!isCurrent()) return
          selectedConversation.current = displayedConversation.current
          setReadFailure(caught)
          throw caught
        })
        .finally(() => {
          if (!isCurrent()) return
          readController.current = null
          currentRead.current = null
          setIsLoading(false)
        })
      currentRead.current = reading
      return reading
    },
    [applySnapshot, cancelRead, readSnapshot],
  )

  return {
    conversationId,
    selectedConversation,
    currentRead,
    isLoading,
    readFailure,
    readConversation,
    adoptConversation,
  }
}
