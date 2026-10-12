// Web Crypto only hashes complete buffers. This incremental SHA-256 keeps one
// block so meeting originals are never loaded into a single browser allocation.
const constants = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98,
  0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
  0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8,
  0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819,
  0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
  0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
  0xc67178f2,
])
function rotate(numberValue: number, bitCount: number) {
  return (numberValue >>> bitCount) | (numberValue << (32 - bitCount))
}
export class RecordingSha256 {
  private hashWords = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ])
  private pendingBytes = new Uint8Array(64)
  private pendingByteCount = 0
  private totalByteCount = 0
  private isFinished = false
  update(content: Uint8Array) {
    if (this.isFinished) throw new Error('SHA-256 is already finished')
    this.totalByteCount += content.length
    let contentOffset = 0
    while (contentOffset < content.length) {
      const copyByteCount = Math.min(64 - this.pendingByteCount, content.length - contentOffset)
      this.pendingBytes.set(content.subarray(contentOffset, contentOffset + copyByteCount), this.pendingByteCount)
      contentOffset += copyByteCount
      this.pendingByteCount += copyByteCount
      if (this.pendingByteCount === 64) {
        this.block(this.pendingBytes)
        this.pendingByteCount = 0
      }
    }
  }
  hex() {
    if (this.isFinished) throw new Error('SHA-256 is already finished')
    this.isFinished = true
    this.pendingBytes[this.pendingByteCount++] = 0x80
    this.pendingBytes.fill(0, this.pendingByteCount)
    if (this.pendingByteCount > 56) {
      this.block(this.pendingBytes)
      this.pendingBytes.fill(0)
    }
    const blockView = new DataView(this.pendingBytes.buffer)
    blockView.setUint32(56, Math.floor(this.totalByteCount / 0x20000000))
    blockView.setUint32(60, this.totalByteCount * 8)
    this.block(this.pendingBytes)
    return Array.from(this.hashWords, (word) => word.toString(16).padStart(8, '0')).join('')
  }
  private block(content: Uint8Array) {
    const words = new Uint32Array(64)
    const blockView = new DataView(content.buffer, content.byteOffset, 64)
    for (let wordIndex = 0; wordIndex < 16; wordIndex++) words[wordIndex] = blockView.getUint32(wordIndex * 4)
    for (let wordIndex = 16; wordIndex < 64; wordIndex++) {
      const olderWord = words[wordIndex - 15],
        newerWord = words[wordIndex - 2]
      words[wordIndex] =
        words[wordIndex - 16] +
        (rotate(olderWord, 7) ^ rotate(olderWord, 18) ^ (olderWord >>> 3)) +
        words[wordIndex - 7] +
        (rotate(newerWord, 17) ^ rotate(newerWord, 19) ^ (newerWord >>> 10))
    }
    let [firstWord, secondWord, thirdWord, fourthWord, fifthWord, sixthWord, seventhWord, eighthWord] = this.hashWords
    for (let wordIndex = 0; wordIndex < 64; wordIndex++) {
      const temporaryFirst =
        (eighthWord +
          (rotate(fifthWord, 6) ^ rotate(fifthWord, 11) ^ rotate(fifthWord, 25)) +
          ((fifthWord & sixthWord) ^ (~fifthWord & seventhWord)) +
          constants[wordIndex] +
          words[wordIndex]) >>>
        0
      const temporarySecond =
        ((rotate(firstWord, 2) ^ rotate(firstWord, 13) ^ rotate(firstWord, 22)) +
          ((firstWord & secondWord) ^ (firstWord & thirdWord) ^ (secondWord & thirdWord))) >>>
        0
      eighthWord = seventhWord
      seventhWord = sixthWord
      sixthWord = fifthWord
      fifthWord = (fourthWord + temporaryFirst) >>> 0
      fourthWord = thirdWord
      thirdWord = secondWord
      secondWord = firstWord
      firstWord = (temporaryFirst + temporarySecond) >>> 0
    }
    const updatedWords = [firstWord, secondWord, thirdWord, fourthWord, fifthWord, sixthWord, seventhWord, eighthWord]
    for (let wordIndex = 0; wordIndex < 8; wordIndex++) this.hashWords[wordIndex] += updatedWords[wordIndex]
  }
}
export function recordingDigest(content: Uint8Array): string {
  const digest = new RecordingSha256()
  digest.update(content)
  return digest.hex()
}
