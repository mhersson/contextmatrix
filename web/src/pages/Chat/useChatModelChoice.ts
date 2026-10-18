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

export interface ChatModelChoices {
  chat: ChatModelChoice;
  blueprint: ChatModelChoice;
}

/**
 * The model list and two independent picks, one for chats and one for
 * blueprints, from a single fetch. A failed fetch leaves the list empty, which
 * hides the endpoint picker, and the create POST omits the model.
 * Each pick seeds to its server default so an untouched picker sends what it shows.
 */
export function useChatModelChoice(): ChatModelChoices {
  const [model, setModel] = useState('');
  const [blueprintModel, setBlueprintModel] = useState('');
  const [models, setModels] = useState<ChatModel[]>([]);
  const [defaultModel, setDefaultModel] = useState('');
  const [architectDefault, setArchitectDefault] = useState('');
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
        setArchitectDefault(resp.architect_default);
        setModel((cur) => cur || resp.default);
        setBlueprintModel((cur) => cur || resp.architect_default);
      })
      .catch((e) => {
        console.warn('listChatModels failed; picker disabled', e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return {
    chat: { source, model, defaultModel, models, onChange: setModel },
    blueprint: {
      source,
      model: blueprintModel,
      defaultModel: architectDefault,
      models,
      onChange: setBlueprintModel,
    },
  };
}
