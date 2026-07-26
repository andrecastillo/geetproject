import { useCallback, useEffect, useState } from 'react'
import { Link, Outlet, useLocation, useNavigate, useOutletContext } from 'react-router-dom'
import { ALL_SCOPE, api, type Label, type Project, type Status } from './api'
import NewProjectDialog from './components/NewProjectDialog'
import Sidebar from './components/Sidebar'

export interface AppCtx {
  projects: Project[]
  statuses: Status[]
  labels: Label[]
  scope: string
  reloadProjects: () => Promise<void>
  reloadLabels: () => Promise<void>
  fail: (err: unknown) => void
}

export function useApp() {
  return useOutletContext<AppCtx>()
}

/** Remembering the last scope makes a bare visit to "/" land where you left off. */
export const LAST_SCOPE_KEY = 'geet.lastScope'

export default function App() {
  const [projects, setProjects] = useState<Project[]>([])
  const [statuses, setStatuses] = useState<Status[]>([])
  const [labels, setLabels] = useState<Label[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [creatingProject, setCreatingProject] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()

  // Routes are /p/:scope/b/:view/..., so the scope is the second segment.
  const scope = location.pathname.split('/')[2] ?? ALL_SCOPE

  const fail = useCallback((err: unknown) => {
    setError(err instanceof Error ? err.message : String(err))
  }, [])

  const reloadProjects = useCallback(async () => {
    setProjects(await api.projects())
  }, [])

  const reloadLabels = useCallback(async () => {
    setLabels(await api.labels())
  }, [])

  useEffect(() => {
    Promise.all([api.projects(), api.statuses(), api.labels()])
      .then(([p, s, l]) => {
        setProjects(p)
        setStatuses(s)
        setLabels(l)
      })
      .catch(fail)
      .finally(() => setLoading(false))
  }, [fail])

  useEffect(() => {
    if (scope) localStorage.setItem(LAST_SCOPE_KEY, scope)
  }, [scope])

  const ctx: AppCtx = {
    projects,
    statuses,
    labels,
    scope,
    reloadProjects,
    reloadLabels,
    fail,
  }

  return (
    <div className="app">
      <div className="topbar">
        <Link to="/" className="brand">
          geet
        </Link>
        <span className="spacer" />
      </div>

      <div className="body">
        <Sidebar
          projects={projects}
          currentScope={scope}
          onNewProject={() => setCreatingProject(true)}
        />

        <main className="content">
          {error && (
            <div className="error-banner">
              <span style={{ flex: 1 }}>{error}</span>
              <button className="ghost" onClick={() => setError('')}>
                Dismiss
              </button>
            </div>
          )}
          {loading ? <div className="center-note">Loading…</div> : <Outlet context={ctx} />}
        </main>
      </div>

      {creatingProject && (
        <NewProjectDialog
          existing={projects}
          onClose={() => setCreatingProject(false)}
          onCreated={async (p) => {
            setCreatingProject(false)
            await reloadProjects()
            navigate(`/p/${p.slug}`)
          }}
          onError={fail}
        />
      )}
    </div>
  )
}
