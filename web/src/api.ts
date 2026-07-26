// Types mirror the Go structs in internal/store/models.go. Keep them in step.

export type TicketType = 'epic' | 'task' | 'subtask'

export interface Status {
  id: number
  name: string
  slug: string
  color: string
  position: number
  is_done: boolean
}

export interface Label {
  id: number
  name: string
  color: string
}

export interface Ticket {
  id: number
  key: string
  type: TicketType
  title: string
  description: string
  status_id: number
  status?: Status
  parent_id?: number
  parent_key?: string
  parent_title?: string
  labels: Label[]
  created_at: string
  updated_at: string
}

export interface Card extends Ticket {
  position: number
  subtasks: Ticket[]
}

export interface Column {
  status: Status
  cards: Card[]
}

export interface Board {
  id: number
  name: string
  slug: string
  filter_type: TicketType | 'any'
  filter_label_mode: 'any' | 'all'
  filter_labels: Label[]
  position: number
  created_at: string
}

export interface BoardView extends Board {
  columns: Column[]
}

export interface Comment {
  id: number
  ticket_id: number
  body: string
  created_at: string
  updated_at: string
}

export interface TicketDetail extends Ticket {
  children: Ticket[]
  comments: Comment[]
  descendant_count: number
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let message = `${method} ${path} failed (${res.status})`
    try {
      const data = await res.json()
      if (data?.error) message = data.error
    } catch {
      // Non-JSON error body; the generic message is the best we have.
    }
    throw new ApiError(res.status, message)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  statuses: () => request<Status[]>('GET', '/api/statuses'),

  labels: () => request<Label[]>('GET', '/api/labels'),
  createLabel: (name: string, color: string) =>
    request<Label>('POST', '/api/labels', { name, color }),
  deleteLabel: (id: number) => request<void>('DELETE', `/api/labels/${id}`),

  boards: () => request<Board[]>('GET', '/api/boards'),
  board: (slug: string) => request<BoardView>('GET', `/api/boards/${slug}`),
  createBoard: (input: {
    name: string
    filter_type: string
    filter_label_ids?: number[]
    filter_label_mode?: string
  }) => request<Board>('POST', '/api/boards', input),
  updateBoard: (
    slug: string,
    patch: Partial<{
      name: string
      filter_type: string
      filter_label_mode: string
      filter_label_ids: number[]
    }>,
  ) => request<Board>('PATCH', `/api/boards/${slug}`, patch),
  deleteBoard: (slug: string) => request<void>('DELETE', `/api/boards/${slug}`),
  setColumns: (slug: string, status_ids: number[]) =>
    request<BoardView>('PUT', `/api/boards/${slug}/columns`, { status_ids }),

  ticket: (key: string) => request<TicketDetail>('GET', `/api/tickets/${key}`),
  createTicket: (input: {
    type: TicketType
    title: string
    description?: string
    status?: string
    parent?: string
    label_ids?: number[]
  }) => request<Ticket>('POST', '/api/tickets', input),
  // Only the keys present in `patch` are touched; the server leaves the rest
  // alone, so callers must never send a whole ticket back.
  updateTicket: (
    key: string,
    patch: Partial<{
      title: string
      description: string
      type: TicketType
      status: string
      parent: string
      label_ids: number[]
    }>,
  ) => request<Ticket>('PATCH', `/api/tickets/${key}`, patch),
  deleteTicket: (key: string) => request<void>('DELETE', `/api/tickets/${key}`),
  move: (key: string, board: string, status: string, after: string) =>
    request<BoardView>('POST', `/api/tickets/${key}/move`, { board, status, after }),

  comments: (key: string) => request<Comment[]>('GET', `/api/tickets/${key}/comments`),
  addComment: (key: string, body: string) =>
    request<Comment>('POST', `/api/tickets/${key}/comments`, { body }),
  updateComment: (id: number, body: string) =>
    request<Comment>('PATCH', `/api/comments/${id}`, { body }),
  deleteComment: (id: number) => request<void>('DELETE', `/api/comments/${id}`),
}
