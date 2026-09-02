// Package publish pushes our local registry (internal/db) to the Monday.com
// Fuel Board.
package publish

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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

	// Fuel Board column IDs actually written by this publisher. Everything
	// else on that board (Status, Fuel Card, Card PIN, Next Station, Note,
	// Truck Stop, Address, Route, Exit, Refuel Amount, Driver Message) is a
	// dispatcher-owned manual workflow and must never be touched here.
	// colMapLink is the one exception to "we only write our own fields" —
	// it's confirmed repurposed: we compute it from live truck position +
	// whatever the dispatcher put in Truck Stop/Address, replacing what was
	// previously a fully manual link.
	colMC                = "text_mm6nfsgn"
	colUnit              = "board_relation_mm6nz81f"
	colDriver            = "text_mm6nfz8b"
	colPhone             = "text_mm6n3g56"
	colLoadID            = "text_mm6nnzap"
	colFuelLevel         = "numeric_mm6nqb3z"
	colFuelMPG           = "numeric_mm6npm9x"
	colOriginDestination = "text_mm6n6cwx"
	colDEFLevel          = "text_mm6pzqm2"
	colTruckStop         = "text_mm6n2qfs"
	colAddress           = "text_mm6njh6g"
	colMapLink           = "link_mm6n53pv"
)

// PublishFuelBoard creates a Fuel Board item for every unit that doesn't
// have one yet (item name = unit number, linked to its matching IN/OUT board
// item if found), and updates MC/Driver/Phone/Load ID/Fuel Level/MPG/DEF
// Level/Origin-Destination/Map Link on every unit's existing item. Map Link
// is only set when a fuel stop (Truck Stop or Address) has been entered and
// we have a current GPS fix for the unit — see buildMapLink. Everything else
// on the board is left untouched.
func PublishFuelBoard(ctx context.Context, pool *pgxpool.Pool, client *monday.Client, fuelBoardID string) (created, updated int, err error) {
	units, err := db.ListUnits(ctx, pool)
	if err != nil {
		return 0, 0, fmt.Errorf("list units: %w", err)
	}
	if len(units) == 0 {
		return 0, 0, nil
	}

	needsLookup := false
	for _, u := range units {
		if u.MondayItemID == nil {
			needsLookup = true
			break
		}
	}

	var inOutItems []monday.Item
	if needsLookup {
		inOutItems, err = client.ListItems(ctx, unitInOutBoardID)
		if err != nil {
			return 0, 0, fmt.Errorf("list IN/OUT board items: %w", err)
		}

		// Reconcile against the Fuel Board's ACTUAL current items before
		// deciding create-vs-update — a unit with no locally-known
		// monday_item_id might already have an item on the board (e.g. a
		// prior publish run created it but the response was lost to a
		// timeout/crash before we could persist the id, as happened once
		// already). Backfilling from the board's real state, rather than
		// trusting our local cache alone, is what makes retrying after a
		// failure safe instead of duplicate-creating.
		fuelBoardItems, err := client.ListItems(ctx, fuelBoardID)
		if err != nil {
			return 0, 0, fmt.Errorf("list fuel board items: %w", err)
		}
		fuelBoardItemIDByName := make(map[string]string, len(fuelBoardItems))
		for _, it := range fuelBoardItems {
			fuelBoardItemIDByName[it.Name] = it.ID
		}
		for i, u := range units {
			if u.MondayItemID != nil {
				continue
			}
			id, ok := fuelBoardItemIDByName[u.UnitNumber]
			if !ok {
				continue
			}
			if _, err := db.UpsertUnit(ctx, pool, db.UnitUpsert{
				UnitNumber:   u.UnitNumber,
				MondayItemID: &id,
			}); err != nil {
				return 0, 0, fmt.Errorf("backfill monday_item_id for unit %s: %w", u.UnitNumber, err)
			}
			units[i].MondayItemID = &id
		}
	}

	// Fuel-stop text (Truck Stop/Address) is only relevant for units that
	// already have an item AND a current GPS fix — those are exactly the
	// units that could get a Map Link this run.
	needsFuelStopLookup := false
	for _, u := range units {
		if u.MondayItemID != nil && u.Latitude != nil && u.Longitude != nil {
			needsFuelStopLookup = true
			break
		}
	}
	var fuelStops map[string]map[string]string
	if needsFuelStopLookup {
		fuelStops, err = client.GetTextColumns(ctx, fuelBoardID, []string{colTruckStop, colAddress})
		if err != nil {
			return 0, 0, fmt.Errorf("get fuel stop columns: %w", err)
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
			if driver.Destination != nil {
				cv[colOriginDestination] = *driver.Destination
			}
		}

		if u.FuelLevelPercent != nil {
			cv[colFuelLevel] = formatNumber(*u.FuelLevelPercent)
		}
		if u.MPG != nil {
			cv[colFuelMPG] = formatNumber(*u.MPG)
		}
		if u.DEFLevelPercent != nil {
			// colDEFLevel is a plain text column (not "numbers" like Fuel
			// Level %/MPG), so it has no built-in "%" unit formatting —
			// append it explicitly.
			cv[colDEFLevel] = formatNumber(*u.DEFLevelPercent) + "%"
		}

		if u.MondayItemID != nil && u.Latitude != nil && u.Longitude != nil {
			if link := buildMapLink(*u.Latitude, *u.Longitude, fuelStops[*u.MondayItemID][colTruckStop], fuelStops[*u.MondayItemID][colAddress]); link != "" {
				cv[colMapLink] = map[string]any{"url": link, "text": "Route"}
			}
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

// buildMapLink returns a Google Maps directions URL from the unit's live
// GPS position to the dispatcher-entered fuel stop, or "" if no fuel stop
// has been entered yet. truckStop/address are passed straight through as
// free text — Google Maps geocodes the destination itself when the link is
// opened, so no geocoding step is needed on our side (confirmed: no
// coordinates required for the destination param, just an address string).
func buildMapLink(lat, lng float64, truckStop, address string) string {
	dest := strings.TrimSpace(strings.TrimSpace(truckStop) + " " + strings.TrimSpace(address))
	if dest == "" {
		return ""
	}
	origin := fmt.Sprintf("%f,%f", lat, lng)
	return "https://www.google.com/maps/dir/?api=1&origin=" + url.QueryEscape(origin) + "&destination=" + url.QueryEscape(dest)
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
