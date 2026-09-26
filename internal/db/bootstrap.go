package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// BootstrapResult reports what Bootstrap changed.
type BootstrapResult struct {
	Role             string
	RoleCreated      bool
	DatabasesCreated []string
	DatabasesExisted []string
}

// Bootstrap prepares a PostgreSQL server for izci using a superuser
// connection (adminURL). It is idempotent:
//
//   - creates the login role named in appURL (or resets its password and strips
//     SUPERUSER/BYPASSRLS so row-level security applies to it), and
//   - creates every database named in appURL and extraURLs, owned by that role.
//
// All URLs must use the same role; extraURLs typically holds the integration
// test database.
func Bootstrap(ctx context.Context, adminURL, appURL string, extraURLs ...string) (BootstrapResult, error) {
	var res BootstrapResult
	if adminURL == "" {
		return res, errors.New("DB_ADMIN_URL is not set: it must point at a superuser connection (see .env.example)")
	}
	app, err := pgx.ParseConfig(appURL)
	if err != nil {
		return res, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if app.User == "" || app.Password == "" || app.Database == "" {
		return res, errors.New("DATABASE_URL must contain a user, a password and a database name")
	}
	databases := []string{app.Database}
	for _, u := range extraURLs {
		if u == "" {
			continue
		}
		extra, err := pgx.ParseConfig(u)
		if err != nil {
			return res, fmt.Errorf("parse %q: %w", redactURL(u), err)
		}
		if extra.User != app.User {
			return res, fmt.Errorf("database %q must use role %q (got %q)", extra.Database, app.User, extra.User)
		}
		databases = append(databases, extra.Database)
	}

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return res, fmt.Errorf("connect with DB_ADMIN_URL: %w", err)
	}
	defer admin.Close(ctx)

	var super bool
	if err := admin.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&super); err != nil {
		return res, fmt.Errorf("inspect admin role: %w", err)
	}
	if !super {
		return res, errors.New("DB_ADMIN_URL must connect as a superuser")
	}

	role := pgx.Identifier{app.User}.Sanitize()
	password := quoteLiteral(app.Password)
	res.Role = app.User

	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, app.User).Scan(&exists); err != nil {
		return res, fmt.Errorf("check role: %w", err)
	}
	if exists {
		if _, err := admin.Exec(ctx, fmt.Sprintf(
			`ALTER ROLE %s WITH LOGIN PASSWORD %s NOSUPERUSER NOCREATEROLE NOBYPASSRLS`, role, password)); err != nil {
			return res, fmt.Errorf("update role %s: %w", app.User, err)
		}
	} else {
		if _, err := admin.Exec(ctx, fmt.Sprintf(
			`CREATE ROLE %s WITH LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`, role, password)); err != nil {
			return res, fmt.Errorf("create role %s: %w", app.User, err)
		}
		res.RoleCreated = true
	}

	for _, name := range databases {
		var dbExists bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&dbExists); err != nil {
			return res, fmt.Errorf("check database %s: %w", name, err)
		}
		if dbExists {
			res.DatabasesExisted = append(res.DatabasesExisted, name)
			if _, err := admin.Exec(ctx, fmt.Sprintf(`ALTER DATABASE %s OWNER TO %s`, pgx.Identifier{name}.Sanitize(), role)); err != nil {
				return res, fmt.Errorf("set owner of %s: %w", name, err)
			}
			continue
		}
		// template0 keeps the new database free of extensions preinstalled in
		// template1 (the postgis image ships PostGIS there).
		if _, err := admin.Exec(ctx, fmt.Sprintf(
			`CREATE DATABASE %s OWNER %s TEMPLATE template0 ENCODING 'UTF8'`, pgx.Identifier{name}.Sanitize(), role)); err != nil {
			return res, fmt.Errorf("create database %s: %w", name, err)
		}
		res.DatabasesCreated = append(res.DatabasesCreated, name)
	}
	return res, nil
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// redactURL hides the password in a connection string for error messages.
func redactURL(u string) string {
	cfg, err := pgx.ParseConfig(u)
	if err != nil || cfg.Password == "" {
		return u
	}
	return strings.Replace(u, cfg.Password, "*****", 1)
}
