import { ProjectList } from '../Dashboard/ProjectList'
import { TabBar } from '../TabBar/TabBar'
import { ConversationView } from '../Conversation/ConversationView'
import { WorkspaceInfo } from '../Sidebar/WorkspaceInfo'
import { useTabs } from '../../stores/useTabs'

export function AppShell() {
  const { activeTabID } = useTabs()

  return (
    <div className="h-screen flex flex-col bg-medusa-bg">
      {/* Tab bar */}
      <TabBar />

      {/* Main content */}
      <div className="flex-1 flex overflow-hidden">
        {/* Left sidebar: projects */}
        <aside className="w-64 border-r border-medusa-border overflow-y-auto flex-shrink-0 hidden md:block">
          <ProjectList />
        </aside>

        {/* Center: conversation or empty state */}
        <main className="flex-1 overflow-hidden">
          {activeTabID ? (
            <ConversationView tabID={activeTabID} />
          ) : (
            <div className="h-full flex items-center justify-center text-medusa-muted">
              <div className="text-center">
                <p className="text-lg">No active tab</p>
                <p className="text-sm mt-1">Select a workspace and launch a tab to get started.</p>
              </div>
            </div>
          )}
        </main>

        {/* Right sidebar: workspace info */}
        <aside className="w-72 border-l border-medusa-border overflow-y-auto flex-shrink-0 hidden lg:block">
          <WorkspaceInfo />
        </aside>
      </div>
    </div>
  )
}
