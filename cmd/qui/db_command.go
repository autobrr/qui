// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/autobrr/qui/internal/database"
)

func RunDBCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database operations",
	}

	cmd.AddCommand(runDBMigrateCommand())
	return cmd
}

func runDBMigrateCommand() *cobra.Command {
	var (
		fromSQLite string
		toPostgres string
		dryRun     bool
		apply      bool
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Offline one-shot SQLite to Postgres migration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fromSQLite == "" {
				return errors.New("--from-sqlite is required")
			}
			if toPostgres == "" {
				return errors.New("--to-postgres is required")
			}
			if dryRun == apply {
				return errors.New("set exactly one of --dry-run or --apply")
			}

			report, err := database.MigrateSQLiteToPostgres(cmd.Context(), database.SQLiteToPostgresMigrationOptions{
				SQLitePath:  fromSQLite,
				PostgresDSN: toPostgres,
				Apply:       apply,
			})
			if err != nil {
				return err
			}

			mode := "dry-run"
			if apply {
				mode = "apply"
			}

			cmd.Printf("SQLite -> Postgres migration (%s)\n", mode)
			cmd.Printf("Tables: %d\n", len(report.Tables))
			for _, table := range report.Tables {
				cmd.Printf("  - %s: sqlite=%d postgres=%d\n", table.Table, table.SQLiteRows, table.PostgresRows)
			}

			if len(report.MissingPostgresTables) > 0 {
				cmd.Printf("Missing Postgres tables: %d\n", len(report.MissingPostgresTables))
				for _, table := range report.MissingPostgresTables {
					cmd.Printf("  - %s\n", table)
				}
			}

			if apply {
				cmd.Println("Migration applied successfully.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&fromSQLite, "from-sqlite", "", "Path to source SQLite database file")
	cmd.Flags().StringVar(&toPostgres, "to-postgres", "", "Destination Postgres DSN")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and report counts without importing")
	cmd.Flags().BoolVar(&apply, "apply", false, "Run migration and import data into Postgres")

	return cmd
}
