export type SSEHandler = (event: Record<string, unknown>) => void

export class SSEClient {
  private source: EventSource | null = null
  private url: string
  private handlers: Set<SSEHandler> = new Set()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private closed = false

  constructor(url: string) {
    this.url = url
  }

  connect(): void {
    if (this.closed) return
    this.source = new EventSource(this.url)

    this.source.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        this.handlers.forEach((h) => h(data))
      } catch {
        // ignore parse errors
      }
    }

    this.source.onerror = () => {
      this.source?.close()
      if (!this.closed) {
        this.reconnectTimer = setTimeout(() => this.connect(), 3000)
      }
    }
  }

  onEvent(handler: SSEHandler): () => void {
    this.handlers.add(handler)
    return () => this.handlers.delete(handler)
  }

  close(): void {
    this.closed = true
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.source?.close()
    this.handlers.clear()
  }
}
