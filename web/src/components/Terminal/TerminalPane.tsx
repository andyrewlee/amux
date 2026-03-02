import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import api from '../../api/client'
import '@xterm/xterm/css/xterm.css'

interface TerminalPaneProps {
  tabID: string
}

export function TerminalPane({ tabID }: TerminalPaneProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!containerRef.current) return

    const term = new Terminal({
      theme: {
        background: '#0d1117',
        foreground: '#e6edf3',
        cursor: '#58a6ff',
        selectionBackground: '#264f78',
      },
      fontFamily: 'monospace',
      fontSize: 14,
      cursorBlink: true,
    })

    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(containerRef.current)
    fit.fit()
    termRef.current = term

    // Connect WebSocket
    const ws = new WebSocket(api.tabPTYWSURL(tabID))
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(event.data))
      }
    }

    // Send terminal input
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'input', data: btoa(data) }))
      }
    })

    // Handle resize
    const resizeObs = new ResizeObserver(() => {
      fit.fit()
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', rows: term.rows, cols: term.cols }))
      }
    })
    resizeObs.observe(containerRef.current)

    return () => {
      resizeObs.disconnect()
      ws.close()
      term.dispose()
    }
  }, [tabID])

  return <div ref={containerRef} className="h-full w-full" />
}
