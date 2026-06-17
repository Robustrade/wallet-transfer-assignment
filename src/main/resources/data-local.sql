-- Demo seed data — only applied in the local profile (H2 in-memory)
insert into wallets (id, balance, created_at, updated_at)
values
  ('wallet_1', 1000, now(), now()),
  ('wallet_2', 0,    now(), now()),
  ('wallet_3', 500,  now(), now());
