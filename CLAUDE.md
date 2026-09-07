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

**The route id hashes the route, not the prices.** One row per route: `last_seen_at` advances on every sighting, `max_edge_bps` keeps the best, and the legs are replaced only when a sighting beats it — so the stored trade sequence is the best seen, not the latest.

**Venue protocols differ and the code reflects it.** Coinbase publishes full depth, so removals are explicit. Kraken publishes a depth-limited book and never deletes a level its own window pushed out, so `bookState.trim` is mandatory or stale levels resurface as best-of-book; its CRC32 checksum is the only gap detection, and reproducing it needs decimal precision from the `instrument` channel because the book channel sends `50000.10` as `50000.1`. Prices decode as `json.Number` to keep the literal. Any disconnect invalidates that venue's books — a resumed stream cannot be told from a gapped one.

**API shape.** Collections are enveloped (`{"opportunities": [...], "count", "limit"}`) so a cursor can be added later; a single record is bare. `limit` reports the value actually applied after clamping. Malformed query params are 400, `storage.ErrNotFound` is 404, store errors are logged in full and answered generically.

**Compose has no ordering guarantee under podman.** podman-compose ignores `depends_on` conditions and turns them into `--requires`, which demands the dependency be *running* — so a one-shot `migrate` that has exited breaks single-container restarts (`podman restart api-server` fails with "container state improper"). Workaround is a full `podman-compose down && up`. Docker Compose honours the conditions correctly.

**Seeded rows are not detections.** `seed` loads four demo routes on every `up`; the README must say so.

## Code style

- Comments only where the code cannot speak: rationale, units, protocol quirks. Never restate the next line, and no banner blocks.
- Stdlib `testing` only, no testify. Table-driven subtests with names that read as claims ("skips a spread the fees eat"). Failure messages state got and want.
- Dependencies are deliberately three: shopspring/decimal, jackc/pgx, coder/websocket.
- Tests needing a database or network sit behind `db` / `live` build tags.
