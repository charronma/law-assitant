import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { authDisabled, supabase } from '../lib/supabase';
import { AuthContext, type AuthState, type AuthUser } from './authContext';

const DEV_USER: AuthUser = { email: 'dev（已关闭鉴权）' };

export default function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(authDisabled ? DEV_USER : null);
  const [loading, setLoading] = useState(!authDisabled && supabase !== null);

  useEffect(() => {
    if (authDisabled || !supabase) return;
    let active = true;

    supabase.auth.getSession().then(({ data }) => {
      if (!active) return;
      setUser(data.session?.user.email ? { email: data.session.user.email } : null);
      setLoading(false);
    });

    // Fires on sign-in/out, token refresh, and sign-out from another tab.
    const { data: sub } = supabase.auth.onAuthStateChange((_event, session) => {
      setUser(session?.user.email ? { email: session.user.email } : null);
    });

    return () => {
      active = false;
      sub.subscription.unsubscribe();
    };
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    if (!supabase) throw new Error('未配置 Supabase');
    const { error } = await supabase.auth.signInWithPassword({ email, password });
    if (error) throw error;
  }, []);

  const signUp = useCallback(async (email: string, password: string) => {
    if (!supabase) throw new Error('未配置 Supabase');
    const { data, error } = await supabase.auth.signUp({ email, password });
    if (error) throw error;
    return data.session === null;
  }, []);

  const signOut = useCallback(async () => {
    if (!supabase) return;
    await supabase.auth.signOut();
  }, []);

  const value = useMemo<AuthState>(
    () => ({
      user,
      loading,
      misconfigured: !authDisabled && supabase === null,
      signIn,
      signUp,
      signOut,
    }),
    [user, loading, signIn, signUp, signOut],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
