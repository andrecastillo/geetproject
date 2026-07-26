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
import { NavLink, useNavigate, useParams } from 'react-router-dom'
import { useApp } from '../App'
import { ALL_SCOPE, api, type Board, type BoardView, type Card } from '../api'
import BoardColumn, { columnDroppableId } from '../components/BoardColumn'
import NewTicketDialog from '../components/NewTicketDialog'
import ScopeSettings from '../components/ScopeSettings'
import TicketModal from '../components/TicketModal'
import { CardBody } from '../components/TicketCard'

export default function BoardPage() {
  const { scope = ALL_SCOPE, view: viewSlug, key } = useParams()
  const { projects, statuses, labels, fail } = useApp()
  const navigate = useNavigate()

  const [views, setViews] = useState<Board[]>([])
  const [board, setBoard] = useState<BoardView | null>(null)
  const [draggingKey, setDraggingKey] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)

  const project = projects.find((p) => p.slug === scope)
  const isAll = scope === ALL_SCOPE
  const scopeName = isAll ? 'All projects' : (project?.name ?? scope)

  // A pointer must travel a few pixels before a drag starts, otherwise every
  // click on a title or a status dropdown would be swallowed by the sensor.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  const loadViews = useCallback(async () => {
    try {
      setViews(await api.boards(scope))
    } catch (e) {
      fail(e)
      setViews([])
    }
  }, [scope, fail])

  useEffect(() => {
    void loadViews()
  }, [loadViews])

  // Landing on a scope with no view named picks its first one.
  useEffect(() => {
    if (!viewSlug && views.length > 0) {
      navigate(`/p/${scope}/b/${views[0].slug}`, { replace: true })
    }
  }, [viewSlug, views, scope, navigate])

  const reload = useCallback(async () => {
    if (!viewSlug) return
    try {
      setBoard(await api.board(scope, viewSlug))
    } catch (e) {
      fail(e)
    }
  }, [scope, viewSlug, fail])

  useEffect(() => {
    setBoard(null)
    void reload()
  }, [reload])

  const openTicket = (k: string) => navigate(`/p/${scope}/b/${viewSlug}/t/${k}`)
  const closeTicket = () => navigate(`/p/${scope}/b/${viewSlug}`)

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
    if (!over || !board) return

    const movedKey = String(active.id)
    const overId = String(over.id)
    const source = board.columns.find((c) => c.cards.some((x) => x.key === movedKey))
    if (!source) return

    let target = board.columns.find((c) => columnDroppableId(c.status.slug) === overId)
    let overCardKey = ''
    if (!target) {
      target = board.columns.find((c) => c.cards.some((x) => x.key === overId))
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
      setBoard(await api.move(movedKey, scope, board.slug, target.status.slug, after))
    } catch (err) {
      fail(err)
      await reload()
    }
  }

  if (!isAll && projects.length === 0) {
    return (
      <div className="center-note">
        No projects yet. Create one from the sidebar — tickets have to live somewhere.
      </div>
    )
  }

  const draggedCard: Card | undefined =
    draggingKey && board
      ? board.columns.flatMap((c) => c.cards).find((c) => c.key === draggingKey)
      : undefined

  const noop = () => {}

  return (
    <>
      <div className="scope-head">
        <div className="scope-title">
          {!isAll && project && (
            <span className="dot" style={{ background: project.color || '#6b7383' }} />
          )}
          <strong>{scopeName}</strong>
          {!isAll && project && <span className="p-prefix">{project.prefix}</span>}
        </div>

        <nav className="view-tabs">
          {views.map((v) => (
            <NavLink
              key={v.slug}
              to={`/p/${scope}/b/${v.slug}`}
              className={({ isActive }) => 'view-tab' + (isActive ? ' active' : '')}
            >
              {v.name}
            </NavLink>
          ))}
        </nav>

        <span className="spacer" />
        <button className="ghost" onClick={() => setSettingsOpen(true)}>
          Views &amp; labels
        </button>
        <button className="primary" onClick={() => setCreating(true)}>
          + New ticket
        </button>
      </div>

      {!board ? (
        <div className="center-note">
          {views.length === 0 ? 'This scope has no views yet.' : 'Loading board…'}
        </div>
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCorners}
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
        >
          <div className="board">
            {board.columns.map((col) => (
              <BoardColumn
                key={col.status.id}
                column={col}
                statuses={statuses}
                showProject={isAll}
                onOpen={openTicket}
                onStatusChange={changeStatus}
                onDelete={deleteTicket}
              />
            ))}
            {board.columns.length === 0 && (
              <div className="center-note">
                This view has no columns. Add some under “Views &amp; labels”.
              </div>
            )}
          </div>

          <DragOverlay>
            {draggedCard && (
              <div className="card" style={{ cursor: 'grabbing' }}>
                <CardBody
                  card={draggedCard}
                  statuses={statuses}
                  showProject={isAll}
                  onOpen={noop}
                  onStatusChange={noop}
                  onDelete={noop}
                />
              </div>
            )}
          </DragOverlay>
        </DndContext>
      )}

      {creating && (
        <NewTicketDialog
          projects={projects}
          statuses={statuses}
          labels={labels}
          defaultProject={isAll ? '' : scope}
          defaultType={board?.filter_type === 'any' || !board ? 'task' : board.filter_type}
          defaultLabelIds={board?.filter_labels.map((l) => l.id) ?? []}
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
          projects={projects}
          statuses={statuses}
          labels={labels}
          onClose={closeTicket}
          onOpen={openTicket}
          onChanged={reload}
          onDelete={deleteTicket}
          onError={fail}
        />
      )}

      {settingsOpen && (
        <ScopeSettings
          scope={scope}
          views={views}
          onClose={() => setSettingsOpen(false)}
          onViewsChanged={async () => {
            await loadViews()
            await reload()
          }}
        />
      )}
    </>
  )
}
