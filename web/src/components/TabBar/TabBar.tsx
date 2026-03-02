import { useTabs } from '../../stores/useTabs'
import type { TabInfo } from '../../api/types'

export function TabBar() {
  const { tabs, activeTabID, setActiveTab, closeTab } = useTabs()

  if (tabs.length === 0) return null

  return (
    <div className="flex items-center border-b border-medusa-border bg-medusa-surface overflow-x-auto">
      {tabs.map((tab) => (
        <TabItem
          key={tab.id}
          tab={tab}
          isActive={tab.id === activeTabID}
          onSelect={() => setActiveTab(tab.id)}
          onClose={() => closeTab(tab.id)}
        />
      ))}
    </div>
  )
}

function TabItem({
  tab,
  isActive,
  onSelect,
  onClose,
}: {
  tab: TabInfo
  isActive: boolean
  onSelect: () => void
  onClose: () => void
}) {
  const stateColor = {
    starting: 'text-medusa-yellow',
    running: 'text-medusa-green',
    idle: 'text-medusa-accent',
    stopped: 'text-medusa-muted',
    error: 'text-medusa-red',
    closed: 'text-medusa-muted',
  }[tab.state]

  return (
    <div
      className={`flex items-center gap-2 px-3 py-2 text-sm cursor-pointer border-b-2 transition-colors whitespace-nowrap ${
        isActive
          ? 'border-medusa-accent text-medusa-text bg-medusa-bg'
          : 'border-transparent text-medusa-muted hover:text-medusa-text hover:bg-medusa-bg/50'
      }`}
      onClick={onSelect}
    >
      <span className={`w-2 h-2 rounded-full ${stateColor} bg-current`} />
      <span>
        {tab.assistant || 'claude'}
        {tab.turn_count ? ` (${tab.turn_count})` : ''}
      </span>
      {tab.total_cost_usd ? (
        <span className="text-xs text-medusa-muted">${tab.total_cost_usd.toFixed(2)}</span>
      ) : null}
      <button
        className="ml-1 text-medusa-muted hover:text-medusa-red transition-colors"
        onClick={(e) => {
          e.stopPropagation()
          onClose()
        }}
      >
        x
      </button>
    </div>
  )
}
