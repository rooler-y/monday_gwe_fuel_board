package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

const unitColumns = `id, unit_number, samsara_vehicle_id, company_id, fuel_level_percent, mpg, def_level_percent, monday_item_id, created_at, updated_at`

// UnitUpsert holds the fields a collector wants to set on a unit. Only
// UnitNumber is required; other fields are optional per-collector (e.g. the
// Samsara collector sets SamsaraVehicleID/FuelLevelPercent/MPG/DEFLevelPercent,
// the DB/Sheets collector sets CompanyID, the Monday publisher sets
// MondayItemID) and existing values are preserved when nil.
type UnitUpsert struct {
	UnitNumber       string
	SamsaraVehicleID *string
	CompanyID        *int64
	FuelLevelPercent *float64
	MPG              *float64
	DEFLevelPercent  *float64
	MondayItemID     *string
}

func UpsertUnit(ctx context.Context, pool *pgxpool.Pool, in UnitUpsert) (*Unit, error) {
	row := pool.QueryRow(ctx, `
		INSERT INTO units (unit_number, samsara_vehicle_id, company_id, fuel_level_percent, mpg, def_level_percent, monday_item_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (unit_number) DO UPDATE SET
			samsara_vehicle_id = COALESCE(EXCLUDED.samsara_vehicle_id, units.samsara_vehicle_id),
			company_id = COALESCE(EXCLUDED.company_id, units.company_id),
			fuel_level_percent = COALESCE(EXCLUDED.fuel_level_percent, units.fuel_level_percent),
			mpg = COALESCE(EXCLUDED.mpg, units.mpg),
			def_level_percent = COALESCE(EXCLUDED.def_level_percent, units.def_level_percent),
			monday_item_id = COALESCE(EXCLUDED.monday_item_id, units.monday_item_id),
			updated_at = now()
		RETURNING `+unitColumns,
		in.UnitNumber, in.SamsaraVehicleID, in.CompanyID, in.FuelLevelPercent, in.MPG, in.DEFLevelPercent, in.MondayItemID)

	return scanUnit(row)
}

func GetUnitByUnitNumber(ctx context.Context, pool *pgxpool.Pool, unitNumber string) (*Unit, error) {
	row := pool.QueryRow(ctx, `SELECT `+unitColumns+` FROM units WHERE unit_number = $1`, unitNumber)
	return scanUnit(row)
}

func GetUnitBySamsaraVehicleID(ctx context.Context, pool *pgxpool.Pool, samsaraVehicleID string) (*Unit, error) {
	row := pool.QueryRow(ctx, `SELECT `+unitColumns+` FROM units WHERE samsara_vehicle_id = $1`, samsaraVehicleID)
	return scanUnit(row)
}

func ListUnits(ctx context.Context, pool *pgxpool.Pool) ([]Unit, error) {
	rows, err := pool.Query(ctx, `SELECT `+unitColumns+` FROM units ORDER BY unit_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var units []Unit
	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, err
		}
		units = append(units, *u)
	}
	return units, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUnit(row rowScanner) (*Unit, error) {
	var u Unit
	if err := row.Scan(&u.ID, &u.UnitNumber, &u.SamsaraVehicleID, &u.CompanyID, &u.FuelLevelPercent, &u.MPG, &u.DEFLevelPercent, &u.MondayItemID, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}
