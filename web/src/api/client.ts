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
  JiraEpicPreview,
  JiraImportEpicInput,
  JiraImportResult,
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

  // App config
  async getAppConfig(): Promise<AppConfig> {
    return this.request<AppConfig>('/app/config');
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

  // Jira
  async getJiraStatus(): Promise<{ configured: boolean; base_url?: string }> {
    return this.request<{ configured: boolean; base_url?: string }>('/jira/status');
  }

  async previewJiraEpic(epicKey: string): Promise<JiraEpicPreview> {
    return this.request<JiraEpicPreview>(`/jira/epic/${encodeURIComponent(epicKey)}`);
  }

  async importJiraEpic(input: JiraImportEpicInput): Promise<JiraImportResult> {
    return this.request<JiraImportResult>('/jira/import-epic', {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }

  async fetchBranches(project: string): Promise<string[]> {
    return this.request<string[]>(`/projects/${project}/branches`);
  }
}

export const api = new APIClient();

export function isAPIError(err: unknown): err is { error: string; code?: string; details?: string } {
  return err != null && typeof err === 'object' && 'error' in err;
}
