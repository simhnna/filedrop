import type { YWebRTCSignaling, SignalingPayload } from './signaling'
import { Pake, sdpFingerprints, toBase64 } from './pake'

export interface PeerEvents {
  connected: () => void
  disconnected: () => void
  authFailed?: () => void
  controlMessage: (msg: unknown) => void
  dataChunk: (buffer: ArrayBuffer) => void
}

const ICE_SERVERS: RTCIceServer[] = [
  { urls: 'stun:stun.cloudflare.com:3478' },
]

// Give up on a connection whose PAKE hasn't completed by then
const AUTH_TIMEOUT_MS = 15_000

// Pause sending when the buffer exceeds 1 MB, resume at 256 KB
const BUFFER_HIGH = 1_048_576
const BUFFER_LOW  =   262_144

export class FiledropPeer {
  readonly peerId = crypto.randomUUID()

  private pc: RTCPeerConnection
  private controlChannel: RTCDataChannel | null = null
  private dataChannel: RTCDataChannel | null = null
  private remotePeerId: string | null = null
  private offerSent = false
  private announceInterval: ReturnType<typeof setInterval> | null = null
  private unsubSignaling: (() => void) | null = null
  private authenticated = false
  private authFailed = false
  private pake: Pake | null = null
  private authTimer: ReturnType<typeof setTimeout> | null = null

  // Queue of { buffer, resolve } waiting for buffer headroom
  private sendQueue: Array<{ buffer: ArrayBuffer; resolve: () => void }> = []

  constructor(
    private readonly signaling: YWebRTCSignaling,
    private readonly role: 'host' | 'peer',
    /** Room secret; never sent anywhere, only fed into the PAKE */
    private readonly secret: string,
    private readonly events: PeerEvents,
  ) {
    this.pc = new RTCPeerConnection({ iceServers: ICE_SERVERS })
    this.setupPeerConnection()

    if (role === 'host') {
      this.setupDataChannels()
    } else {
      this.pc.ondatachannel = ({ channel }) => {
        if (channel.label === 'control') this.bindControl(channel)
        if (channel.label === 'data') this.bindData(channel)
      }
    }

    this.unsubSignaling = signaling.onMessage(this.handleSignaling.bind(this))
    this.startAnnouncing()
  }

  // ── Announcing ──────────────────────────────────────────────────────────────

  private startAnnouncing() {
    const announce = () => this.signaling.publish({
      type: 'announce',
      role: this.role,
      peerId: this.peerId,
    })
    announce()
    // Re-announce every 4 s until connected (handles missed announces)
    this.announceInterval = setInterval(() => {
      if (this.authenticated) {
        clearInterval(this.announceInterval!)
        this.announceInterval = null
      } else {
        announce()
      }
    }, 4_000)
  }

  // ── PeerConnection setup ────────────────────────────────────────────────────

  private setupPeerConnection() {
    this.pc.onicecandidate = ({ candidate }) => {
      if (candidate && this.remotePeerId) {
        this.signaling.publish({
          type: 'ice',
          from: this.peerId,
          to: this.remotePeerId,
          candidate: candidate.toJSON() as unknown as SignalingPayload,
        })
      }
    }

    this.pc.onconnectionstatechange = () => {
      const s = this.pc.connectionState
      if (s === 'disconnected' || s === 'failed' || s === 'closed') {
        this.events.disconnected()
      }
    }
  }

  private setupDataChannels() {
    this.bindControl(this.pc.createDataChannel('control', { ordered: true }))
    const data = this.pc.createDataChannel('data', { ordered: true })
    data.bufferedAmountLowThreshold = BUFFER_LOW
    this.bindData(data)
  }

  private bindControl(ch: RTCDataChannel) {
    this.controlChannel = ch
    ch.onopen = () => this.onControlOpen()
    ch.onmessage = (e: MessageEvent<string>) => {
      try {
        const msg = JSON.parse(e.data)
        if (!this.authenticated) {
          this.handleAuthMessage(msg)
        } else {
          this.events.controlMessage(msg)
        }
      } catch { /* ignore */ }
    }
  }

  private bindData(ch: RTCDataChannel) {
    this.dataChannel = ch
    ch.binaryType = 'arraybuffer'
    ch.bufferedAmountLowThreshold = BUFFER_LOW
    ch.onbufferedamountlow = () => this.drainQueue()
    ch.onmessage = (e: MessageEvent<ArrayBuffer>) => {
      if (this.authenticated) this.events.dataChunk(e.data)
    }
  }

  // ── Auth ────────────────────────────────────────────────────────────────────
  //
  // Both sides run the PAKE (see pake.ts) as soon as the control channel opens:
  //   → { type: 'pake', share }          our public share
  //   → { type: 'pake-confirm', mac }    sent once we've seen the other share
  // The connection counts as established only after the other side's MAC checks
  // out, which proves it knows the room secret and sees the same DTLS fingerprints.

  private onControlOpen() {
    const pake = this.ensurePake()
    if (!pake) return
    this.sendControl({ type: 'pake', share: toBase64(pake.share) })
    this.authTimer = setTimeout(() => this.failAuth(), AUTH_TIMEOUT_MS)
  }

  /** Created lazily: the other side's share may be handled before our onopen. */
  private ensurePake(): Pake | null {
    if (this.pake) return this.pake
    const local = this.pc.localDescription?.sdp
    const remote = this.pc.remoteDescription?.sdp
    if (!local || !remote) { this.failAuth(); return null }
    const [hostFp, peerFp] = this.role === 'host'
      ? [sdpFingerprints(local), sdpFingerprints(remote)]
      : [sdpFingerprints(remote), sdpFingerprints(local)]
    this.pake = new Pake(this.secret, this.role, hostFp, peerFp)
    return this.pake
  }

  private handleAuthMessage(msg: unknown) {
    const m = msg as Record<string, unknown>
    const pake = this.ensurePake()
    if (!pake) return
    if (m.type === 'pake' && typeof m.share === 'string') {
      try {
        this.sendControl({ type: 'pake-confirm', mac: toBase64(pake.receiveShare(m.share)) })
      } catch {
        this.failAuth()
      }
    } else if (m.type === 'pake-confirm' && typeof m.mac === 'string') {
      if (!pake.verifyConfirm(m.mac)) { this.failAuth(); return }
      if (this.authTimer !== null) clearTimeout(this.authTimer)
      this.authenticated = true
      this.events.connected()
    }
  }

  private failAuth() {
    if (this.authFailed || this.authenticated) return
    this.authFailed = true
    this.events.authFailed?.()
    this.close()
  }

  // ── Signaling ───────────────────────────────────────────────────────────────

  private async handleSignaling(data: SignalingPayload) {
    const msg = data as Record<string, unknown>

    // Ignore our own messages
    if (msg.from === this.peerId) return

    if (msg.type === 'announce') {
      const theirRole = msg.role as string
      const theirId = msg.peerId as string

      // When joiner announces, host creates the offer (once)
      if (this.role === 'host' && theirRole === 'peer' && !this.offerSent) {
        this.remotePeerId = theirId
        this.offerSent = true
        await this.createOffer()
      }

      // When host announces, joiner notes the peerId (offer arrives shortly)
      if (this.role === 'peer' && theirRole === 'host') {
        this.remotePeerId = theirId
      }
    }

    if (msg.type === 'offer' && msg.to === this.peerId) {
      this.remotePeerId = msg.from as string
      await this.pc.setRemoteDescription({ type: 'offer', sdp: msg.sdp as string })
      const answer = await this.pc.createAnswer()
      await this.pc.setLocalDescription(answer)
      this.signaling.publish({
        type: 'answer',
        from: this.peerId,
        to: this.remotePeerId,
        sdp: answer.sdp as string,
      })
    }

    if (msg.type === 'answer' && msg.to === this.peerId) {
      await this.pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp as string })
    }

    if (msg.type === 'ice' && msg.to === this.peerId && msg.candidate) {
      await this.pc.addIceCandidate(msg.candidate as RTCIceCandidateInit)
    }
  }

  private async createOffer() {
    const offer = await this.pc.createOffer()
    await this.pc.setLocalDescription(offer)
    this.signaling.publish({
      type: 'offer',
      from: this.peerId,
      to: this.remotePeerId!,
      sdp: offer.sdp as string,
    })
  }

  // ── Sending ─────────────────────────────────────────────────────────────────

  sendControl(msg: unknown) {
    if (this.controlChannel?.readyState === 'open') {
      this.controlChannel.send(JSON.stringify(msg))
    }
  }

  /** Resolves once the chunk has been handed to the data channel. */
  sendChunk(buffer: ArrayBuffer): Promise<void> {
    return new Promise(resolve => {
      const ch = this.dataChannel
      if (!ch || ch.readyState !== 'open') {
        resolve() // drop silently; transfer layer handles retries on reconnect
        return
      }
      if (ch.bufferedAmount < BUFFER_HIGH) {
        ch.send(buffer)
        resolve()
      } else {
        this.sendQueue.push({ buffer, resolve })
      }
    })
  }

  private drainQueue() {
    const ch = this.dataChannel
    if (!ch || ch.readyState !== 'open') return
    while (this.sendQueue.length > 0 && ch.bufferedAmount < BUFFER_HIGH) {
      const { buffer, resolve } = this.sendQueue.shift()!
      ch.send(buffer)
      resolve()
    }
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  close() {
    if (this.announceInterval !== null) clearInterval(this.announceInterval)
    if (this.authTimer !== null) clearTimeout(this.authTimer)
    this.unsubSignaling?.()
    this.pc.close()
  }
}
