-- +goose Up
-- Uploaded category icons. A category using one has icon = '/api/categories/<id>/icon?v=<n>'.
CREATE TABLE category_icons (
    category_id INTEGER PRIMARY KEY REFERENCES categories(id) ON DELETE CASCADE,
    mime        TEXT    NOT NULL,
    data        BLOB    NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- +goose Down
DROP TABLE category_icons;
