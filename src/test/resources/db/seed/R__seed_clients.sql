-- R__seed_clients.sql
-- Test-only client registry. The X-Client-Id header is now mandatory, so tests
-- must register every client id they exercise. Production deployments register
-- their own clients out-of-band.

INSERT INTO clients (id, name) VALUES ('test-client', 'Test Client') ON CONFLICT (id) DO NOTHING;
INSERT INTO clients (id, name) VALUES ('client-1',    'Client 1')    ON CONFLICT (id) DO NOTHING;
INSERT INTO clients (id, name) VALUES ('client-2',    'Client 2')    ON CONFLICT (id) DO NOTHING;
