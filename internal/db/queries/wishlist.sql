-- name: ListWishlistItems :many
SELECT w.*, CAST(COALESCE(i.updated_at, 0) AS INTEGER) AS image_updated_at,
    CAST(COALESCE(t.amount_cents, 0) AS INTEGER) AS txn_amount, CAST(COALESCE(t.date, '') AS TEXT) AS txn_date,
    CAST(COALESCE(m.name, t.description, '') AS TEXT) AS txn_name
FROM wishlist_items w
LEFT JOIN wishlist_images i ON i.item_id = w.id
LEFT JOIN transactions t ON t.id = w.bought_txn_id
LEFT JOIN merchants m ON m.id = t.merchant_id
WHERE w.household_id = ?
ORDER BY w.created_at DESC, w.id DESC;

-- name: GetWishlistItem :one
SELECT * FROM wishlist_items WHERE id = ? AND household_id = ?;

-- name: CreateWishlistItem :one
INSERT INTO wishlist_items (household_id, title, url, store, price_cents, price_source, price_checked_at,
    image_source_url, stars, saves_money, notes, added_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateWishlistItem :exec
UPDATE wishlist_items SET title = ?, url = ?, store = ?, price_cents = ?, price_source = ?, price_checked_at = ?,
    image_source_url = ?, stars = ?, saves_money = ?, notes = ?
WHERE id = ? AND household_id = ?;

-- name: SetWishlistBought :exec
UPDATE wishlist_items SET bought_at = ?, bought_txn_id = ? WHERE id = ? AND household_id = ?;

-- name: DeleteWishlistItem :exec
DELETE FROM wishlist_items WHERE id = ? AND household_id = ?;

-- name: CountOpenWishlistItems :one
SELECT COUNT(*) FROM wishlist_items WHERE household_id = ? AND bought_at IS NULL;

-- name: ListWishlistWanters :many
SELECT ww.item_id, ww.user_id FROM wishlist_wanters ww
JOIN wishlist_items w ON w.id = ww.item_id
WHERE w.household_id = ?
ORDER BY ww.item_id, ww.user_id;

-- name: ClearWishlistWanters :exec
DELETE FROM wishlist_wanters WHERE item_id = ?;

-- name: AddWishlistWanter :exec
INSERT OR IGNORE INTO wishlist_wanters (item_id, user_id) VALUES (?, ?);

-- name: GetWishlistImage :one
SELECT i.mime, i.data, i.updated_at FROM wishlist_images i JOIN wishlist_items w ON w.id = i.item_id
WHERE i.item_id = ? AND w.household_id = ?;

-- name: SetWishlistImage :exec
INSERT INTO wishlist_images (item_id, mime, data, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (item_id) DO UPDATE SET mime = excluded.mime, data = excluded.data, updated_at = excluded.updated_at;

-- name: DeleteWishlistImage :exec
DELETE FROM wishlist_images WHERE item_id = ?;
