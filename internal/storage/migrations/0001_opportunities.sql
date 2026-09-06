CREATE TABLE opportunities (
    id            TEXT PRIMARY KEY,
    strategy      TEXT        NOT NULL,
    config_id     TEXT        NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at  TIMESTAMPTZ NOT NULL,
    start_asset   TEXT        NOT NULL,
    amount_in     NUMERIC     NOT NULL,
    amount_out    NUMERIC     NOT NULL,
    gross_profit  NUMERIC     NOT NULL,
    fees          NUMERIC     NOT NULL,
    net_profit    NUMERIC     NOT NULL,
    net_edge_bps  NUMERIC     NOT NULL,
    max_edge_bps  NUMERIC     NOT NULL,
    legs          JSONB       NOT NULL
);

CREATE INDEX opportunities_strategy_last_seen_idx
    ON opportunities (strategy, last_seen_at DESC);
