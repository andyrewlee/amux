import { useRef, useEffect, useState, useCallback } from 'react'
import { useTabs } from '../../stores/useTabs'
import { MessageBubble } from './MessageBubble'
import { InputBar } from './InputBar'
import { StatusBar } from './StatusBar'

export function ConversationView({ tabID }: { tabID: string }) {
  const { messages, tabs, sendPrompt, interruptTab } = useTabs()
  const tabMessages = messages[tabID] || []
  const tab = tabs.find((t) => t.id === tabID)
  const scrollRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [tabMessages.length, autoScroll])

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 100)
  }, [])

  const handleSend = useCallback(
    (text: string) => {
      sendPrompt(tabID, text)
    },
    [tabID, sendPrompt],
  )

  const handleInterrupt = useCallback(() => {
    interruptTab(tabID)
  }, [tabID, interruptTab])

  // Group consecutive stream events into the most recent assistant message
  const displayMessages = tabMessages.filter(
    (m) => m.type === 'assistant' || m.type === 'user' || m.type === 'result' || m.type === 'system',
  )

  return (
    <div className="h-full flex flex-col">
      {/* Messages */}
      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-y-auto p-4 space-y-3">
        {displayMessages.length === 0 && (
          <div className="text-medusa-muted text-center py-8">
            Waiting for messages...
          </div>
        )}
        {displayMessages.map((msg, i) => (
          <MessageBubble key={msg.uuid || `msg-${i}`} message={msg} />
        ))}
      </div>

      {/* Status bar */}
      {tab && <StatusBar tab={tab} />}

      {/* Input */}
      <InputBar
        onSend={handleSend}
        onInterrupt={handleInterrupt}
        isRunning={tab?.state === 'running'}
        disabled={tab?.state === 'closed' || tab?.state === 'error'}
      />
    </div>
  )
}
