import { createContext, useContext } from 'react';

export interface AuthUser {
  email: string;
}

export interface AuthState {
  user: AuthUser | null;
  /** True until the persisted session (if any) has been restored. */
  loading: boolean;
  /** True when neither Supabase nor the dev bypass is configured. */
  misconfigured: boolean;
  signIn: (email: string, password: string) => Promise<void>;
  /** Resolves `true` when the user must confirm their email before signing in. */
  signUp: (email: string, password: string) => Promise<boolean>;
  signOut: () => Promise<void>;
}

export const AuthContext = createContext<AuthState | null>(null);

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within <AuthProvider>');
  return ctx;
}
