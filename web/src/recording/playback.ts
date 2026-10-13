import { locateTimelinePosition, pieceAfter, type TimelinePiece } from './timeline'

// What the one player shows: where on the recording's timeline it is, whether
// it is playing, and which piece (recording medium) that position is in.
export interface PlaybackState {
  positionMS: number
  isPlaying: boolean
  activeMediaId: string | null
  errorMessage: string | null
}

const unavailableMessage = 'The original passage is not available for playback.'

// RecordingPassagePlayback is the one player of a recording. Each piece has its
// own media element (registered by its media id), but only one of them plays at
// a time and the controller presents them as a single timeline: seeking maps a
// position to the piece and offset, playback goes on into the next piece, and
// the gap between two pieces is skipped. Passage bounds are absolute recording
// times; each original keeps its own timeline offset.
export class RecordingPassagePlayback {
  private fallbacks = new Map<
    string,
    { prepare: () => Promise<HTMLMediaElement>; isOriginalUnsupported: () => boolean }
  >()
  private players = new Map<string, HTMLMediaElement>()
  private detachers = new Map<string, () => void>()
  private pieces: TimelinePiece[] = []
  private state: PlaybackState = { positionMS: 0, isPlaying: false, activeMediaId: null, errorMessage: null }
  private listeners = new Set<() => void>()
  private stopPassage: (() => void) | null = null
  // A passage played from the processing panel stops at its end; playing from a
  // position does not, and goes on into the next piece.
  private isBounded = false
  // Where a bubble played from the conversation ends on the timeline. Pausing
  // keeps it so that playing on stops there too; a seek or a new selection
  // that is not a bubble clears it.
  private boundEndMS: number | null = null
  private playbackGeneration = 0

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  getState = () => this.state

  private setState(change: Partial<PlaybackState>) {
    const next = { ...this.state, ...change }
    if (
      next.positionMS === this.state.positionMS &&
      next.isPlaying === this.state.isPlaying &&
      next.activeMediaId === this.state.activeMediaId &&
      next.errorMessage === this.state.errorMessage
    )
      return
    this.state = next
    for (const listener of this.listeners) listener()
  }

  // configure says where the pieces sit on the timeline. Until a piece has been
  // chosen, the first one is the active piece.
  configure(pieces: TimelinePiece[]) {
    this.pieces = pieces
    if (!pieces.some((piece) => piece.mediaId === this.state.activeMediaId))
      this.setState({ activeMediaId: pieces[0]?.mediaId ?? null, positionMS: 0, isPlaying: false })
  }

  private pieceStartMS(mediaId: string) {
    return this.pieces.find((piece) => piece.mediaId === mediaId)?.startMS ?? 0
  }

  register(mediaId: string, player: HTMLMediaElement | null) {
    const previousPlayer = this.players.get(mediaId)
    if (previousPlayer === player) return
    if (previousPlayer) previousPlayer.pause()
    this.detachers.get(mediaId)?.()
    this.detachers.delete(mediaId)
    if (!player) {
      this.players.delete(mediaId)
      return
    }
    this.players.set(mediaId, player)
    const isActive = () => this.state.activeMediaId === mediaId && this.players.get(mediaId) === player
    const follow = () => {
      if (isActive()) this.setState({ positionMS: this.pieceStartMS(mediaId) + player.currentTime * 1000 })
    }
    const onPlay = () => {
      if (isActive()) this.setState({ isPlaying: true, errorMessage: null })
    }
    const onPause = () => {
      if (isActive()) this.setState({ isPlaying: false })
    }
    const onEnded = () => {
      if (isActive() && !this.isBounded) void this.continueIntoNextPiece(mediaId)
    }
    const events: [string, () => void][] = [
      ['timeupdate', follow],
      ['seeked', follow],
      ['play', onPlay],
      ['pause', onPause],
      ['ended', onEnded],
    ]
    for (const [name, handler] of events) player.addEventListener(name, handler)
    this.detachers.set(mediaId, () => {
      for (const [name, handler] of events) player.removeEventListener(name, handler)
    })
  }

  registerFallback(
    mediaId: string,
    fallback: { prepare: () => Promise<HTMLMediaElement>; isOriginalUnsupported: () => boolean } | null,
  ) {
    if (fallback) this.fallbacks.set(mediaId, fallback)
    else this.fallbacks.delete(mediaId)
  }

  private async continueIntoNextPiece(mediaId: string) {
    const next = pieceAfter(this.pieces, mediaId)
    if (!next) {
      this.setState({ isPlaying: false })
      return
    }
    try {
      await this.playFrom(next.mediaId, 0, null)
    } catch (caught) {
      this.setState({ isPlaying: false, errorMessage: caught instanceof Error ? caught.message : String(caught) })
    }
  }

  // seekTo moves the player to a position on the timeline, and plays from there
  // when asked to. A position in a gap lands at the start of the next piece.
  async seekTo(positionMS: number, shouldPlay: boolean) {
    this.boundEndMS = null
    await this.seekWithin(positionMS, shouldPlay, null)
  }

  // playRange plays one stretch of the timeline, a conversation bubble, from
  // its start and stops at its end. The end is in the same piece as the start;
  // a stretch that runs past the end of its piece stops there.
  async playRange(startMS: number, endMS: number) {
    this.boundEndMS = endMS
    try {
      await this.seekWithin(startMS, true, endMS)
    } catch (caught) {
      this.boundEndMS = null
      throw caught
    }
  }

  private async seekWithin(positionMS: number, shouldPlay: boolean, endMS: number | null) {
    const located = locateTimelinePosition(this.pieces, positionMS)
    if (!located) throw new Error(unavailableMessage)
    if (shouldPlay) {
      // The end is bounded by the start's piece, so a stretch past its end plays to
      // the end of the piece and stops, never on into the next one.
      const endSeconds =
        endMS === null ? null : Math.max(0, Math.min(endMS, located.piece.endMS) - located.piece.startMS) / 1000
      await this.playFrom(located.piece.mediaId, located.offsetMS / 1000, endSeconds)
      return
    }
    this.stop()
    this.setState({ activeMediaId: located.piece.mediaId, positionMS: located.positionMS, isPlaying: false })
    const player = this.players.get(located.piece.mediaId)
    if (player) player.currentTime = located.offsetMS / 1000
  }

  // toggle pauses what plays, or plays on from where the player stopped; a
  // player that reached the end of the recording begins again.
  async toggle(timelineEndMS: number) {
    if (this.state.isPlaying) {
      this.stop()
      return
    }
    if (this.boundEndMS !== null && this.state.positionMS < this.boundEndMS) {
      await this.playRange(this.state.positionMS, this.boundEndMS)
      return
    }
    await this.seekTo(this.state.positionMS >= timelineEndMS ? 0 : this.state.positionMS, true)
  }

  async play(mediaId: string, passageStartMS: number, passageEndMS: number, timelineStartMS: number) {
    this.boundEndMS = null
    const startSeconds = (passageStartMS - timelineStartMS) / 1000
    const endSeconds = (passageEndMS - timelineStartMS) / 1000
    if (
      !this.players.get(mediaId) ||
      !Number.isFinite(startSeconds) ||
      !Number.isFinite(endSeconds) ||
      startSeconds < 0 ||
      endSeconds <= startSeconds
    )
      throw new Error(unavailableMessage)
    await this.playFrom(mediaId, startSeconds, endSeconds)
  }

  // playFrom plays one piece from an offset, up to endSeconds when given.
  private async playFrom(mediaId: string, startSeconds: number, endSeconds: number | null) {
    this.stop()
    const playbackGeneration = this.playbackGeneration
    let player = this.players.get(mediaId)
    if (!player) throw new Error(unavailableMessage)
    this.isBounded = endSeconds !== null
    this.setState({
      activeMediaId: mediaId,
      positionMS: this.pieceStartMS(mediaId) + startSeconds * 1000,
      errorMessage: null,
    })
    const playPlayer = async (selectedPlayer: HTMLMediaElement) => {
      if (endSeconds !== null) {
        const stopAtEnd = () => {
          if (selectedPlayer.currentTime >= endSeconds) {
            this.stopPassage?.()
            this.stopPassage = null
            selectedPlayer.pause()
            selectedPlayer.currentTime = endSeconds
          }
        }
        selectedPlayer.addEventListener('timeupdate', stopAtEnd)
        selectedPlayer.addEventListener('seeked', stopAtEnd)
        this.stopPassage = () => {
          selectedPlayer.removeEventListener('timeupdate', stopAtEnd)
          selectedPlayer.removeEventListener('seeked', stopAtEnd)
        }
      }
      selectedPlayer.currentTime = startSeconds
      await selectedPlayer.play()
    }
    const fallback = this.fallbacks.get(mediaId)
    let hasPreparedPreview = false
    try {
      if (fallback?.isOriginalUnsupported()) {
        hasPreparedPreview = true
        player = await fallback.prepare()
        if (playbackGeneration !== this.playbackGeneration) return
      }
      try {
        await playPlayer(player)
      } catch (caught) {
        if (playbackGeneration !== this.playbackGeneration) return
        if (
          hasPreparedPreview ||
          !fallback ||
          !(player.error?.code === 4 || (caught instanceof Error && caught.name === 'NotSupportedError'))
        )
          throw caught
        this.stopPassage?.()
        this.stopPassage = null
        player.pause()
        player = await fallback.prepare()
        if (playbackGeneration !== this.playbackGeneration) return
        await playPlayer(player)
      }
    } catch (caught) {
      // A navigation or another selection can supersede preparation and playback.
      if (playbackGeneration !== this.playbackGeneration) return
      this.stop()
      throw caught
    }
  }

  async playCompatible(mediaId: string) {
    this.boundEndMS = null
    this.stop()
    this.setState({ activeMediaId: mediaId })
    const playbackGeneration = this.playbackGeneration
    const fallback = this.fallbacks.get(mediaId)
    if (!fallback) throw new Error('Compatible playback is not available for this original.')
    try {
      const player = await fallback.prepare()
      if (playbackGeneration !== this.playbackGeneration) return
      await player.play()
    } catch (caught) {
      if (playbackGeneration !== this.playbackGeneration) return
      this.stop()
      throw caught
    }
  }

  // end stops what plays and forgets where a bubble was to end, so that playing
  // again from the top player goes on as it does from a seek.
  end() {
    this.boundEndMS = null
    this.stop()
  }

  stop() {
    this.playbackGeneration++
    this.stopPassage?.()
    this.stopPassage = null
    this.isBounded = false
    for (const player of this.players.values()) player.pause()
    this.setState({ isPlaying: false })
  }
}
