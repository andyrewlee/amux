import { create } from 'zustand'
import { SSEClient } from '../api/events'
import api from '../api/client'

interface ConnectionState {
  connected: boolean
  token: string
  sse: SSEClient | null
  setToken: (token: string) => void
  connect: () => void
  disconnect: () => void
}

export const useConnection = create<ConnectionState>((set, get) => ({
  connected: false,
  token: new URLSearchParams(window.location.search).get('token') || localStorage.getItem('medusa_token') || '',
  sse: null,

  setToken: (token) => {
    api.setToken(token)
    localStorage.setItem('medusa_token', token)
    set({ token })
  },

  connect: () => {
    const { token } = get()
    if (!token) return

    api.setToken(token)
    const sse = new SSEClient(api.sseURL())
    sse.onEvent(() => {
      // SSE connected
      set({ connected: true })
    })
    sse.connect()
    set({ sse, connected: true })
  },

  disconnect: () => {
    get().sse?.close()
    set({ sse: null, connected: false })
  },
}))
