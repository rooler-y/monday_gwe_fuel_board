// Package targetdb reads (read-only) from the external dispatch system's
// Postgres database — trucks, drivers, loads, waypoints, places — scoped to
// one company/tenant.
package targetdb

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Row is one truck (unit) in scope, left-joined with whoever is currently
// assigned to it and what they're doing. Driver/load/destination fields are
// nil when the truck currently has no active driver assigned.
type Row struct {
	UnitNumber        string
	ProviderVehicleID *string

	DriverFirstName *string
	DriverLastName  *string
	DriverPhone     *string

	LoadNumber  *string
	Destination *string
}

func FetchCompanyName(ctx context.Context, pool *pgxpool.Pool, companyID int64) (string, error) {
	var name string
	err := pool.QueryRow(ctx, `SELECT name FROM companies WHERE id = $1`, companyID).Scan(&name)
	return name, err
}

// FetchUnitsAndDrivers returns one row per non-deleted truck belonging to
// companyID, with its currently assigned active driver (if any), that
// driver's current load number, and the load's current waypoint location
// (Load.current_waypoint_id is the dispatch system's own "where this load is
// headed right now" pointer).
func FetchUnitsAndDrivers(ctx context.Context, pool *pgxpool.Pool, companyID int64) ([]Row, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			t.truck_number,
			t.provider_vehicle_id,
			u.first_name,
			u.last_name,
			u.phone,
			l.load_number,
			COALESCE(p.name, NULLIF(concat_ws(', ', p.city, p.state), '')) AS destination
		FROM trucks t
		LEFT JOIN drivers d ON d.current_truck_id = t.id
			AND d.is_deleted = false
			AND d.employment_status = 'active'
		LEFT JOIN users u ON u.id = d.user_id AND u.is_deleted = false
		LEFT JOIN loads l ON l.id = d.current_load_id AND l.is_deleted = false
		LEFT JOIN waypoints w ON w.id = l.current_waypoint_id AND w.is_deleted = false
		LEFT JOIN places p ON p.id = w.place_id AND p.is_deleted = false
		WHERE t.is_deleted = false AND t.company_id = $1
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.UnitNumber, &r.ProviderVehicleID, &r.DriverFirstName, &r.DriverLastName, &r.DriverPhone, &r.LoadNumber, &r.Destination); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
