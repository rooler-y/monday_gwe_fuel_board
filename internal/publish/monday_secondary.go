package publish

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/db"
	"fuelboard/internal/sources/monday"
)

// Fuel Board 2.0 column IDs actually written by this publisher. Everything
// else on that board (Status, Origin - Destination, Miles, Gallons, Next
// Station, Note, Card#, Card PIN, Max Miles, Fuel Tank Capacity, Fuel
// Efficiency M/G) is entered/owned by the other side.
const (
	// This is numeric_mm6v3cdm, not the original text_mm6v4esn: the column
	// was changed from text to numbers type on the board, which — confirmed
	// live — deletes and recreates it under a new id rather than converting
	// in place.
	colSecondaryFuel   = "numeric_mm6v3cdm"
	colSecondaryDriver = "text_mm6vm6py"
	colSecondaryLoadID = "text_mm6vf8sv"
	// colSecondaryPhone is a "phone" type column, not "text" like the main
	// board's Phone column — confirmed live it needs
	// {"phone": "<digits only, no formatting>", "countryShortName": "US"},
	// and rejects a plain string or a number with dashes/spaces.
	colSecondaryPhone = "phone_mm6v6kq1"
)

// PublishSecondaryBoard writes the live fuel level, and — for items whose
// name matched one of our own units (see RunSecondaryBoard) — that unit's
// current Driver/Phone/Load ID, back to Fuel Board 2.0. Update-only: items
// there are created and deleted entirely by the other side, so we never
// create or delete anything on this board.
func PublishSecondaryBoard(ctx context.Context, pool *pgxpool.Pool, client *monday.Client, boardID string) (updated int, err error) {
	units, err := db.ListSecondaryBoardUnits(ctx, pool)
	if err != nil {
		return 0, fmt.Errorf("list secondary board units: %w", err)
	}

	var updates []monday.UpdateOp
	for _, u := range units {
		cv := monday.ColumnValues{}

		if u.FuelLevelPercent != nil {
			cv[colSecondaryFuel] = formatNumber(*u.FuelLevelPercent)
		}

		if u.UnitNumber != nil {
			localUnit, err := db.GetUnitByUnitNumber(ctx, pool, *u.UnitNumber)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// matched name no longer resolves to a unit — nothing to add
			case err != nil:
				return 0, fmt.Errorf("unit %s: %w", *u.UnitNumber, err)
			default:
				driver, err := db.GetDriverByUnitID(ctx, pool, localUnit.ID)
				switch {
				case errors.Is(err, pgx.ErrNoRows):
					// no driver currently on this unit — leave Driver/Phone/Load ID untouched
				case err != nil:
					return 0, fmt.Errorf("driver for unit %s: %w", *u.UnitNumber, err)
				default:
					if driver.DriverName != "" {
						cv[colSecondaryDriver] = driver.DriverName
					}
					if driver.PhoneNumber != nil {
						if digits := digitsOnly(*driver.PhoneNumber); digits != "" {
							cv[colSecondaryPhone] = map[string]any{"phone": digits, "countryShortName": "US"}
						}
					}
					if driver.LoadNumber != nil {
						cv[colSecondaryLoadID] = *driver.LoadNumber
					}
				}
			}
		}

		if len(cv) == 0 {
			continue
		}
		updates = append(updates, monday.UpdateOp{
			BoardID:      boardID,
			ItemID:       u.MondayItemID,
			ColumnValues: cv,
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

// digitsOnly strips everything but digits — Fuel Board 2.0's Phone column
// (a "phone" type column, unlike the main board's plain-text one) rejects
// any formatting characters (confirmed live: dashes alone trigger a
// ColumnValueException).
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
