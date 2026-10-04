import type { ReactNode } from 'react';
import { useAuth } from './authContext';
import LoginPage from '../components/auth/LoginPage';

/** Renders children only for signed-in users; everyone else sees the login page. */
export default function AuthGate({ children }: { children: ReactNode }) {
  const { user, loading, misconfigured } = useAuth();

  if (misconfigured) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 p-6">
        <div className="max-w-md bg-white rounded-xl shadow p-6 text-sm text-gray-700 space-y-2">
          <h1 className="text-lg font-bold text-gray-900">尚未配置登录</h1>
          <p>
            请设置环境变量 <code>VITE_SUPABASE_URL</code> 和 <code>VITE_SUPABASE_ANON_KEY</code>
            （参考 <code>web/.env.example</code>）。
          </p>
          <p>本地开发可设置 <code>VITE_AUTH_DISABLED=true</code> 跳过登录（后端需同时设置 <code>AUTH_DISABLED=true</code>）。</p>
        </div>
      </div>
    );
  }

  if (loading) {
    return <div className="min-h-screen flex items-center justify-center bg-gray-100 text-gray-500">加载中…</div>;
  }

  return user ? <>{children}</> : <LoginPage />;
}
