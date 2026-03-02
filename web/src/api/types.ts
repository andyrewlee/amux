// Mirrors the Go service layer types

export type TabState = 'starting' | 'running' | 'idle' | 'stopped' | 'error' | 'closed'
export type TabKind = 'claude' | 'pty'

export interface TabInfo {
  id: string
  workspace_id: string
  kind: TabKind
  assistant: string
  state: TabState
  session_id: string
  created_at: string
  total_cost_usd?: number
  model?: string
  turn_count?: number
}

export interface SDKMessage {
  type: 'system' | 'assistant' | 'user' | 'result' | 'stream_event'
  subtype?: string
  uuid?: string
  session_id?: string
  message?: MessageContent
  event?: StreamEvent
  result?: string
  total_cost_usd?: number
  timestamp?: string
}

export interface MessageContent {
  role?: string
  content?: ContentBlock[]
}

export interface ContentBlock {
  type: string
  text?: string
  name?: string // tool name
  input?: Record<string, unknown>
  tool_use_id?: string
  content?: string | ContentBlock[]
}

export interface StreamEvent {
  type: string
  delta?: {
    type: string
    text?: string
  }
}

export interface Project {
  name: string
  path: string
  profile: string
  workspaces: Workspace[]
}

export interface Workspace {
  name: string
  branch: string
  base: string
  repo: string
  root: string
  profile: string
  archived: boolean
  allow_edits: boolean
  isolated: boolean
  skip_permissions: boolean
}

export interface ProjectGroup {
  name: string
  repos: GroupRepo[]
  profile: string
  workspaces: GroupWorkspace[]
}

export interface GroupRepo {
  path: string
  name: string
}

export interface GroupWorkspace {
  name: string
  group_name: string
}

export interface GitStatus {
  branch: string
  ahead: number
  behind: number
  staged: FileChange[]
  unstaged: FileChange[]
  untracked: string[]
}

export interface FileChange {
  path: string
  status: string
}

// WebSocket messages
export interface WSClientMessage {
  type: 'prompt' | 'interrupt' | 'ping'
  text?: string
}

export interface WSServerMessage {
  type: 'connected' | 'history' | 'message' | 'state_changed' | 'closed' | 'pong' | 'error' | 'cost_update'
  tab_id?: string
  state?: TabState
  session_id?: string
  data?: SDKMessage
  messages?: SDKMessage[]
  total_cost_usd?: number
  reason?: string
  error?: string
}
