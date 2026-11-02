import type { ChatCall } from './chatCallApi'

// The calls this tab has been told of, by id, as the signed-in person sees
// them: what ListGoingChatCalls and ReadGoingChatCall answered, and each
// change ChatCallEvents has told since. Every part of the dashboard that draws
// a call (the ringing card, the banner at the top of a chat, a call's line in
// the transcript) reads from here, so they agree without asking the server
// separately.
//
// Kept apart from React so that it can be tested by calling it.
export class ChatCallStore {
  private calls = new Map<string, ChatCall>()
  // When each call was last written, in the order of writes, so that a read
  // that was answered after a newer event does not undo it.
  private writes = new Map<string, number>()
  private writeCount = 0
  private snapshot: ChatCall[] = []
  private readonly listeners = new Set<() => void>()

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  getSnapshot = (): ChatCall[] => this.snapshot

  private publish() {
    this.snapshot = [...this.calls.values()]
    this.listeners.forEach((listener) => listener())
  }

  // apply keeps the call as it is now, whatever it was before.
  apply(call: ChatCall) {
    this.calls.set(call.id, call)
    this.writes.set(call.id, ++this.writeCount)
    this.publish()
  }

  // writeMark is the point in the order of writes a read is made at, to be
  // handed to settleConversation and settleGoing when its answer arrives.
  writeMark(): number {
    return this.writeCount
  }

  // settleGoing takes what ListGoingChatCalls answered, which is every call
  // going that rings the person or has them on. A call this tab thought was
  // ringing them or had them on, and is not in the list, has ended since,
  // unless an event wrote it after the read began.
  settleGoing(listed: ChatCall[], markedAt: number) {
    const listedIds = new Set(listed.map((call) => call.id))
    for (const call of this.calls.values()) {
      const isMine = call.isGoing && (call.askerRingState === 'ringing' || call.isAskerOn)
      if (isMine && !listedIds.has(call.id) && (this.writes.get(call.id) ?? 0) <= markedAt) {
        this.calls.delete(call.id)
        this.writes.delete(call.id)
      }
    }
    for (const call of listed) {
      if ((this.writes.get(call.id) ?? 0) <= markedAt) {
        this.calls.set(call.id, call)
        this.writes.set(call.id, ++this.writeCount)
      }
    }
    this.publish()
  }

  // settleConversation takes what ReadGoingChatCall answered for a chat: its
  // going call, or none. A call going there that this tab held, and the
  // server says is not, has ended since, unless an event wrote it after the
  // read began.
  settleConversation(conversationId: string, going: ChatCall | null, markedAt: number) {
    if (going) {
      if ((this.writes.get(going.id) ?? 0) <= markedAt) this.apply(going)
      return
    }
    let isChanged = false
    for (const call of this.calls.values()) {
      if (call.conversationId === conversationId && call.isGoing && (this.writes.get(call.id) ?? 0) <= markedAt) {
        this.calls.delete(call.id)
        this.writes.delete(call.id)
        isChanged = true
      }
    }
    if (isChanged) this.publish()
  }
}

// ringingCalls are the calls going that are ringing the person now.
export function ringingCalls(calls: ChatCall[]): ChatCall[] {
  return calls.filter((call) => call.isGoing && call.askerRingState === 'ringing')
}

// goingCallOf is the call going in a chat, as the person's conversation for
// it names the chat; null when none is.
export function goingCallOf(calls: ChatCall[], conversationId: string): ChatCall | null {
  return calls.find((call) => call.conversationId === conversationId && call.isGoing) ?? null
}
