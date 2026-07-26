import { useState } from 'react'
import { api, type Project } from '../api'

const PALETTE = ['#60a5fa', '#c4a2fc', '#7fd1b9', '#fbbf24', '#f87171', '#4ade80']

interface Props {
  existing: Project[]
  onClose: () => void
  onCreated: (p: Project) => void | Promise<void>
  onError: (err: unknown) => void
}

// derivePrefix mirrors the server's rule so the field can be previewed before
// submitting. The server still has the final say.
function derivePrefix(name: string): string {
  return Array.from(name)
    .filter((c) => /[a-z0-9]/i.test(c))
    .slice(0, 6)
    .join('')
    .toUpperCase()
}

export default function NewProjectDialog({ existing, onClose, onCreated, onError }: Props) {
  const [name, setName] = useState('')
  const [prefix, setPrefix] = useState('')
  const [prefixTouched, setPrefixTouched] = useState(false)
  const [color, setColor] = useState(PALETTE[existing.length % PALETTE.length])
  const [busy, setBusy] = useState(false)

  const effectivePrefix = prefixTouched ? prefix : derivePrefix(name)

  const submit = async () => {
    if (!name.trim()) return
    setBusy(true)
    try {
      const p = await api.createProject({
        name: name.trim(),
        prefix: effectivePrefix || undefined,
        color,
      })
      await onCreated(p)
    } catch (e) {
      onError(e)
      setBusy(false)
    }
  }

  return (
    <div className="backdrop" onClick={onClose}>
      <div className="modal narrow" onClick={(e) => e.stopPropagation()}>
        <h2>New project</h2>

        <div className="field">
          <label>Name</label>
          <input
            type="text"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void submit()
            }}
            placeholder="mini-kg"
          />
        </div>

        <div className="field">
          <label>Ticket prefix — keys in this project look like {effectivePrefix || 'ABC'}-1</label>
          <input
            type="text"
            value={effectivePrefix}
            onChange={(e) => {
              setPrefixTouched(true)
              setPrefix(e.target.value.toUpperCase())
            }}
            placeholder="KG"
            maxLength={6}
          />
        </div>

        <div className="field">
          <label>Colour</label>
          <div className="chips">
            {PALETTE.map((c) => (
              <button
                key={c}
                type="button"
                className="chip"
                onClick={() => setColor(c)}
                style={{
                  background: c,
                  borderColor: color === c ? 'var(--text)' : 'transparent',
                  width: 28,
                  height: 22,
                }}
                aria-label={c}
              />
            ))}
          </div>
        </div>

        <div className="actions">
          <button className="ghost" onClick={onClose}>
            Cancel
          </button>
          <button className="primary" disabled={busy || !name.trim()} onClick={() => void submit()}>
            Create
          </button>
        </div>
      </div>
    </div>
  )
}
