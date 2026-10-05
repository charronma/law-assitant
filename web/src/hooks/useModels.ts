import { useCallback, useEffect, useState } from 'react';
import type { ModelOption } from '../types';
import { getModels } from '../services/api';
import { readStoredModel, writeStoredModel } from '../lib/models';

/** Loads the selectable models and tracks the user's choice and which models ran out of quota. */
export function useModels() {
  const [models, setModels] = useState<ModelOption[]>([]);
  const [defaultId, setDefaultId] = useState('');
  const [preferred, setPreferred] = useState<string | null>(readStoredModel);
  // Models confirmed out of quota during this page session (not persisted: quotas reset).
  const [exhausted, setExhausted] = useState<ReadonlySet<string>>(() => new Set());

  useEffect(() => {
    let active = true;
    getModels()
      .then(list => {
        if (!active) return;
        setModels(list.models);
        setDefaultId(list.default);
      })
      .catch(() => {
        /* no selector; requests then use the server's default model */
      });
    return () => {
      active = false;
    };
  }, []);

  // A stored choice that is no longer offered falls back to the server default.
  const selected = preferred && models.some(m => m.id === preferred) ? preferred : defaultId;

  const select = useCallback((id: string) => {
    setPreferred(id);
    writeStoredModel(id);
  }, []);

  const markExhausted = useCallback((id: string) => {
    setExhausted(prev => (prev.has(id) ? prev : new Set(prev).add(id)));
  }, []);

  return { models, selected, select, exhausted, markExhausted };
}
