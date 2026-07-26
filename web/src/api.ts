// Types mirror the Go structs in internal/store/models.go. Keep them in step.

export type TicketType = 'epic' | 'task' | 'subtask'

/** The cross-project scope, used wherever a project slug is expected. */
export const ALL_SCOPE = 'all'

export interface Project {
  id: number
  name: string
  slug: string
  prefix: string
  color: string
  position: number
  created_at: string
  ticket_count: number
}

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
  project_id: number
  project_slug: string
  project_name: string
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
  /** null on the cross-project scope, where project_slug is "all". */
  project_id: number | null
  project_slug: string
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

  projects: () => request<Project[]>('GET', '/api/projects'),
  project: (slug: string) => request<Project>('GET', `/api/projects/${slug}`),
  createProject: (input: { name: string; slug?: string; prefix?: string; color?: string }) =>
    request<Project>('POST', '/api/projects', input),
  updateProject: (
    slug: string,
    patch: Partial<{ name: string; prefix: string; color: string; position: number }>,
  ) => request<Project>('PATCH', `/api/projects/${slug}`, patch),
  deleteProject: (slug: string) =>
    request<{ deleted_tickets: number }>('DELETE', `/api/projects/${slug}`),

  // Views live under a scope: a project slug, or ALL_SCOPE. A view slug alone
  // is not unique, so every one of these takes the scope.
  boards: (scope: string) => request<Board[]>('GET', `/api/projects/${scope}/boards`),
  board: (scope: string, slug: string) =>
    request<BoardView>('GET', `/api/projects/${scope}/boards/${slug}`),
  createBoard: (
    scope: string,
    input: {
      name: string
      filter_type: string
      filter_label_ids?: number[]
      filter_label_mode?: string
    },
  ) => request<Board>('POST', `/api/projects/${scope}/boards`, input),
  updateBoard: (
    scope: string,
    slug: string,
    patch: Partial<{
      name: string
      filter_type: string
      filter_label_mode: string
      filter_label_ids: number[]
    }>,
  ) => request<Board>('PATCH', `/api/projects/${scope}/boards/${slug}`, patch),
  deleteBoard: (scope: string, slug: string) =>
    request<void>('DELETE', `/api/projects/${scope}/boards/${slug}`),
  setColumns: (scope: string, slug: string, status_ids: number[]) =>
    request<BoardView>('PUT', `/api/projects/${scope}/boards/${slug}/columns`, { status_ids }),

  ticket: (key: string) => request<TicketDetail>('GET', `/api/tickets/${key}`),
  createTicket: (input: {
    project?: string
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
      project: string
      label_ids: number[]
    }>,
  ) => request<Ticket>('PATCH', `/api/tickets/${key}`, patch),
  deleteTicket: (key: string) => request<void>('DELETE', `/api/tickets/${key}`),
  move: (key: string, project: string, board: string, status: string, after: string) =>
    request<BoardView>('POST', `/api/tickets/${key}/move`, { project, board, status, after }),

  comments: (key: string) => request<Comment[]>('GET', `/api/tickets/${key}/comments`),
  addComment: (key: string, body: string) =>
    request<Comment>('POST', `/api/tickets/${key}/comments`, { body }),
  updateComment: (id: number, body: string) =>
    request<Comment>('PATCH', `/api/comments/${id}`, { body }),
  deleteComment: (id: number) => request<void>('DELETE', `/api/comments/${id}`),
}
