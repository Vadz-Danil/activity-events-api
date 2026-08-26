-- +goose Up
CREATE TABLE activity_buckets (
    user_id        bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    bucket_start   timestamptz NOT NULL,
    event_count    bigint      NOT NULL,
    type_counts    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    first_event_at timestamptz NOT NULL,
    last_event_at  timestamptz NOT NULL,
    computed_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, bucket_start),
    CONSTRAINT activity_buckets_event_count_check CHECK (event_count >= 0),
    CONSTRAINT activity_buckets_type_counts_check CHECK (jsonb_typeof(type_counts) = 'object')
);

CREATE INDEX activity_buckets_bucket_start_idx ON activity_buckets (bucket_start);

CREATE TABLE aggregation_runs (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bucket_start  timestamptz NOT NULL,
    bucket_end    timestamptz NOT NULL,
    status        text        NOT NULL,
    trigger       text        NOT NULL,
    users_touched integer     NOT NULL DEFAULT 0,
    started_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    error         text,
    CONSTRAINT aggregation_runs_status_check
        CHECK (status IN ('succeeded', 'failed', 'skipped')),
    CONSTRAINT aggregation_runs_trigger_check
        CHECK (trigger IN ('schedule', 'manual')),
    CONSTRAINT aggregation_runs_window_check CHECK (bucket_end > bucket_start)
);

CREATE INDEX aggregation_runs_started_at_idx ON aggregation_runs (started_at DESC, id DESC);

CREATE INDEX aggregation_runs_bucket_idx ON aggregation_runs (bucket_start DESC)
    WHERE status = 'succeeded';

-- +goose Down
DROP TABLE aggregation_runs;
DROP TABLE activity_buckets;
