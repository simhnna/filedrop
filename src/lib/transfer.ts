import type { FiledropPeer } from './peer'
import { ReceiveStore, type StoredFile, popcount } from './storage'

export const CHUNK_SIZE = 65_536 // 64 KB

export interface FileInfo {
  /** Per-connection id, used in the chunk header. */
  id: number
  /** Fingerprint (name + size + lastModified) identifying the file across connections and sessions. */
  key: string
  name: string
  size: number
  totalChunks: number
}

export interface LocalFileState extends FileInfo {
  file: File
  sentChunks: number
  status: 'queued' | 'sending' | 'done'
}

type ControlMessage =
  | { type: 'manifest'; files: FileInfo[] }
  | { type: 'have'; fileId: number; ranges?: Range[]; bitmap?: string }
  | { type: 'request'; fileId: number; ranges: Range[] }
  | { type: 'reject'; fileId: number }
  | { type: 'text'; content: string }

export type Range = [number, number]

/** SHA-256 over name, size and lastModified, truncated to 128 bits (hex). Must match the CLI. */
export async function fileKey(file: File): Promise<string> {
  const data = new TextEncoder().encode(`${file.name}\n${file.size}\n${file.lastModified}`)
  const hash = new Uint8Array(await crypto.subtle.digest('SHA-256', data))
  return [...hash.slice(0, 16)].map(b => b.toString(16).padStart(2, '0')).join('')
}

export interface TransferCallbacks {
  /** First call means the peer accepted the file; sentChunks includes chunks it already had */
  onLocalProgress: (id: number, sentChunks: number) => void
  /** The peer declined one of our files */
  onRejected: (id: number) => void
  /** Files offered by the remote peer — call acceptFile/rejectFile for each */
  onFileOffered: (files: FileInfo[]) => void
  /** A paused transfer (earlier connection or session) was picked up by this connection */
  onResumed: (info: FileInfo, receivedCount: number) => void
  onRemoteProgress: (id: number, receivedChunks: number) => void
  onFileComplete: (id: number, file: File | Blob, name: string) => void
  onTextReceived: (content: string) => void
}

export class FileTransferManager {
  peer: FiledropPeer | null = null

  private local = new Map<number, LocalFileState>()
  /** Files this connection is receiving, by per-connection id */
  private remote = new Map<number, StoredFile>()
  private pendingOffers = new Map<number, FileInfo>()
  private pendingManifests: FileInfo[][] = []
  private activeSend: Promise<void> = Promise.resolve()
  // Per-file write queues ensure OPFS writes are serialised
  private writeQueues = new Map<number, Promise<void>>()
  /** Chunks received but not yet written, per file; finalize only once this drains. */
  private pendingWrites = new Map<number, number>()

  constructor(private readonly cb: TransferCallbacks, private readonly store: ReceiveStore) {}

  /** Connection closed: finish pending writes and hand unfinished files back to the store. */
  async close(): Promise<void> {
    await Promise.all(this.writeQueues.values())
    for (const entry of this.remote.values()) await this.store.release(entry.info.key)
    this.remote.clear()
  }

  // ── Incoming from peer ────────────────────────────────────────────────────────

  handleControl(raw: unknown): void {
    const msg = raw as ControlMessage

    if (msg.type === 'manifest') {
      const toOffer: FileInfo[] = []
      for (const info of msg.files) {
        // Senders without fingerprints get a random key: no resume, but no collisions either
        if (!info.key) info.key = `anon-${crypto.randomUUID()}`
        const existing = this.remote.get(info.id)
        if (existing) {
          this.sendHave(info.id, existing.received, existing.info.totalChunks)
          continue
        }
        switch (this.store.state(info.key)) {
          case 'unknown':
            this.pendingOffers.set(info.id, info)
            toOffer.push(info)
            break
          case 'paused':
            // Accepted earlier (previous connection or session) — resume without asking
            this.store.claim(info, this).then(entry => {
              if (!entry) return
              this.remote.set(info.id, entry)
              this.cb.onResumed(info, entry.receivedCount)
              this.sendHave(info.id, entry.received, info.totalChunks)
            }).catch(err => console.error('[transfer] resume failed:', err))
            break
          case 'complete':
          case 'downloaded':
            // Already have it: tell the sender so it can mark the file as sent
            this.sendHave(info.id, new Uint8Array(Math.ceil(info.totalChunks / 8)).fill(0xff), info.totalChunks)
            break
          case 'busy':
            // Another peer is sending the same file right now — ignore the duplicate
            break
        }
      }
      if (toOffer.length > 0) this.cb.onFileOffered(toOffer)
    }

    if (msg.type === 'have') {
      const local = this.local.get(msg.fileId)
      if (!local) return
      const haveRanges = msg.bitmap
        ? decodeBitmap(msg.bitmap, local.totalChunks)
        : (msg.ranges ?? [])
      const missing = invertRanges(haveRanges, local.totalChunks)
      // Progress starts from what the receiver already has (resume)
      local.sentChunks = local.totalChunks - missing.reduce((n, [s, e]) => n + e - s + 1, 0)
      this.cb.onLocalProgress(local.id, local.sentChunks)
      if (missing.length > 0) {
        this.activeSend = this.activeSend.then(() => this.sendChunks(local, missing))
      } else {
        local.status = 'done'
      }
    }

    if (msg.type === 'request') {
      const local = this.local.get(msg.fileId)
      if (local) {
        this.activeSend = this.activeSend.then(() => this.sendChunks(local, msg.ranges))
      }
    }

    if (msg.type === 'reject') {
      if (this.local.has(msg.fileId)) this.cb.onRejected(msg.fileId)
    }

    if (msg.type === 'text') {
      this.cb.onTextReceived(msg.content)
    }
  }

  handleData(buffer: ArrayBuffer): void {
    if (buffer.byteLength < 8) return
    const view = new DataView(buffer)
    const fileId = view.getUint32(0)
    const chunkIdx = view.getUint32(4)

    const entry = this.remote.get(fileId)
    if (!entry) return

    // Dedup check via bitmap
    const byteIdx = chunkIdx >> 3
    const bitMask = 1 << (chunkIdx & 7)
    if (entry.received[byteIdx] & bitMask) return

    // Mark received immediately (optimistic — before async write)
    entry.received[byteIdx] |= bitMask
    entry.receivedCount++
    this.cb.onRemoteProgress(fileId, entry.receivedCount)

    const chunkData = buffer.slice(8)
    const key = entry.info.key
    this.pendingWrites.set(fileId, (this.pendingWrites.get(fileId) ?? 0) + 1)

    this.enqueueWrite(fileId, async () => {
      let pending: number
      try {
        await this.store.write(key, chunkIdx, chunkIdx * CHUNK_SIZE, chunkData)
        // Periodic checkpoint to flush data to disk
        if (entry.receivedCount % ReceiveStore.CHECKPOINT_EVERY === 0) {
          await this.store.checkpoint(key)
        }
      } finally {
        pending = this.pendingWrites.get(fileId)! - 1
        this.pendingWrites.set(fileId, pending)
      }
      // receivedCount runs ahead of the writes: the last chunk can arrive while
      // earlier ones are still queued, so wait until this was the last queued write.
      if (pending === 0 && entry.receivedCount >= entry.info.totalChunks && this.remote.has(fileId)) {
        this.remote.delete(fileId)
        this.pendingWrites.delete(fileId)
        const file = await this.store.finalize(key)
        this.cb.onFileComplete(fileId, file, entry.info.name)
      }
    })
  }

  // ── Accept / reject offered files ─────────────────────────────────────────────

  /** Returns false if the file can't be accepted (another peer is already sending it, or it arrived meanwhile). */
  async acceptFile(id: number): Promise<boolean> {
    const info = this.pendingOffers.get(id)
    if (!info) return false
    this.pendingOffers.delete(id)

    const entry = await this.store.claim(info, this)
    if (!entry) return false
    this.remote.set(id, entry)
    this.sendHave(id, entry.received, info.totalChunks)

    // Edge case: empty file, nothing to wait for
    if (entry.receivedCount >= info.totalChunks) {
      this.remote.delete(id)
      this.cb.onFileComplete(id, await this.store.finalize(info.key), info.name)
    }
    return true
  }

  rejectFile(id: number): void {
    this.pendingOffers.delete(id)
    this.peer?.sendControl({ type: 'reject', fileId: id })
  }

  // ── Outgoing files ────────────────────────────────────────────────────────────

  addFile(file: File, id: number, key: string): void {
    const totalChunks = Math.ceil(file.size / CHUNK_SIZE)
    const state: LocalFileState = { id, key, name: file.name, size: file.size, totalChunks, file, sentChunks: 0, status: 'queued' }
    this.local.set(id, state)
    const manifest: FileInfo[] = [{ id, key, name: file.name, size: file.size, totalChunks }]
    if (this.peer) {
      this.peer.sendControl({ type: 'manifest', files: manifest })
    } else {
      this.pendingManifests.push(manifest)
    }
  }

  onPeerConnected(): void {
    for (const manifest of this.pendingManifests) {
      this.peer!.sendControl({ type: 'manifest', files: manifest })
    }
    this.pendingManifests = []
  }

  // ── Outgoing text ─────────────────────────────────────────────────────────────

  sendText(content: string): void {
    this.peer?.sendControl({ type: 'text', content })
  }

  // ── Internals ─────────────────────────────────────────────────────────────────

  private async sendChunks(local: LocalFileState, ranges: Range[]): Promise<void> {
    if (!this.peer) return
    local.status = 'sending'
    for (const [start, end] of ranges) {
      for (let i = start; i <= end; i++) {
        const offset = i * CHUNK_SIZE
        const chunk = await local.file.slice(offset, offset + CHUNK_SIZE).arrayBuffer()
        const buf = new ArrayBuffer(8 + chunk.byteLength)
        const dv = new DataView(buf)
        dv.setUint32(0, local.id)
        dv.setUint32(4, i)
        new Uint8Array(buf).set(new Uint8Array(chunk), 8)
        await this.peer.sendChunk(buf)
        local.sentChunks++
        this.cb.onLocalProgress(local.id, local.sentChunks)
      }
    }
    if (local.sentChunks >= local.totalChunks) local.status = 'done'
  }

  private sendHave(fileId: number, received: Uint8Array, totalChunks: number): void {
    if (!this.peer) return
    const ranges = bitmapToRanges(received, totalChunks)
    if (ranges.length > 500) {
      this.peer.sendControl({ type: 'have', fileId, bitmap: encodeBitmap(received) })
    } else {
      this.peer.sendControl({ type: 'have', fileId, ranges })
    }
  }

  private enqueueWrite(fileId: number, fn: () => Promise<void>): void {
    const prev = this.writeQueues.get(fileId) ?? Promise.resolve()
    const next = prev.then(fn).catch(err => console.error('[transfer] write error:', err))
    this.writeQueues.set(fileId, next)
  }
}

// ── Range / bitmap helpers ────────────────────────────────────────────────────

/** Convert a received-chunks bitmap to sorted [start, end] ranges. */
export function bitmapToRanges(bitmap: Uint8Array, totalChunks: number): Range[] {
  const ranges: Range[] = []
  let start = -1
  for (let i = 0; i <= totalChunks; i++) {
    const has = i < totalChunks && ((bitmap[i >> 3] >> (i & 7)) & 1) === 1
    if (has && start === -1) { start = i }
    else if (!has && start !== -1) { ranges.push([start, i - 1]); start = -1 }
  }
  return ranges
}

/** Invert "have" ranges to get "missing" ranges. */
export function invertRanges(have: Range[], total: number): Range[] {
  const missing: Range[] = []
  let pos = 0
  for (const [s, e] of have) {
    if (pos < s) missing.push([pos, s - 1])
    pos = e + 1
  }
  if (pos < total) missing.push([pos, total - 1])
  return missing
}

function encodeBitmap(bitmap: Uint8Array): string {
  return btoa(String.fromCharCode(...bitmap))
}

export function decodeBitmap(b64: string, totalChunks: number): Range[] {
  const bytes = Uint8Array.from(atob(b64), c => c.charCodeAt(0))
  const fake = new Uint8Array(Math.ceil(totalChunks / 8))
  fake.set(bytes.slice(0, fake.length))
  return bitmapToRanges(fake, totalChunks)
}

export { popcount }
