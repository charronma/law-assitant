import type { ModelOption } from '../types';

const STORAGE_KEY = 'law-assistant:model';

/** Display order and Chinese names of the tiers. */
export const TIER_GROUPS: { tier: string; label: string }[] = [
  { tier: 'flagship', label: '旗舰' },
  { tier: 'standard', label: '标准' },
  { tier: 'fast', label: '快速' },
];

/** Group models by tier, keeping the server's order inside each group. Unknown tiers count as "standard". */
export function groupByTier(models: ModelOption[]): { tier: string; label: string; models: ModelOption[] }[] {
  const known = new Set(TIER_GROUPS.map(g => g.tier));
  return TIER_GROUPS.map(g => ({
    ...g,
    models: models.filter(m => (known.has(m.tier) ? m.tier : 'standard') === g.tier),
  })).filter(g => g.models.length > 0);
}

/**
 * The model to fall back to after `failedId` ran out of quota: the next one in
 * list order (wrapping around) that is not known to be exhausted.
 */
export function nextAvailableModel(
  models: ModelOption[],
  failedId: string,
  exhausted: ReadonlySet<string>,
): ModelOption | undefined {
  if (models.length === 0) return undefined;
  const start = models.findIndex(m => m.id === failedId);
  for (let step = 1; step <= models.length; step++) {
    const candidate = models[(start + step + models.length) % models.length];
    if (candidate.id !== failedId && !exhausted.has(candidate.id)) return candidate;
  }
  return undefined;
}

// localStorage can be missing, blocked, or throw (private windows, quota, ...):
// every access is guarded and the app must work without it.
export function readStoredModel(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

export function writeStoredModel(id: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, id);
  } catch {
    /* ignore */
  }
}
