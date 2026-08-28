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
// have been through MatchSamsaraUnits) — both fuel pulls below only make
// sense for units we've already linked.
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

// PullSamsaraFuelLevels updates units.fuel_level_percent from Samsara's live
// vehicle stats. Meant to run frequently (e.g. every 15 min via cron).
func PullSamsaraFuelLevels(ctx context.Context, pool *pgxpool.Pool, client *samsara.Client) (updated int, err error) {
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

	fuelPercents, err := client.GetVehicleFuelPercents(ctx, vehicleIDs)
	if err != nil {
		return 0, fmt.Errorf("get fuel percents: %w", err)
	}

	for vehicleID, pct := range fuelPercents {
		u, ok := unitByVehicleID[vehicleID]
		if !ok {
			continue
		}
		pct := pct
		if _, err := db.UpsertUnit(ctx, pool, db.UnitUpsert{
			UnitNumber:       u.UnitNumber,
			FuelLevelPercent: &pct,
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
