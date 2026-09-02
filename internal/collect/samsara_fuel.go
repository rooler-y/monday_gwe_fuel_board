package collect

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/db"
	"fuelboard/internal/sources/samsara"
)

// matchedUnits returns units that already have a samsara_vehicle_id (i.e.
// have been through MatchSamsaraUnits) — every pull below only makes sense
// for units we've already linked.
func matchedUnits(ctx context.Context, pool *pgxpool.Pool) ([]db.Unit, error) {
	units, err := db.ListUnits(ctx, pool)
	if err != nil {
		return nil, err
	}
	var out []db.Unit
	for _, u := range units {
		if u.SamsaraVehicleID != nil {
			out = append(out, u)
		}
	}
	return out, nil
}

// PullSamsaraLiveLevels updates units.fuel_level_percent,
// units.def_level_percent, and units.latitude/longitude from Samsara's live
// vehicle stats (one combined request). Meant to run frequently (e.g. every
// 15 min via cron) — the position feed is what keeps the Map Link fresh.
func PullSamsaraLiveLevels(ctx context.Context, pool *pgxpool.Pool, client *samsara.Client) (updated int, err error) {
	units, err := matchedUnits(ctx, pool)
	if err != nil {
		return 0, fmt.Errorf("list matched units: %w", err)
	}
	if len(units) == 0 {
		return 0, nil
	}

	vehicleIDs := make([]string, len(units))
	unitByVehicleID := make(map[string]db.Unit, len(units))
	for i, u := range units {
		vehicleIDs[i] = *u.SamsaraVehicleID
		unitByVehicleID[*u.SamsaraVehicleID] = u
	}

	levels, err := client.GetVehicleLiveLevels(ctx, vehicleIDs)
	if err != nil {
		return 0, fmt.Errorf("get live levels: %w", err)
	}

	for vehicleID, lv := range levels {
		u, ok := unitByVehicleID[vehicleID]
		if !ok {
			continue
		}
		if _, err := db.UpsertUnit(ctx, pool, db.UnitUpsert{
			UnitNumber:       u.UnitNumber,
			FuelLevelPercent: lv.FuelPercent,
			DEFLevelPercent:  lv.DEFPercent,
			Latitude:         lv.Latitude,
			Longitude:        lv.Longitude,
		}); err != nil {
			return updated, fmt.Errorf("upsert unit %s: %w", u.UnitNumber, err)
		}
		updated++
	}

	return updated, nil
}

// PullSamsaraFuelEfficiency updates units.mpg from Samsara's daily
// fuel-energy report for the settled window (see samsara.SettledFuelReportWindow).
// Meant to run once a day.
func PullSamsaraFuelEfficiency(ctx context.Context, pool *pgxpool.Pool, client *samsara.Client) (updated int, err error) {
	units, err := matchedUnits(ctx, pool)
	if err != nil {
		return 0, fmt.Errorf("list matched units: %w", err)
	}
	if len(units) == 0 {
		return 0, nil
	}

	vehicleIDs := make([]string, len(units))
	unitByVehicleID := make(map[string]db.Unit, len(units))
	for i, u := range units {
		vehicleIDs[i] = *u.SamsaraVehicleID
		unitByVehicleID[*u.SamsaraVehicleID] = u
	}

	start, end := samsara.SettledFuelReportWindow(time.Now())
	reports, err := client.GetFuelEnergyReports(ctx, vehicleIDs, start, end)
	if err != nil {
		return 0, fmt.Errorf("get fuel-energy reports: %w", err)
	}

	for _, r := range reports {
		if r.EfficiencyMPG == nil {
			continue
		}
		u, ok := unitByVehicleID[r.VehicleID]
		if !ok {
			continue
		}
		mpg := *r.EfficiencyMPG
		if _, err := db.UpsertUnit(ctx, pool, db.UnitUpsert{
			UnitNumber: u.UnitNumber,
			MPG:        &mpg,
		}); err != nil {
			return updated, fmt.Errorf("upsert unit %s: %w", u.UnitNumber, err)
		}
		updated++
	}

	return updated, nil
}
