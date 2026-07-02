INSERT INTO wallets (wallet_id, balance, version, created_at, updated_at)
SELECT 'wallet_1', 100000, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM wallets WHERE wallet_id = 'wallet_1');

INSERT INTO wallets (wallet_id, balance, version, created_at, updated_at)
SELECT 'wallet_2', 50000, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM wallets WHERE wallet_id = 'wallet_2');

INSERT INTO wallets (wallet_id, balance, version, created_at, updated_at)
SELECT 'wallet_3', 0, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM wallets WHERE wallet_id = 'wallet_3');
