// Package collect orchestrates pulling normalized data from a source and
// upserting it into our own registry (internal/db).
package collect

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/db"
	"fuelboard/internal/sources/targetdb"
)

// RunTargetDB pulls units and their currently assigned drivers from the
// external dispatch DB (scoped to targetCompanyID) and upserts them into our
// own registry on ownPool.
//
// Per-row failures are collected and returned together at the end rather
// than aborting the loop — one bad row (e.g. a data integrity issue on a
// single truck) must not starve every truck after it in the result set of
// its update this run.
func RunTargetDB(ctx context.Context, ownPool, targetPool *pgxpool.Pool, targetCompanyID int64) error {
	companyName, err := targetdb.FetchCompanyName(ctx, targetPool, targetCompanyID)
	if err != nil {
		return fmt.Errorf("fetch target company: %w", err)
	}

	company, err := db.UpsertCompany(ctx, ownPool, companyName)
	if err != nil {
		return fmt.Errorf("upsert company %q: %w", companyName, err)
	}

	rows, err := targetdb.FetchUnitsAndDrivers(ctx, targetPool, targetCompanyID)
	if err != nil {
		return fmt.Errorf("fetch units/drivers: %w", err)
	}

	var errs []error
	for _, r := range rows {
		if err := upsertRow(ctx, ownPool, company.ID, r); err != nil {
			errs = append(errs, fmt.Errorf("unit %s: %w", r.UnitNumber, err))
		}
	}

	return errors.Join(errs...)
}

func upsertRow(ctx context.Context, ownPool *pgxpool.Pool, companyID int64, r targetdb.Row) error {
	unit, err := db.UpsertUnit(ctx, ownPool, db.UnitUpsert{
		UnitNumber: r.UnitNumber,
		CompanyID:  &companyID,
	})
	if err != nil {
		return fmt.Errorf("upsert unit: %w", err)
	}

	driverName := fullName(r.DriverFirstName, r.DriverLastName)
	if driverName == "" {
		return nil // no active driver currently assigned to this unit
	}

	fields := db.DriverUpsert{
		DriverName:  &driverName,
		PhoneNumber: r.DriverPhone,
		CompanyID:   &companyID,
		UnitID:      &unit.ID,
		LoadNumber:  r.LoadNumber,
		Destination: r.Destination,
	}

	existing, err := db.GetDriverByName(ctx, ownPool, driverName)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := db.CreateDriver(ctx, ownPool, fields); err != nil {
			return fmt.Errorf("create driver %q: %w", driverName, err)
		}
	case err != nil:
		return fmt.Errorf("lookup driver %q: %w", driverName, err)
	default:
		if _, err := db.UpdateDriver(ctx, ownPool, existing.ID, fields); err != nil {
			return fmt.Errorf("update driver %q: %w", driverName, err)
		}
	}
	return nil
}

func fullName(first, last *string) string {
	switch {
	case first != nil && last != nil:
		return *first + " " + *last
	case first != nil:
		return *first
	case last != nil:
		return *last
	default:
		return ""
	}
}
