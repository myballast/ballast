-- Reverses 017_catalog_endpoint.up.sql. The endpoints imported into catalogs are
-- dropped with their table; scenarios made from them are untouched, since a
-- scenario copies what it needs.

BEGIN;

DROP TABLE IF EXISTS catalog_endpoint;

COMMIT;
