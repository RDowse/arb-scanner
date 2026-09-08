# arb-scanner

Cross-venue crypto arbitrage detector, built for an assessment. Two processes sharing only Postgres: `detector` consumes venue feeds and persists opportunities, `api-server` serves them read-only, so the API keeps answering when a feed or the detector dies.

## Commands

```bash
./scripts/test.sh                                   # gofmt + vet + unit tests, no network or DB
./scripts/test.sh -tags=db -run TestPostgres -v     # Postgres tests; creates arb_test if absent
./scripts/test.sh -tags=live -run TestKrakenLive -v # dials real exchanges
./scripts/migrate.sh                                # apply internal/storage/migrations/*.sql
./scripts/seed.sh [--reset]                         # load demo rows
podman-compose up -d --build --force-recreate api-server
```

Compose never rebuilds on source changes — after touching `cmd/` or `internal/`, pass `--build --force-recreate` or you are testing the previous binary. `TEST_DATABASE_URL` defaults to `arb_test`; the db tests drop and recreate the schema, so never point them at `arb`.

## Layout

```
cmd/detector, cmd/api-server   two binaries, one image, target per service
internal/venue/                coinbase.go, kraken.go, bookState, shared reconnect loop
internal/market/               Book, Level — data only, no fee or walk logic
internal/strategy/             Strategy interface, Rank, crossvenue.go
internal/opportunity/          Opportunity, Leg, route id, chain validation
internal/detector/             tick loop joining feeds to strategies to the sink
internal/storage/              postgres.go lifecycle, sink.go write, query.go read
internal/api/                  handlers, Store interface
```

Interfaces are declared at their consumer, never beside the implementation: `api.Store`, `detector.Feed`, `detector.Sink`.

## Design decisions worth knowing

**Decimal everywhere.** No float in the profit path — exchange inputs are decimal strings and stored rows must be re-derivable exactly. pgx needs no decimal adapter: `decimal.Decimal` satisfies `driver.Valuer`/`sql.Scanner`, so `NUMERIC` round-trips.

**The store is append-only: one row per sighting, not per route.** Every tick a route still pays is a plain insert, so an edge's persistence and decay survive. `route_id` hashes the route (strategy, start asset, each leg's venue/symbol/side) and is stable across ticks; `id` hashes the route plus the tick, so re-storing a tick is a no-op rather than a duplicate. A route's history is the rows sharing a `route_id`.

**Venue protocols differ and the code reflects it.** Coinbase publishes full depth, so removals are explicit. Kraken publishes a depth-limited book and never deletes a level its own window pushed out, so `bookState.trim` is mandatory or stale levels resurface as best-of-book; its CRC32 checksum is the only gap detection, and reproducing it needs decimal precision from the `instrument` channel because the book channel sends `50000.10` as `50000.1`. Prices decode as `json.Number` to keep the literal. Any disconnect invalidates that venue's books — a resumed stream cannot be told from a gapped one.

**API shape.** Collections are enveloped (`{"opportunities": [...], "count", "limit"}`) so a cursor can be added later. `limit` reports the value actually applied after clamping. Malformed query params are 400, store errors are logged in full and answered generically.

**Compose orders on postgres, not migrate, because of podman.** podman-compose ignores `depends_on` conditions and turns them into `--requires`. podman drops already-running containers from a start's input list, so an edge *through* the exited one-shot `migrate` can never resolve once postgres is up — `podman-compose up` itself fails, not just single-container restarts, and no `down && up` recovers it. So `detector` and `api-server` depend on `postgres: service_healthy` instead. Both only ping on open, so a cold start where migrate has not finished costs a few seconds of errors rather than a failure. Docker Compose would honour `service_completed_successfully` correctly; restoring it there means giving the services their own wait-for-schema.

**Seeded rows are not detections.** `seed` loads four demo routes on every `up`, one sighting each. Ids are deterministic, so re-running it changes nothing. The README must say the rows are demo data.

## Code style

- Comments only where the code cannot speak: rationale, units, protocol quirks. Never restate the next line, and no banner blocks.
- Stdlib `testing` only, no testify. Table-driven subtests with names that read as claims ("skips a spread the fees eat"). Failure messages state got and want.
- Dependencies are deliberately three: shopspring/decimal, jackc/pgx, coder/websocket.
- Tests needing a database or network sit behind `db` / `live` build tags.
