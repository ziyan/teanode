// Passage bounds are absolute recording times; each original keeps its own timeline offset.
export class RecordingPassagePlayback {
  private players = new Map<string, HTMLMediaElement>()
  private stopPassage: (() => void) | null = null
  private playbackGeneration = 0

  register(mediaId: string, player: HTMLMediaElement | null) {
    const previousPlayer = this.players.get(mediaId)
    if (previousPlayer && previousPlayer !== player) previousPlayer.pause()
    if (player) this.players.set(mediaId, player)
    else this.players.delete(mediaId)
  }

  async play(mediaId: string, passageStartMS: number, passageEndMS: number, timelineStartMS: number) {
    this.stop()
    const playbackGeneration = this.playbackGeneration
    const player = this.players.get(mediaId)
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
    const stopAtEnd = () => {
      if (player.currentTime >= endSeconds) {
        this.stopPassage?.()
        this.stopPassage = null
        player.pause()
        player.currentTime = endSeconds
      }
    }
    player.addEventListener('timeupdate', stopAtEnd)
    player.addEventListener('seeked', stopAtEnd)
    this.stopPassage = () => {
      player.removeEventListener('timeupdate', stopAtEnd)
      player.removeEventListener('seeked', stopAtEnd)
    }
    player.currentTime = startSeconds
    try {
      await player.play()
    } catch (caught) {
      // A navigation or another selection can supersede this playback promise.
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
