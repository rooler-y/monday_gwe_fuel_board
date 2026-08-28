ALTER TABLE drivers ADD COLUMN monday_item_id TEXT UNIQUE;
ALTER TABLE units DROP COLUMN monday_item_id;
