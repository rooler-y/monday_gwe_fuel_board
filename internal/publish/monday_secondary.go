package publish

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/db"
	"fuelboard/internal/sources/monday"
)

// colSecondaryFuel is the "Fuel" column on Fuel Board 2.0 — the only field
// we write there. Everything else on that board (Drivers, Phone, Status,
// Load ID, Origin - Destination, Miles, Gallons, Next Station, Note,
// Card#, Card PIN, Max Miles, Fuel Tank Capacity, Fuel Efficiency M/G) is
// entered/owned by the other side.
//
// This is numeric_mm6v3cdm, not the original text_mm6v4esn: the column was
// changed from text to numbers type on the board, which — confirmed live —
// deletes and recreates it under a new id rather than converting in place.
const colSecondaryFuel = "numeric_mm6v3cdm"

// PublishSecondaryBoard writes the live fuel level we've collected back to
// Fuel Board 2.0. Update-only: items there are created and deleted entirely
// by the other side, so we never create or delete anything on this board.
func PublishSecondaryBoard(ctx context.Context, pool *pgxpool.Pool, client *monday.Client, boardID string) (updated int, err error) {
	units, err := db.ListSecondaryBoardUnits(ctx, pool)
	if err != nil {
		return 0, fmt.Errorf("list secondary board units: %w", err)
	}

	var updates []monday.UpdateOp
	for _, u := range units {
		if u.FuelLevelPercent == nil {
			continue
		}
		updates = append(updates, monday.UpdateOp{
			BoardID: boardID,
			ItemID:  u.MondayItemID,
			ColumnValues: monday.ColumnValues{
				colSecondaryFuel: formatNumber(*u.FuelLevelPercent),
			},
		})
	}
	if len(updates) == 0 {
		return 0, nil
	}

	_, batchErr := client.BatchApply(ctx, nil, updates)
	if batchErr != nil {
		// The other side can delete an item at any point between our last
		// collect and this publish — a stale monday_item_id here means the
		// row itself is dead, so drop it (unlike the main Fuel Board, there's
		// no "recreate next run" to preserve; RunSecondaryBoard just picks
		// the item back up if it ever reappears).
		if healErr := healStaleSecondaryUnits(ctx, pool, client, updates); healErr != nil {
			return 0, fmt.Errorf("batch apply: %w (additionally failed to heal stale references: %v)", batchErr, healErr)
		}
		return 0, fmt.Errorf("batch apply: %w", batchErr)
	}

	return len(updates), nil
}

func healStaleSecondaryUnits(ctx context.Context, pool *pgxpool.Pool, client *monday.Client, updates []monday.UpdateOp) error {
	itemIDs := make([]string, len(updates))
	for i, u := range updates {
		itemIDs[i] = u.ItemID
	}

	states, err := client.GetItemStates(ctx, itemIDs)
	if err != nil {
		return fmt.Errorf("get item states: %w", err)
	}

	var errs []error
	for _, itemID := range itemIDs {
		if states[itemID] == "active" {
			continue
		}
		if err := db.DeleteSecondaryBoardUnit(ctx, pool, itemID); err != nil {
			errs = append(errs, fmt.Errorf("delete stale secondary board unit %s: %w", itemID, err))
		}
	}
	return errors.Join(errs...)
}
