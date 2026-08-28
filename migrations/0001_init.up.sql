CREATE TABLE companies (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE units (
    id                  BIGSERIAL PRIMARY KEY,
    unit_number         TEXT NOT NULL UNIQUE,
    samsara_vehicle_id  TEXT UNIQUE,
    company_id          BIGINT REFERENCES companies(id),
    fuel_level_percent  NUMERIC(5,2),
    mpg                 NUMERIC(6,2),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE drivers (
    id                 BIGSERIAL PRIMARY KEY,
    samsara_driver_id  TEXT UNIQUE,
    driver_name        TEXT NOT NULL,
    phone_number       TEXT,
    company_id         BIGINT REFERENCES companies(id),
    unit_id            BIGINT REFERENCES units(id),
    load_number        TEXT,
    destination        TEXT,
    monday_item_id     TEXT UNIQUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_drivers_unit_id ON drivers(unit_id);
CREATE INDEX idx_units_company_id ON units(company_id);
CREATE INDEX idx_drivers_company_id ON drivers(company_id);
