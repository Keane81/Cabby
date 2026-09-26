create table cabber (
  id            uuid primary key default gen_random_uuid(),
  name          text not null check (name = btrim(name) and length(name) between 1 and 64),
  email         text not null check (length(email) <= 254),
  password_hash text not null,
  created_at    timestamptz not null default now()
);

create unique index cabber_email_key on cabber (email);

create table cabber_session (
  id           uuid primary key default gen_random_uuid(),
  token_hash   bytea not null unique,
  cabber_id    uuid not null references cabber (id) on delete cascade,
  created_at   timestamptz not null default now(),
  expires_at   timestamptz not null,
  last_seen_at timestamptz not null default now(),
  revoked_at   timestamptz
);

create index cabber_session_active on cabber_session (cabber_id) where revoked_at is null;
create index cabber_session_expiry on cabber_session (expires_at);
