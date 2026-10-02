create table cabber_location (
  id          bigint generated always as identity primary key,
  cabber_id   uuid not null,
  latitude    double precision not null check (latitude between -90 and 90),
  longitude   double precision not null check (longitude between -180 and 180),
  received_at timestamptz not null
);

create index cabber_location_cabber_received on cabber_location (cabber_id, received_at desc, id desc);
