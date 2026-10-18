-- Reverses 013_invitations.up.sql. The ballast_invite role is left in place, as
-- ballast_auth is by 003's down: roles belong to the cluster, not to one schema.

BEGIN;

DROP FUNCTION IF EXISTS invitation_lookup(text);
DROP TABLE IF EXISTS invitation;

COMMIT;
