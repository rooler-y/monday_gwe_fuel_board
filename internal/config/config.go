package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string

	// TargetDBURL is the external dispatch system's Postgres DB we read from
	// (read-only). TargetDBCompanyID scopes every query to one company/tenant
	// within that DB.
	TargetDBURL       string
	TargetDBCompanyID string

	MondayAPIToken string
	MondayBoardID  string

	SamsaraAPIToken string

	GoogleServiceAccountJSON string
	GoogleSheetID            string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		TargetDBURL:              os.Getenv("TARGET_DB_URL"),
		TargetDBCompanyID:        os.Getenv("TARGET_DB_COMPANY_ID"),
		MondayAPIToken:           os.Getenv("MONDAY_API_TOKEN"),
		MondayBoardID:            os.Getenv("MONDAY_BOARD_ID"),
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
