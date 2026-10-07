// CPace-style PAKE over ristretto255, bound to the DTLS fingerprints of the
// WebRTC connection. Must stay byte-for-byte compatible with cli/internal/pake.
//
// Both sides know the room secret (the words after the signaling topic). Each
// derives a generator from the secret and both DTLS fingerprints, exchanges
// one point, and the two sides prove to each other that they derived the same
// key. A signaling server that swapped the SDP fingerprints (MITM) ends up with
// different generators on each half and can't complete the exchange without
// guessing the secret — and it gets one guess per connection attempt.

import { ristretto255, ristretto255_hasher } from '@noble/curves/ed25519.js'
import { bytesToNumberLE, randomBytes } from '@noble/curves/utils.js'
import { sha256, sha512 } from '@noble/hashes/sha2.js'
import { hmac } from '@noble/hashes/hmac.js'

const Point = ristretto255.Point
type Role = 'host' | 'peer'

const enc = new TextEncoder()

/** Length-prefixed concatenation (4-byte big-endian length before each part). */
function lv(...parts: (string | Uint8Array)[]): Uint8Array {
  const bytes = parts.map(p => typeof p === 'string' ? enc.encode(p) : p)
  const out = new Uint8Array(bytes.reduce((n, b) => n + 4 + b.length, 0))
  const view = new DataView(out.buffer)
  let off = 0
  for (const b of bytes) {
    view.setUint32(off, b.length)
    out.set(b, off + 4)
    off += 4 + b.length
  }
  return out
}

/**
 * Canonical form of the DTLS fingerprints in an SDP: every `a=fingerprint:`
 * line as "<alg lowercase> <hex uppercase>", deduplicated, sorted, comma-joined.
 */
export function sdpFingerprints(sdp: string): string {
  const fps = new Set<string>()
  for (const m of sdp.matchAll(/^a=fingerprint:(\S+) (\S+)\s*$/gm)) {
    fps.add(`${m[1].toLowerCase()} ${m[2].toUpperCase()}`)
  }
  return [...fps].sort().join(',')
}

export function toBase64(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function fromBase64(s: string): Uint8Array {
  return Uint8Array.from(atob(s), c => c.charCodeAt(0))
}

function constantTimeEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  let diff = 0
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i]
  return diff === 0
}

export class Pake {
  private readonly scalar: bigint
  /** Our public share, to send as `{ type: 'pake', share }` */
  readonly share: Uint8Array
  private isk: Uint8Array | null = null

  constructor(
    secret: string,
    private readonly role: Role,
    private readonly hostFp: string,
    private readonly peerFp: string,
    /** Randomness source; tests pass a deterministic one to check against the CLI's vectors. */
    random: (n: number) => Uint8Array = randomBytes,
  ) {
    const g = ristretto255_hasher.deriveToCurve!(
      sha512(lv('filedrop-cpace-v1', secret, hostFp, peerFp)),
    )
    let s = 0n
    while (s === 0n) s = Point.Fn.create(bytesToNumberLE(random(64)))
    this.scalar = s
    this.share = g.multiply(s).toBytes()
  }

  /**
   * Processes the other side's share and returns our confirmation MAC.
   * Throws on a malformed or degenerate share, or if called twice: every
   * share we answer gives the other side one guess at the secret.
   */
  receiveShare(theirShareB64: string): Uint8Array {
    if (this.isk) throw new Error('duplicate PAKE share')
    const theirShare = fromBase64(theirShareB64)
    const k = Point.fromBytes(theirShare).multiply(this.scalar)
    if (k.is0()) throw new Error('degenerate PAKE share')
    const [hostShare, peerShare] = this.role === 'host'
      ? [this.share, theirShare] : [theirShare, this.share]
    this.isk = sha512(lv('filedrop-cpace-isk', this.hostFp, this.peerFp, hostShare, peerShare, k.toBytes()))
    return this.mac(this.role)
  }

  /** Checks the other side's confirmation MAC. */
  verifyConfirm(theirMacB64: string): boolean {
    if (!this.isk) return false
    return constantTimeEqual(fromBase64(theirMacB64), this.mac(this.role === 'host' ? 'peer' : 'host'))
  }

  private mac(label: Role): Uint8Array {
    return hmac(sha256, this.isk!, enc.encode(`filedrop-confirm-${label}`))
  }
}
