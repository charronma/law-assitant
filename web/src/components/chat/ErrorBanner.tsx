import { useState } from 'react';
import { TriangleAlert, X } from 'lucide-react';
import type { ChatError, ModelOption } from '../../types';
import { nextAvailableModel } from '../../lib/models';
import ModelSelect from './ModelSelect';

interface ErrorBannerProps {
  error: ChatError;
  models: ModelOption[];
  /** The model currently selected in the header. */
  selected: string;
  exhausted: ReadonlySet<string>;
  onDismiss: () => void;
  onSelectModel: (id: string) => void;
  /** Resend the last message, optionally with a different model. */
  onRetry: (modelId?: string) => void;
}

const BUTTON = 'rounded-lg px-3 py-1.5 text-sm font-medium transition-colors';

export default function ErrorBanner({
  error,
  models,
  selected,
  exhausted,
  onDismiss,
  onSelectModel,
  onRetry,
}: ErrorBannerProps) {
  const [picking, setPicking] = useState(false);

  const failedId = error.model ?? selected;
  const failedLabel = models.find(m => m.id === failedId)?.label ?? failedId;

  let title: string;
  let hint: string | null = null;
  let tone: 'danger' | 'warning' = 'danger';
  let actions: React.ReactNode = null;

  switch (error.code) {
    case 'QUOTA_EXHAUSTED': {
      const next = nextAvailableModel(models, failedId, exhausted);
      title = `当前模型「${failedLabel}」的免费额度已用完`;
      hint = '你的消息已保留，换一个模型即可继续，无需重新输入。';
      actions = (
        <>
          <button
            type="button"
            onClick={() => setPicking(p => !p)}
            className={`${BUTTON} border border-red-300 bg-white text-red-700 hover:bg-red-100`}
          >
            切换模型
          </button>
          {next ? (
            <>
              <button
                type="button"
                onClick={() => onRetry(next.id)}
                className={`${BUTTON} bg-red-600 text-white hover:bg-red-700`}
              >
                换个模型重试
              </button>
              <span className="text-xs text-red-700">将使用「{next.label}」</span>
            </>
          ) : (
            <span className="text-xs font-medium text-red-700">所有模型的额度都已用完，请稍后再试或联系管理员</span>
          )}
        </>
      );
      break;
    }
    case 'INVALID_API_KEY':
      title = '服务端 API Key 配置错误，请联系管理员';
      break;
    case 'RATE_LIMITED':
      tone = 'warning';
      title = '请求太频繁，请稍后再试';
      actions = (
        <button
          type="button"
          onClick={() => onRetry()}
          className={`${BUTTON} bg-amber-600 text-white hover:bg-amber-700`}
        >
          重试
        </button>
      );
      break;
    case 'INVALID_MODEL':
      title = '所选模型已不可用，请重新选择';
      actions = (
        <button
          type="button"
          onClick={() => setPicking(p => !p)}
          className={`${BUTTON} border border-red-300 bg-white text-red-700 hover:bg-red-100`}
        >
          切换模型
        </button>
      );
      break;
    case 'UNAUTHORIZED':
      title = error.message;
      break;
    default:
      title = error.message;
      actions = (
        <button
          type="button"
          onClick={() => onRetry()}
          className={`${BUTTON} bg-red-600 text-white hover:bg-red-700`}
        >
          重试
        </button>
      );
  }

  const palette =
    tone === 'warning'
      ? 'border-amber-400 bg-amber-50 text-amber-900'
      : 'border-red-400 bg-red-50 text-red-900';
  const iconColor = tone === 'warning' ? 'text-amber-600' : 'text-red-600';

  return (
    <div role="alert" className={`mx-4 mt-3 rounded-xl border-2 p-4 shadow-sm ${palette}`}>
      <div className="flex items-start gap-3">
        <TriangleAlert size={22} className={`mt-0.5 shrink-0 ${iconColor}`} aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <p className="font-semibold">{title}</p>
          {hint && <p className="mt-0.5 text-sm">{hint}</p>}
          {actions && <div className="mt-3 flex flex-wrap items-center gap-2">{actions}</div>}
          {picking && models.length > 0 && (
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <ModelSelect models={models} value={selected} onChange={onSelectModel} exhausted={exhausted} />
              <button
                type="button"
                onClick={() => onRetry(selected)}
                className={`${BUTTON} bg-red-600 text-white hover:bg-red-700`}
              >
                用所选模型重试
              </button>
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={onDismiss}
          aria-label="关闭提示"
          className="shrink-0 rounded p-1 hover:bg-black/5"
        >
          <X size={18} />
        </button>
      </div>
    </div>
  );
}
