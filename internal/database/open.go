// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/domain"
)

type OpenOptions struct {
	Engine           string
	SQLitePath       string
	PostgresDSN      string
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDatabase string
	PostgresSSLMode  string
	ConnectTimeout   time.Duration
	MaxOpenConns     int
	MaxIdleConns     int
	ConnMaxLifetime  time.Duration
}

func Open(opts OpenOptions) (*DB, error) {
	dialect, err := parseDialect(opts.Engine)
	if err != nil {
		return nil, err
	}

	switch dialect {
	case DialectSQLite:
		if strings.TrimSpace(opts.SQLitePath) == "" {
			return nil, errors.New("sqlite database path is required")
		}
		return New(opts.SQLitePath)
	case DialectPostgres:
		dsn := strings.TrimSpace(opts.PostgresDSN)
		if dsn == "" {
			dsn = buildPostgresDSN(opts)
		}
		if dsn == "" {
			return nil, errors.New("postgres dsn is required")
		}
		return newPostgres(dsn, opts)
	default:
		return nil, fmt.Errorf("unsupported database engine %q", opts.Engine)
	}
}

func OpenFromConfig(cfg *domain.Config, sqlitePath string) (*DB, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}

	return Open(OpenOptions{
		Engine:           cfg.DatabaseEngine,
		SQLitePath:       sqlitePath,
		PostgresDSN:      cfg.DatabaseDSN,
		PostgresHost:     cfg.DatabaseHost,
		PostgresPort:     cfg.DatabasePort,
		PostgresUser:     cfg.DatabaseUser,
		PostgresPassword: cfg.DatabasePassword,
		PostgresDatabase: cfg.DatabaseName,
		PostgresSSLMode:  cfg.DatabaseSSLMode,
		ConnectTimeout:   time.Duration(cfg.DatabaseConnectTimeout) * time.Second,
		MaxOpenConns:     cfg.DatabaseMaxOpenConns,
		MaxIdleConns:     cfg.DatabaseMaxIdleConns,
		ConnMaxLifetime:  time.Duration(cfg.DatabaseConnMaxLifetime) * time.Second,
	})
}

func buildPostgresDSN(opts OpenOptions) string {
	host := strings.TrimSpace(opts.PostgresHost)
	user := strings.TrimSpace(opts.PostgresUser)
	dbName := strings.TrimSpace(opts.PostgresDatabase)
	if host == "" || user == "" || dbName == "" {
		return ""
	}

	port := opts.PostgresPort
	if port <= 0 {
		port = 5432
	}

	sslMode := strings.TrimSpace(opts.PostgresSSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}

	connectTimeout := opts.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}

	q := url.Values{}
	q.Set("sslmode", sslMode)
	q.Set("connect_timeout", strconv.Itoa(int(connectTimeout/time.Second)))

	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, opts.PostgresPassword),
		Host:     fmt.Sprintf("%s:%d", host, port),
		Path:     "/" + dbName,
		RawQuery: q.Encode(),
	}

	return u.String()
}
