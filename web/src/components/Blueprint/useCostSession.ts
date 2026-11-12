import { useChatLiveData } from '../../hooks/useChatLiveData';
import type { CostSession } from './stages';

/** CM's session record for a blueprint's cost basis, as ChatThread publishes it. */
export function useCostSession(chatId: string | undefined): CostSession {
  const live = useChatLiveData(chatId);
  return { llm_channel: live?.llmChannel, subscription_cost_usd: live?.subscriptionCostUsd };
}
