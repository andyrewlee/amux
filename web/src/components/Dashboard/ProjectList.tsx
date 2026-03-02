import { useProjects } from '../../stores/useProjects'
import { useTabs } from '../../stores/useTabs'
import type { Project, Workspace } from '../../api/types'

export function ProjectList() {
  const { projects, selectedProjectPath, selectedWorkspaceID, selectProject, selectWorkspace, loading } = useProjects()

  if (loading) {
    return <div className="p-4 text-medusa-muted text-sm">Loading projects...</div>
  }

  if (projects.length === 0) {
    return (
      <div className="p-4 text-medusa-muted text-sm">
        No projects registered. Add a git repository from the server.
      </div>
    )
  }

  return (
    <div className="p-2">
      <h2 className="px-2 py-1 text-xs font-semibold text-medusa-muted uppercase tracking-wider">
        Projects
      </h2>
      {projects.map((project) => (
        <ProjectItem
          key={project.path}
          project={project}
          isSelected={project.path === selectedProjectPath}
          selectedWorkspaceID={selectedWorkspaceID}
          onSelectProject={() => selectProject(project.path)}
          onSelectWorkspace={selectWorkspace}
        />
      ))}
    </div>
  )
}

function ProjectItem({
  project,
  isSelected,
  selectedWorkspaceID,
  onSelectProject,
  onSelectWorkspace,
}: {
  project: Project
  isSelected: boolean
  selectedWorkspaceID: string | null
  onSelectProject: () => void
  onSelectWorkspace: (id: string) => void
}) {
  return (
    <div className="mb-1">
      <button
        className={`w-full text-left px-2 py-1.5 rounded text-sm hover:bg-medusa-surface transition-colors ${
          isSelected ? 'bg-medusa-surface text-medusa-text' : 'text-medusa-muted'
        }`}
        onClick={onSelectProject}
      >
        <span className="font-medium">{project.name}</span>
        {project.profile && (
          <span className="ml-2 text-xs text-medusa-purple">({project.profile})</span>
        )}
      </button>
      {isSelected && project.workspaces && (
        <div className="ml-3 mt-0.5">
          {project.workspaces
            .filter((ws) => !ws.archived)
            .map((ws) => (
              <WorkspaceItem
                key={wsID(ws)}
                workspace={ws}
                isSelected={wsID(ws) === selectedWorkspaceID}
                onSelect={() => onSelectWorkspace(wsID(ws))}
              />
            ))}
        </div>
      )}
    </div>
  )
}

function WorkspaceItem({
  workspace,
  isSelected,
  onSelect,
}: {
  workspace: Workspace
  isSelected: boolean
  onSelect: () => void
}) {
  const { launchTab } = useTabs()

  return (
    <div className="flex items-center group">
      <button
        className={`flex-1 text-left px-2 py-1 rounded text-xs hover:bg-medusa-surface transition-colors ${
          isSelected ? 'bg-medusa-surface text-medusa-accent' : 'text-medusa-muted'
        }`}
        onClick={onSelect}
      >
        {workspace.name}
        <span className="ml-1 opacity-60">({workspace.branch})</span>
      </button>
      {isSelected && (
        <button
          className="px-1.5 text-medusa-green opacity-0 group-hover:opacity-100 transition-opacity text-xs"
          onClick={() => launchTab(wsID(workspace), { assistant: 'claude' })}
          title="Launch Claude"
        >
          +
        </button>
      )}
    </div>
  )
}

function wsID(ws: Workspace): string {
  return `${ws.repo}:${ws.root}`
}
