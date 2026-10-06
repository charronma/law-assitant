import { Component, type ErrorInfo, type ReactNode } from 'react';

interface State {
  error: Error | null;
}

/** Keeps a render error from blanking the whole app: shows a recovery screen instead. */
export default class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('UI crashed:', error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert" className="min-h-screen flex flex-col items-center justify-center gap-3 bg-gray-100 p-6 text-center">
        <p className="text-lg font-semibold text-gray-800">页面出错了</p>
        <p className="max-w-sm text-sm text-gray-500">你的对话已保存，刷新页面即可继续。如果反复出现，请联系管理员。</p>
        <button
          onClick={() => window.location.reload()}
          className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500"
        >
          刷新页面
        </button>
      </div>
    );
  }
}
