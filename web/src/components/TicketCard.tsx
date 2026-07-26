import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import type { Card, Status, Ticket } from '../api'

interface Props {
  card: Card
  statuses: Status[]
  /** Cross-project boards need to say which project a card belongs to; inside
   *  a single project the chip would be noise on every card. */
  showProject?: boolean
  onOpen: (key: string) => void
  onStatusChange: (key: string, statusSlug: string) => void
  onDelete: (key: string) => void
  dragging?: boolean
}

export function CardBody({ card, statuses, showProject, onOpen, onStatusChange, onDelete }: Props) {
  return (
    <>
      <div className="card-top">
        <span className={`type-badge type-${card.type}`}>{card.type}</span>
        <span>{card.key}</span>
        {showProject && <span className="project-chip">{card.project_name}</span>}
        <span className="spacer" />
        <button
          className="ghost"
          title="Delete ticket"
          style={{ padding: '0 5px', lineHeight: 1 }}
          onClick={(e) => {
            e.stopPropagation()
            onDelete(card.key)
          }}
        >
          ×
        </button>
      </div>

      {/* A sub-task that broke out of its parent's column needs to say where it
          came from, or it reads as an orphan. */}
      {card.type === 'subtask' && card.parent_key && (
        <div
          className="breadcrumb"
          onClick={(e) => {
            e.stopPropagation()
            onOpen(card.parent_key!)
          }}
          style={{ cursor: 'pointer' }}
        >
          ↳ in {card.parent_key} {card.parent_title}
        </div>
      )}

      <div className="card-title" onClick={() => onOpen(card.key)}>
        {card.title}
      </div>

      {card.labels.length > 0 && (
        <div className="chips">
          {card.labels.map((l) => (
            <span
              key={l.id}
              className="chip"
              style={l.color ? { borderColor: l.color, color: l.color } : undefined}
            >
              {l.name}
            </span>
          ))}
        </div>
      )}

      <div className="card-foot">
        <StatusSelect
          statuses={statuses}
          value={card.status?.slug ?? ''}
          onChange={(slug) => onStatusChange(card.key, slug)}
        />
      </div>

      {card.subtasks.length > 0 && (
        <div className="subtasks">
          {card.subtasks.map((st: Ticket) => (
            <div className="subtask-row" key={st.key}>
              <span className="st-title" title={st.title} onClick={() => onOpen(st.key)}>
                {st.title}
              </span>
              <StatusSelect
                statuses={statuses}
                value={st.status?.slug ?? ''}
                onChange={(slug) => onStatusChange(st.key, slug)}
              />
            </div>
          ))}
        </div>
      )}
    </>
  )
}

export function StatusSelect({
  statuses,
  value,
  onChange,
}: {
  statuses: Status[]
  value: string
  onChange: (slug: string) => void
}) {
  return (
    <select
      className="status-select"
      value={value}
      onPointerDown={(e) => e.stopPropagation()}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => onChange(e.target.value)}
    >
      {statuses.map((s) => (
        <option key={s.id} value={s.slug}>
          {s.name}
        </option>
      ))}
    </select>
  )
}

export default function TicketCard(props: Props) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: props.card.key,
  })

  return (
    <div
      ref={setNodeRef}
      className={'card' + (isDragging ? ' dragging' : '')}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      {...attributes}
      {...listeners}
    >
      <CardBody {...props} />
    </div>
  )
}
