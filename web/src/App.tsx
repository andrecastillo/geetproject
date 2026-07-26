import { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useOutletContext } from 'react-router-dom'
import { api, type Board, type Label, type Status } from './api'
import BoardSettings from './components/BoardSettings'

export interface AppCtx {
  boards: Board[]
  statuses: Status[]
  labels: Label[]
  reloadBoards: () => Promise<void>
  reloadLabels: () => Promise<void>
  fail: (err: unknown) => void
}

export function useApp() {
  return useOutletContext<AppCtx>()
}

export default function App() {
  const [boards, setBoards] = useState<Board[]>([])
  const [statuses, setStatuses] = useState<Status[]>([])
  const [labels, setLabels] = useState<Label[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const location = useLocation()

  const fail = useCallback((err: unknown) => {
    setError(err instanceof Error ? err.message : String(err))
  }, [])

  const reloadBoards = useCallback(async () => {
    setBoards(await api.boards())
  }, [])

  const reloadLabels = useCallback(async () => {
    setLabels(await api.labels())
  }, [])

  useEffect(() => {
    Promise.all([api.boards(), api.statuses(), api.labels()])
      .then(([b, s, l]) => {
        setBoards(b)
        setStatuses(s)
        setLabels(l)
      })
      .catch(fail)
      .finally(() => setLoading(false))
  }, [fail])

  const ctx: AppCtx = { boards, statuses, labels, reloadBoards, reloadLabels, fail }
  const currentSlug = location.pathname.split('/')[2] ?? ''

  return (
    <div className="app">
      <div className="topbar">
        <Link to="/" className="brand">
          geet
        </Link>
        <nav className="board-tabs">
          {boards.map((b) => (
            <NavLink
              key={b.slug}
              to={`/b/${b.slug}`}
              className={({ isActive }) => 'board-tab' + (isActive ? ' active' : '')}
            >
              {b.name}
            </NavLink>
          ))}
        </nav>
        <span className="spacer" />
        <button className="ghost" onClick={() => setSettingsOpen(true)}>
          Boards &amp; labels
        </button>
      </div>

      {error && (
        <div className="error-banner">
          <span style={{ flex: 1 }}>{error}</span>
          <button className="ghost" onClick={() => setError('')}>
            Dismiss
          </button>
        </div>
      )}

      {loading ? <div className="center-note">Loading…</div> : <Outlet context={ctx} />}

      {settingsOpen && (
        <BoardSettings ctx={ctx} currentSlug={currentSlug} onClose={() => setSettingsOpen(false)} />
      )}
    </div>
  )
}
