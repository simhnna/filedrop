<script lang="ts">
  import { onMount } from 'svelte'
  import { YWebRTCSignaling } from './lib/signaling'
  import { FiledropPeer } from './lib/peer'
  import { FileTransferManager, CHUNK_SIZE, fileKey, type FileInfo } from './lib/transfer'
  import { ReceiveStore } from './lib/storage'
  import { parseRoomId } from './lib/roomName'
  import QrCode from './lib/QrCode.svelte'
  import Header from './lib/Header.svelte'
  import Footer from './lib/Footer.svelte'
  import { takePendingFiles } from './lib/pending'

  const SIGNALING_URL = 'wss://connect.hannaweb.eu'

  let { roomId, navigate, theme }: {
    roomId: string
    navigate: (to: string) => void
    theme: 'dark' | 'light'
  } = $props()

  const { signalingId, secret } = $derived(parseRoomId(roomId))
  const isHost = $derived(sessionStorage.getItem(`host:${roomId}`) === '1')
  const codeWords = $derived(roomId.split('-'))
  const joinUrl = $derived(`${window.location.origin}/r/#${roomId}`)

  const isMac = /Mac/.test(navigator.platform)

  type Status = 'waiting' | 'connected' | 'disconnected' | 'auth-failed'
  let status = $state<Status>('waiting')
  let qrOpen = $state(false)
  let qrPop = $state<HTMLElement>()
  let qrBtn = $state<HTMLElement>()
  let linkCopied = $state(false)

  // Each failed PAKE gives whoever attempted it one guess at the room secret, so
  // the host stops listening for new peers after a handful of them
  const MAX_AUTH_FAILURES = 5
  let authFailures = 0
  let locked = $state(false)

  type DownloadMode = 'auto' | 'manual'
  let downloadMode = $state<DownloadMode>('manual')

  interface RemoteFile extends FileInfo {
    /** Connection currently offering/sending the file; null once that peer is gone */
    slotId: string | null
    receivedChunks: number
    status: 'offered' | 'receiving' | 'paused' | 'done'
    fileRef?: File | Blob
  }

  interface OutgoingFile extends FileInfo {
    file: File
    /** Per connected peer (slot id): chunks it has, or null while the offer is pending */
    sent: Record<string, number | null>
    /** Number of peers that received the whole file, including ones that have since left */
    deliveredTo: number
  }

  interface ReceivedText { id: number; content: string }

  interface PeerSlot {
    id: string
    signaling: YWebRTCSignaling
    peer: FiledropPeer
    transfer: FileTransferManager
    receivedTexts: ReceivedText[]
    connected: boolean
    closed: boolean
  }

  let slots = $state<PeerSlot[]>([])
  let localFiles = $state<OutgoingFile[]>([])
  let remoteFiles = $state<RemoteFile[]>([])
  let store: ReceiveStore
  let outgoingText = $state('')
  let textCopied = $state<number | null>(null)
  let dragging = $state(false)

  let connectedPeers = $derived(slots.filter(s => s.connected).length)
  let allReceivedTexts = $derived(
    slots.flatMap(s => s.receivedTexts).sort((a, b) => b.id - a.id)
  )
  let isListening = $derived(slots.some(s => !s.connected))

  let nextFileId = 1

  function findRemote(slotId: string, id: number) {
    return remoteFiles.find(f => f.slotId === slotId && f.id === id)
  }

  function destroySlot(slotId: string) {
    const slot = slots.find(s => s.id === slotId)
    if (!slot || slot.closed) return
    slot.closed = true
    slot.peer.close()
    slot.signaling.close()
    slot.transfer.close().catch(console.error)
    slots = slots.filter(s => s.id !== slotId)
    for (const lf of localFiles) delete lf.sent[slotId]
    // Offers die with the connection; unfinished files wait for any peer to resume them
    remoteFiles = remoteFiles.filter(f => !(f.slotId === slotId && f.status === 'offered'))
    for (const f of remoteFiles) {
      if (f.slotId !== slotId) continue
      f.slotId = null
      if (f.status === 'receiving') f.status = 'paused'
    }
  }

  function createSlot() {
    const slotId = crypto.randomUUID()
    const signaling = new YWebRTCSignaling(SIGNALING_URL, `filedrop:${signalingId}`)

    const slot: PeerSlot = {
      id: slotId, signaling, peer: null!, transfer: null!,
      receivedTexts: [], connected: false, closed: false,
    }

    const t = new FileTransferManager({
      onLocalProgress(id, sentChunks) {
        const f = localFiles.find(f => f.id === id)
        if (!f || !(slotId in f.sent)) return
        const prev = f.sent[slotId]
        if ((prev === null || prev < f.totalChunks) && sentChunks >= f.totalChunks) f.deliveredTo++
        f.sent[slotId] = sentChunks
      },
      onRejected(id) {
        const f = localFiles.find(f => f.id === id)
        if (f) delete f.sent[slotId]
      },
      onFileOffered(files: FileInfo[]) {
        for (const info of files) {
          if (findRemote(slotId, info.id)) continue
          remoteFiles = [...remoteFiles, { ...info, slotId, receivedChunks: 0, status: 'offered' }]
          if (downloadMode === 'auto') acceptFile(slotId, info.id)
        }
      },
      onResumed(info, receivedCount) {
        const f = remoteFiles.find(f => f.key === info.key && f.status === 'paused')
        if (f) {
          Object.assign(f, { id: info.id, slotId, receivedChunks: receivedCount, status: 'receiving' })
        } else {
          remoteFiles = [...remoteFiles, { ...info, slotId, receivedChunks: receivedCount, status: 'receiving' }]
        }
      },
      onRemoteProgress(id, receivedChunks) {
        const f = findRemote(slotId, id)
        if (f) f.receivedChunks = receivedChunks
      },
      onFileComplete(id, file, _name) {
        const f = findRemote(slotId, id)
        if (f) { f.status = 'done'; f.fileRef = file; f.receivedChunks = f.totalChunks }
      },
      onTextReceived(content) {
        const rs = slots.find(s => s.id === slotId)
        if (!rs) return
        rs.receivedTexts = [{ id: Date.now(), content }, ...rs.receivedTexts]
      },
    }, store)

    const p = new FiledropPeer(signaling, isHost ? 'host' : 'peer', secret, {
      connected() {
        if (slot.closed) return
        const rs = slots.find(s => s.id === slotId)
        if (!rs) return
        rs.connected = true
        t.peer = p
        // Late joiners get every file shared so far
        for (const lf of localFiles) {
          lf.sent[slotId] = null
          t.addFile(lf.file, lf.id, lf.key)
        }
        t.onPeerConnected()
        status = 'connected'
        if (isHost && !isListening && !locked) createSlot()
      },
      disconnected() {
        if (slot.closed) return
        destroySlot(slotId)
        if (isHost) {
          if (connectedPeers === 0) status = 'waiting'
          if (!isListening && !locked) createSlot()
        } else {
          status = 'disconnected'
        }
      },
      authFailed() {
        if (slot.closed) return
        destroySlot(slotId)
        if (isHost) {
          if (++authFailures >= MAX_AUTH_FAILURES) locked = true
          else if (!isListening) createSlot()
        } else {
          status = 'auth-failed'
        }
      },
      controlMessage(msg) { t.handleControl(msg) },
      dataChunk(buf) { t.handleData(buf) },
    })

    slot.peer = p
    slot.transfer = t
    slots = [...slots, slot]
  }

  // Outgoing files can't survive a reload, so warn while any are still unsent
  let hasUnsentFiles = $derived(localFiles.some(f => sendProgress(f).state !== 'sent'))
  $effect(() => {
    if (!hasUnsentFiles) return
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault() }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  })

  onMount(() => {
    let unmounted = false
    ReceiveStore.open(roomId).then(s => {
      if (unmounted) return
      store = s
      // Files from a previous session: finished ones can be downloaded, the rest resume when the sender returns
      remoteFiles = s.restored().map(f => ({
        ...f.info, slotId: null, receivedChunks: f.receivedCount,
        status: f.file ? 'done' : 'paused', fileRef: f.file,
      }))
      createSlot()
      // Files chosen on the landing page: queue them; they go out once a peer connects
      const picked = takePendingFiles()
      if (picked.length) addFiles(picked)
    })
    return () => {
      unmounted = true
      for (const slot of [...slots]) destroySlot(slot.id)
    }
  })

  function setAskBeforeReceiving(ask: boolean) {
    downloadMode = ask ? 'manual' : 'auto'
    if (ask) return
    for (const f of remoteFiles) {
      if (f.status === 'offered' && f.slotId) acceptFile(f.slotId, f.id)
    }
  }

  async function addFiles(files: FileList | File[]) {
    // Copy first: the input's FileList is cleared right after this is called
    for (const file of Array.from(files)) {
      const id = nextFileId++
      const key = await fileKey(file)
      localFiles = [...localFiles, {
        id, key, name: file.name, size: file.size,
        totalChunks: Math.ceil(file.size / CHUNK_SIZE),
        file, sent: {}, deliveredTo: 0,
      }]
      const lf = localFiles[localFiles.length - 1]
      for (const slot of slots) {
        if (!slot.connected) continue
        lf.sent[slot.id] = null
        slot.transfer.addFile(file, id, key)
      }
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault(); dragging = false
    const files = e.dataTransfer?.files
    if (files?.length) addFiles(files)
  }
  function onDragOver(e: DragEvent) { e.preventDefault(); dragging = true }
  function onDragLeave() { dragging = false }

  function onInputChange(e: Event) {
    const input = e.currentTarget as HTMLInputElement
    if (input.files) addFiles(input.files)
    input.value = ''
  }

  function acceptFile(slotId: string, id: number) {
    const slot = slots.find(s => s.id === slotId)
    const f = findRemote(slotId, id)
    if (!slot || !f) return
    f.status = 'receiving'
    slot.transfer.acceptFile(id).then(ok => {
      // Another peer is already sending the same file
      if (!ok) remoteFiles = remoteFiles.filter(r => r !== f)
    })
  }

  function rejectFile(slotId: string, id: number) {
    slots.find(s => s.id === slotId)?.transfer.rejectFile(id)
    remoteFiles = remoteFiles.filter(f => !(f.slotId === slotId && f.id === id))
  }

  function downloadFile(f: RemoteFile) {
    if (!f.fileRef) return
    const url = URL.createObjectURL(f.fileRef)
    const a = document.createElement('a')
    a.href = url; a.download = f.name; a.click()
    setTimeout(() => URL.revokeObjectURL(url), 30_000)
    store.markDownloaded(f.key).catch(console.error)
    remoteFiles = remoteFiles.filter(r => r !== f)
  }

  function sendText() {
    const content = outgoingText.trim()
    if (!content || connectedPeers === 0) return
    for (const slot of slots) {
      if (slot.connected) slot.transfer.sendText(content)
    }
    outgoingText = ''
  }

  function onTextKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) sendText()
  }

  async function copyText(txt: ReceivedText) {
    await navigator.clipboard.writeText(txt.content)
    textCopied = txt.id
    setTimeout(() => { textCopied = null }, 1500)
  }

  function dismissText(id: number) {
    for (const slot of slots) {
      slot.receivedTexts = slot.receivedTexts.filter(t => t.id !== id)
    }
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && qrOpen) {
      qrOpen = false
      qrBtn?.focus()
    }
  }

  // The QR button is excluded so its own toggle handler isn't immediately undone
  function onWindowPointerdown(e: PointerEvent) {
    if (!qrOpen) return
    const target = e.target as Node
    if (qrPop?.contains(target) || qrBtn?.contains(target)) return
    qrOpen = false
  }

  const canShare = typeof navigator.share === 'function'

  async function shareLink() {
    try {
      await navigator.share({ title: 'FileDrop', url: joinUrl })
    } catch {
      // user dismissed the share sheet
    }
  }

  async function copyLink() {
    await navigator.clipboard.writeText(joinUrl)
    linkCopied = true
    setTimeout(() => { linkCopied = false }, 1500)
  }

  function pct(done: number, total: number) {
    return total === 0 ? 0 : Math.round((done / total) * 100)
  }

  /** Combined progress of one outgoing file across all connected peers. */
  function sendProgress(f: OutgoingFile) {
    const peers = Object.values(f.sent)
    const accepted = peers.filter((v): v is number => v !== null)
    const finished = accepted.filter(v => v >= f.totalChunks).length
    const inFlight = accepted.length > finished
    const state: 'sending' | 'sent' | 'waiting' =
      inFlight ? 'sending'
      : finished === peers.length && (peers.length > 0 || f.deliveredTo > 0) ? 'sent'
      : 'waiting'
    const percent =
      state === 'sending' ? pct(accepted.reduce((a, b) => a + b, 0), f.totalChunks * accepted.length)
      : state === 'sent' ? 100
      : pct(finished, peers.length)
    return { state, percent, finished, peers: peers.length }
  }

  function fmtBytes(n: number): string {
    if (n < 1_024) return `${n} B`
    if (n < 1_048_576) return `${(n / 1_024).toFixed(1)} KB`
    if (n < 1_073_741_824) return `${(n / 1_048_576).toFixed(1)} MB`
    return `${(n / 1_073_741_824).toFixed(2)} GB`
  }

  function fileExt(name: string): string {
    const m = name.match(/\.([^.]+)$/)
    return m ? m[1].toUpperCase().slice(0, 4) : '?'
  }

  const statusColor: Record<Status, string> = {
    waiting: 'var(--faint)',
    connected: 'var(--ok)',
    disconnected: 'var(--danger)',
    'auth-failed': 'var(--danger)',
  }

  function statusLabel(s: Status): string {
    if (s === 'waiting') return !isHost ? 'Connecting…' : locked ? 'Room locked' : 'Waiting for peer…'
    if (s === 'connected') return 'Connected'
    if (s === 'auth-failed') return 'Verification failed'
    return 'Disconnected'
  }
</script>

<svelte:window onkeydown={onWindowKeydown} onpointerdown={onWindowPointerdown} />

<div class="pm t-{theme} r-wrap">
  <div class="r-inner">

    <div class="r-header"><Header {navigate} /></div>

    <!-- ── Session code ── -->
    <div class="r-code-row">
      <div style="min-width: 0">
        <div class="row r-label-row">
          <span class="label">Session code</span>
    <div class="status-badge" style="color: {statusColor[status]}">
      <span
        class="dot"
        class:waiting={status === 'waiting'}
        class:error={status === 'disconnected' || status === 'auth-failed'}
      ></span>
      {status === 'connected'
        ? `Connected · ${connectedPeers} ${connectedPeers === 1 ? 'peer' : 'peers'}`
        : statusLabel(status)}
    </div>
        </div>
        <div class="r-code" aria-label="Session code {roomId}"><span>{codeWords[0]}-{codeWords[1]}-</span><mark>{codeWords[2]}-{codeWords[3]}</mark></div>
      </div>
        <div class="row r-share-actions">
          {#if canShare}
            <button class="btn btn-ghost btn-sm" onclick={shareLink}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 8.25H7.5a2.25 2.25 0 0 0-2.25 2.25v9a2.25 2.25 0 0 0 2.25 2.25h9a2.25 2.25 0 0 0 2.25-2.25v-9a2.25 2.25 0 0 0-2.25-2.25H15M12 15V2.25m0 0-3 3m3-3 3 3"/></svg>
              Share
            </button>
          {/if}
          <button class="btn btn-ghost btn-sm" onclick={copyLink}>
            {#if linkCopied}
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--ok)" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4.5 12.75l6 6 9-13.5"/></svg>
              Copied
            {:else}
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 9V5.25A2.25 2.25 0 0 1 11.25 3h7.5A2.25 2.25 0 0 1 21 5.25v7.5A2.25 2.25 0 0 1 18.75 15H15M5.25 9h7.5A2.25 2.25 0 0 1 15 11.25v7.5A2.25 2.25 0 0 1 12.75 21h-7.5A2.25 2.25 0 0 1 3 18.75v-7.5A2.25 2.25 0 0 1 5.25 9Z"/></svg>
              Copy
            {/if}
          </button>
          <button
            class="btn btn-sm"
            class:btn-primary={qrOpen}
            class:btn-ghost={!qrOpen}
            aria-expanded={qrOpen}
            bind:this={qrBtn}
            onclick={() => (qrOpen = !qrOpen)}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4.5 4.5h4v4h-4zM15.5 4.5h4v4h-4zM4.5 15.5h4v4h-4zM15.5 15.5h1.8v1.8h-1.8zM19.3 19.3h.7v.7M15.5 19.7v.3M19.7 15.5h.3"/></svg>
            QR
          </button>
        </div>

      <!-- QR popover -->
      <div class="qr-pop" class:open={qrOpen} bind:this={qrPop}>
        <div class="row r-qr-body">
          <div class="qr-tile">
            <QrCode url={joinUrl} size={150} />
          </div>
          <div class="r-qr-text">
            <div class="r-qr-title">Scan to join</div>
            <div class="muted" style="font-size: 13.5px; line-height: 1.5; margin-top: 6px">
              Point a phone camera at the code to open this session — no app required.
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="r-cols">
    <div class="r-left">

    <!-- ── Drop zone ── -->
    <label
      class="drop r-drop"
      class:dragging
      ondrop={onDrop}
      ondragover={onDragOver}
      ondragleave={onDragLeave}
      aria-label="File drop area"
    >
      <div class="r-dz-content">
        <div class="dz-title">
          <span class="r-pointer-only">Drop files here to send</span>
          <span class="r-touch-only">Tap to pick files to send</span>
        </div>
        <span class="btn btn-primary btn-sm">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 4.5v15m7.5-7.5h-15"/></svg>
          Browse files
        </span>
        <input type="file" multiple hidden onchange={onInputChange} />
      </div>
    </label>

    <!-- ── Sending ── -->
    {#if localFiles.length > 0}
      <div class="label" style="margin: 24px 0 12px">Sending</div>
      {#each localFiles as f (f.id)}
        {@const p = sendProgress(f)}
        <div class="file r-file">
          <span class="glyph">{fileExt(f.name)}</span>
          <div style="flex: 1; min-width: 0">
            <div class="fname">{f.name}</div>
            <div class="row" style="gap: 12px; margin-top: 9px">
              <span class="fsize">{fmtBytes(f.size)}</span>
              <span class="bar"><i style="width: {p.percent}%"></i></span>
              {#if p.state === 'sent'}
                <span class="pct" style="color: var(--ok)">Sent</span>
              {:else if p.state === 'sending'}
                <span class="pct">{p.percent}%</span>
              {:else}
                <span class="pct" style="width: auto">Waiting</span>
              {/if}
            </div>
            {#if p.peers > 1}
              <div class="fsize" style="margin-top: 5px">Sent to {p.finished} of {p.peers} peers</div>
            {:else if p.peers === 0 && p.state === 'waiting'}
              <div class="fsize" style="margin-top: 5px">Sends when a peer joins</div>
            {/if}
          </div>
        </div>
      {/each}
    {/if}

    <!-- ── Incoming ── -->
    {#if remoteFiles.length > 0}
      <div class="label" style="margin: 24px 0 12px">Incoming</div>
      {#each remoteFiles as f (f.slotId ? f.slotId + ':' + f.id : f.key)}
        <div class="file r-file">
          <span class="glyph">{fileExt(f.name)}</span>
          <div style="flex: 1; min-width: 0">
            <div class="fname">{f.name}</div>
            {#if f.status === 'offered'}
              <div class="fsize" style="margin-top: 5px">{fmtBytes(f.size)}</div>
            {:else if f.status === 'receiving' || f.status === 'paused'}
              <div class="row" style="gap: 12px; margin-top: 9px">
                <span class="fsize">{fmtBytes(f.size)}</span>
                <span class="bar"><i style="width: {pct(f.receivedChunks, f.totalChunks)}%"></i></span>
                <span class="pct">{pct(f.receivedChunks, f.totalChunks)}%</span>
              </div>
              {#if f.status === 'paused'}
                <div class="fsize" style="margin-top: 5px">Paused · resumes when the sender shares it again</div>
              {/if}
            {:else if f.fileRef}
              <div class="fsize" style="margin-top: 5px">{fmtBytes(f.size)} · received</div>
            {/if}
          </div>
          {#if f.status === 'offered' && f.slotId}
            {@const slotId = f.slotId}
            <div class="row r-file-actions">
              <button class="btn btn-primary btn-sm" onclick={() => acceptFile(slotId, f.id)}>Accept</button>
              <button class="btn btn-ghost btn-sm" onclick={() => rejectFile(slotId, f.id)}>Reject</button>
            </div>
          {:else if f.status === 'done' && f.fileRef}
            <button class="btn btn-primary btn-sm r-file-actions" onclick={() => downloadFile(f)}>
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3"/></svg>
              Download
            </button>
          {/if}
        </div>
      {/each}
    {/if}

    </div>

    <div class="r-right">
      <div class="panel r-card row between r-ask">
        <div>
          <div class="r-card-title">Ask before receiving</div>
          <div class="muted" style="font-size: 13px; margin-top: 2px">Approve each file first</div>
        </div>
      <label class="switch" class:on={downloadMode === 'manual'}>
        <input
          type="checkbox"
          class="sr-only"
          role="switch"
          aria-label="Ask before receiving"
          checked={downloadMode === 'manual'}
          onchange={(e) => setAskBeforeReceiving((e.currentTarget as HTMLInputElement).checked)}
        />
        <span class="track"><span class="knob"></span></span>
      </label>
      </div>

    <!-- ── Received texts ── -->
    {#if allReceivedTexts.length > 0}
      <div class="label" style="margin: 24px 0 12px">Received text</div>
      {#each allReceivedTexts as txt (txt.id)}
        <div class="msg r-msg">
          {txt.content}
          <div class="row" style="gap: 8px; margin-top: 14px">
            <button class="btn btn-ghost btn-sm" onclick={() => copyText(txt)}>
              {#if textCopied === txt.id}
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--ok)" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4.5 12.75l6 6 9-13.5"/></svg>
                Copied
              {:else}
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 9V5.25A2.25 2.25 0 0 1 11.25 3h7.5A2.25 2.25 0 0 1 21 5.25v7.5A2.25 2.25 0 0 1 18.75 15H15M5.25 9h7.5A2.25 2.25 0 0 1 15 11.25v7.5A2.25 2.25 0 0 1 12.75 21h-7.5A2.25 2.25 0 0 1 3 18.75v-7.5A2.25 2.25 0 0 1 5.25 9Z"/></svg>
                Copy
              {/if}
            </button>
            <button
              class="btn btn-ghost btn-sm"
              style="background: transparent; border-color: transparent; box-shadow: none; color: var(--muted)"
              onclick={() => dismissText(txt.id)}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 18 18 6M6 6l12 12"/></svg>
              Dismiss
            </button>
          </div>
        </div>
      {/each}
    {/if}

    <!-- ── Text compose ── -->
    <div class="panel r-card r-compose">
    <div class="r-card-title" style="margin-bottom: 12px">Send text</div>
    <textarea
      class="ta"
      placeholder="Paste text to send to the other side…"
      bind:value={outgoingText}
      onkeydown={onTextKeydown}
    ></textarea>
    <div class="row between r-send-row">
      {#if connectedPeers === 0}
        <span class="faint" style="font-size: 12.5px">You can send once someone joins</span>
      {:else}
        <span class="faint r-pointer-only" style="font-size: 12.5px">{isMac ? '⌘' : 'Ctrl'} + Enter to send</span>
      {/if}
      <button
        class="btn btn-primary btn-sm"
        onclick={sendText}
        disabled={!outgoingText.trim() || connectedPeers === 0}
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 12 3.27 3.6a.6.6 0 0 1 .82-.72l16.2 8.1a.6.6 0 0 1 0 1.08l-16.2 8.1a.6.6 0 0 1-.82-.72L6 12Zm0 0h7"/></svg>
        Send
      </button>
    </div>
    </div>
    </div>
    </div>

    <div class="r-footer-gap"></div>
    <Footer />

  </div>
</div>

<style>
  .r-wrap {
    min-height: 100dvh;
    padding: 40px 64px 32px;
    display: flex;
    flex-direction: column;
  }

  .r-inner {
    flex: 1;
    width: 100%;
    max-width: 1080px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }

  .r-footer-gap { height: 48px; flex: none; }
  .r-header { margin-bottom: 36px; }
  .r-label-row { gap: 16px; flex-wrap: wrap; }

  /* Session code: big, last two words (the secret) highlighted */
  .r-code-row {
    position: relative;
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: 20px;
    flex-wrap: wrap;
    padding-bottom: 24px;
    margin-bottom: 28px;
    border-bottom: 2px solid var(--line);
  }
  .r-code {
    margin-top: 8px;
    font-family: var(--mono);
    font-weight: 500;
    font-size: clamp(26px, 4vw, 44px);
    letter-spacing: -.02em;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
  .r-code mark { background: var(--hl); color: #111110; }
  /* Wrap only between the public words and the secret, never inside either half */
  .r-code > * { white-space: nowrap; }
  .r-share-actions { gap: 10px; flex: none; }
  .r-share-actions :global(.btn) { padding: 12px 20px; }

  .r-qr-title {
    font-family: var(--display);
    font-weight: 800;
    font-size: 18px;
    color: var(--ink);
  }
  .r-qr-body { gap: 20px; align-items: center; }
  .r-qr-text { max-width: 200px; }

  /* Two columns: transfers left, settings + text right */
  .r-cols {
    display: grid;
    grid-template-columns: minmax(0, 7fr) minmax(0, 4fr);
    gap: 40px;
    align-items: start;
  }
  .r-left, .r-right { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .r-card { box-shadow: none; padding: 18px 22px; }
  .r-card-title { font-weight: 800; font-size: 17px; }
  .r-ask { gap: 16px; }
  .r-compose { display: flex; flex-direction: column; }

  .r-drop { display: block; cursor: pointer; }
  .r-dz-content { display: flex; flex-direction: column; align-items: center; }

  .r-left :global(.label) { margin: 10px 0 0 !important; }
  .r-right :global(.label) { margin: 10px 0 0 !important; }
  .r-file-actions { gap: 8px; flex: none; }
  .r-msg { box-shadow: none; }

  .r-send-row { margin-top: 12px; gap: 10px; flex-wrap: wrap; }

  /* Touch devices can't drag-and-drop or use keyboard shortcuts */
  .r-touch-only { display: none; }
  @media (hover: none) and (pointer: coarse) {
    .r-pointer-only { display: none; }
    .r-touch-only { display: inline; }
    .r-send-row { justify-content: flex-end; }
  }

  @media (max-width: 900px) {
    .r-cols { grid-template-columns: minmax(0, 1fr); gap: 14px; }
  }

  @media (max-width: 600px) {
    .r-wrap { padding: 20px 16px; }
    .r-code-row { flex-direction: column; align-items: stretch; }
    .r-share-actions { flex: none; }
    .r-share-actions > :global(.btn) { flex: 1; }

    /* QR popover spans the row and stacks vertically */
    .r-code-row :global(.qr-pop) { left: 0; right: 0; width: auto; max-width: none; }
    .r-qr-body { flex-direction: column; text-align: center; gap: 16px; }
    .r-qr-text { max-width: none; }

    .r-drop { padding: 28px 20px; }

    /* Action buttons drop below the file name and fill the row */
    .r-file { flex-wrap: wrap; }
    .r-file-actions { flex: 1 0 100%; }
    .r-file-actions > :global(.btn) { flex: 1; }
    .r-send-row :global(.btn) { padding: 12px 22px; font-size: 15px; }
  }
</style>
