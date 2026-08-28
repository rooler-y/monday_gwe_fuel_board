package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DriverUpsert holds fields to write on a driver. All fields are optional
// pointers except where a function's doc says otherwise; nil means "leave
// unchanged" on update paths.
type DriverUpsert struct {
	SamsaraDriverID *string
	DriverName      *string
	PhoneNumber     *string
	CompanyID       *int64
	UnitID          *int64
	LoadNumber      *string
	Destination     *string
}

const driverColumns = `id, samsara_driver_id, driver_name, phone_number, company_id, unit_id, load_number, destination, created_at, updated_at`

// CreateDriver inserts a new driver row. DriverName must be set.
func CreateDriver(ctx context.Context, pool *pgxpool.Pool, in DriverUpsert) (*Driver, error) {
	row := pool.QueryRow(ctx, `
		INSERT INTO drivers (samsara_driver_id, driver_name, phone_number, company_id, unit_id, load_number, destination)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+driverColumns,
		in.SamsaraDriverID, in.DriverName, in.PhoneNumber, in.CompanyID, in.UnitID, in.LoadNumber, in.Destination)
	return scanDriver(row)
}

// UpdateDriver partially updates an existing driver by id; nil fields in in are left unchanged.
func UpdateDriver(ctx context.Context, pool *pgxpool.Pool, id int64, in DriverUpsert) (*Driver, error) {
	row := pool.QueryRow(ctx, `
		UPDATE drivers SET
			samsara_driver_id = COALESCE($1, samsara_driver_id),
			driver_name = COALESCE($2, driver_name),
			phone_number = COALESCE($3, phone_number),
			company_id = COALESCE($4, company_id),
			unit_id = COALESCE($5, unit_id),
			load_number = COALESCE($6, load_number),
			destination = COALESCE($7, destination),
			updated_at = now()
		WHERE id = $8
		RETURNING `+driverColumns,
		in.SamsaraDriverID, in.DriverName, in.PhoneNumber, in.CompanyID, in.UnitID, in.LoadNumber, in.Destination, id)
	return scanDriver(row)
}

// UpsertDriverBySamsaraDriverID creates or updates a driver keyed on their Samsara driver id.
// SamsaraDriverID and DriverName must be set.
func UpsertDriverBySamsaraDriverID(ctx context.Context, pool *pgxpool.Pool, in DriverUpsert) (*Driver, error) {
	row := pool.QueryRow(ctx, `
		INSERT INTO drivers (samsara_driver_id, driver_name, phone_number, company_id, unit_id, load_number, destination)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (samsara_driver_id) DO UPDATE SET
			driver_name = COALESCE(EXCLUDED.driver_name, drivers.driver_name),
			phone_number = COALESCE(EXCLUDED.phone_number, drivers.phone_number),
			company_id = COALESCE(EXCLUDED.company_id, drivers.company_id),
			unit_id = COALESCE(EXCLUDED.unit_id, drivers.unit_id),
			load_number = COALESCE(EXCLUDED.load_number, drivers.load_number),
			destination = COALESCE(EXCLUDED.destination, drivers.destination),
			updated_at = now()
		RETURNING `+driverColumns,
		in.SamsaraDriverID, in.DriverName, in.PhoneNumber, in.CompanyID, in.UnitID, in.LoadNumber, in.Destination)
	return scanDriver(row)
}

func GetDriverByName(ctx context.Context, pool *pgxpool.Pool, name string) (*Driver, error) {
	row := pool.QueryRow(ctx, `SELECT `+driverColumns+` FROM drivers WHERE driver_name = $1`, name)
	return scanDriver(row)
}

func GetDriverBySamsaraDriverID(ctx context.Context, pool *pgxpool.Pool, samsaraDriverID string) (*Driver, error) {
	row := pool.QueryRow(ctx, `SELECT `+driverColumns+` FROM drivers WHERE samsara_driver_id = $1`, samsaraDriverID)
	return scanDriver(row)
}

// GetDriverByUnitID returns the driver currently linked to a unit (the most
// recently updated one, in case more than one driver row still points at
// this unit_id — the DB collector never clears unit_id when a driver is
// reassigned away with nobody replacing them, so a stale link can persist).
// Returns pgx.ErrNoRows if no driver is linked to this unit.
func GetDriverByUnitID(ctx context.Context, pool *pgxpool.Pool, unitID int64) (*Driver, error) {
	row := pool.QueryRow(ctx, `
		SELECT `+driverColumns+` FROM drivers WHERE unit_id = $1 ORDER BY updated_at DESC LIMIT 1
	`, unitID)
	return scanDriver(row)
}

func scanDriver(row rowScanner) (*Driver, error) {
	var d Driver
	if err := row.Scan(&d.ID, &d.SamsaraDriverID, &d.DriverName, &d.PhoneNumber, &d.CompanyID, &d.UnitID, &d.LoadNumber, &d.Destination, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}
