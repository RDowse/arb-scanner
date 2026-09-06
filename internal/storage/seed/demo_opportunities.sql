-- Demonstration data: four cross-venue routes between kraken and coinbase on
-- the pairs config/strategies.yaml configures, priced so that every row's
-- numbers agree with each other -- amount_in is cost plus the buy fee,
-- amount_out is proceeds less the sell fee, and net_edge_bps is net_profit over
-- amount_in, using each venue's real taker fee (kraken 26bps, coinbase 40bps).
--
-- Both directions of a pair appear, but only one of them can pay at any given
-- instant, so the reverse routes carry older last_seen_at values: dislocations
-- that have since closed. Timestamps are relative to when this runs, so the
-- API's from/to filters have something to select.
--
-- Not a migration: this is opt-in demo data, applied by scripts/seed.sh.
-- Re-running it changes nothing, so it is safe against a populated table.

INSERT INTO opportunities (
	id, strategy, config_id, first_seen_at, last_seen_at, start_asset,
	amount_in, amount_out, gross_profit, fees, net_profit, net_edge_bps,
	max_edge_bps, legs
) VALUES
(
  md5('cross_venue|v1|USD|kraken|BTC/USD|buy|coinbase|BTC/USD|sell'), 'cross_venue', 'v1',
  now() - interval '18 minutes', now() - interval '45 seconds', 'USD',
  20052, 20087.328, 168, 132.672, 35.328,
  17.6182, 23.7846,
  jsonb_build_array(
    jsonb_build_object(
      'venue', 'kraken', 'symbol', 'BTC/USD', 'side', 'buy',
      'base', 'BTC', 'quote', 'USD',
      'asset_in', 'USD', 'asset_out', 'BTC',
      'amount_in', '20052', 'amount_out', '0.4',
      'price', '50000', 'qty', '0.4', 'fee', '52',
      'levels_consumed', 2, 'book_at', to_jsonb(now() - interval '2 seconds')
    ),
    jsonb_build_object(
      'venue', 'coinbase', 'symbol', 'BTC/USD', 'side', 'sell',
      'base', 'BTC', 'quote', 'USD',
      'asset_in', 'BTC', 'asset_out', 'USD',
      'amount_in', '0.4', 'amount_out', '20087.328',
      'price', '50420', 'qty', '0.4', 'fee', '80.672',
      'levels_consumed', 1, 'book_at', to_jsonb(now() - interval '2 seconds')
    )
  )
),
(
  md5('cross_venue|v1|USD|coinbase|BTC/USD|buy|kraken|BTC/USD|sell'), 'cross_venue', 'v1',
  now() - interval '2 hours', now() - interval '35 minutes', 'USD',
  37484.34, 37559.5905, 322.5, 247.2495, 75.2505,
  20.0752, 20.0752,
  jsonb_build_array(
    jsonb_build_object(
      'venue', 'coinbase', 'symbol', 'BTC/USD', 'side', 'buy',
      'base', 'BTC', 'quote', 'USD',
      'asset_in', 'USD', 'asset_out', 'BTC',
      'amount_in', '37484.34', 'amount_out', '0.75',
      'price', '49780', 'qty', '0.75', 'fee', '149.34',
      'levels_consumed', 1, 'book_at', to_jsonb(now() - interval '31 minutes')
    ),
    jsonb_build_object(
      'venue', 'kraken', 'symbol', 'BTC/USD', 'side', 'sell',
      'base', 'BTC', 'quote', 'USD',
      'asset_in', 'BTC', 'asset_out', 'USD',
      'amount_in', '0.75', 'amount_out', '37559.5905',
      'price', '50210', 'qty', '0.75', 'fee', '97.9095',
      'levels_consumed', 3, 'book_at', to_jsonb(now() - interval '31 minutes')
    )
  )
),
(
  md5('cross_venue|v1|USD|kraken|ETH/USD|buy|coinbase|ETH/USD|sell'), 'cross_venue', 'v1',
  now() - interval '9 minutes', now() - interval '20 seconds', 'USD',
  14528.27556, 14562.3168, 130.2, 96.15876, 34.04124,
  23.431, 42.1758,
  jsonb_build_array(
    jsonb_build_object(
      'venue', 'kraken', 'symbol', 'ETH/USD', 'side', 'buy',
      'base', 'ETH', 'quote', 'USD',
      'asset_in', 'USD', 'asset_out', 'ETH',
      'amount_in', '14528.27556', 'amount_out', '6',
      'price', '2415.1', 'qty', '6', 'fee', '37.67556',
      'levels_consumed', 1, 'book_at', to_jsonb(now() - interval '1 second')
    ),
    jsonb_build_object(
      'venue', 'coinbase', 'symbol', 'ETH/USD', 'side', 'sell',
      'base', 'ETH', 'quote', 'USD',
      'asset_in', 'ETH', 'asset_out', 'USD',
      'amount_in', '6', 'amount_out', '14562.3168',
      'price', '2436.8', 'qty', '6', 'fee', '58.4832',
      'levels_consumed', 2, 'book_at', to_jsonb(now() - interval '1 second')
    )
  )
),
(
  md5('cross_venue|v1|USD|coinbase|ETH/USD|buy|kraken|ETH/USD|sell'), 'cross_venue', 'v1',
  now() - interval '3 hours', now() - interval '52 minutes', 'USD',
  8442.5607, 8465.08341, 78.225, 55.70229, 22.52271,
  26.6776, 26.6776,
  jsonb_build_array(
    jsonb_build_object(
      'venue', 'coinbase', 'symbol', 'ETH/USD', 'side', 'buy',
      'base', 'ETH', 'quote', 'USD',
      'asset_in', 'USD', 'asset_out', 'ETH',
      'amount_in', '8442.5607', 'amount_out', '3.5',
      'price', '2402.55', 'qty', '3.5', 'fee', '33.6357',
      'levels_consumed', 2, 'book_at', to_jsonb(now() - interval '48 minutes')
    ),
    jsonb_build_object(
      'venue', 'kraken', 'symbol', 'ETH/USD', 'side', 'sell',
      'base', 'ETH', 'quote', 'USD',
      'asset_in', 'ETH', 'asset_out', 'USD',
      'amount_in', '3.5', 'amount_out', '8465.08341',
      'price', '2424.9', 'qty', '3.5', 'fee', '22.06659',
      'levels_consumed', 2, 'book_at', to_jsonb(now() - interval '48 minutes')
    )
  )
)
ON CONFLICT (id) DO NOTHING;
