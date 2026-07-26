import { useDroppable } from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import type { Column, Status } from '../api'
import TicketCard from './TicketCard'

export const columnDroppableId = (slug: string) => `col:${slug}`

interface Props {
  column: Column
  statuses: Status[]
  showProject?: boolean
  onOpen: (key: string) => void
  onStatusChange: (key: string, statusSlug: string) => void
  onDelete: (key: string) => void
}

export default function BoardColumn({
  column,
  statuses,
  showProject,
  onOpen,
  onStatusChange,
  onDelete,
}: Props) {
  const { setNodeRef, isOver } = useDroppable({ id: columnDroppableId(column.status.slug) })
  const keys = column.cards.map((c) => c.key)

  // Nested sub-tasks are counted too: they are work on this board even though
  // they are not cards of their own.
  const nested = column.cards.reduce((n, c) => n + c.subtasks.length, 0)

  return (
    <section className="column">
      <header className="column-head">
        <span className="dot" style={{ background: column.status.color || '#666' }} />
        {column.status.name}
        <span className="count">
          {column.cards.length}
          {nested > 0 && ` (+${nested})`}
        </span>
      </header>
      <div ref={setNodeRef} className={'column-body' + (isOver ? ' over' : '')}>
        <SortableContext items={keys} strategy={verticalListSortingStrategy}>
          {column.cards.map((card) => (
            <TicketCard
              key={card.key}
              card={card}
              statuses={statuses}
              showProject={showProject}
              onOpen={onOpen}
              onStatusChange={onStatusChange}
              onDelete={onDelete}
            />
          ))}
        </SortableContext>
        {column.cards.length === 0 && <div className="empty-hint">Drop cards here</div>}
      </div>
    </section>
  )
}
