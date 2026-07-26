import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { AppCtx } from '../App'
import { api, type Board } from '../api'
import LabelPicker from './LabelPicker'

interface Props {
  ctx: AppCtx
  currentSlug: string
  onClose: () => void
}

export default function BoardSettings({ ctx, currentSlug, onClose }: Props) {
  const { boards, statuses, labels, reloadBoards, reloadLabels, fail } = ctx
  const navigate = useNavigate()

  const [selected, setSelected] = useState(currentSlug || boards[0]?.slug || '')
  const [columns, setColumns] = useState<number[]>([])
  const [newBoardName, setNewBoardName] = useState('')
  const [newBoardType, setNewBoardType] = useState('task')
  const [newLabelName, setNewLabelName] = useState('')

  const board: Board | undefined = boards.find((b) => b.slug === selected)

  useEffect(() => {
    if (!selected) return
    api
      .board(selected)
      .then((v) => setColumns(v.columns.map((c) => c.status.id)))
      .catch(fail)
  }, [selected, fail])

  const toggleColumn = async (id: number) => {
    const next = columns.includes(id) ? columns.filter((x) => x !== id) : [...columns, id]
    // Keep columns in the statuses' own order rather than click order, so the
    // board doesn't end up with Done sitting before Todo.
    const ordered = statuses.filter((s) => next.includes(s.id)).map((s) => s.id)
    setColumns(ordered)
    try {
      await api.setColumns(selected, ordered)
    } catch (e) {
      fail(e)
    }
  }

  return (
    <div className="backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>Boards &amp; labels</h2>
          <span className="spacer" style={{ flex: 1 }} />
          <button className="ghost" onClick={onClose}>
            ✕
          </button>
        </div>

        <div className="field">
          <label>Board</label>
          <select value={selected} onChange={(e) => setSelected(e.target.value)}>
            {boards.map((b) => (
              <option key={b.slug} value={b.slug}>
                {b.name}
              </option>
            ))}
          </select>
        </div>

        {board && (
          <>
            <div className="field">
              <label>Name</label>
              <input
                type="text"
                defaultValue={board.name}
                key={board.slug}
                onBlur={async (e) => {
                  const name = e.target.value.trim()
                  if (!name || name === board.name) return
                  try {
                    await api.updateBoard(board.slug, { name })
                    await reloadBoards()
                  } catch (err) {
                    fail(err)
                  }
                }}
              />
            </div>

            <div className="field">
              <label>Shows which ticket types</label>
              <select
                value={board.filter_type}
                onChange={async (e) => {
                  try {
                    await api.updateBoard(board.slug, { filter_type: e.target.value })
                    await reloadBoards()
                  } catch (err) {
                    fail(err)
                  }
                }}
              >
                <option value="any">Everything</option>
                <option value="epic">Epics only</option>
                <option value="task">Tasks only</option>
                <option value="subtask">Sub-tasks only</option>
              </select>
            </div>

            <div className="field">
              <label>
                Only tickets with these labels (none selected means no label filter)
              </label>
              <LabelPicker
                labels={labels}
                selected={board.filter_labels.map((l) => l.id)}
                onChange={async (ids) => {
                  try {
                    await api.updateBoard(board.slug, { filter_label_ids: ids })
                    await reloadBoards()
                  } catch (err) {
                    fail(err)
                  }
                }}
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

            {boards.length > 1 && (
              <button
                className="danger"
                onClick={async () => {
                  if (!window.confirm(`Delete the board "${board.name}"? Tickets are not deleted.`))
                    return
                  try {
                    await api.deleteBoard(board.slug)
                    await reloadBoards()
                    const next = boards.find((b) => b.slug !== board.slug)
                    setSelected(next?.slug ?? '')
                    if (currentSlug === board.slug && next) navigate(`/b/${next.slug}`)
                  } catch (err) {
                    fail(err)
                  }
                }}
              >
                Delete this board
              </button>
            )}
          </>
        )}

        <div className="section-title">Add a board</div>
        <div className="row">
          <input
            type="text"
            placeholder="Board name"
            value={newBoardName}
            onChange={(e) => setNewBoardName(e.target.value)}
          />
          <select
            value={newBoardType}
            onChange={(e) => setNewBoardType(e.target.value)}
            style={{ width: 160 }}
          >
            <option value="any">Everything</option>
            <option value="epic">Epics only</option>
            <option value="task">Tasks only</option>
            <option value="subtask">Sub-tasks only</option>
          </select>
          <button
            disabled={!newBoardName.trim()}
            onClick={async () => {
              try {
                const b = await api.createBoard({
                  name: newBoardName.trim(),
                  filter_type: newBoardType,
                })
                setNewBoardName('')
                await reloadBoards()
                setSelected(b.slug)
              } catch (err) {
                fail(err)
              }
            }}
          >
            Add
          </button>
        </div>

        <div className="section-title">Labels</div>
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
                    await reloadBoards()
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
                await api.createLabel(
                  newLabelName.trim(),
                  palette[labels.length % palette.length],
                )
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
