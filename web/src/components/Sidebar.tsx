import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { ALL_SCOPE, type Project } from '../api'

interface Props {
  projects: Project[]
  currentScope: string
  onNewProject: () => void
}

export default function Sidebar({ projects, currentScope, onNewProject }: Props) {
  const [collapsed, setCollapsed] = useState(false)

  if (collapsed) {
    return (
      <aside className="sidebar collapsed">
        <button className="ghost" title="Show projects" onClick={() => setCollapsed(false)}>
          »
        </button>
      </aside>
    )
  }

  return (
    <aside className="sidebar">
      <div className="sidebar-head">
        <span>Projects</span>
        <button className="ghost" title="Hide projects" onClick={() => setCollapsed(true)}>
          «
        </button>
      </div>

      <nav className="project-list">
        {/* Pinned: everything, across every project. */}
        <NavLink
          to={`/p/${ALL_SCOPE}`}
          className={() => 'project-item' + (currentScope === ALL_SCOPE ? ' active' : '')}
        >
          <span className="dot all-dot" />
          <span className="p-name">All projects</span>
        </NavLink>

        <div className="sidebar-rule" />

        {projects.map((p) => (
          <NavLink
            key={p.slug}
            to={`/p/${p.slug}`}
            className={() => 'project-item' + (currentScope === p.slug ? ' active' : '')}
          >
            <span className="dot" style={{ background: p.color || '#6b7383' }} />
            <span className="p-name" title={p.name}>
              {p.name}
            </span>
            <span className="p-prefix">{p.prefix}</span>
          </NavLink>
        ))}

        {projects.length === 0 && (
          <p className="sidebar-empty">
            No projects yet. Tickets live in a project, so make one to get started.
          </p>
        )}
      </nav>

      <button className="new-project" onClick={onNewProject}>
        + New project
      </button>
    </aside>
  )
}
