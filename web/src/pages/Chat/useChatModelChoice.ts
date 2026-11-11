import { useEffect, useState } from 'react';
import { api } from '../../api/client';
import { useSSEBus } from '../../hooks/useSSEBus';
import type { ChatModel, ChatModelList } from '../../types';

export interface ChatModelChoice {
  source: ChatModelList['source'];
  model: string;
  defaultModel: string;
  models: ChatModel[];
  onChange: (model: string) => void;
}

export interface ChatModelChoices {
  chat: ChatModelChoice;
  blueprint: ChatModelChoice;
}

interface ModelList {
  source: ChatModelList['source'];
  models: ChatModel[];
  defaultModel: string;
}

const EMPTY: ModelList = { source: 'endpoint', models: [], defaultModel: '' };

/** A pick survives a refetch only while the new list still has it. */
function keepOrDefault(pick: string, models: ChatModel[], defaultModel: string): string {
  return pick && models.some((m) => m.id === pick) ? pick : defaultModel;
}

/**
 * Two model lists and two independent picks, one for chats and one for
 * blueprints. Each kind gets its own list because the LLM channel can start
 * the two kinds in different modes. A failed fetch leaves that list empty,
 * which hides the endpoint picker, and the create POST omits the model.
 * Each pick seeds to its server default so an untouched picker sends what it shows.
 * Both lists refetch on every LLM channel event, since a switch changes catalogs.
 */
export function useChatModelChoice(): ChatModelChoices {
  const [model, setModel] = useState('');
  const [blueprintModel, setBlueprintModel] = useState('');
  const [chatList, setChatList] = useState<ModelList>(EMPTY);
  const [blueprintList, setBlueprintList] = useState<ModelList>(EMPTY);
  const [channelEpoch, setChannelEpoch] = useState(0);
  const { subscribe } = useSSEBus();

  useEffect(() => subscribe('llm_channel.*', () => setChannelEpoch((n) => n + 1)), [subscribe]);

  useEffect(() => {
    let cancelled = false;
    void api
      .listChatModels()
      .then((resp) => {
        if (cancelled) return;
        setChatList({ source: resp.source ?? 'endpoint', models: resp.models, defaultModel: resp.default });
        setModel((cur) => keepOrDefault(cur, resp.models, resp.default));
      })
      .catch((e) => {
        console.warn('listChatModels failed; picker disabled', e);
      });
    void api
      .listChatModels('architect')
      .then((resp) => {
        if (cancelled) return;
        setBlueprintList({ source: resp.source ?? 'endpoint', models: resp.models, defaultModel: resp.architect_default });
        setBlueprintModel((cur) => keepOrDefault(cur, resp.models, resp.architect_default));
      })
      .catch((e) => {
        console.warn('listChatModels(architect) failed; picker disabled', e);
      });
    return () => {
      cancelled = true;
    };
  }, [channelEpoch]);

  return {
    chat: { ...chatList, model, onChange: setModel },
    blueprint: { ...blueprintList, model: blueprintModel, onChange: setBlueprintModel },
  };
}
