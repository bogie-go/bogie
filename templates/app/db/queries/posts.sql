-- Queries for the example resource. One file per table; sqlc compiles each
-- -- name: into a method on models.Queries. The hand-written half, which
-- speaks domain.Post rather than pgtype, is app/models/posts.go.

-- name: GetPost :one
SELECT * FROM posts WHERE id = $1;

-- name: ListPosts :many
SELECT * FROM posts ORDER BY created_at DESC, id DESC LIMIT $1;

-- name: CreatePost :one
INSERT INTO posts (title, body) VALUES ($1, $2) RETURNING *;

-- name: UpdatePost :one
UPDATE posts SET title = $2, body = $3, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeletePost :execrows
DELETE FROM posts WHERE id = $1;
