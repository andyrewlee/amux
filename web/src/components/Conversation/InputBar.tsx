import { useState, useRef, useCallback } from 'react'

interface InputBarProps {
  onSend: (text: string) => void
  onInterrupt: () => void
  isRunning?: boolean
  disabled?: boolean
}

export function InputBar({ onSend, onInterrupt, isRunning, disabled }: InputBarProps) {
  const [text, setText] = useState('')
  const inputRef = useRef<HTMLTextAreaElement>(null)

  const handleSubmit = useCallback(() => {
    const trimmed = text.trim()
    if (!trimmed) return
    onSend(trimmed)
    setText('')
    inputRef.current?.focus()
  }, [text, onSend])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        handleSubmit()
      }
    },
    [handleSubmit],
  )

  return (
    <div className="border-t border-medusa-border bg-medusa-surface p-3">
      <div className="flex items-end gap-2">
        <textarea
          ref={inputRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={disabled ? 'Session ended' : 'Type a message... (Enter to send, Shift+Enter for newline)'}
          disabled={disabled}
          className="flex-1 bg-medusa-bg border border-medusa-border rounded-lg px-3 py-2 text-sm text-medusa-text placeholder-medusa-muted resize-none focus:outline-none focus:border-medusa-accent min-h-[40px] max-h-[200px]"
          rows={1}
        />
        {isRunning ? (
          <button
            onClick={onInterrupt}
            className="px-4 py-2 bg-medusa-red text-white rounded-lg text-sm font-medium hover:opacity-90 transition-opacity"
          >
            Stop
          </button>
        ) : (
          <button
            onClick={handleSubmit}
            disabled={disabled || !text.trim()}
            className="px-4 py-2 bg-medusa-accent text-white rounded-lg text-sm font-medium hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Send
          </button>
        )}
      </div>
    </div>
  )
}
