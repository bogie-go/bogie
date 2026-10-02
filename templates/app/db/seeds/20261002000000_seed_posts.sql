-- Seed data for development. Versioned like a migration and tracked in its
-- own table (seed_db_version), so `db seed` applies each file once and
-- `db seed-down --yes` undoes the last one. Author one with:
--
--   make g_seed_add_demo_comments
--
-- Seeds run in development and test (db prepare); production is never seeded
-- by accident because nothing there calls it.

-- +goose Up
INSERT INTO posts (title, body) VALUES
    ('Hello from the seed', 'This post was inserted by db/seeds. Delete the file when you have your own.'),
    ('A second post', 'Two rows, so a listing shows an order.');

-- +goose Down
DELETE FROM posts WHERE title IN ('Hello from the seed', 'A second post');
