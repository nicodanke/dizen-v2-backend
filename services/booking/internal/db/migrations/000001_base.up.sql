-- Base migration every service starts from.
--
-- It creates the outbox table used for transactional event publication (01 section 4.3):
-- the service writes the event in the same transaction as the state change, and a worker
-- publishes it afterwards. Without this, a booking can be confirmed and the email never
-- sent, or the other way round.
--
-- The columns beyond those documented in 02 section 1 -- attempts, last_error,
-- available_at -- are what the retry with backoff in the worker needs (decision D-10).

create table if not exists outbox (
    id           bigint generated always as identity primary key,

    -- Aggregate the event belongs to, such as 'booking' or 'user'.
    aggregate    text        not null,

    -- Identifier of the aggregate instance. Kept as text because each domain has its own
    -- key type, and the outbox must not care.
    aggregate_id text,

    -- AMQP routing key, such as 'booking.confirmed'.
    routing_key  text        not null,

    payload      jsonb       not null,

    -- Set by the worker once the broker confirms publication. Null means pending.
    published_at timestamptz,

    -- Retry bookkeeping.
    attempts     integer     not null default 0,
    last_error   text,

    -- Earliest moment the worker may try again; drives the exponential backoff.
    available_at timestamptz not null default now(),

    created_at   timestamptz not null default now()
);

-- The worker's only query: pending events that are due, in insertion order. The partial
-- index keeps it proportional to the backlog rather than to the whole table, which matters
-- because published rows are kept for auditing.
create index if not exists outbox_pending_idx
    on outbox (available_at, id)
    where published_at is null;
