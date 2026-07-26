import { useCallback, useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api, type Label, type Project, type Status, type TicketDetail } from '../api'
import LabelPicker from './LabelPicker'
import NewTicketDialog from './NewTicketDialog'
import { StatusSelect } from './TicketCard'

interface Props {
  ticketKey: string
  projects: Project[]
  statuses: Status[]
  labels: Label[]
  onClose: () => void
  onOpen: (key: string) => void
  onChanged: () => void | Promise<void>
  onDelete: (key: string) => void
  onError: (err: unknown) => void
}

export default function TicketModal({
  ticketKey,
  projects,
  statuses,
  labels,
  onClose,
  onOpen,
  onChanged,
  onDelete,
  onError,
}: Props) {
  const [t, setT] = useState<TicketDetail | null>(null)
  const [title, setTitle] = useState('')
  const [editingBody, setEditingBody] = useState(false)
  const [body, setBody] = useState('')
  const [newComment, setNewComment] = useState('')
  const [addingChild, setAddingChild] = useState(false)

  const load = useCallback(async () => {
    try {
      const d = await api.ticket(ticketKey)
      setT(d)
      setTitle(d.title)
      setBody(d.description)
    } catch (e) {
      onError(e)
    }
  }, [ticketKey, onError])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Every save sends only the field that changed. The server leaves everything
  // else untouched, which is what stops an edit here from wiping a description.
  const patch = async (fields: Parameters<typeof api.updateTicket>[1]) => {
    try {
      await api.updateTicket(ticketKey, fields)
      await load()
      await onChanged()
    } catch (e) {
      onError(e)
    }
  }

  if (!t) {
    return (
      <div className="backdrop" onClick={onClose}>
        <div className="modal" onClick={(e) => e.stopPropagation()}>
          <div className="center-note">Loading…</div>
        </div>
      </div>
    )
  }

  const childType = t.type === 'epic' ? 'task' : t.type === 'task' ? 'subtask' : null

  return (
    <div className="backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <span className={`type-badge type-${t.type}`}>{t.type}</span>
          <span className="muted" style={{ fontFamily: 'ui-monospace, monospace' }}>
            {t.key}
          </span>
          {t.parent_key && (
            <span
              className="breadcrumb"
              style={{ cursor: 'pointer' }}
              onClick={() => onOpen(t.parent_key!)}
            >
              ↳ in {t.parent_key} {t.parent_title}
            </span>
          )}
          <span className="spacer" />
          <StatusSelect
            statuses={statuses}
            value={t.status?.slug ?? ''}
            onChange={(slug) => void patch({ status: slug })}
          />
          <button className="ghost" onClick={onClose}>
            ✕
          </button>
        </div>

        <div className="field">
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onBlur={() => title.trim() && title !== t.title && void patch({ title: title.trim() })}
            style={{ fontSize: 18, fontWeight: 600 }}
          />
        </div>

        <div className="field">
          <label>
            Project
            {t.children.length > 0 && ` \u2014 moving this also moves ${t.children.length} child ticket${
              t.children.length === 1 ? '' : 's'
            }`}
          </label>
          <select
            value={t.project_slug}
            disabled={!!t.parent_key}
            title={
              t.parent_key
                ? `A ticket lives in its parent's project. Move ${t.parent_key} instead.`
                : undefined
            }
            onChange={(e) => {
              const target = projects.find((p) => p.slug === e.target.value)
              if (!target) return
              if (
                t.children.length > 0 &&
                !window.confirm(
                  `Move ${t.key} and its ${t.children.length} child ticket${
                    t.children.length === 1 ? '' : 's'
                  } to ${target.name}?`,
                )
              ) {
                return
              }
              void patch({ project: target.slug })
            }}
          >
            {projects.map((p) => (
              <option key={p.slug} value={p.slug}>
                {p.name}
              </option>
            ))}
          </select>
        </div>

        <div className="field">
          <label>Labels</label>
          <LabelPicker
            labels={labels}
            selected={t.labels.map((l) => l.id)}
            onChange={(ids) => void patch({ label_ids: ids })}
          />
        </div>

        <div className="section-title">
          Description
          <button
            className="ghost"
            style={{ float: 'right', marginTop: -4 }}
            onClick={() => {
              if (editingBody && body !== t.description) void patch({ description: body })
              setEditingBody(!editingBody)
            }}
          >
            {editingBody ? 'Save' : 'Edit'}
          </button>
        </div>
        {editingBody ? (
          <textarea rows={10} value={body} onChange={(e) => setBody(e.target.value)} />
        ) : (
          <div className="markdown">
            {t.description.trim() ? (
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{t.description}</ReactMarkdown>
            ) : (
              <span className="muted">No description yet.</span>
            )}
          </div>
        )}

        {childType && (
          <>
            <div className="section-title">
              {t.type === 'epic' ? 'Tickets in this epic' : 'Sub-tasks'} ({t.children.length})
              <button
                className="ghost"
                style={{ float: 'right', marginTop: -4 }}
                onClick={() => setAddingChild(true)}
              >
                + Add
              </button>
            </div>
            {t.children.length === 0 && <div className="muted">Nothing here yet.</div>}
            {t.children.map((c) => (
              <div className="child-row" key={c.key}>
                <span className={`type-badge type-${c.type}`}>{c.type}</span>
                <span className="muted" style={{ fontFamily: 'ui-monospace, monospace' }}>
                  {c.key}
                </span>
                <span className="c-title" onClick={() => onOpen(c.key)}>
                  {c.title}
                </span>
                <StatusSelect
                  statuses={statuses}
                  value={c.status?.slug ?? ''}
                  onChange={async (slug) => {
                    try {
                      await api.updateTicket(c.key, { status: slug })
                      await load()
                      await onChanged()
                    } catch (e) {
                      onError(e)
                    }
                  }}
                />
              </div>
            ))}
          </>
        )}

        <div className="section-title">Comments ({t.comments.length})</div>
        {t.comments.map((c) => (
          <div className="comment" key={c.id}>
            <div className="comment-meta">
              <span>{new Date(c.created_at).toLocaleString()}</span>
              <span className="spacer" style={{ flex: 1 }} />
              <button
                className="ghost"
                onClick={async () => {
                  if (!window.confirm('Delete this comment?')) return
                  try {
                    await api.deleteComment(c.id)
                    await load()
                  } catch (e) {
                    onError(e)
                  }
                }}
              >
                Delete
              </button>
            </div>
            <div className="markdown" style={{ border: 'none', padding: 0, background: 'none' }}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{c.body}</ReactMarkdown>
            </div>
          </div>
        ))}
        <textarea
          rows={3}
          placeholder="Add a comment (markdown)…"
          value={newComment}
          onChange={(e) => setNewComment(e.target.value)}
        />
        <div className="actions">
          <button
            className="danger"
            style={{ marginRight: 'auto' }}
            onClick={() => onDelete(t.key)}
          >
            Delete ticket
          </button>
          <button
            disabled={!newComment.trim()}
            onClick={async () => {
              try {
                await api.addComment(t.key, newComment.trim())
                setNewComment('')
                await load()
              } catch (e) {
                onError(e)
              }
            }}
          >
            Comment
          </button>
        </div>

        {addingChild && childType && (
          <NewTicketDialog
            projects={projects}
            statuses={statuses}
            labels={labels}
            defaultProject={t.project_slug}
            defaultType={childType}
            defaultParent={t.key}
            lockType
            onClose={() => setAddingChild(false)}
            onCreated={async () => {
              setAddingChild(false)
              await load()
              await onChanged()
            }}
            onError={onError}
          />
        )}
      </div>
    </div>
  )
}
