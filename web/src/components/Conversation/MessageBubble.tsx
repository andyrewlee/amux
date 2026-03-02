import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { SDKMessage, ContentBlock } from '../../api/types'
import { ToolCallCard } from './ToolCallCard'

export function MessageBubble({ message }: { message: SDKMessage }) {
  if (message.type === 'system') {
    return (
      <div className="text-xs text-medusa-muted px-3 py-1 bg-medusa-surface/50 rounded">
        Session: {message.session_id || 'initializing...'}
      </div>
    )
  }

  if (message.type === 'result') {
    return (
      <div className="text-xs px-3 py-2 bg-medusa-surface border border-medusa-border rounded">
        <span className="text-medusa-green font-medium">Result: </span>
        <span className="text-medusa-muted">{message.result || message.subtype}</span>
        {message.total_cost_usd !== undefined && (
          <span className="ml-2 text-medusa-yellow">${message.total_cost_usd.toFixed(4)}</span>
        )}
      </div>
    )
  }

  const isAssistant = message.type === 'assistant'
  const content = message.message?.content || []

  return (
    <div className={`flex ${isAssistant ? 'justify-start' : 'justify-end'}`}>
      <div
        className={`max-w-[85%] rounded-lg px-4 py-3 ${
          isAssistant
            ? 'bg-medusa-surface border border-medusa-border'
            : 'bg-medusa-accent/20 border border-medusa-accent/30'
        }`}
      >
        <div className="text-xs text-medusa-muted mb-1 font-medium">
          {isAssistant ? 'Claude' : 'You'}
        </div>
        <div className="space-y-2">
          {content.map((block, i) => (
            <ContentBlockView key={i} block={block} />
          ))}
          {typeof message.message?.content === 'string' && (
            <div className="prose prose-invert prose-sm max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {message.message.content}
              </ReactMarkdown>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function ContentBlockView({ block }: { block: ContentBlock }) {
  if (block.type === 'text' && block.text) {
    return (
      <div className="prose prose-invert prose-sm max-w-none">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{block.text}</ReactMarkdown>
      </div>
    )
  }

  if (block.type === 'tool_use') {
    return <ToolCallCard name={block.name || ''} input={block.input || {}} />
  }

  if (block.type === 'tool_result') {
    const resultContent =
      typeof block.content === 'string'
        ? block.content
        : Array.isArray(block.content)
          ? block.content.map((b) => b.text || '').join('\n')
          : ''

    if (!resultContent) return null

    return (
      <details className="text-xs">
        <summary className="cursor-pointer text-medusa-muted hover:text-medusa-text">
          Tool result
        </summary>
        <pre className="mt-1 p-2 bg-medusa-bg rounded overflow-x-auto text-medusa-muted whitespace-pre-wrap">
          {resultContent.slice(0, 2000)}
          {resultContent.length > 2000 && '...'}
        </pre>
      </details>
    )
  }

  return null
}
