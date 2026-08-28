// Package publish pushes our local registry (internal/db) to the Monday.com
// Fuel Board.
package publish

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/db"
	"fuelboard/internal/sources/monday"
)

const (
	fuelBoardGroupID = "topics"
	// unitInOutBoardID is the "IN/OUT" board the Fuel Board's "Unit"
	// board_relation column connects to — items there are named by unit
	// number (sometimes without a fleet prefix, e.g. unit "GL0856" appears
	// there as "0856"), matched the same two-way-substring way as Samsara
	// vehicles.
	unitInOutBoardID = "18392501548"

	// Fuel Board column IDs. Only these are ever written — every other
	// column on that board (Status, Fuel Card, Card PIN, Next Station,
	// Note, Truck Stop, Address, Route, Exit, Refuel Amount, Map Link,
	// Driver Message) is a dispatcher-owned manual workflow and must never
	// be touched here.
	colMC        = "text_mm6nfsgn"
	colUnit      = "board_relation_mm6nz81f"
	colDriver    = "text_mm6nfz8b"
	colPhone     = "text_mm6n3g56"
	colLoadID    = "text_mm6nnzap"
	colFuelLevel = "numeric_mm6nqb3z"
	colFuelMPG   = "numeric_mm6npm9x"
)

// PublishFuelBoard creates a Fuel Board item for every unit that doesn't
// have one yet (item name = unit number, linked to its matching IN/OUT board
// item if found), and updates MC/Driver/Phone/Load ID/Fuel Level/MPG on every
// unit's existing item. Everything else on the board is left untouched.
func PublishFuelBoard(ctx context.Context, pool *pgxpool.Pool, client *monday.Client, fuelBoardID string) (created, updated int, err error) {
	units, err := db.ListUnits(ctx, pool)
	if err != nil {
		return 0, 0, fmt.Errorf("list units: %w", err)
	}
	if len(units) == 0 {
		return 0, 0, nil
	}

	var inOutItems []monday.Item
	for _, u := range units {
		if u.MondayItemID == nil {
			inOutItems, err = client.ListItems(ctx, unitInOutBoardID)
			if err != nil {
				return 0, 0, fmt.Errorf("list IN/OUT board items: %w", err)
			}
			break
		}
	}

	companyNames := map[int64]string{}

	var creates []monday.CreateOp
	var createdForUnit []db.Unit // parallel to creates, so results map back
	var updates []monday.UpdateOp

	for _, u := range units {
		cv := monday.ColumnValues{}

		if u.CompanyID != nil {
			name, err := companyName(ctx, pool, companyNames, *u.CompanyID)
			if err != nil {
				return 0, 0, fmt.Errorf("company for unit %s: %w", u.UnitNumber, err)
			}
			if name != "" {
				cv[colMC] = name
			}
		}

		driver, err := db.GetDriverByUnitID(ctx, pool, u.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// no driver currently on this unit — leave Driver/Phone/Load ID untouched
		case err != nil:
			return 0, 0, fmt.Errorf("driver for unit %s: %w", u.UnitNumber, err)
		default:
			if driver.DriverName != "" {
				cv[colDriver] = driver.DriverName
			}
			if driver.PhoneNumber != nil {
				cv[colPhone] = *driver.PhoneNumber
			}
			if driver.LoadNumber != nil {
				cv[colLoadID] = *driver.LoadNumber
			}
		}

		if u.FuelLevelPercent != nil {
			cv[colFuelLevel] = formatNumber(*u.FuelLevelPercent)
		}
		if u.MPG != nil {
			cv[colFuelMPG] = formatNumber(*u.MPG)
		}

		if u.MondayItemID == nil {
			if id := matchInOutItem(u.UnitNumber, inOutItems); id != "" {
				cv[colUnit] = map[string]any{"item_ids": []string{id}}
			}
			creates = append(creates, monday.CreateOp{
				BoardID:      fuelBoardID,
				GroupID:      fuelBoardGroupID,
				ItemName:     u.UnitNumber,
				ColumnValues: cv,
			})
			createdForUnit = append(createdForUnit, u)
		} else {
			updates = append(updates, monday.UpdateOp{
				BoardID:      fuelBoardID,
				ItemID:       *u.MondayItemID,
				ColumnValues: cv,
			})
		}
	}

	createdIDs, batchErr := client.BatchApply(ctx, creates, updates)

	// Persist every item that actually landed on the board FIRST, even if
	// batchErr is set — a partial batch failure must not make us re-create
	// items on the next run for units that already got one.
	for i, id := range createdIDs {
		if id == "" {
			continue
		}
		if _, err := db.UpsertUnit(ctx, pool, db.UnitUpsert{
			UnitNumber:   createdForUnit[i].UnitNumber,
			MondayItemID: &id,
		}); err != nil {
			return len(creates), len(updates), fmt.Errorf("save monday_item_id for unit %s (after batch error: %v): %w", createdForUnit[i].UnitNumber, batchErr, err)
		}
	}

	if batchErr != nil {
		return 0, 0, fmt.Errorf("batch apply: %w", batchErr)
	}

	return len(creates), len(updates), nil
}

func companyName(ctx context.Context, pool *pgxpool.Pool, cache map[int64]string, id int64) (string, error) {
	if name, ok := cache[id]; ok {
		return name, nil
	}
	c, err := db.GetCompanyByID(ctx, pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	cache[id] = c.Name
	return c.Name, nil
}

// matchInOutItem finds the IN/OUT board item for unitNumber using the same
// case-insensitive two-way substring rule as the Samsara vehicle matcher —
// it also happens to handle a unit number like "GL0856" matching an IN/OUT
// item literally named "0856" (substring either direction), without needing
// prefix-specific logic.
func matchInOutItem(unitNumber string, items []monday.Item) string {
	num := strings.ToLower(unitNumber)
	for _, it := range items {
		name := strings.ToLower(it.Name)
		if name != "" && (strings.Contains(name, num) || strings.Contains(num, name)) {
			return it.ID
		}
	}
	return ""
}

func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}
