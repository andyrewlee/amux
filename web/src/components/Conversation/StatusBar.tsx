import type { TabInfo } from '../../api/types'

export function StatusBar({ tab }: { tab: TabInfo }) {
  const stateLabel = {
    starting: 'Starting...',
    running: 'Running',
    idle: 'Idle',
    stopped: 'Stopped',
    error: 'Error',
    closed: 'Closed',
  }[tab.state]

  const stateColor = {
    starting: 'text-medusa-yellow',
    running: 'text-medusa-green',
    idle: 'text-medusa-accent',
    stopped: 'text-medusa-muted',
    error: 'text-medusa-red',
    closed: 'text-medusa-muted',
  }[tab.state]

  return (
    <div className="flex items-center gap-4 px-4 py-1.5 bg-medusa-surface border-t border-medusa-border text-xs text-medusa-muted">
      <span className={stateColor}>{stateLabel}</span>
      {tab.model && <span>Model: {tab.model}</span>}
      {tab.turn_count !== undefined && tab.turn_count > 0 && <span>Turns: {tab.turn_count}</span>}
      {tab.total_cost_usd !== undefined && tab.total_cost_usd > 0 && (
        <span className="text-medusa-yellow">Cost: ${tab.total_cost_usd.toFixed(4)}</span>
      )}
      {tab.session_id && (
        <span className="ml-auto font-mono opacity-50">{tab.session_id.slice(0, 8)}</span>
      )}
    </div>
  )
}
