-- The tier selected by the client, not the tier actually served or billed.
-- Existing/older agents have no evidence of a tier; do not assume Standard.
ALTER TABLE usage_events ADD COLUMN service_tier TEXT COLLATE "C" NOT NULL DEFAULT '';
