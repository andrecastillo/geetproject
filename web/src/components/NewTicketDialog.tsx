import { useState } from 'react'
import { api, type Label, type Status, type TicketType } from '../api'
import LabelPicker from './LabelPicker'

interface Props {
  statuses: Status[]
  labels: Label[]
  defaultType: TicketType
  defaultLabelIds?: number[]
  defaultParent?: string
  lockType?: boolean
  onClose: () => void
  onCreated: () => void | Promise<void>
  onError: (err: unknown) => void
}

export default function NewTicketDialog({
  statuses,
  labels,
  defaultType,
  defaultLabelIds = [],
  defaultParent = '',
  lockType = false,
  onClose,
  onCreated,
  onError,
}: Props) {
  const [type, setType] = useState<TicketType>(defaultType)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [status, setStatus] = useState(statuses[0]?.slug ?? '')
  const [parent, setParent] = useState(defaultParent)
  const [labelIds, setLabelIds] = useState<number[]>(defaultLabelIds)
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    if (!title.trim()) return
    setBusy(true)
    try {
      await api.createTicket({
        type,
        title: title.trim(),
        description,
        status,
        parent: parent.trim(),
        label_ids: labelIds,
      })
      await onCreated()
    } catch (e) {
      onError(e)
      setBusy(false)
    }
  }

  return (
    <div className="backdrop" onClick={onClose}>
      <div className="modal narrow" onClick={(e) => e.stopPropagation()}>
        <h2>New ticket</h2>

        <div className="field">
          <label>Title</label>
          <input
            type="text"
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void submit()
            }}
            placeholder="What needs doing?"
          />
        </div>

        <div className="row" style={{ marginBottom: 12 }}>
          <div className="field" style={{ flex: 1, marginBottom: 0 }}>
            <label>Type</label>
            <select
              value={type}
              disabled={lockType}
              onChange={(e) => setType(e.target.value as TicketType)}
            >
              <option value="epic">Epic</option>
              <option value="task">Task</option>
              <option value="subtask">Sub-task</option>
            </select>
          </div>
          <div className="field" style={{ flex: 1, marginBottom: 0 }}>
            <label>Status</label>
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              {statuses.map((s) => (
                <option key={s.id} value={s.slug}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
        </div>

        {type !== 'epic' && (
          <div className="field">
            <label>
              Parent {type === 'subtask' ? '(required — a task key)' : '(optional — an epic key)'}
            </label>
            <input
              type="text"
              value={parent}
              onChange={(e) => setParent(e.target.value)}
              placeholder="T-4"
            />
          </div>
        )}

        <div className="field">
          <label>Labels</label>
          <LabelPicker labels={labels} selected={labelIds} onChange={setLabelIds} />
        </div>

        <div className="field">
          <label>Description (markdown)</label>
          <textarea rows={5} value={description} onChange={(e) => setDescription(e.target.value)} />
        </div>

        <div className="actions">
          <button className="ghost" onClick={onClose}>
            Cancel
          </button>
          <button className="primary" disabled={busy || !title.trim()} onClick={() => void submit()}>
            Create
          </button>
        </div>
      </div>
    </div>
  )
}
