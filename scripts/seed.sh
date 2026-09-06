#!/usr/bin/env bash
#
# Loads demonstration opportunities into $DATABASE_URL. 
#
#   DATABASE_URL=postgres://arb:arb@localhost:5432/arb?sslmode=disable ./scripts/seed.sh
#
# Pass --reset to empty the table first. Uses a local psql when installed,
# otherwise runs one inside the same postgres image compose uses.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

RESET=false
if [[ "${1:-}" == "--reset" ]]; then
	RESET=true
	shift
fi

DATABASE_URL="${DATABASE_URL:-${1:-}}"
if [[ -z "$DATABASE_URL" ]]; then
	echo "error: DATABASE_URL is unset and no connection string was given" >&2
	exit 1
fi

SEEDS=internal/storage/seed

if command -v psql >/dev/null 2>&1; then
	SQL_DIR="$SEEDS"
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

	SQL_DIR=/seed
	psql_run() {
		"$ENGINE" run --rm --network host \
			-v "$PWD/$SEEDS":/seed:ro \
			"$PG_IMAGE" psql "$DATABASE_URL" -v ON_ERROR_STOP=1 "$@"
	}
fi

# Postgres may still be starting and the migration may not have run yet, so
# wait for the table rather than depending on the orchestrator to order us.
wait_for_schema() {
	local attempts="${SEED_WAIT_SECONDS:-60}"

	for _ in $(seq 1 "$attempts"); do
		if [[ -n "$(psql_run -tAc "SELECT to_regclass('opportunities')" 2>/dev/null)" ]]; then
			return 0
		fi
		sleep 1
	done

	echo "error: no opportunities table after ${attempts}s; run ./scripts/migrate.sh first" >&2
	return 1
}

wait_for_schema

if [[ "$RESET" == true ]]; then
	echo "==> emptying opportunities"
	psql_run -q -c "TRUNCATE opportunities"
fi

for path in "$SEEDS"/*.sql; do
	name="$(basename "$path")"
	echo "==> loading $name"
	psql_run -q --single-transaction -f "$SQL_DIR/$name"
done

echo "==> seeded $(psql_run -tAc 'SELECT count(*) FROM opportunities') opportunities"
