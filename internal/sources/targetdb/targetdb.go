// Package targetdb reads (read-only) from the external dispatch system's
// Postgres database — trucks, drivers, loads, waypoints, places — scoped to
// one company/tenant.
package targetdb

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Row is one truck (unit) in scope, joined to its currently assigned
// actively-employed driver — trucks with no such driver are excluded
// entirely (see the INNER JOIN in FetchUnitsAndDrivers), so DriverFirstName/
// DriverLastName/DriverPhone are effectively always present here. Load/
// destination stay nullable since a driver might not have a current load.
//
// Deliberately does NOT include trucks.provider_vehicle_id: it turned out
// not to be unique in this DB (confirmed live — e.g. two different
// truck_numbers, including what looks like the same physical unit recorded
// twice under two different truck_number spellings, sharing one
// provider_vehicle_id), so it can't safely feed units.samsara_vehicle_id
// (UNIQUE). samsara_vehicle_id is populated solely by the Samsara collector's
// own live vehicle-name matching (internal/collect/samsara_match.go).
type Row struct {
	UnitNumber string

	DriverFirstName *string
	DriverLastName  *string
	DriverPhone     *string

	LoadNumber  *string
	Destination *string // "city, state zip", not a full street address
}

func FetchCompanyName(ctx context.Context, pool *pgxpool.Pool, companyID int64) (string, error) {
	var name string
	err := pool.QueryRow(ctx, `SELECT name FROM companies WHERE id = $1`, companyID).Scan(&name)
	return name, err
}

// FetchUnitsAndDrivers returns one row per non-deleted truck belonging to
// companyID that currently has an actively-employed driver assigned
// (trucks with no such driver are excluded entirely — not returned as an
// empty-driver row), with that driver's current load number and the load's
// current waypoint location as "city, state zip" (Load.current_waypoint_id
// is the dispatch system's own "where this load is headed right now"
// pointer).
func FetchUnitsAndDrivers(ctx context.Context, pool *pgxpool.Pool, companyID int64) ([]Row, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			t.truck_number,
			u.first_name,
			u.last_name,
			u.phone,
			l.load_number,
			NULLIF(concat_ws(' ', concat_ws(', ', p.city, p.state), p.postal_code), '') AS destination
		FROM trucks t
		JOIN drivers d ON d.current_truck_id = t.id
			AND d.is_deleted = false
			AND d.employment_status = 'active'
		JOIN users u ON u.id = d.user_id AND u.is_deleted = false
		LEFT JOIN loads l ON l.id = d.current_load_id AND l.is_deleted = false
		LEFT JOIN waypoints w ON w.id = l.current_waypoint_id AND w.is_deleted = false
		LEFT JOIN places p ON p.id = w.place_id AND p.is_deleted = false
		WHERE t.is_deleted = false AND t.company_id = $1
		ORDER BY t.truck_number
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.UnitNumber, &r.DriverFirstName, &r.DriverLastName, &r.DriverPhone, &r.LoadNumber, &r.Destination); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
