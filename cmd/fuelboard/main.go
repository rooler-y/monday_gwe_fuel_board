package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelboard/internal/collect"
	"fuelboard/internal/config"
	"fuelboard/internal/db"
	"fuelboard/internal/publish"
	"fuelboard/internal/sources/monday"
	"fuelboard/internal/sources/samsara"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fuelboard <command>\n\ncommands:\n  migrate                     apply pending SQL migrations\n  collect-db                  pull units/drivers from the target dispatch DB\n  collect-samsara-match       match units to Samsara vehicles by unit number\n  collect-samsara-stats       pull live fuel level % (run frequently)\n  collect-samsara-fuel-report pull daily settled MPG efficiency report\n  publish-monday              create/update Fuel Board items from our registry")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db connect error:", err)
		os.Exit(1)
	}
	defer pool.Close()

	switch os.Args[1] {
	case "migrate":
		if err := runMigrations(ctx, pool); err != nil {
			fmt.Fprintln(os.Stderr, "migrate error:", err)
			os.Exit(1)
		}
	case "collect-db":
		if err := runCollectDB(ctx, cfg, pool); err != nil {
			fmt.Fprintln(os.Stderr, "collect-db error:", err)
			os.Exit(1)
		}
	case "collect-samsara-match":
		if err := runCollectSamsaraMatch(ctx, cfg, pool); err != nil {
			fmt.Fprintln(os.Stderr, "collect-samsara-match error:", err)
			os.Exit(1)
		}
	case "collect-samsara-stats":
		if err := runCollectSamsaraStats(ctx, cfg, pool); err != nil {
			fmt.Fprintln(os.Stderr, "collect-samsara-stats error:", err)
			os.Exit(1)
		}
	case "collect-samsara-fuel-report":
		if err := runCollectSamsaraFuelReport(ctx, cfg, pool); err != nil {
			fmt.Fprintln(os.Stderr, "collect-samsara-fuel-report error:", err)
			os.Exit(1)
		}
	case "publish-monday":
		if err := runPublishMonday(ctx, cfg, pool); err != nil {
			fmt.Fprintln(os.Stderr, "publish-monday error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		os.Exit(1)
	}
}

func runCollectDB(ctx context.Context, cfg *config.Config, ownPool *pgxpool.Pool) error {
	if cfg.TargetDBURL == "" || cfg.TargetDBCompanyID == "" {
		return fmt.Errorf("TARGET_DB_URL and TARGET_DB_COMPANY_ID must be set")
	}
	companyID, err := strconv.ParseInt(cfg.TargetDBCompanyID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid TARGET_DB_COMPANY_ID %q: %w", cfg.TargetDBCompanyID, err)
	}

	targetPool, err := db.Connect(ctx, cfg.TargetDBURL)
	if err != nil {
		return fmt.Errorf("connect target db: %w", err)
	}
	defer targetPool.Close()

	if err := collect.RunTargetDB(ctx, ownPool, targetPool, companyID); err != nil {
		return err
	}
	fmt.Println("collect-db done")
	return nil
}

func runCollectSamsaraMatch(ctx context.Context, cfg *config.Config, ownPool *pgxpool.Pool) error {
	if cfg.SamsaraAPIToken == "" {
		return fmt.Errorf("SAMSARA_API_TOKEN must be set")
	}
	client := samsara.NewClient(cfg.SamsaraAPIToken)

	matched, err := collect.MatchSamsaraUnits(ctx, ownPool, client)
	if err != nil {
		return err
	}
	fmt.Printf("collect-samsara-match done: %d units matched\n", matched)
	return nil
}

func runCollectSamsaraStats(ctx context.Context, cfg *config.Config, ownPool *pgxpool.Pool) error {
	if cfg.SamsaraAPIToken == "" {
		return fmt.Errorf("SAMSARA_API_TOKEN must be set")
	}
	client := samsara.NewClient(cfg.SamsaraAPIToken)

	updated, err := collect.PullSamsaraFuelLevels(ctx, ownPool, client)
	if err != nil {
		return err
	}
	fmt.Printf("collect-samsara-stats done: %d units updated\n", updated)
	return nil
}

func runCollectSamsaraFuelReport(ctx context.Context, cfg *config.Config, ownPool *pgxpool.Pool) error {
	if cfg.SamsaraAPIToken == "" {
		return fmt.Errorf("SAMSARA_API_TOKEN must be set")
	}
	client := samsara.NewClient(cfg.SamsaraAPIToken)

	updated, err := collect.PullSamsaraFuelEfficiency(ctx, ownPool, client)
	if err != nil {
		return err
	}
	fmt.Printf("collect-samsara-fuel-report done: %d units updated\n", updated)
	return nil
}

func runPublishMonday(ctx context.Context, cfg *config.Config, ownPool *pgxpool.Pool) error {
	if cfg.MondayAPIToken == "" || cfg.MondayBoardID == "" {
		return fmt.Errorf("MONDAY_API_TOKEN and MONDAY_BOARD_ID must be set")
	}
	client := monday.NewClient(cfg.MondayAPIToken)

	created, updated, err := publish.PublishFuelBoard(ctx, ownPool, client, cfg.MondayBoardID)
	if err != nil {
		return err
	}
	fmt.Printf("publish-monday done: %d created, %d updated\n", created, updated)
	return nil
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := filepath.Glob("migrations/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, f := range files {
		name := filepath.Base(f)

		var alreadyApplied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`, name).Scan(&alreadyApplied); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if alreadyApplied {
			continue
		}

		sqlBytes, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("exec %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		fmt.Println("applied", name)
	}
	return nil
}
