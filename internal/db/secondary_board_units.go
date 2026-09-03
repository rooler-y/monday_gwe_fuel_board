package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SecondaryBoardUnit is one item on "Fuel Board 2.0" — a board whose items
// (trucks) are created and deleted entirely by the other side. We only ever
// read the Samsara vehicle ID they enter and write back the live fuel
// level, so this mirrors far less than Unit does.
type SecondaryBoardUnit struct {
	ID               int64
	MondayItemID     string
	SamsaraVehicleID string
	FuelLevelPercent *float64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const secondaryBoardUnitColumns = `id, monday_item_id, samsara_vehicle_id, fuel_level_percent, created_at, updated_at`

// UpsertSecondaryBoardUnitSamsaraID records (or updates) which Samsara
// vehicle ID a board item currently has entered against it. Unlike
// UpsertUnit, this is a full overwrite of samsara_vehicle_id, not a
// COALESCE — if the other side changes the value on the board, we want
// that change, not to keep stale data.
func UpsertSecondaryBoardUnitSamsaraID(ctx context.Context, pool *pgxpool.Pool, mondayItemID, samsaraVehicleID string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO secondary_board_units (monday_item_id, samsara_vehicle_id)
		VALUES ($1, $2)
		ON CONFLICT (monday_item_id) DO UPDATE SET
			samsara_vehicle_id = EXCLUDED.samsara_vehicle_id,
			updated_at = now()
	`, mondayItemID, samsaraVehicleID)
	return err
}

func SetSecondaryBoardUnitFuelLevel(ctx context.Context, pool *pgxpool.Pool, mondayItemID string, fuelLevelPercent *float64) error {
	_, err := pool.Exec(ctx, `UPDATE secondary_board_units SET fuel_level_percent = $2, updated_at = now() WHERE monday_item_id = $1`, mondayItemID, fuelLevelPercent)
	return err
}

// DeleteSecondaryBoardUnitsNotIn removes rows for items no longer present
// on the board — the other side deletes items freely, and this is how we
// notice. Deletes everything if mondayItemIDs is empty (handled explicitly
// rather than via = ANY($1), since a nil/empty slice there is NULL-typed
// and would match nothing, not everything).
func DeleteSecondaryBoardUnitsNotIn(ctx context.Context, pool *pgxpool.Pool, mondayItemIDs []string) error {
	if len(mondayItemIDs) == 0 {
		_, err := pool.Exec(ctx, `DELETE FROM secondary_board_units`)
		return err
	}
	_, err := pool.Exec(ctx, `DELETE FROM secondary_board_units WHERE NOT (monday_item_id = ANY($1))`, mondayItemIDs)
	return err
}

func DeleteSecondaryBoardUnit(ctx context.Context, pool *pgxpool.Pool, mondayItemID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM secondary_board_units WHERE monday_item_id = $1`, mondayItemID)
	return err
}

func ListSecondaryBoardUnits(ctx context.Context, pool *pgxpool.Pool) ([]SecondaryBoardUnit, error) {
	rows, err := pool.Query(ctx, `SELECT `+secondaryBoardUnitColumns+` FROM secondary_board_units ORDER BY monday_item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SecondaryBoardUnit
	for rows.Next() {
		var u SecondaryBoardUnit
		if err := rows.Scan(&u.ID, &u.MondayItemID, &u.SamsaraVehicleID, &u.FuelLevelPercent, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
