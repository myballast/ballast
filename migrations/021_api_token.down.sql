-- Reverses 021_api_token.up.sql. The role is dropped last, once nothing it owns
-- or may read remains.

BEGIN;

DROP FUNCTION IF EXISTS api_token_lookup(text);
DROP TABLE IF EXISTS api_token;

REVOKE USAGE ON SCHEMA public FROM ballast_token;

DROP ROLE ballast_token;

COMMIT;
