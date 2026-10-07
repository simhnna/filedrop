export type SignalingPayload = Record<string, unknown>

type MessageHandler = (data: SignalingPayload, clients: number) => void

interface YMessage {
  type: 'subscribe' | 'unsubscribe' | 'publish' | 'ping' | 'pong'
  topics?: string[]
  topic?: string
  data?: SignalingPayload
  clients?: number
}

export class YWebRTCSignaling {
  private ws: WebSocket | null = null
  private handlers: MessageHandler[] = []
  private pingInterval: ReturnType<typeof setInterval> | null = null
  private reconnectTimeout: ReturnType<typeof setTimeout> | null = null
  private closed = false

  constructor(
    private readonly url: string,
    private readonly topic: string,
  ) {
    this.connect()
  }

  private connect() {
    this.ws = new WebSocket(this.url)

    this.ws.onopen = () => {
      this.send({ type: 'subscribe', topics: [this.topic] })
      this.pingInterval = setInterval(() => this.send({ type: 'ping' }), 30_000)
    }

    this.ws.onmessage = (event: MessageEvent<string>) => {
      let msg: YMessage
      try {
        msg = JSON.parse(event.data) as YMessage
      } catch {
        return
      }
      if (msg.type === 'publish' && msg.topic === this.topic && msg.data) {
        const clients = msg.clients ?? 0
        for (const h of this.handlers) h(msg.data, clients)
      }
    }

    this.ws.onclose = () => {
      if (this.pingInterval !== null) {
        clearInterval(this.pingInterval)
        this.pingInterval = null
      }
      if (!this.closed) {
        this.reconnectTimeout = setTimeout(() => this.connect(), 2_000)
      }
    }

    this.ws.onerror = () => this.ws?.close()
  }

  private send(msg: YMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  publish(data: SignalingPayload) {
    this.send({ type: 'publish', topic: this.topic, data })
  }

  onMessage(handler: MessageHandler): () => void {
    this.handlers.push(handler)
    return () => {
      this.handlers = this.handlers.filter(h => h !== handler)
    }
  }

  close() {
    this.closed = true
    if (this.reconnectTimeout !== null) clearTimeout(this.reconnectTimeout)
    if (this.pingInterval !== null) clearInterval(this.pingInterval)
    if (this.ws) {
      this.send({ type: 'unsubscribe', topics: [this.topic] })
      this.ws.close()
    }
  }
}
