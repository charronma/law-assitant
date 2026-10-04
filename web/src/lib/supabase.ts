import { createClient } from '@supabase/supabase-js';

/** Local development only: skip login entirely (backend must run with AUTH_DISABLED=true). */
export const authDisabled = import.meta.env.VITE_AUTH_DISABLED === 'true';

const url = import.meta.env.VITE_SUPABASE_URL as string | undefined;
const anonKey = import.meta.env.VITE_SUPABASE_ANON_KEY as string | undefined;

/** `null` when the Supabase env vars are not configured. */
export const supabase = url && anonKey ? createClient(url, anonKey) : null;
