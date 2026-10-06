-- Uploaded documents: originals in a private Storage bucket, metadata and the
-- extracted text in Postgres. As with chat data, the Go backend acts with the
-- signed-in user's own JWT, so Row Level Security is the last line of defence.

create table public.uploaded_files (
  id             uuid primary key default gen_random_uuid(),
  user_id        uuid not null default auth.uid() references auth.users (id) on delete cascade,
  -- Uploads usually happen before the first message, so the session is optional.
  session_id     uuid references public.chat_sessions (id) on delete set null,
  filename       text not null check (char_length(filename) between 1 and 255),
  content_type   text not null default '',
  size           bigint not null check (size >= 0),
  storage_path   text not null,
  extracted_text text not null,
  created_at     timestamptz not null default now()
);

create index uploaded_files_user_idx on public.uploaded_files (user_id, created_at desc);
create index uploaded_files_session_idx on public.uploaded_files (session_id) where session_id is not null;

alter table public.uploaded_files enable row level security;

revoke all on public.uploaded_files from anon, authenticated;
grant select, insert, delete on public.uploaded_files to authenticated;

create policy "own files: select" on public.uploaded_files
  for select to authenticated using (user_id = (select auth.uid()));
-- A file may only point at a storage object in the user's own folder, and at a
-- session the user owns.
create policy "own files: insert" on public.uploaded_files
  for insert to authenticated
  with check (
    user_id = (select auth.uid())
    and (storage.foldername(storage_path))[1] = (select auth.uid())::text
    and (session_id is null or exists (
          select 1 from public.chat_sessions s
           where s.id = session_id and s.user_id = (select auth.uid())))
  );
create policy "own files: delete" on public.uploaded_files
  for delete to authenticated using (user_id = (select auth.uid()));

-- Private bucket, 20 MB per object (matches the server's upload limit).
insert into storage.buckets (id, name, public, file_size_limit)
values ('uploads', 'uploads', false, 20971520)
on conflict (id) do nothing;

-- Objects live under "<user id>/..."; users only see and manage their own folder.
create policy "own uploads: select" on storage.objects
  for select to authenticated
  using (bucket_id = 'uploads' and (storage.foldername(name))[1] = (select auth.uid())::text);
create policy "own uploads: insert" on storage.objects
  for insert to authenticated
  with check (bucket_id = 'uploads' and (storage.foldername(name))[1] = (select auth.uid())::text);
create policy "own uploads: delete" on storage.objects
  for delete to authenticated
  using (bucket_id = 'uploads' and (storage.foldername(name))[1] = (select auth.uid())::text);
