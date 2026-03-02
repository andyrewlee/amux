import type { WSServerMessage, WSClientMessage } from './types'

export type WSHandler = (msg: WSServerMessage) => void

export class TabWebSocket {
  private ws: WebSocket | null = null
  private url: string
  private handlers: Set<WSHandler> = new Set()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = 1000
  private maxReconnectDelay = 30000
  private closed = false

  constructor(url: string) {
    this.url = url
  }

  connect(): void {
    if (this.closed) return
    try {
      this.ws = new WebSocket(this.url)
    } catch {
      this.scheduleReconnect()
      return
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: WSServerMessage = JSON.parse(event.data)
        this.handlers.forEach((h) => h(msg))
      } catch {
        // ignore parse errors
      }
    }

    this.ws.onopen = () => {
      this.reconnectDelay = 1000
    }

    this.ws.onclose = () => {
      if (!this.closed) {
        this.scheduleReconnect()
      }
    }

    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  send(msg: WSClientMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  sendPrompt(text: string): void {
    this.send({ type: 'prompt', text })
  }

  sendInterrupt(): void {
    this.send({ type: 'interrupt' })
  }

  onMessage(handler: WSHandler): () => void {
    this.handlers.add(handler)
    return () => this.handlers.delete(handler)
  }

  close(): void {
    this.closed = true
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
    }
    this.ws?.close()
    this.handlers.clear()
  }

  private scheduleReconnect(): void {
    if (this.closed) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay)
      this.connect()
    }, this.reconnectDelay)
  }
}
