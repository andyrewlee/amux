import { useProjects } from '../../stores/useProjects'
import { useTabs } from '../../stores/useTabs'

export function WorkspaceInfo() {
  const { projects, selectedProjectPath, selectedWorkspaceID } = useProjects()
  const { tabs, activeTabID } = useTabs()

  const project = projects.find((p) => p.path === selectedProjectPath)
  const workspace = project?.workspaces?.find(
    (ws) => `${ws.repo}:${ws.root}` === selectedWorkspaceID,
  )
  const activeTab = tabs.find((t) => t.id === activeTabID)

  if (!project) {
    return (
      <div className="p-4 text-medusa-muted text-sm">
        Select a project to see details.
      </div>
    )
  }

  return (
    <div className="p-4 space-y-4">
      {/* Project info */}
      <section>
        <h3 className="text-xs font-semibold text-medusa-muted uppercase tracking-wider mb-2">
          Project
        </h3>
        <div className="text-sm space-y-1">
          <div className="text-medusa-text font-medium">{project.name}</div>
          <div className="text-medusa-muted text-xs font-mono truncate">{project.path}</div>
          {project.profile && (
            <div className="text-medusa-purple text-xs">Profile: {project.profile}</div>
          )}
        </div>
      </section>

      {/* Workspace info */}
      {workspace && (
        <section>
          <h3 className="text-xs font-semibold text-medusa-muted uppercase tracking-wider mb-2">
            Workspace
          </h3>
          <div className="text-sm space-y-1">
            <div className="text-medusa-text">{workspace.name}</div>
            <div className="text-medusa-muted text-xs">Branch: {workspace.branch}</div>
            {workspace.base && (
              <div className="text-medusa-muted text-xs">Base: {workspace.base}</div>
            )}
            <div className="text-medusa-muted text-xs font-mono truncate">{workspace.root}</div>
            <div className="flex gap-2 mt-1">
              {workspace.allow_edits && (
                <span className="text-xs px-1.5 py-0.5 bg-medusa-green/20 text-medusa-green rounded">
                  edits
                </span>
              )}
              {workspace.isolated && (
                <span className="text-xs px-1.5 py-0.5 bg-medusa-yellow/20 text-medusa-yellow rounded">
                  isolated
                </span>
              )}
              {workspace.skip_permissions && (
                <span className="text-xs px-1.5 py-0.5 bg-medusa-red/20 text-medusa-red rounded">
                  skip-perms
                </span>
              )}
            </div>
          </div>
        </section>
      )}

      {/* Active tab info */}
      {activeTab && (
        <section>
          <h3 className="text-xs font-semibold text-medusa-muted uppercase tracking-wider mb-2">
            Active Tab
          </h3>
          <div className="text-sm space-y-1">
            <div className="text-medusa-text">{activeTab.assistant || 'claude'}</div>
            <div className="text-medusa-muted text-xs">State: {activeTab.state}</div>
            <div className="text-medusa-muted text-xs">Kind: {activeTab.kind}</div>
            {activeTab.session_id && (
              <div className="text-medusa-muted text-xs font-mono">
                Session: {activeTab.session_id.slice(0, 12)}...
              </div>
            )}
            {activeTab.total_cost_usd !== undefined && activeTab.total_cost_usd > 0 && (
              <div className="text-medusa-yellow text-xs">
                Cost: ${activeTab.total_cost_usd.toFixed(4)}
              </div>
            )}
          </div>
        </section>
      )}
    </div>
  )
}
