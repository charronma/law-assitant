-- Chat persistence for the law assistant.
--
-- The Go backend talks to PostgREST with the *signed-in user's own JWT* (plus
-- the public publishable key), so every statement runs as the `authenticated`
-- role and Row Level Security is the last line of defence: a user can only
-- ever see or change rows whose user_id is their own auth.uid().

create table public.chat_sessions (
  id         uuid primary key default gen_random_uuid(),
  user_id    uuid not null default auth.uid() references auth.users (id) on delete cascade,
  module     text not null check (module in
               ('consult', 'pleading', 'contract', 'evidence_org', 'evidence', 'communication')),
  title      text not null check (char_length(title) <= 200),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table public.chat_messages (
  id         uuid primary key default gen_random_uuid(),
  session_id uuid not null references public.chat_sessions (id) on delete cascade,
  user_id    uuid not null default auth.uid() references auth.users (id) on delete cascade,
  role       text not null check (role in ('user', 'assistant')),
  content    text not null,
  file_ids   text[] not null default '{}',
  created_at timestamptz not null default now()
);

create index chat_sessions_user_updated_idx on public.chat_sessions (user_id, updated_at desc);
create index chat_messages_session_created_idx on public.chat_messages (session_id, created_at, id);
create index chat_messages_user_idx on public.chat_messages (user_id);

-- Keep the session's updated_at fresh and derive its title from the first user
-- message (first 20 characters), mirroring the in-memory store's behaviour.
create function public.touch_chat_session() returns trigger
language plpgsql
set search_path = ''
as $$
begin
  update public.chat_sessions s
     set updated_at = now(),
         title = case
           when new.role = 'user'
                and (select count(*) from public.chat_messages m where m.session_id = new.session_id) = 1
           then left(new.content, 20) || case when char_length(new.content) > 20 then '...' else '' end
           else s.title
         end
   where s.id = new.session_id;
  return new;
end;
$$;

revoke all on function public.touch_chat_session() from public, anon, authenticated;

create trigger chat_messages_touch_session
  after insert on public.chat_messages
  for each row execute function public.touch_chat_session();

-- Row Level Security
alter table public.chat_sessions enable row level security;
alter table public.chat_messages enable row level security;

-- Privileges: signed-in users only; anonymous visitors get nothing.
revoke all on public.chat_sessions, public.chat_messages from anon, authenticated;
grant select, insert, delete on public.chat_sessions to authenticated;
grant update (title, updated_at) on public.chat_sessions to authenticated;
grant select, insert on public.chat_messages to authenticated;

create policy "own sessions: select" on public.chat_sessions
  for select to authenticated using (user_id = (select auth.uid()));
create policy "own sessions: insert" on public.chat_sessions
  for insert to authenticated with check (user_id = (select auth.uid()));
create policy "own sessions: update" on public.chat_sessions
  for update to authenticated
  using (user_id = (select auth.uid())) with check (user_id = (select auth.uid()));
create policy "own sessions: delete" on public.chat_sessions
  for delete to authenticated using (user_id = (select auth.uid()));

create policy "own messages: select" on public.chat_messages
  for select to authenticated using (user_id = (select auth.uid()));
-- A message may only be added to a session the user owns.
create policy "own messages: insert" on public.chat_messages
  for insert to authenticated with check (
    user_id = (select auth.uid())
    and exists (
      select 1 from public.chat_sessions s
      where s.id = session_id and s.user_id = (select auth.uid())
    )
  );
