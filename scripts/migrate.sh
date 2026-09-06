#!/usr/bin/env bash
#
# Applies internal/storage/migrations/*.sql to $DATABASE_URL in filename order,
# once each, tracked in schema_migrations.
#
#   DATABASE_URL=postgres://arb:arb@localhost:5432/arb?sslmode=disable ./scripts/migrate.sh
#
# Uses a local psql when installed, otherwise runs one inside the same postgres
# image compose uses, so no local client is required. The container falls back
# to host networking, so point DATABASE_URL at a published port rather than a
# compose service name when running it that way.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

DATABASE_URL="${DATABASE_URL:-${1:-}}"
if [[ -z "$DATABASE_URL" ]]; then
	echo "error: DATABASE_URL is unset and no connection string was given" >&2
	exit 1
fi

MIGRATIONS=internal/storage/migrations

if command -v psql >/dev/null 2>&1; then
	SQL_DIR="$MIGRATIONS"
	psql_run() { psql "$DATABASE_URL" -v ON_ERROR_STOP=1 "$@"; }
else
	ENGINE="${CONTAINER_ENGINE:-}"
	if [[ -z "$ENGINE" ]]; then
		if command -v docker >/dev/null 2>&1; then
			ENGINE=docker
		elif command -v podman >/dev/null 2>&1; then
			ENGINE=podman
		else
			echo "error: no local psql, and neither docker nor podman found on PATH" >&2
			exit 1
		fi
	fi

	PG_IMAGE="${PG_IMAGE:-docker.io/library/postgres:16-alpine}"
	echo "==> no local psql; running in ${PG_IMAGE} via ${ENGINE}"

	SQL_DIR=/migrations
	psql_run() {
		"$ENGINE" run --rm --network host \
			-v "$PWD/$MIGRATIONS":/migrations:ro \
			"$PG_IMAGE" psql "$DATABASE_URL" -v ON_ERROR_STOP=1 "$@"
	}
fi

# The server may still be starting, so wait for it rather than depending on the
# orchestrator to order us after it.
wait_for_postgres() {
	local attempts="${MIGRATE_WAIT_SECONDS:-60}"

	for _ in $(seq 1 "$attempts"); do
		if psql_run -q -tAc "SELECT 1" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done

	echo "error: postgres did not accept connections within ${attempts}s" >&2
	return 1
}

wait_for_postgres

psql_run -q -c "CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)"

for path in "$MIGRATIONS"/*.sql; do
	version="$(basename "$path" .sql)"

	if [[ -n "$(psql_run -tAc "SELECT 1 FROM schema_migrations WHERE version = '$version'")" ]]; then
		echo "==> $version already applied"
		continue
	fi

	# One transaction per migration: the file and its version row land together.
	echo "==> applying $version"
	psql_run -q --single-transaction \
		-f "$SQL_DIR/$version.sql" \
		-c "INSERT INTO schema_migrations (version) VALUES ('$version')"
done

echo "==> schema up to date"
