package collect

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/db"
	"fuelboard/internal/sources/samsara"
)

// MatchSamsaraUnits matches Samsara vehicles to our units by unit_number,
// using the same two-way case-insensitive substring rule as the sibling
// tracking system's truck<->vehicle matcher (a name isn't always an exact
// equal of the unit number — this tolerates "Truck 1234" style decoration in
// either direction) and skipping vehicles Samsara marks deactivated. Updates
// samsara_vehicle_id on each matched unit; does not touch anything else.
func MatchSamsaraUnits(ctx context.Context, pool *pgxpool.Pool, client *samsara.Client) (matched int, err error) {
	units, err := db.ListUnits(ctx, pool)
	if err != nil {
		return 0, fmt.Errorf("list units: %w", err)
	}
	// Deterministic scan order (Samsara/DB row order isn't guaranteed) — one
	// unit is claimed by at most the first vehicle that matches it.
	sort.Slice(units, func(i, j int) bool { return units[i].UnitNumber < units[j].UnitNumber })

	vehicles, err := client.ListVehicles(ctx)
	if err != nil {
		return 0, fmt.Errorf("list samsara vehicles: %w", err)
	}

	claimed := make(map[string]bool, len(units))
	for _, v := range vehicles {
		if samsara.IsDeactivated(v.Name) {
			continue
		}
		name := strings.ToLower(v.Name)

		for _, u := range units {
			if claimed[u.UnitNumber] {
				continue
			}
			num := strings.ToLower(u.UnitNumber)
			if strings.Contains(name, num) || strings.Contains(num, name) {
				vehicleID := v.ID
				if _, err := db.UpsertUnit(ctx, pool, db.UnitUpsert{
					UnitNumber:       u.UnitNumber,
					SamsaraVehicleID: &vehicleID,
				}); err != nil {
					return matched, fmt.Errorf("upsert unit %s: %w", u.UnitNumber, err)
				}
				claimed[u.UnitNumber] = true
				matched++
				break
			}
		}
	}

	return matched, nil
}
