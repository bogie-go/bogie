-- The example resource every new app starts with, so there is a working
-- shape to copy: this migration, db/queries/posts.sql, the service, the
-- controller and their tests. Delete the lot once you have your own.
--
-- Conventions worth keeping: uuid ids, timestamptz, NOT NULL with a default
-- rather than nullable, and a Down that really undoes the Up.

-- +goose Up
CREATE TABLE posts (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    title      text        NOT NULL,
    body       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE posts;
