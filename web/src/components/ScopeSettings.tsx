import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '../App'
import { ALL_SCOPE, api, type Board } from '../api'
import LabelPicker from './LabelPicker'

interface Props {
  scope: string
  views: Board[]
  onClose: () => void
  onViewsChanged: () => void | Promise<void>
}

export default function ScopeSettings({ scope, views, onClose, onViewsChanged }: Props) {
  const { projects, statuses, labels, reloadProjects, reloadLabels, fail } = useApp()
  const navigate = useNavigate()

  const isAll = scope === ALL_SCOPE
  const project = projects.find((p) => p.slug === scope)

  const [selected, setSelected] = useState(views[0]?.slug ?? '')
  const [columns, setColumns] = useState<number[]>([])
  const [newViewName, setNewViewName] = useState('')
  const [newViewType, setNewViewType] = useState('any')
  const [newLabelName, setNewLabelName] = useState('')

  const view = views.find((v) => v.slug === selected)

  useEffect(() => {
    if (!selected) return
    api
      .board(scope, selected)
      .then((v) => setColumns(v.columns.map((c) => c.status.id)))
      .catch(fail)
  }, [scope, selected, fail])

  const toggleColumn = async (id: number) => {
    const next = columns.includes(id) ? columns.filter((x) => x !== id) : [...columns, id]
    // Keep columns in the statuses' own order rather than click order, so a
    // board doesn't end up with Done sitting before Todo.
    const ordered = statuses.filter((s) => next.includes(s.id)).map((s) => s.id)
    setColumns(ordered)
    try {
      await api.setColumns(scope, selected, ordered)
      await onViewsChanged()
    } catch (e) {
      fail(e)
    }
  }

  const patchView = async (patch: Parameters<typeof api.updateBoard>[2]) => {
    try {
      await api.updateBoard(scope, selected, patch)
      await onViewsChanged()
    } catch (e) {
      fail(e)
    }
  }

  return (
    <div className="backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>{isAll ? 'All projects' : (project?.name ?? scope)}</h2>
          <span className="spacer" style={{ flex: 1 }} />
          <button className="ghost" onClick={onClose}>
            ✕
          </button>
        </div>

        {/* ---- project ---- */}
        {project && (
          <>
            <div className="section-title">Project</div>
            <div className="row" style={{ marginBottom: 10 }}>
              <div className="field" style={{ flex: 2, marginBottom: 0 }}>
                <label>Name</label>
                <input
                  type="text"
                  defaultValue={project.name}
                  key={project.slug}
                  onBlur={async (e) => {
                    const name = e.target.value.trim()
                    if (!name || name === project.name) return
                    try {
                      await api.updateProject(project.slug, { name })
                      await reloadProjects()
                    } catch (err) {
                      fail(err)
                    }
                  }}
                />
              </div>
              <div className="field" style={{ flex: 1, marginBottom: 0 }}>
                <label>Prefix</label>
                <input
                  type="text"
                  defaultValue={project.prefix}
                  key={project.slug + '-prefix'}
                  maxLength={6}
                  onBlur={async (e) => {
                    const prefix = e.target.value.trim().toUpperCase()
                    if (!prefix || prefix === project.prefix) return
                    try {
                      await api.updateProject(project.slug, { prefix })
                      await reloadProjects()
                    } catch (err) {
                      fail(err)
                    }
                  }}
                />
              </div>
            </div>
            <p className="muted" style={{ fontSize: 12, marginTop: 0 }}>
              Changing the prefix only affects new tickets. Existing keys keep theirs, so
              references already written down still resolve.
            </p>

            <button
              className="danger"
              onClick={async () => {
                const msg =
                  project.ticket_count > 0
                    ? `Delete the project "${project.name}"?\n\nThis will also delete ${project.ticket_count} ticket${
                        project.ticket_count === 1 ? '' : 's'
                      } and their comments. This cannot be undone.`
                    : `Delete the empty project "${project.name}"?`
                if (!window.confirm(msg)) return
                try {
                  await api.deleteProject(project.slug)
                  await reloadProjects()
                  onClose()
                  navigate(`/p/${ALL_SCOPE}`)
                } catch (err) {
                  fail(err)
                }
              }}
            >
              Delete project
              {project.ticket_count > 0 && ` and its ${project.ticket_count} tickets`}
            </button>
          </>
        )}

        {/* ---- views ---- */}
        <div className="section-title">Views in this {isAll ? 'scope' : 'project'}</div>
        <div className="field">
          <select value={selected} onChange={(e) => setSelected(e.target.value)}>
            {views.map((v) => (
              <option key={v.slug} value={v.slug}>
                {v.name}
              </option>
            ))}
          </select>
        </div>

        {view && (
          <>
            <div className="field">
              <label>Name</label>
              <input
                type="text"
                defaultValue={view.name}
                key={view.slug}
                onBlur={(e) => {
                  const name = e.target.value.trim()
                  if (name && name !== view.name) void patchView({ name })
                }}
              />
            </div>

            <div className="field">
              <label>Shows which ticket types</label>
              <select
                value={view.filter_type}
                onChange={(e) => void patchView({ filter_type: e.target.value })}
              >
                <option value="any">Everything</option>
                <option value="epic">Epics only</option>
                <option value="task">Tasks only</option>
                <option value="subtask">Sub-tasks only</option>
              </select>
            </div>

            <div className="field">
              <label>Only tickets with these labels (none selected means no label filter)</label>
              <LabelPicker
                labels={labels}
                selected={view.filter_labels.map((l) => l.id)}
                onChange={(ids) => void patchView({ filter_label_ids: ids })}
              />
            </div>

            <div className="field">
              <label>Columns</label>
              <div className="chips">
                {statuses.map((s) => {
                  const on = columns.includes(s.id)
                  return (
                    <button
                      key={s.id}
                      className="chip"
                      onClick={() => void toggleColumn(s.id)}
                      style={{
                        cursor: 'pointer',
                        borderColor: on ? s.color : 'var(--border)',
                        color: on ? s.color : 'var(--text-faint)',
                      }}
                    >
                      {on ? '✓ ' : ''}
                      {s.name}
                    </button>
                  )
                })}
              </div>
            </div>

            {views.length > 1 && (
              <button
                className="danger"
                onClick={async () => {
                  if (!window.confirm(`Delete the view "${view.name}"? Tickets are not deleted.`))
                    return
                  try {
                    await api.deleteBoard(scope, view.slug)
                    setSelected(views.find((v) => v.slug !== view.slug)?.slug ?? '')
                    await onViewsChanged()
                  } catch (err) {
                    fail(err)
                  }
                }}
              >
                Delete this view
              </button>
            )}
          </>
        )}

        <div className="section-title">Add a view</div>
        <div className="row">
          <input
            type="text"
            placeholder="View name"
            value={newViewName}
            onChange={(e) => setNewViewName(e.target.value)}
          />
          <select
            value={newViewType}
            onChange={(e) => setNewViewType(e.target.value)}
            style={{ width: 160 }}
          >
            <option value="any">Everything</option>
            <option value="epic">Epics only</option>
            <option value="task">Tasks only</option>
            <option value="subtask">Sub-tasks only</option>
          </select>
          <button
            disabled={!newViewName.trim()}
            onClick={async () => {
              try {
                const b = await api.createBoard(scope, {
                  name: newViewName.trim(),
                  filter_type: newViewType,
                })
                setNewViewName('')
                await onViewsChanged()
                setSelected(b.slug)
              } catch (err) {
                fail(err)
              }
            }}
          >
            Add
          </button>
        </div>

        {/* ---- labels ---- */}
        <div className="section-title">Labels (shared by every project)</div>
        <div className="chips">
          {labels.map((l) => (
            <span key={l.id} className="chip" style={{ borderColor: l.color, color: l.color }}>
              {l.name}
              <button
                className="ghost"
                style={{ padding: '0 4px', marginLeft: 4, lineHeight: 1 }}
                title="Delete label"
                onClick={async () => {
                  if (!window.confirm(`Delete the label "${l.name}"?`)) return
                  try {
                    await api.deleteLabel(l.id)
                    await reloadLabels()
                    await onViewsChanged()
                  } catch (err) {
                    fail(err)
                  }
                }}
              >
                ×
              </button>
            </span>
          ))}
          {labels.length === 0 && <span className="muted">No labels yet.</span>}
        </div>
        <div className="row" style={{ marginTop: 8 }}>
          <input
            type="text"
            placeholder="New label"
            value={newLabelName}
            onChange={(e) => setNewLabelName(e.target.value)}
          />
          <button
            disabled={!newLabelName.trim()}
            onClick={async () => {
              try {
                const palette = ['#f87171', '#fbbf24', '#4ade80', '#60a5fa', '#c4a2fc', '#7fd1b9']
                await api.createLabel(newLabelName.trim(), palette[labels.length % palette.length])
                setNewLabelName('')
                await reloadLabels()
              } catch (err) {
                fail(err)
              }
            }}
          >
            Add
          </button>
        </div>
      </div>
    </div>
  )
}
