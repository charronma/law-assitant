import type { ModelOption } from '../../types';
import { groupByTier } from '../../lib/models';

interface ModelSelectProps {
  models: ModelOption[];
  value: string;
  onChange: (id: string) => void;
  /** Models confirmed out of quota: still selectable, but marked and greyed. */
  exhausted: ReadonlySet<string>;
  className?: string;
}

export default function ModelSelect({ models, value, onChange, exhausted, className = '' }: ModelSelectProps) {
  return (
    <select
      aria-label="选择模型"
      value={value}
      onChange={e => onChange(e.target.value)}
      className={`rounded-lg border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 ${className}`}
    >
      {groupByTier(models).map(group => (
        <optgroup key={group.tier} label={group.label}>
          {group.models.map(m => {
            const spent = exhausted.has(m.id);
            return (
              <option key={m.id} value={m.id} className={spent ? 'text-gray-400' : undefined}>
                {spent ? `${m.label}（额度已用完）` : m.label}
              </option>
            );
          })}
        </optgroup>
      ))}
    </select>
  );
}
