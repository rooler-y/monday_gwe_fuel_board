-- "Fuel Board 2.0" (a separate Monday board, id in MONDAY_SECONDARY_BOARD_ID):
-- items there are created and deleted by the other side, not by us. Each
-- item carries a Samsara vehicle ID they enter themselves; our only job is
-- to read it and write back the live fuel level. Keyed by monday_item_id
-- (not unit_number/samsara_vehicle_id) since the item, not the truck, is
-- the thing whose lifecycle we're tracking.
CREATE TABLE secondary_board_units (
    id                  BIGSERIAL PRIMARY KEY,
    monday_item_id      TEXT NOT NULL UNIQUE,
    samsara_vehicle_id  TEXT NOT NULL,
    fuel_level_percent  NUMERIC(5,2),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
