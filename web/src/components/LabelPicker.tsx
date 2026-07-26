import type { Label } from '../api'

interface Props {
  labels: Label[]
  selected: number[]
  onChange: (ids: number[]) => void
}

export default function LabelPicker({ labels, selected, onChange }: Props) {
  if (labels.length === 0) {
    return <span className="muted">No labels yet — add some under “Boards &amp; labels”.</span>
  }
  const toggle = (id: number) =>
    onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id])

  return (
    <div className="chips">
      {labels.map((l) => {
        const on = selected.includes(l.id)
        return (
          <button
            key={l.id}
            type="button"
            className="chip"
            onClick={() => toggle(l.id)}
            style={{
              cursor: 'pointer',
              borderColor: on ? l.color || 'var(--accent)' : 'var(--border)',
              color: on ? l.color || 'var(--accent)' : 'var(--text-faint)',
              background: on ? 'var(--bg-hover)' : 'var(--bg)',
            }}
          >
            {on ? '✓ ' : ''}
            {l.name}
          </button>
        )
      })}
    </div>
  )
}
