package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func UpsertCompany(ctx context.Context, pool *pgxpool.Pool, name string) (*Company, error) {
	row := pool.QueryRow(ctx, `
		INSERT INTO companies (name)
		VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET updated_at = now()
		RETURNING id, name, created_at, updated_at
	`, name)

	var c Company
	if err := row.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func GetCompanyByName(ctx context.Context, pool *pgxpool.Pool, name string) (*Company, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, name, created_at, updated_at FROM companies WHERE name = $1
	`, name)

	var c Company
	if err := row.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func GetCompanyByID(ctx context.Context, pool *pgxpool.Pool, id int64) (*Company, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, name, created_at, updated_at FROM companies WHERE id = $1
	`, id)

	var c Company
	if err := row.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}
