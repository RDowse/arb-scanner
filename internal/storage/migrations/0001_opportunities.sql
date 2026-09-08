-- One row per sighting: the detector inserts on every tick a route still pays,
-- so an edge's persistence and decay are readable rather than collapsed into a
-- high-water mark. A route's history is the rows sharing a route_id.
CREATE TABLE opportunities (
    id           TEXT PRIMARY KEY,
    route_id     TEXT        NOT NULL,
    strategy     TEXT        NOT NULL,
    config_id    TEXT        NOT NULL,
    observed_at  TIMESTAMPTZ NOT NULL,
    start_asset  TEXT        NOT NULL,
    amount_in    NUMERIC     NOT NULL,
    amount_out   NUMERIC     NOT NULL,
    gross_profit NUMERIC     NOT NULL,
    fees         NUMERIC     NOT NULL,
    net_profit   NUMERIC     NOT NULL,
    net_edge_bps NUMERIC     NOT NULL,
    legs         JSONB       NOT NULL
);

CREATE INDEX opportunities_strategy_observed_idx
    ON opportunities (strategy, observed_at DESC);

-- One route's history, newest first.
CREATE INDEX opportunities_route_observed_idx
    ON opportunities (route_id, observed_at DESC);
