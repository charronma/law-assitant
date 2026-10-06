import { useState, type FormEvent } from 'react';
import { Scale } from 'lucide-react';
import { useAuth } from '../../auth/authContext';

type Mode = 'signin' | 'signup';

/** Map Supabase's English error messages to something readable. */
function describeError(err: unknown): string {
  const msg = err instanceof Error ? err.message : '';
  if (/invalid login credentials/i.test(msg)) return '邮箱或密码错误';
  if (/email not confirmed/i.test(msg)) return '邮箱尚未验证，请先查收验证邮件';
  if (/already registered/i.test(msg)) return '该邮箱已注册，请直接登录';
  if (/password should be at least/i.test(msg)) return '密码太短，至少需要 6 位';
  if (/rate limit/i.test(msg)) return '操作过于频繁，请稍后再试';
  return msg || '操作失败，请稍后重试';
}

export default function LoginPage() {
  const { signIn, signUp } = useAuth();
  const [mode, setMode] = useState<Mode>('signin');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      if (mode === 'signin') {
        await signIn(email.trim(), password);
      } else {
        const needsConfirmation = await signUp(email.trim(), password);
        if (needsConfirmation) {
          setNotice('注册成功，请查收验证邮件，点击链接后再登录。');
          setMode('signin');
        }
      }
    } catch (err) {
      setError(describeError(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="min-h-screen flex flex-col items-center justify-center bg-gray-100 p-6">
      <form onSubmit={handleSubmit} className="w-full max-w-sm bg-white rounded-xl shadow p-6 space-y-4">
        <div className="flex items-center gap-2 justify-center">
          <Scale size={26} className="text-indigo-600" />
          <h1 className="text-xl font-bold text-gray-900">AI 法律助手</h1>
        </div>
        <p className="text-center text-sm text-gray-500">{mode === 'signin' ? '登录以继续' : '创建账号'}</p>

        <label className="block text-sm text-gray-700">
          邮箱
          <input
            type="email"
            required
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="mt-1 w-full rounded-lg border border-gray-300 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-indigo-500"
          />
        </label>
        <label className="block text-sm text-gray-700">
          密码
          <input
            type="password"
            required
            minLength={6}
            autoComplete={mode === 'signin' ? 'current-password' : 'new-password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="mt-1 w-full rounded-lg border border-gray-300 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-indigo-500"
          />
        </label>

        {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
        {notice && <p role="status" className="text-sm text-green-700">{notice}</p>}

        <button
          type="submit"
          disabled={busy}
          className="w-full rounded-lg bg-indigo-600 hover:bg-indigo-500 disabled:opacity-60 text-white py-2 text-sm font-medium transition-colors"
        >
          {busy ? '请稍候…' : mode === 'signin' ? '登录' : '注册'}
        </button>

        <button
          type="button"
          onClick={() => {
            setMode(mode === 'signin' ? 'signup' : 'signin');
            setError(null);
            setNotice(null);
          }}
          className="w-full text-sm text-indigo-600 hover:underline"
        >
          {mode === 'signin' ? '没有账号？注册' : '已有账号？登录'}
        </button>
      </form>
      <p className="mt-4 max-w-sm text-center text-xs leading-5 text-gray-400">
        本服务由 AI 生成内容，仅供参考，不构成法律意见。你的对话与上传文件会按账号保存，并发送至第三方大模型服务处理，请勿提交不必要的敏感个人信息。
      </p>
    </div>
  );
}
