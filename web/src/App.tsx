import { useEffect, useState } from 'react'
import { useConnection } from './stores/useConnection'
import { useProjects } from './stores/useProjects'
import { AppShell } from './components/Layout/AppShell'

export default function App() {
  const { token, setToken, connect, connected } = useConnection()
  const { fetch: fetchProjects } = useProjects()
  const [tokenInput, setTokenInput] = useState('')

  useEffect(() => {
    if (token) {
      connect()
      fetchProjects()
    }
  }, [token, connect, fetchProjects])

  if (!token || !connected) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-medusa-bg">
        <div className="bg-medusa-surface border border-medusa-border rounded-lg p-8 max-w-md w-full">
          <h1 className="text-2xl font-bold text-medusa-text mb-2">Medusa</h1>
          <p className="text-medusa-muted mb-6">Enter your server token to connect.</p>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              setToken(tokenInput)
            }}
          >
            <input
              type="text"
              value={tokenInput}
              onChange={(e) => setTokenInput(e.target.value)}
              placeholder="mds_..."
              className="w-full bg-medusa-bg border border-medusa-border rounded px-3 py-2 text-medusa-text placeholder-medusa-muted mb-4 focus:outline-none focus:border-medusa-accent"
              autoFocus
            />
            <button
              type="submit"
              className="w-full bg-medusa-accent text-white rounded px-4 py-2 font-medium hover:opacity-90 transition-opacity"
            >
              Connect
            </button>
          </form>
        </div>
      </div>
    )
  }

  return <AppShell />
}
