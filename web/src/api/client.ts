import type {
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
} from '../types';

const BASE_URL = '/api';

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
  async getCards(project: string, filter?: CardFilter): Promise<Card[]> {
    const params = new URLSearchParams();
    if (filter) {
      if (filter.state) params.set('state', filter.state);
      if (filter.type) params.set('type', filter.type);
      if (filter.priority) params.set('priority', filter.priority);
      if (filter.agent) params.set('agent', filter.agent);
      if (filter.label) params.set('label', filter.label);
      if (filter.parent) params.set('parent', filter.parent);
      if (filter.external_id) params.set('external_id', filter.external_id);
    }
    const query = params.toString();
    const path = `/projects/${project}/cards${query ? `?${query}` : ''}`;
    return this.request<Card[]>(path);
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

  // Sync
  async triggerSync(): Promise<SyncStatus> {
    return this.request<SyncStatus>('/sync', { method: 'POST' });
  }

  async getSyncStatus(): Promise<SyncStatus> {
    return this.request<SyncStatus>('/sync');
  }

  // Runner
  async runCard(project: string, id: string): Promise<Card> {
    return this.request<Card>(`/projects/${project}/cards/${id}/run`, {
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
}

export const api = new APIClient();

export function isAPIError(err: unknown): err is { error: string; code?: string; details?: string } {
  return err != null && typeof err === 'object' && 'error' in err;
}
