import type { FileInfo } from './transfer'

// Checkpoint every N chunks: close+reopen the writable so data is committed to disk.
// 512 × 64 KB = 32 MB between checkpoints.
const CHECKPOINT_EVERY = 512

// Entries untouched for this long are deleted on startup (partials, unread completes, .done markers).
const MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000

const DIR_PREFIX = 'filedrop-'
const SUFFIXES = ['.bin', '.bitmap', '.meta.json', '.done']

export interface StoredFile {
  /** `info.key` identifies the file; `info.id` belongs to whichever connection claimed it last. */
  info: FileInfo
  received: Uint8Array  // chunk bitmap — always in RAM (~4 KB per 2 GB file)
  receivedCount: number
  /** Set once all chunks are written. */
  file?: File | Blob
  /** The transfer manager currently writing this file, or null if paused. */
  owner: object | null
}

export type KeyState = 'unknown' | 'paused' | 'busy' | 'complete' | 'downloaded'

/**
 * Receive-side storage shared by every peer slot in a room, keyed by the file's
 * fingerprint key. Persists to OPFS under `filedrop-<roomId>/<key>.*`, or keeps
 * chunks in memory when OPFS is unavailable.
 */
export class ReceiveStore {
  private dir: FileSystemDirectoryHandle | null = null
  private files = new Map<string, StoredFile>()
  private downloaded = new Set<string>()
  private writables = new Map<string, FileSystemWritableFileStream>()
  private handles = new Map<string, FileSystemFileHandle>()
  private chunks = new Map<string, (ArrayBuffer | null)[]>()  // memory fallback

  static async open(roomId: string): Promise<ReceiveStore> {
    const store = new ReceiveStore()
    try {
      const root = await navigator.storage.getDirectory()
      await sweep(root)
      store.dir = await root.getDirectoryHandle(DIR_PREFIX + roomId, { create: true })
      await store.load()
    } catch (err) {
      console.warn('[storage] OPFS unavailable, keeping received files in memory:', err)
      store.dir = null
    }
    return store
  }

  /** Files left over from a previous session: partial (paused) or complete but not yet downloaded. */
  restored(): StoredFile[] {
    return [...this.files.values()]
  }

  state(key: string): KeyState {
    if (this.downloaded.has(key)) return 'downloaded'
    const f = this.files.get(key)
    if (!f) return 'unknown'
    if (f.file) return 'complete'
    return f.owner ? 'busy' : 'paused'
  }

  /**
   * Take ownership of a file for writing. Returns null if it's already complete,
   * downloaded, or being written by another owner. Ownership is taken synchronously,
   * so two slots offered the same key can't both claim it.
   */
  async claim(info: FileInfo, owner: object): Promise<StoredFile | null> {
    const state = this.state(info.key)
    if (state !== 'unknown' && state !== 'paused') return null

    let entry = this.files.get(info.key)
    if (entry) {
      entry.owner = owner
      entry.info = info
    } else {
      entry = {
        info, owner,
        received: new Uint8Array(Math.ceil(info.totalChunks / 8)),
        receivedCount: 0,
      }
      this.files.set(info.key, entry)
    }

    if (this.dir) {
      if (!this.writables.has(info.key)) {
        // Persist metadata so we can restore on next session
        const metaHandle = await this.dir.getFileHandle(`${info.key}.meta.json`, { create: true })
        const mw = await metaHandle.createWritable()
        await mw.write(JSON.stringify(info))
        await mw.close()

        const fh = await this.dir.getFileHandle(`${info.key}.bin`, { create: true })
        const w = await fh.createWritable({ keepExistingData: true })
        await w.truncate(info.size)  // pre-allocate / ensure correct size
        this.handles.set(info.key, fh)
        this.writables.set(info.key, w)
      }
    } else if (!this.chunks.has(info.key)) {
      this.chunks.set(info.key, new Array<ArrayBuffer | null>(info.totalChunks).fill(null))
    }
    return entry
  }

  async write(key: string, chunkIdx: number, offset: number, data: ArrayBuffer): Promise<void> {
    if (!this.dir) {
      const c = this.chunks.get(key)
      if (c) c[chunkIdx] = data
      return
    }
    const w = this.writables.get(key)
    if (!w) return
    await w.write({ type: 'write', position: offset, data })
  }

  /** Flush and reopen writable so data is committed; also persist bitmap. */
  async checkpoint(key: string): Promise<void> {
    const entry = this.files.get(key)
    const w = this.writables.get(key)
    const fh = this.handles.get(key)
    if (!entry || !w || !fh) return
    await w.close()
    await this.saveBitmap(key, entry.received)
    this.writables.set(key, await fh.createWritable({ keepExistingData: true }))
  }

  /** Close writable, persist bitmap, return the assembled File for download. */
  async finalize(key: string): Promise<File | Blob> {
    const entry = this.files.get(key)
    if (!entry) throw new Error(`Unknown file ${key}`)
    entry.owner = null
    if (!this.dir) {
      entry.file = new Blob(this.chunks.get(key) as ArrayBuffer[])
      this.chunks.delete(key)  // the Blob holds the data now
      return entry.file
    }
    const w = this.writables.get(key)
    if (w) { await w.close(); this.writables.delete(key) }
    await this.saveBitmap(key, entry.received)
    const fh = this.handles.get(key) ?? await this.dir.getFileHandle(`${key}.bin`)
    entry.file = await fh.getFile()
    return entry.file
  }

  /** Give up ownership (peer left mid-transfer); the file becomes resumable by any slot. */
  async release(key: string): Promise<void> {
    const entry = this.files.get(key)
    if (!entry || entry.file) return
    entry.owner = null
    const w = this.writables.get(key)
    if (w) {
      try { await w.close() } catch { /* ignore */ }
      this.writables.delete(key)
      await this.saveBitmap(key, entry.received)
    }
  }

  /** Delete the data after the user downloaded it, and remember the key so re-offers are ignored. */
  async markDownloaded(key: string): Promise<void> {
    this.files.delete(key)
    this.chunks.delete(key)
    this.handles.delete(key)
    this.downloaded.add(key)
    if (!this.dir) return
    for (const suffix of ['.bin', '.bitmap', '.meta.json']) {
      try { await this.dir.removeEntry(key + suffix) } catch { /* ignore if missing */ }
    }
    const h = await this.dir.getFileHandle(`${key}.done`, { create: true })
    const w = await h.createWritable()
    await w.write(String(Date.now()))
    await w.close()
  }

  private async load(): Promise<void> {
    if (!this.dir) return
    for await (const [name, handle] of this.dir.entries()) {
      if (handle.kind !== 'file') continue
      if (name.endsWith('.done')) {
        this.downloaded.add(name.slice(0, -'.done'.length))
        continue
      }
      if (!name.endsWith('.meta.json')) continue
      try {
        const info = JSON.parse(await (await (handle as FileSystemFileHandle).getFile()).text()) as FileInfo
        if (!info.key) continue  // pre-fingerprint entry; the sweep removes it eventually
        const received = await this.loadBitmap(info.key, info.totalChunks)
        const entry: StoredFile = { info, received, receivedCount: popcount(received), owner: null }
        if (entry.receivedCount >= info.totalChunks) {
          entry.file = await (await this.dir.getFileHandle(`${info.key}.bin`)).getFile()
        }
        this.files.set(info.key, entry)
      } catch { /* skip corrupt entries */ }
    }
  }

  private async saveBitmap(key: string, bitmap: Uint8Array): Promise<void> {
    if (!this.dir) return
    const h = await this.dir.getFileHandle(`${key}.bitmap`, { create: true })
    const w = await h.createWritable()
    await w.write(bitmap.buffer as ArrayBuffer)
    await w.close()
  }

  private async loadBitmap(key: string, totalChunks: number): Promise<Uint8Array> {
    const empty = new Uint8Array(Math.ceil(totalChunks / 8))
    if (!this.dir) return empty
    try {
      const h = await this.dir.getFileHandle(`${key}.bitmap`)
      empty.set(new Uint8Array(await (await h.getFile()).arrayBuffer()).slice(0, empty.length))
    } catch { /* no bitmap yet */ }
    return empty
  }

  static readonly CHECKPOINT_EVERY = CHECKPOINT_EVERY
}

/**
 * Delete entries in every room directory whose files were all last touched more than
 * MAX_AGE_MS ago, then remove room directories that end up empty.
 */
async function sweep(root: FileSystemDirectoryHandle): Promise<void> {
  const cutoff = Date.now() - MAX_AGE_MS
  for await (const [dirName, dirHandle] of root.entries()) {
    if (dirHandle.kind !== 'directory' || !dirName.startsWith(DIR_PREFIX)) continue
    const dir = dirHandle as FileSystemDirectoryHandle

    // Group files by entry (base name) and track the newest modification per entry
    const entries = new Map<string, { names: string[]; newest: number }>()
    for await (const [name, handle] of dir.entries()) {
      if (handle.kind !== 'file') continue
      const suffix = SUFFIXES.find(s => name.endsWith(s))
      const base = suffix ? name.slice(0, -suffix.length) : name
      let modified = 0
      try { modified = (await (handle as FileSystemFileHandle).getFile()).lastModified } catch { /* locked */ }
      const e = entries.get(base) ?? { names: [], newest: 0 }
      e.names.push(name)
      e.newest = Math.max(e.newest, modified)
      entries.set(base, e)
    }

    let remaining = 0
    for (const { names, newest } of entries.values()) {
      if (newest >= cutoff) { remaining += names.length; continue }
      for (const name of names) {
        try { await dir.removeEntry(name) } catch { remaining++ }
      }
    }
    if (remaining === 0) {
      try { await root.removeEntry(dirName) } catch { /* in use by another tab */ }
    }
  }
}

export function popcount(bitmap: Uint8Array): number {
  let n = 0
  for (let b of bitmap) { while (b) { n += b & 1; b >>>= 1 } }
  return n
}
