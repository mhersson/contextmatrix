import { useEffect, useRef, useState } from 'react';
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
 * the two kinds in different modes. A failed first fetch leaves that list
 * empty, which hides the endpoint picker, and the create POST omits the model;
 * a failed refetch keeps the previous list and pick.
 * Each pick seeds to its server default so an untouched picker sends what it shows.
 * Both lists refetch when an LLM channel event names a mode other than the one
 * last seen; usage readings fire the same event without a switch.
 */
export function useChatModelChoice(): ChatModelChoices {
  const [model, setModel] = useState('');
  const [blueprintModel, setBlueprintModel] = useState('');
  const [chatList, setChatList] = useState<ModelList>(EMPTY);
  const [blueprintList, setBlueprintList] = useState<ModelList>(EMPTY);
  const [channelEpoch, setChannelEpoch] = useState(0);
  // The global mode the lists match: seeded by the first fetch, else by the first event.
  const seenMode = useRef<string | null>(null);
  const { subscribe } = useSSEBus();

  useEffect(
    () =>
      subscribe('llm_channel.*', (e) => {
        const mode = e.data?.mode;
        if (typeof mode !== 'string' || mode === seenMode.current) return;
        seenMode.current = mode;
        setChannelEpoch((n) => n + 1);
      }),
    [subscribe],
  );

  useEffect(() => {
    let cancelled = false;
    const chatFetch = api.listChatModels();
    const blueprintFetch = api.listChatModels('architect');
    void chatFetch
      .then((resp) => {
        if (cancelled) return;
        setChatList({ source: resp.source ?? 'endpoint', models: resp.models, defaultModel: resp.default });
        setModel((cur) => keepOrDefault(cur, resp.models, resp.default));
      })
      .catch((e) => {
        console.warn('listChatModels failed; picker disabled', e);
      });
    void blueprintFetch
      .then((resp) => {
        if (cancelled) return;
        setBlueprintList({ source: resp.source ?? 'endpoint', models: resp.models, defaultModel: resp.architect_default });
        setBlueprintModel((cur) => keepOrDefault(cur, resp.models, resp.architect_default));
      })
      .catch((e) => {
        console.warn('listChatModels(architect) failed; picker disabled', e);
      });
    // A list on claude_code means the mode is; with none, it is endpoint or no list follows the mode.
    void Promise.allSettled([chatFetch, blueprintFetch]).then((results) => {
      if (cancelled || seenMode.current !== null) return;
      seenMode.current = results.some((r) => r.status === 'fulfilled' && r.value.source === 'claude_code')
        ? 'claude_code'
        : 'endpoint';
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
