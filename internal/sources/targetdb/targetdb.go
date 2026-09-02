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

	LoadNumber *string
	// Route is the load's full waypoint sequence as "city, state => city,
	// state => ...", in stop order — not just the current/next waypoint.
	Route *string
}

func FetchCompanyName(ctx context.Context, pool *pgxpool.Pool, companyID int64) (string, error) {
	var name string
	err := pool.QueryRow(ctx, `SELECT name FROM companies WHERE id = $1`, companyID).Scan(&name)
	return name, err
}

// FetchUnitsAndDrivers returns one row per non-deleted truck belonging to
// companyID that currently has an actively-employed driver on a "company"
// contract (company_cpm or company_percentage — lease/owner-operator
// drivers are excluded, per the user) assigned (trucks with no such driver
// are excluded entirely — not returned as an empty-driver row), with that
// driver's current load number and the load's full waypoint sequence
// (every non-deleted waypoint, in stop order, as "city, state" chained by
// " => ") — not just the current/next stop.
func FetchUnitsAndDrivers(ctx context.Context, pool *pgxpool.Pool, companyID int64) ([]Row, error) {
	rows, err := pool.Query(ctx, `
		WITH load_route AS (
			SELECT
				w.load_id,
				string_agg(
					NULLIF(concat_ws(', ', p.city, p.state), ''),
					' => ' ORDER BY w.order
				) AS route
			FROM waypoints w
			JOIN places p ON p.id = w.place_id AND p.is_deleted = false
			WHERE w.is_deleted = false
			GROUP BY w.load_id
		)
		SELECT
			t.truck_number,
			u.first_name,
			u.last_name,
			u.phone,
			l.load_number,
			lr.route
		FROM trucks t
		JOIN drivers d ON d.current_truck_id = t.id
			AND d.is_deleted = false
			AND d.employment_status = 'active'
			AND d.driver_contract_type IN ('company_cpm', 'company_percentage')
		JOIN users u ON u.id = d.user_id AND u.is_deleted = false
		LEFT JOIN loads l ON l.id = d.current_load_id AND l.is_deleted = false
		LEFT JOIN load_route lr ON lr.load_id = l.id
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
		if err := rows.Scan(&r.UnitNumber, &r.DriverFirstName, &r.DriverLastName, &r.DriverPhone, &r.LoadNumber, &r.Route); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
