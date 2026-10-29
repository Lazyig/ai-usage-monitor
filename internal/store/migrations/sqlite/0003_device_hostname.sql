-- Operator-given names must survive agent heartbeats.
--
-- Agents report their hostname with every ingest; until now that overwrote the
-- name chosen when the device was registered. `name` is now owned by the
-- operator (POST /devices, PATCH /devices/{id}); the reported hostname is kept
-- separately for display.
ALTER TABLE devices ADD COLUMN hostname TEXT NOT NULL DEFAULT '';
