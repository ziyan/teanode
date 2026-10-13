// Passage bounds are absolute recording times; each original keeps its own timeline offset.
export class RecordingPassagePlayback {
  private fallbacks = new Map<
    string,
    { prepare: () => Promise<HTMLMediaElement>; isOriginalUnsupported: () => boolean }
  >()
  private players = new Map<string, HTMLMediaElement>()
  private stopPassage: (() => void) | null = null
  private playbackGeneration = 0

  register(mediaId: string, player: HTMLMediaElement | null) {
    const previousPlayer = this.players.get(mediaId)
    if (previousPlayer && previousPlayer !== player) previousPlayer.pause()
    if (player) this.players.set(mediaId, player)
    else this.players.delete(mediaId)
  }

  registerFallback(
    mediaId: string,
    fallback: { prepare: () => Promise<HTMLMediaElement>; isOriginalUnsupported: () => boolean } | null,
  ) {
    if (fallback) this.fallbacks.set(mediaId, fallback)
    else this.fallbacks.delete(mediaId)
  }

  async play(mediaId: string, passageStartMS: number, passageEndMS: number, timelineStartMS: number) {
    this.stop()
    const playbackGeneration = this.playbackGeneration
    let player = this.players.get(mediaId)
    const startSeconds = (passageStartMS - timelineStartMS) / 1000
    const endSeconds = (passageEndMS - timelineStartMS) / 1000
    if (
      !player ||
      !Number.isFinite(startSeconds) ||
      !Number.isFinite(endSeconds) ||
      startSeconds < 0 ||
      endSeconds <= startSeconds
    )
      throw new Error('The original passage is not available for playback.')
    const playPlayer = async (selectedPlayer: HTMLMediaElement) => {
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
    this.stop()
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

  stop() {
    this.playbackGeneration++
    this.stopPassage?.()
    this.stopPassage = null
    for (const player of this.players.values()) player.pause()
  }
}
