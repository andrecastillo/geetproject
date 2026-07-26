import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCorners,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import { arrayMove } from '@dnd-kit/sortable'
import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useApp } from '../App'
import { api, type BoardView, type Card } from '../api'
import BoardColumn, { columnDroppableId } from '../components/BoardColumn'
import NewTicketDialog from '../components/NewTicketDialog'
import TicketModal from '../components/TicketModal'
import { CardBody } from '../components/TicketCard'

export default function BoardPage() {
  const { slug, key } = useParams()
  const { boards, statuses, labels, fail } = useApp()
  const navigate = useNavigate()

  const [view, setView] = useState<BoardView | null>(null)
  const [draggingKey, setDraggingKey] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  // A pointer must travel a few pixels before a drag starts, otherwise every
  // click on a title or a status dropdown would be swallowed by the sensor.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  useEffect(() => {
    if (!slug && boards.length > 0) navigate(`/b/${boards[0].slug}`, { replace: true })
  }, [slug, boards, navigate])

  const reload = useCallback(async () => {
    if (!slug) return
    try {
      setView(await api.board(slug))
    } catch (e) {
      fail(e)
    }
  }, [slug, fail])

  useEffect(() => {
    void reload()
  }, [reload])

  const openTicket = (k: string) => navigate(`/b/${slug}/t/${k}`)
  const closeTicket = () => navigate(`/b/${slug}`)

  const changeStatus = async (ticketKey: string, statusSlug: string) => {
    try {
      await api.updateTicket(ticketKey, { status: statusSlug })
      await reload()
    } catch (e) {
      fail(e)
    }
  }

  const deleteTicket = async (ticketKey: string) => {
    try {
      const detail = await api.ticket(ticketKey)
      const extra =
        detail.descendant_count > 0
          ? `\n\nThis will also delete ${detail.descendant_count} child ticket${
              detail.descendant_count === 1 ? '' : 's'
            }.`
          : ''
      if (!window.confirm(`Delete ${detail.key} "${detail.title}"?${extra}`)) return
      await api.deleteTicket(ticketKey)
      if (key === ticketKey) closeTicket()
      await reload()
    } catch (e) {
      fail(e)
    }
  }

  const onDragStart = (e: DragStartEvent) => setDraggingKey(String(e.active.id))

  const onDragEnd = async (e: DragEndEvent) => {
    setDraggingKey(null)
    const { active, over } = e
    if (!over || !view) return

    const movedKey = String(active.id)
    const overId = String(over.id)
    const source = view.columns.find((c) => c.cards.some((x) => x.key === movedKey))
    if (!source) return

    let target = view.columns.find((c) => columnDroppableId(c.status.slug) === overId)
    let overCardKey = ''
    if (!target) {
      target = view.columns.find((c) => c.cards.some((x) => x.key === overId))
      overCardKey = overId
    }
    if (!target) return

    // Work out which card the moved one should land below, which is what the
    // API takes. Within a column, reorder first so dragging up and dragging
    // down both land where the pointer is rather than off by one.
    let after = ''
    if (source.status.id === target.status.id) {
      const oldIndex = target.cards.findIndex((x) => x.key === movedKey)
      const overIndex = overCardKey
        ? target.cards.findIndex((x) => x.key === overCardKey)
        : target.cards.length - 1
      if (oldIndex === overIndex || overIndex < 0) return
      const reordered = arrayMove(target.cards, oldIndex, overIndex)
      const at = reordered.findIndex((x) => x.key === movedKey)
      after = at > 0 ? reordered[at - 1].key : ''
    } else {
      const rest = target.cards.filter((x) => x.key !== movedKey)
      const i = overCardKey ? rest.findIndex((x) => x.key === overCardKey) : rest.length
      const insertAt = i >= 0 ? i : rest.length
      after = insertAt > 0 ? rest[insertAt - 1].key : ''
    }

    try {
      // The server returns the reassembled board. Letting it decide is what
      // keeps nesting and break-out correct without duplicating that rule here.
      setView(await api.move(movedKey, view.slug, target.status.slug, after))
    } catch (err) {
      fail(err)
      await reload()
    }
  }

  if (!slug) return <div className="center-note">No boards yet.</div>
  if (!view) return <div className="center-note">Loading board…</div>

  const draggedCard: Card | undefined = draggingKey
    ? view.columns.flatMap((c) => c.cards).find((c) => c.key === draggingKey)
    : undefined

  const noop = () => {}

  return (
    <>
      <div className="topbar" style={{ borderBottom: 'none', paddingBottom: 0 }}>
        <strong>{view.name}</strong>
        <span className="muted">
          {view.filter_type === 'any' ? 'all ticket types' : `${view.filter_type}s only`}
          {view.filter_labels.length > 0 &&
            ` · ${view.filter_label_mode === 'all' ? 'all of' : 'any of'} ${view.filter_labels
              .map((l) => l.name)
              .join(', ')}`}
        </span>
        <span className="spacer" />
        <button className="primary" onClick={() => setCreating(true)}>
          + New ticket
        </button>
      </div>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragStart={onDragStart}
        onDragEnd={onDragEnd}
      >
        <div className="board">
          {view.columns.map((col) => (
            <BoardColumn
              key={col.status.id}
              column={col}
              statuses={statuses}
              onOpen={openTicket}
              onStatusChange={changeStatus}
              onDelete={deleteTicket}
            />
          ))}
          {view.columns.length === 0 && (
            <div className="center-note">
              This board has no columns. Add some under “Boards &amp; labels”.
            </div>
          )}
        </div>

        <DragOverlay>
          {draggedCard && (
            <div className="card" style={{ cursor: 'grabbing' }}>
              <CardBody
                card={draggedCard}
                statuses={statuses}
                onOpen={noop}
                onStatusChange={noop}
                onDelete={noop}
              />
            </div>
          )}
        </DragOverlay>
      </DndContext>

      {creating && (
        <NewTicketDialog
          statuses={statuses}
          labels={labels}
          defaultType={view.filter_type === 'any' ? 'task' : view.filter_type}
          defaultLabelIds={view.filter_labels.map((l) => l.id)}
          onClose={() => setCreating(false)}
          onCreated={async () => {
            setCreating(false)
            await reload()
          }}
          onError={fail}
        />
      )}

      {key && (
        <TicketModal
          ticketKey={key}
          statuses={statuses}
          labels={labels}
          onClose={closeTicket}
          onOpen={openTicket}
          onChanged={reload}
          onDelete={deleteTicket}
          onError={fail}
        />
      )}
    </>
  )
}
