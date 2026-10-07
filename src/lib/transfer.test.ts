import { describe, expect, it, vi } from 'vitest'
import type { FiledropPeer } from './peer'
import { ReceiveStore } from './storage'
import {
  CHUNK_SIZE, FileTransferManager, bitmapToRanges, decodeBitmap, fileKey, invertRanges,
  type FileInfo, type Range, type TransferCallbacks,
} from './transfer'
import vectors from '../../testdata/compat.json'

describe('compatibility with the CLI', () => {
  for (const c of vectors.fileKeys) {
    it(`fingerprints ${c.name}`, async () => {
      const file = new File([new Uint8Array(c.size)], c.name, { lastModified: c.lastModified })
      expect(await fileKey(file)).toBe(c.key)
    })
  }

  for (const c of vectors.have) {
    it(`decodes have messages: ${c.name}`, () => {
      expect(decodeBitmap(c.bitmap, c.totalChunks)).toEqual(c.ranges)
      expect(invertRanges(c.ranges as Range[], c.totalChunks)).toEqual(c.missing)
    })
  }
})

describe('bitmapToRanges', () => {
  it('round-trips through invertRanges', () => {
    const bitmap = new Uint8Array([0b10110011, 0b00000001, 0b10000000])
    const have = bitmapToRanges(bitmap, 24)
    expect(have).toEqual([[0, 1], [4, 5], [7, 8], [23, 23]])
    expect(invertRanges(have, 24)).toEqual([[2, 3], [6, 6], [9, 22]])
  })
})

// ── FileTransferManager, two instances wired back to back ─────────────────────

type Callbacks = { [K in keyof TransferCallbacks]: ReturnType<typeof vi.fn<TransferCallbacks[K]>> }

function callbacks(overrides: Partial<TransferCallbacks> = {}): Callbacks {
  return {
    onLocalProgress: vi.fn(), onRejected: vi.fn(), onFileOffered: vi.fn(), onResumed: vi.fn(),
    onRemoteProgress: vi.fn(), onFileComplete: vi.fn(), onTextReceived: vi.fn(),
    ...overrides,
  } as Callbacks
}

/**
 * Connects a sender and a receiver the way the data channels would: control
 * messages round-trip through JSON. `dropAfter` stops delivering chunks after
 * that many, as if the connection broke.
 */
function connect(sender: FileTransferManager, receiver: FileTransferManager, dropAfter = Infinity) {
  const stats = { chunks: 0 }
  const fake = (to: FileTransferManager, isSender: boolean) => ({
    sendControl: (msg: unknown) => to.handleControl(JSON.parse(JSON.stringify(msg))),
    sendChunk: async (buf: ArrayBuffer) => {
      if (!isSender || stats.chunks >= dropAfter) return
      stats.chunks++
      to.handleData(buf.slice(0))
    },
  }) as unknown as FiledropPeer
  sender.peer = fake(receiver, true)
  receiver.peer = fake(sender, false)
  sender.onPeerConnected()
  receiver.onPeerConnected()
  return stats
}

function testFile(size: number, name = 'test.bin'): File {
  const data = new Uint8Array(size)
  for (let i = 0; i < size; i++) data[i] = (i * 31 + (i >> 16)) & 0xff
  return new File([data], name, { lastModified: 1_700_000_000_000 })
}

/** Resolves with the received file's bytes once the receiver completes it. */
function completion(cb: Callbacks): Promise<Uint8Array> {
  return new Promise(resolve => {
    cb.onFileComplete.mockImplementation(async (_id, file) => resolve(new Uint8Array(await file.arrayBuffer())))
  })
}

async function bytesOf(file: File) {
  return new Uint8Array(await file.arrayBuffer())
}

/** Accepts every offer as soon as it arrives (auto download mode). */
function autoAccept(cb: Callbacks, getManager: () => FileTransferManager) {
  cb.onFileOffered.mockImplementation((files: FileInfo[]) => {
    for (const f of files) void getManager().acceptFile(f.id)
  })
}

describe('FileTransferManager', () => {
  it('transfers a multi-chunk file', async () => {
    const file = testFile(3 * CHUNK_SIZE + 123)
    const sendCb = callbacks(), recvCb = callbacks()
    const sender = new FileTransferManager(sendCb, new ReceiveStore())
    const receiver = new FileTransferManager(recvCb, new ReceiveStore())
    autoAccept(recvCb, () => receiver)
    const done = completion(recvCb)

    const stats = connect(sender, receiver)
    sender.addFile(file, 1, await fileKey(file))

    expect(await done).toEqual(await bytesOf(file))
    expect(stats.chunks).toBe(4)
    expect(recvCb.onFileOffered).toHaveBeenCalledWith([expect.objectContaining({ name: 'test.bin', totalChunks: 4 })])
    expect(sendCb.onLocalProgress).toHaveBeenLastCalledWith(1, 4)
  })

  it('sends manifests queued before the peer connected', async () => {
    const file = testFile(10)
    const recvCb = callbacks()
    const sender = new FileTransferManager(callbacks(), new ReceiveStore())
    const receiver = new FileTransferManager(recvCb, new ReceiveStore())
    sender.addFile(file, 1, await fileKey(file))
    expect(recvCb.onFileOffered).not.toHaveBeenCalled()
    connect(sender, receiver)
    expect(recvCb.onFileOffered).toHaveBeenCalledOnce()
  })

  it('completes an empty file on accept', async () => {
    const file = testFile(0, 'empty')
    const recvCb = callbacks()
    const sender = new FileTransferManager(callbacks(), new ReceiveStore())
    const receiver = new FileTransferManager(recvCb, new ReceiveStore())
    autoAccept(recvCb, () => receiver)
    const done = completion(recvCb)
    connect(sender, receiver)
    sender.addFile(file, 1, await fileKey(file))
    expect(await done).toEqual(new Uint8Array(0))
  })

  it('tells the sender about rejected files', async () => {
    const file = testFile(10)
    const sendCb = callbacks(), recvCb = callbacks()
    const sender = new FileTransferManager(sendCb, new ReceiveStore())
    const receiver = new FileTransferManager(recvCb, new ReceiveStore())
    recvCb.onFileOffered.mockImplementation(files => receiver.rejectFile(files[0].id))
    const stats = connect(sender, receiver)
    sender.addFile(file, 7, await fileKey(file))
    expect(sendCb.onRejected).toHaveBeenCalledWith(7)
    expect(stats.chunks).toBe(0)
  })

  it('resumes a broken transfer with only the missing chunks', async () => {
    const file = testFile(5 * CHUNK_SIZE)
    const key = await fileKey(file)
    const store = new ReceiveStore()

    // First connection breaks after two chunks
    const recv1 = callbacks()
    const sender1 = new FileTransferManager(callbacks(), new ReceiveStore())
    const receiver1 = new FileTransferManager(recv1, store)
    autoAccept(recv1, () => receiver1)
    connect(sender1, receiver1, 2)
    sender1.addFile(file, 1, key)
    await vi.waitFor(() => expect(recv1.onRemoteProgress).toHaveBeenLastCalledWith(1, 2))
    await receiver1.close()
    expect(store.state(key)).toBe('paused')

    // Second connection: new per-connection id, same key — resumes without asking
    const sendCb = callbacks(), recv2 = callbacks()
    const sender2 = new FileTransferManager(sendCb, new ReceiveStore())
    const receiver2 = new FileTransferManager(recv2, store)
    const done = completion(recv2)
    const stats = connect(sender2, receiver2)
    sender2.addFile(file, 9, key)

    expect(await done).toEqual(await bytesOf(file))
    expect(recv2.onFileOffered).not.toHaveBeenCalled()
    expect(recv2.onResumed).toHaveBeenCalledWith(expect.objectContaining({ id: 9, key }), 2)
    expect(stats.chunks).toBe(3)
    expect(sendCb.onLocalProgress.mock.calls[0]).toEqual([9, 2])
  })

  it('answers re-offers of downloaded files with a full have', async () => {
    const file = testFile(2 * CHUNK_SIZE)
    const key = await fileKey(file)
    const store = new ReceiveStore()

    const recv1 = callbacks()
    const receiver1 = new FileTransferManager(recv1, store)
    autoAccept(recv1, () => receiver1)
    const done = completion(recv1)
    const sender1 = new FileTransferManager(callbacks(), new ReceiveStore())
    connect(sender1, receiver1)
    sender1.addFile(file, 1, key)
    await done
    await store.markDownloaded(key)
    expect(store.state(key)).toBe('downloaded')

    const sendCb = callbacks(), recv2 = callbacks()
    const sender2 = new FileTransferManager(sendCb, new ReceiveStore())
    const stats = connect(sender2, new FileTransferManager(recv2, store))
    sender2.addFile(file, 1, key)

    expect(recv2.onFileOffered).not.toHaveBeenCalled()
    expect(sendCb.onLocalProgress).toHaveBeenCalledWith(1, 2)
    expect(stats.chunks).toBe(0)
  })

  it('ignores a file another peer is already sending', async () => {
    const file = testFile(2 * CHUNK_SIZE)
    const key = await fileKey(file)
    const store = new ReceiveStore()

    const recvA = callbacks()
    const receiverA = new FileTransferManager(recvA, store)
    // Delivers no chunks, so the file stays busy
    const senderA = new FileTransferManager(callbacks(), new ReceiveStore())
    connect(senderA, receiverA, 0)
    autoAccept(recvA, () => receiverA)
    senderA.addFile(file, 1, key)
    await vi.waitFor(() => expect(store.state(key)).toBe('busy'))

    const recvB = callbacks()
    const senderB = new FileTransferManager(callbacks(), new ReceiveStore())
    const stats = connect(senderB, new FileTransferManager(recvB, store))
    senderB.addFile(file, 1, key)
    expect(recvB.onFileOffered).not.toHaveBeenCalled()
    expect(stats.chunks).toBe(0)
  })

  it('delivers text', () => {
    const recvCb = callbacks()
    const sender = new FileTransferManager(callbacks(), new ReceiveStore())
    connect(sender, new FileTransferManager(recvCb, new ReceiveStore()))
    sender.sendText('hello\nworld')
    expect(recvCb.onTextReceived).toHaveBeenCalledWith('hello\nworld')
  })
})
