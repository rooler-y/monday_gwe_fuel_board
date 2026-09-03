package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string

	// TargetDBURL is the external dispatch system's Postgres DB we read from
	// (read-only). TargetDBCompanyIDs scopes every query to one or more
	// company/tenant rows within that DB, each synced independently — read
	// from TARGET_DB_COMPANY_ID_1, TARGET_DB_COMPANY_ID_2, etc. (numbered
	// from 1, stopping at the first gap).
	TargetDBURL        string
	TargetDBCompanyIDs []int64

	MondayAPIToken string
	MondayBoardID  string
	// MondaySecondaryBoardID is "Fuel Board 2.0" — a second board where items
	// (trucks) are created and deleted by the other side, not by us. We only
	// ever update existing items on it, never create/delete.
	MondaySecondaryBoardID string

	SamsaraAPIToken string

	GoogleServiceAccountJSON string
	GoogleSheetID            string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	var targetDBCompanyIDs []int64
	for i := 1; ; i++ {
		v := os.Getenv(fmt.Sprintf("TARGET_DB_COMPANY_ID_%d", i))
		if v == "" {
			break
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid TARGET_DB_COMPANY_ID_%d %q: %w", i, v, err)
		}
		targetDBCompanyIDs = append(targetDBCompanyIDs, id)
	}

	cfg := &Config{
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		TargetDBURL:              os.Getenv("TARGET_DB_URL"),
		TargetDBCompanyIDs:       targetDBCompanyIDs,
		MondayAPIToken:           os.Getenv("MONDAY_API_TOKEN"),
		MondayBoardID:            os.Getenv("MONDAY_BOARD_ID"),
		MondaySecondaryBoardID:   os.Getenv("MONDAY_SECONDARY_BOARD_ID"),
		SamsaraAPIToken:          os.Getenv("SAMSARA_API_TOKEN"),
		GoogleServiceAccountJSON: os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON"),
		GoogleSheetID:            os.Getenv("GOOGLE_SHEET_ID"),
	}

	var missing []string
	for name, val := range map[string]string{
		"DATABASE_URL": cfg.DatabaseURL,
	} {
		if val == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %v", missing)
	}

	return cfg, nil
}
