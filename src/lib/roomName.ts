import { WORDLIST } from './wordlist'

export const ROOM_WORDS = 4
const WORDS = new Set(WORDLIST)

export function generateRoomName(): string {
  const words = Array.from({ length: ROOM_WORDS }, () => WORDLIST[secureRandInt(WORDLIST.length)])
  return words.join('-')
}

/**
 * Turns user input — a bare code (any case, dashes or spaces) or a pasted
 * …/r/#code link — into a canonical room ID. Must match the CLI's
 * roomname.Normalize.
 */
export function normalizeRoomCode(input: string): { roomId: string } | { error: string } {
  const code = input.slice(input.lastIndexOf('#') + 1)
  const words = code.toLowerCase().split(/[\s-]+/).filter(Boolean)
  if (words.length !== ROOM_WORDS) return { error: `Room codes are ${ROOM_WORDS} words.` }
  const unknown = words.find((w) => !WORDS.has(w))
  if (unknown) return { error: `“${unknown}” isn't a room code word.` }
  return { roomId: words.join('-') }
}

/**
 * The first two words pick the signaling topic; the rest is the PAKE secret,
 * which never leaves the browser. The signaling server sees only the topic, so
 * it has to guess the secret words (~21 bits) — online, one try per connection.
 */
export function parseRoomId(roomId: string): { signalingId: string; secret: string } {
  const words = roomId.split('-')
  return { signalingId: words.slice(0, 2).join('-'), secret: words.slice(2).join('-') }
}

// Rejection sampling to avoid modulo bias
function secureRandInt(max: number): number {
  const limit = Math.floor(0x1_0000_0000 / max) * max
  let val: number
  do {
    val = crypto.getRandomValues(new Uint32Array(1))[0]
  } while (val >= limit)
  return val % max
}
