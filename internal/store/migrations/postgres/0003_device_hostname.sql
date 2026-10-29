-- Agent-reported hostname, kept apart from the operator-owned name. See
-- ../sqlite/0003_device_hostname.sql.
ALTER TABLE devices ADD COLUMN hostname TEXT COLLATE "C" NOT NULL DEFAULT '';
