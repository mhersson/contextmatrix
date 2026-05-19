import type {
  AppConfig,
  Card,
  ProjectConfig,
  CardFilter,
  APIError,
  CreateCardInput,
  CreateProjectInput,
  UpdateCardInput,
  UpdateProjectInput,
  PatchCardInput,
  CardContext,
  DashboardData,
  SyncStatus,
  StopAllResponse,
  TaskSkillSummary,
  KnowledgeBaseSummary,
  KnowledgeDocResponse,
  RefreshPlan,
  RefreshJobStatus,
  RefreshStatusResponse,
  ChatSession,
  ChatStatus,
  ChatMessage,
  ChatModelList,
  ActivityFeedResponse,
  RunnerHealth,
} from '../types';

const BASE_URL = '/api';

// Wire shape for GET /api/projects/:project/cards. Not exported — callers see
// the flat Card[] returned by getCards().
interface CardPage {
  items: Card[];
  next_cursor?: string;
  total?: number;
}

class APIClient {
  private agentId: string | null = null;

  setAgentId(id: string | null): void {
    this.agentId = id;
  }

  getAgentId(): string | null {
    return this.agentId;
  }

  private async request<T>(
    path: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers: HeadersInit = {
      'Content-Type': 'application/json',
      'X-Requested-With': 'contextmatrix',
      ...options.headers,
    };

    if (this.agentId) {
      (headers as Record<string, string>)['X-Agent-ID'] = this.agentId;
    }

    const response = await fetch(`${BASE_URL}${path}`, {
      ...options,
      headers,
    });

    if (!response.ok) {
      let error: APIError;
      try {
        error = await response.json();
      } catch {
        error = {
          error: response.statusText,
          code: 'UNKNOWN_ERROR',
        };
      }
      throw error;
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return response.json();
  }

  // Projects
  async getProjects(): Promise<ProjectConfig[]> {
    return this.request<ProjectConfig[]>('/projects');
  }

  async getProject(name: string): Promise<ProjectConfig> {
    return this.request<ProjectConfig>(`/projects/${name}`);
  }

  async createProject(input: CreateProjectInput): Promise<ProjectConfig> {
    return this.request<ProjectConfig>('/projects', {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }

  async updateProject(
    name: string,
    input: UpdateProjectInput
  ): Promise<ProjectConfig> {
    return this.request<ProjectConfig>(`/projects/${name}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    });
  }

  async deleteProject(name: string): Promise<void> {
    return this.request<void>(`/projects/${name}`, {
      method: 'DELETE',
    });
  }

  // Cards
  //
  // The server paginates GET /api/projects/:project/cards via a cursor envelope
  // (items / next_cursor / total). This helper walks every page transparently
  // and returns the flat list existing callers (useBoard, etc.) already expect.
  // The default per-request limit matches the server default (500) so small
  // projects still complete in a single round-trip.
  async getCards(project: string, filter?: CardFilter): Promise<Card[]> {
    const baseParams = new URLSearchParams();
    if (filter) {
      if (filter.state) baseParams.set('state', filter.state);
      if (filter.type) baseParams.set('type', filter.type);
      if (filter.priority) baseParams.set('priority', filter.priority);
      if (filter.agent) baseParams.set('agent', filter.agent);
      if (filter.label) baseParams.set('label', filter.label);
      if (filter.parent) baseParams.set('parent', filter.parent);
      if (filter.external_id) baseParams.set('external_id', filter.external_id);
      if (filter.vetted !== undefined) baseParams.set('vetted', String(filter.vetted));
    }

    const all: Card[] = [];
    let cursor: string | null = null;
    // Cap iterations as a sanity bound against a pathological server response
    // (e.g. a cursor that never advances). 200 pages × 500 items = 100k cards.
    for (let i = 0; i < 200; i++) {
      const params = new URLSearchParams(baseParams);
      if (cursor) {
        params.set('cursor', cursor);
      }
      const query = params.toString();
      const path = `/projects/${project}/cards${query ? `?${query}` : ''}`;
      const page = await this.request<CardPage>(path);
      all.push(...page.items);
      if (!page.next_cursor) {
        return all;
      }
      cursor = page.next_cursor;
    }
    return all;
  }

  async getCard(project: string, id: string): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}`);
  }

  async createCard(project: string, input: CreateCardInput): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }

  async updateCard(
    project: string,
    id: string,
    input: UpdateCardInput
  ): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    });
  }

  async patchCard(
    project: string,
    id: string,
    input: PatchCardInput
  ): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    });
  }

  async deleteCard(project: string, id: string): Promise<void> {
    return this.request<void>(`/projects/${project}/cards/${id}`, {
      method: 'DELETE',
    });
  }

  // Agent operations
  async claimCard(project: string, id: string, agentId: string): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}/claim`, {
      method: 'POST',
      body: JSON.stringify({ agent_id: agentId }),
    });
  }

  async releaseCard(
    project: string,
    id: string,
    agentId: string
  ): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}/release`, {
      method: 'POST',
      body: JSON.stringify({ agent_id: agentId }),
    });
  }

  async heartbeatCard(
    project: string,
    id: string,
    agentId: string
  ): Promise<void> {
    return this.request<void>(`/projects/${project}/cards/${id}/heartbeat`, {
      method: 'POST',
      body: JSON.stringify({ agent_id: agentId }),
    });
  }

  async addLogEntry(
    project: string,
    id: string,
    agentId: string,
    action: string,
    message: string
  ): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}/log`, {
      method: 'POST',
      body: JSON.stringify({ agent_id: agentId, action, message }),
    });
  }

  async getCardContext(project: string, id: string): Promise<CardContext> {
    return this.request<CardContext>(`/projects/${project}/cards/${id}/context`);
  }

  async getDashboard(project: string): Promise<DashboardData> {
    return this.request<DashboardData>(`/projects/${project}/dashboard`);
  }

  async getActivity(project: string, limit = 50): Promise<ActivityFeedResponse> {
    return this.request<ActivityFeedResponse>(`/projects/${project}/activity?limit=${limit}`);
  }

  async getRunnerHealth(signal?: AbortSignal): Promise<RunnerHealth> {
    return this.request<RunnerHealth>(`/runner/health`, { signal });
  }

  // App config
  async getAppConfig(): Promise<AppConfig> {
    return this.request<AppConfig>('/app/config');
  }

  // Task skills (project default + per-card selectors)
  async getTaskSkills(): Promise<TaskSkillSummary[]> {
    const resp = await this.request<{ skills: TaskSkillSummary[] }>('/task-skills');
    return resp.skills;
  }

  // Sync
  async triggerSync(): Promise<SyncStatus> {
    return this.request<SyncStatus>('/sync', { method: 'POST' });
  }

  async getSyncStatus(): Promise<SyncStatus> {
    return this.request<SyncStatus>('/sync');
  }

  // Runner
  async runCard(
    project: string,
    id: string,
    opts?: { interactive?: boolean }
  ): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}/run`, {
      method: 'POST',
      body: opts?.interactive ? JSON.stringify({ interactive: true }) : undefined,
    });
  }

  async sendCardMessage(
    project: string,
    id: string,
    content: string
  ): Promise<{ ok: boolean; message_id: string }> {
    return this.request<{ ok: boolean; message_id: string }>(
      `/projects/${project}/cards/${id}/message`,
      {
        method: 'POST',
        body: JSON.stringify({ content }),
      }
    );
  }

  async promoteCardToAutonomous(project: string, id: string): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}/promote`, {
      method: 'POST',
    });
  }

  async stopCard(project: string, id: string): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}/stop`, {
      method: 'POST',
    });
  }

  async stopAllCards(project: string): Promise<StopAllResponse> {
    return this.request<StopAllResponse>(
      `/projects/${project}/stop-all`,
      { method: 'POST' }
    );
  }

  async fetchBranches(project: string): Promise<string[]> {
    return this.request<string[]>(`/projects/${project}/branches`);
  }

  // Knowledge base

  async getKnowledgeBase(project: string): Promise<KnowledgeBaseSummary> {
    return this.request<KnowledgeBaseSummary>(
      `/projects/${encodeURIComponent(project)}/knowledge`
    );
  }

  async getKnowledgeDoc(
    project: string,
    repo: string,
    doc: string
  ): Promise<KnowledgeDocResponse> {
    return this.request<KnowledgeDocResponse>(
      `/projects/${encodeURIComponent(project)}/knowledge/${encodeURIComponent(repo)}/${encodeURIComponent(doc)}`
    );
  }

  async putKnowledgeDoc(
    project: string,
    repo: string,
    doc: string,
    content: string,
    opts?: { signal?: AbortSignal },
  ): Promise<void> {
    await this.request<void>(
      `/projects/${encodeURIComponent(project)}/knowledge/${encodeURIComponent(repo)}/${encodeURIComponent(doc)}`,
      { method: 'PUT', body: JSON.stringify({ content }), signal: opts?.signal }
    );
  }

  async getKnowledgeRefreshPlan(project: string, repo: string): Promise<RefreshPlan> {
    return this.request<RefreshPlan>(
      `/projects/${encodeURIComponent(project)}/knowledge/${encodeURIComponent(repo)}/refresh-plan`,
      { method: 'GET' },
    );
  }

  async startKnowledgeRefresh(
    project: string,
    repo: string,
    overwriteDocs: string[],
  ): Promise<RefreshJobStatus> {
    return this.request<RefreshJobStatus>(
      `/projects/${encodeURIComponent(project)}/knowledge/${encodeURIComponent(repo)}/refresh`,
      {
        method: 'POST',
        body: JSON.stringify({ overwrite_docs: overwriteDocs }),
      },
    );
  }

  async getKnowledgeRefreshStatus(project: string): Promise<RefreshStatusResponse> {
    return this.request<RefreshStatusResponse>(
      `/projects/${encodeURIComponent(project)}/knowledge/refresh-status`,
      { method: 'GET' },
    );
  }

  // Chat
  async listChats(filter: { project?: string; status?: ChatStatus } = {}): Promise<ChatSession[]> {
    const q = new URLSearchParams();
    if (filter.project) q.set('project', filter.project);
    if (filter.status) q.set('status', filter.status);
    const qs = q.toString();
    return this.request<ChatSession[]>(`/chats${qs ? `?${qs}` : ''}`);
  }

  async createChat(body: { title?: string; project?: string; model?: string }): Promise<ChatSession> {
    return this.request<ChatSession>('/chats', {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  async listChatModels(): Promise<ChatModelList> {
    return this.request<ChatModelList>('/chats/models');
  }

  async getChat(id: string): Promise<ChatSession> {
    return this.request<ChatSession>(`/chats/${encodeURIComponent(id)}`);
  }

  async patchChat(id: string, body: { title?: string }): Promise<ChatSession> {
    return this.request<ChatSession>(`/chats/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    });
  }

  async deleteChat(id: string): Promise<void> {
    return this.request<void>(`/chats/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async openChat(id: string): Promise<ChatSession> {
    return this.request<ChatSession>(`/chats/${encodeURIComponent(id)}/open`, {
      method: 'POST',
      body: JSON.stringify({}),
    });
  }

  async endChat(id: string): Promise<ChatSession> {
    return this.request<ChatSession>(`/chats/${encodeURIComponent(id)}/end`, {
      method: 'POST',
      body: JSON.stringify({}),
    });
  }

  async clearChatContext(id: string): Promise<void> {
    return this.request<void>(`/chats/${encodeURIComponent(id)}/clear`, {
      method: 'POST',
      body: JSON.stringify({}),
    });
  }

  async sendChatMessage(id: string, content: string): Promise<{ ok: boolean; message_id: string }> {
    return this.request<{ ok: boolean; message_id: string }>(`/chats/${encodeURIComponent(id)}/messages`, {
      method: 'POST',
      body: JSON.stringify({ content }),
    });
  }

  async listChatMessages(
    id: string,
    sinceSeq: number,
    limit: number,
  ): Promise<{ messages: ChatMessage[] }> {
    const qs = new URLSearchParams({
      since_seq: String(sinceSeq),
      limit: String(limit),
    });
    return this.request<{ messages: ChatMessage[] }>(
      `/chats/${encodeURIComponent(id)}/messages?${qs.toString()}`,
    );
  }

  // Images — POST /api/images with multipart/form-data. The request() helper
  // hard-codes Content-Type: application/json, so this method talks to fetch
  // directly and threads the same X-Agent-ID / X-Requested-With headers used
  // by mutation endpoints. `signal` lets callers (e.g. useImageUpload) cancel
  // an in-flight upload when the editor unmounts.
  async uploadImage(file: File, signal?: AbortSignal): Promise<{ id: string; url: string }> {
    const headers: Record<string, string> = {
      'X-Requested-With': 'contextmatrix',
    };
    if (this.agentId) {
      headers['X-Agent-ID'] = this.agentId;
    }

    const body = new FormData();
    body.append('file', file);

    const response = await fetch(`${BASE_URL}/images`, {
      method: 'POST',
      headers,
      body,
      signal,
    });

    if (!response.ok) {
      let err: APIError;
      try {
        err = await response.json();
      } catch {
        err = { error: response.statusText, code: 'UNKNOWN_ERROR' };
      }
      throw err;
    }

    return response.json();
  }
}

export const api = new APIClient();

export function isAPIError(err: unknown): err is { error: string; code?: string; details?: string } {
  return err != null && typeof err === 'object' && 'error' in err;
}

export function errorMessage(err: unknown): string {
  if (err && typeof err === 'object' && 'error' in err) {
    return String((err as { error: unknown }).error);
  }
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}
