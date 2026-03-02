import { useState } from 'react'

const toolIcons: Record<string, string> = {
  Read: 'file',
  Edit: 'edit',
  Write: 'file-plus',
  Bash: 'terminal',
  Glob: 'search',
  Grep: 'search',
  WebFetch: 'globe',
  WebSearch: 'globe',
}

export function ToolCallCard({ name, input }: { name: string; input: Record<string, unknown> }) {
  const [expanded, setExpanded] = useState(false)
  const icon = toolIcons[name] || 'tool'

  // Display-friendly summary
  const summary = toolSummary(name, input)

  return (
    <div className="border border-medusa-border rounded bg-medusa-bg/50 text-sm">
      <button
        className="w-full flex items-center gap-2 px-3 py-2 hover:bg-medusa-surface/50 transition-colors text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <span className="text-medusa-accent text-xs font-mono">[{icon}]</span>
        <span className="font-medium text-medusa-text">{name}</span>
        <span className="flex-1 text-medusa-muted truncate text-xs">{summary}</span>
        <span className="text-medusa-muted text-xs">{expanded ? '-' : '+'}</span>
      </button>
      {expanded && (
        <div className="px-3 pb-2 border-t border-medusa-border">
          <pre className="text-xs text-medusa-muted overflow-x-auto whitespace-pre-wrap mt-2">
            {JSON.stringify(input, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}

function toolSummary(name: string, input: Record<string, unknown>): string {
  switch (name) {
    case 'Read':
      return String(input.file_path || '')
    case 'Edit':
      return String(input.file_path || '')
    case 'Write':
      return String(input.file_path || '')
    case 'Bash':
      return String(input.command || '').slice(0, 80)
    case 'Glob':
      return String(input.pattern || '')
    case 'Grep':
      return String(input.pattern || '')
    case 'WebSearch':
      return String(input.query || '')
    case 'WebFetch':
      return String(input.url || '')
    default:
      return ''
  }
}
