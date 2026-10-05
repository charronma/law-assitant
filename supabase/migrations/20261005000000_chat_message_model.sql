-- Record which model produced each turn.
--
-- Additive and backward compatible: the column is nullable, existing rows keep
-- NULL, and the previous backend (which never sends it) keeps working. Apply
-- this BEFORE deploying a backend that sends `model`, otherwise inserts are
-- rejected with "Could not find the 'model' column".
--
-- Table-level grants and RLS policies already cover new columns, so nothing
-- else changes.
alter table public.chat_messages
  add column model text check (model is null or char_length(model) <= 100);
