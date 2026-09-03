package collect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/db"
	"fuelboard/internal/sources/monday"
	"fuelboard/internal/sources/samsara"
)

// secondarySamsaraIDColumn is the "Samsara ID" text column on Fuel Board
// 2.0 — the one field the other side enters that we read.
const secondarySamsaraIDColumn = "text_mm6vdkkz"

// RunSecondaryBoard syncs "Fuel Board 2.0" (a board whose items are created
// and deleted entirely by the other side): it reads every item's current
// Samsara ID entry, prunes rows for items that got deleted, then pulls live
// fuel level for every distinct vehicle ID found and stores it locally for
// PublishSecondaryBoard to write back.
func RunSecondaryBoard(ctx context.Context, pool *pgxpool.Pool, mondayClient *monday.Client, samsaraClient *samsara.Client, boardID string) (updated int, err error) {
	items, err := mondayClient.ListItems(ctx, boardID)
	if err != nil {
		return 0, fmt.Errorf("list secondary board items: %w", err)
	}

	cols, err := mondayClient.GetTextColumns(ctx, boardID, []string{secondarySamsaraIDColumn})
	if err != nil {
		return 0, fmt.Errorf("get secondary board samsara id column: %w", err)
	}

	var errs []error
	currentItemIDs := make([]string, 0, len(items))
	vehicleIDByItem := make(map[string]string)

	for _, it := range items {
		currentItemIDs = append(currentItemIDs, it.ID)

		vehicleID := strings.TrimSpace(cols[it.ID][secondarySamsaraIDColumn])
		if vehicleID == "" {
			continue
		}
		if err := db.UpsertSecondaryBoardUnitSamsaraID(ctx, pool, it.ID, vehicleID); err != nil {
			errs = append(errs, fmt.Errorf("item %s: save samsara id: %w", it.ID, err))
			continue
		}
		vehicleIDByItem[it.ID] = vehicleID
	}

	if err := db.DeleteSecondaryBoardUnitsNotIn(ctx, pool, currentItemIDs); err != nil {
		errs = append(errs, fmt.Errorf("prune deleted items: %w", err))
	}

	if len(vehicleIDByItem) == 0 {
		return 0, errors.Join(errs...)
	}

	vehicleIDs := make([]string, 0, len(vehicleIDByItem))
	for _, v := range vehicleIDByItem {
		vehicleIDs = append(vehicleIDs, v)
	}

	levels, err := samsaraClient.GetVehicleLiveLevels(ctx, vehicleIDs)
	if err != nil {
		errs = append(errs, fmt.Errorf("get live fuel levels: %w", err))
		return 0, errors.Join(errs...)
	}

	for itemID, vehicleID := range vehicleIDByItem {
		lv, ok := levels[vehicleID]
		if !ok || lv.FuelPercent == nil {
			continue
		}
		if err := db.SetSecondaryBoardUnitFuelLevel(ctx, pool, itemID, lv.FuelPercent); err != nil {
			errs = append(errs, fmt.Errorf("item %s: set fuel level: %w", itemID, err))
			continue
		}
		updated++
	}

	return updated, errors.Join(errs...)
}
