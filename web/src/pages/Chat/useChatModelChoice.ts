import { useEffect, useState } from 'react';
import { api } from '../../api/client';
import type { ChatModel } from '../../types';

export interface ChatModelChoice {
  source: 'openrouter' | 'endpoint';
  model: string;
  defaultModel: string;
  models: ChatModel[];
  onChange: (model: string) => void;
}

/**
 * The chat model list and the current pick. A failed fetch leaves the list
 * empty, which hides the endpoint picker, and the create POST omits the model.
 * The pick seeds to the server default so an untouched picker sends what it shows.
 */
export function useChatModelChoice(): ChatModelChoice {
  const [model, setModel] = useState('');
  const [models, setModels] = useState<ChatModel[]>([]);
  const [defaultModel, setDefaultModel] = useState('');
  const [source, setSource] = useState<'openrouter' | 'endpoint'>('endpoint');

  useEffect(() => {
    let cancelled = false;
    void api
      .listChatModels()
      .then((resp) => {
        if (cancelled) return;
        setModels(resp.models);
        setDefaultModel(resp.default);
        setSource(resp.source ?? 'endpoint');
        setModel((cur) => cur || resp.default);
      })
      .catch((e) => {
        console.warn('listChatModels failed; picker disabled', e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return { source, model, defaultModel, models, onChange: setModel };
}
