-- name: InsertOutboxEvent :one
-- Writes an event inside the caller's transaction. It is the only way an event enters the
-- outbox: the helper in pkg/outbox always runs it on the transaction in progress.
insert into outbox (aggregate, aggregate_id, routing_key, payload)
values ($1, $2, $3, $4)
returning *;

-- name: ClaimPendingOutboxEvents :many
-- Claims a batch of events due for publication.
--
-- `for update skip locked` is what lets several workers run at once without stepping on
-- each other: each takes rows nobody else holds instead of blocking on them.
select * from outbox
where published_at is null
  and available_at <= now()
order by id
limit $1
for update skip locked;

-- name: MarkOutboxEventPublished :exec
-- Marks an event as published once the broker has confirmed it.
update outbox
set published_at = now()
where id = $1;

-- name: RescheduleOutboxEvent :exec
-- Records a failed attempt and pushes the event forward so the retry backs off.
update outbox
set attempts     = attempts + 1,
    last_error   = $2,
    available_at = $3
where id = $1;

-- name: CountPendingOutboxEvents :one
-- Size of the backlog. Exposed as a metric: a backlog that only grows means the worker is
-- not keeping up or the broker is down.
select count(*) from outbox
where published_at is null;

-- name: DeletePublishedOutboxEventsBefore :execrows
-- Prunes already published events. Retention is an operational decision, not a domain one.
delete from outbox
where published_at is not null
  and published_at < $1;
