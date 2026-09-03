-- Matches a Fuel Board 2.0 item to our own unit registry by unit number
-- (the item's Name — confirmed live, e.g. item "GL1286"), so Driver/Phone/
-- Load ID can be pulled from whichever collector (target DB or Sheets) fed
-- that unit, regardless of source.
ALTER TABLE secondary_board_units ADD COLUMN unit_number TEXT;
