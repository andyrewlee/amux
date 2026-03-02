import type { Project, Workspace, TabInfo, SDKMessage, GitStatus, ProjectGroup } from './types'

class APIClient {
  private baseURL: string
  private token: string

  constructor(baseURL: string = '', token: string = '') {
    this.baseURL = baseURL
    this.token = token || new URLSearchParams(window.location.search).get('token') || ''
  }

  setToken(token: string) {
    this.token = token
  }

  private async request<T>(path: string, options?: RequestInit): Promise<T> {
    const res = await fetch(`${this.baseURL}${path}`, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.token}`,
        ...options?.headers,
      },
    })
    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }))
      throw new Error(body.error || res.statusText)
    }
    return res.json()
  }

  // Projects
  async listProjects(): Promise<Project[]> {
    return this.request('/api/v1/projects')
  }

  async addProject(path: string): Promise<void> {
    await this.request('/api/v1/projects', {
      method: 'POST',
      body: JSON.stringify({ path }),
    })
  }

  async removeProject(path: string): Promise<void> {
    await this.request(`/api/v1/projects/${encodeURIComponent(path.slice(1))}`, {
      method: 'DELETE',
    })
  }

  async setProjectProfile(path: string, profile: string): Promise<void> {
    await this.request(`/api/v1/projects/${encodeURIComponent(path.slice(1))}/profile`, {
      method: 'PUT',
      body: JSON.stringify({ profile }),
    })
  }

  async rescanWorkspaces(): Promise<void> {
    await this.request('/api/v1/projects/rescan', { method: 'POST' })
  }

  // Workspaces
  async getWorkspace(wsID: string): Promise<Workspace> {
    return this.request(`/api/v1/workspaces/${wsID}`)
  }

  async createWorkspace(opts: {
    project_path: string
    name: string
    branch_mode?: string
    custom_branch?: string
  }): Promise<Workspace> {
    return this.request('/api/v1/workspaces', {
      method: 'POST',
      body: JSON.stringify(opts),
    })
  }

  async deleteWorkspace(wsID: string): Promise<void> {
    await this.request(`/api/v1/workspaces/${wsID}`, { method: 'DELETE' })
  }

  async renameWorkspace(wsID: string, name: string): Promise<void> {
    await this.request(`/api/v1/workspaces/${wsID}/name`, {
      method: 'PUT',
      body: JSON.stringify({ name }),
    })
  }

  async getGitStatus(wsID: string): Promise<GitStatus> {
    return this.request(`/api/v1/workspaces/${wsID}/git/status`)
  }

  // Tabs
  async listTabs(wsID: string): Promise<TabInfo[]> {
    return this.request(`/api/v1/workspaces/${wsID}/tabs`)
  }

  async launchTab(wsID: string, opts: {
    assistant?: string
    prompt?: string
    skip_permissions?: boolean
  }): Promise<{ tab_id: string }> {
    return this.request(`/api/v1/workspaces/${wsID}/tabs`, {
      method: 'POST',
      body: JSON.stringify(opts),
    })
  }

  async closeTab(tabID: string): Promise<void> {
    await this.request(`/api/v1/tabs/${tabID}`, { method: 'DELETE' })
  }

  async resumeTab(tabID: string): Promise<void> {
    await this.request(`/api/v1/tabs/${tabID}/resume`, { method: 'POST' })
  }

  async interruptTab(tabID: string): Promise<void> {
    await this.request(`/api/v1/tabs/${tabID}/interrupt`, { method: 'POST' })
  }

  async sendPrompt(tabID: string, text: string): Promise<void> {
    await this.request(`/api/v1/tabs/${tabID}/prompt`, {
      method: 'POST',
      body: JSON.stringify({ text }),
    })
  }

  async getTabHistory(tabID: string, since?: string): Promise<SDKMessage[]> {
    const params = since ? `?since=${since}` : ''
    return this.request(`/api/v1/tabs/${tabID}/history${params}`)
  }

  async getTabState(tabID: string): Promise<TabInfo> {
    return this.request(`/api/v1/tabs/${tabID}/state`)
  }

  // Config
  async getConfig(): Promise<Record<string, unknown>> {
    return this.request('/api/v1/config')
  }

  async listProfiles(): Promise<string[]> {
    return this.request('/api/v1/profiles')
  }

  async createProfile(name: string): Promise<void> {
    await this.request('/api/v1/profiles', {
      method: 'POST',
      body: JSON.stringify({ name }),
    })
  }

  async deleteProfile(name: string): Promise<void> {
    await this.request(`/api/v1/profiles/${name}`, { method: 'DELETE' })
  }

  // Groups
  async listGroups(): Promise<ProjectGroup[]> {
    return this.request('/api/v1/groups')
  }

  async createGroup(name: string, repos: string[], profile?: string): Promise<void> {
    await this.request('/api/v1/groups', {
      method: 'POST',
      body: JSON.stringify({ name, repos, profile }),
    })
  }

  async deleteGroup(name: string): Promise<void> {
    await this.request(`/api/v1/groups/${name}`, { method: 'DELETE' })
  }

  // WebSocket URL helpers
  tabWSURL(tabID: string): string {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = this.baseURL || window.location.host
    return `${proto}//${host}/api/v1/tabs/${tabID}/ws?token=${this.token}`
  }

  tabPTYWSURL(tabID: string): string {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = this.baseURL || window.location.host
    return `${proto}//${host}/api/v1/tabs/${tabID}/pty?token=${this.token}`
  }

  sseURL(): string {
    return `${this.baseURL}/api/v1/events?token=${this.token}`
  }
}

export const api = new APIClient()
export default api
