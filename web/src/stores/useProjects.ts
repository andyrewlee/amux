import { create } from 'zustand'
import type { Project } from '../api/types'
import api from '../api/client'

interface ProjectsState {
  projects: Project[]
  loading: boolean
  error: string | null
  selectedProjectPath: string | null
  selectedWorkspaceID: string | null
  fetch: () => Promise<void>
  selectProject: (path: string | null) => void
  selectWorkspace: (id: string | null) => void
  addProject: (path: string) => Promise<void>
  removeProject: (path: string) => Promise<void>
}

export const useProjects = create<ProjectsState>((set, get) => ({
  projects: [],
  loading: false,
  error: null,
  selectedProjectPath: null,
  selectedWorkspaceID: null,

  fetch: async () => {
    set({ loading: true, error: null })
    try {
      const projects = await api.listProjects()
      set({ projects, loading: false })
    } catch (e) {
      set({ error: (e as Error).message, loading: false })
    }
  },

  selectProject: (path) => set({ selectedProjectPath: path, selectedWorkspaceID: null }),
  selectWorkspace: (id) => set({ selectedWorkspaceID: id }),

  addProject: async (path) => {
    await api.addProject(path)
    await get().fetch()
  },

  removeProject: async (path) => {
    await api.removeProject(path)
    await get().fetch()
  },
}))
