import { create } from 'zustand'
import type { TabInfo, SDKMessage, WSServerMessage } from '../api/types'
import api from '../api/client'
import { TabWebSocket } from '../api/ws'

interface TabsState {
  tabs: TabInfo[]
  activeTabID: string | null
  messages: Record<string, SDKMessage[]> // tabID -> messages
  connections: Record<string, TabWebSocket>

  fetchTabs: (wsID: string) => Promise<void>
  setActiveTab: (tabID: string | null) => void
  launchTab: (wsID: string, opts?: { assistant?: string; prompt?: string }) => Promise<string>
  closeTab: (tabID: string) => Promise<void>
  resumeTab: (tabID: string) => Promise<void>
  interruptTab: (tabID: string) => Promise<void>
  sendPrompt: (tabID: string, text: string) => void
  connectTab: (tabID: string) => void
  disconnectTab: (tabID: string) => void
}

export const useTabs = create<TabsState>((set, get) => ({
  tabs: [],
  activeTabID: null,
  messages: {},
  connections: {},

  fetchTabs: async (wsID) => {
    try {
      const tabs = await api.listTabs(wsID)
      set({ tabs })
    } catch {
      // ignore
    }
  },

  setActiveTab: (tabID) => {
    set({ activeTabID: tabID })
    if (tabID && !get().connections[tabID]) {
      get().connectTab(tabID)
    }
  },

  launchTab: async (wsID, opts) => {
    const result = await api.launchTab(wsID, opts || {})
    await get().fetchTabs(wsID)
    set({ activeTabID: result.tab_id })
    get().connectTab(result.tab_id)
    return result.tab_id
  },

  closeTab: async (tabID) => {
    get().disconnectTab(tabID)
    await api.closeTab(tabID)
    set((state) => ({
      tabs: state.tabs.filter((t) => t.id !== tabID),
      activeTabID: state.activeTabID === tabID ? null : state.activeTabID,
    }))
  },

  resumeTab: async (tabID) => {
    await api.resumeTab(tabID)
  },

  interruptTab: async (tabID) => {
    await api.interruptTab(tabID)
  },

  sendPrompt: (tabID, text) => {
    const conn = get().connections[tabID]
    if (conn) {
      conn.sendPrompt(text)
    }
  },

  connectTab: (tabID) => {
    if (get().connections[tabID]) return

    const ws = new TabWebSocket(api.tabWSURL(tabID))

    ws.onMessage((msg: WSServerMessage) => {
      switch (msg.type) {
        case 'history':
          if (msg.messages) {
            set((state) => ({
              messages: { ...state.messages, [tabID]: msg.messages! },
            }))
          }
          break

        case 'message':
          if (msg.data) {
            set((state) => ({
              messages: {
                ...state.messages,
                [tabID]: [...(state.messages[tabID] || []), msg.data!],
              },
            }))
          }
          break

        case 'state_changed':
          set((state) => ({
            tabs: state.tabs.map((t) =>
              t.id === tabID ? { ...t, state: msg.state! } : t
            ),
          }))
          break

        case 'closed':
          set((state) => ({
            tabs: state.tabs.map((t) =>
              t.id === tabID ? { ...t, state: 'closed' } : t
            ),
          }))
          break
      }
    })

    ws.connect()
    set((state) => ({
      connections: { ...state.connections, [tabID]: ws },
    }))
  },

  disconnectTab: (tabID) => {
    const conn = get().connections[tabID]
    if (conn) {
      conn.close()
      set((state) => {
        const { [tabID]: _, ...rest } = state.connections
        return { connections: rest }
      })
    }
  },
}))
