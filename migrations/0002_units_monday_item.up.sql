ALTER TABLE units ADD COLUMN monday_item_id TEXT UNIQUE;
ALTER TABLE drivers DROP COLUMN monday_item_id;
