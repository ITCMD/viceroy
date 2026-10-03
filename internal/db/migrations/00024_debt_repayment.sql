-- +goose Up
-- Debt Repayment: the category formerly seeded as "Loan Repayment" is marked builtin so the
-- budget can split it into one line per debt account. budget_amounts rows with account_id
-- are those per-account amounts (category_id stays the Debt Repayment category).
ALTER TABLE categories ADD COLUMN builtin TEXT NOT NULL DEFAULT '';
UPDATE categories SET builtin = 'debt_repayment', name = 'Debt Repayment'
WHERE name = 'Loan Repayment'
  AND id = (SELECT MIN(c2.id) FROM categories c2 WHERE c2.household_id = categories.household_id AND c2.name = 'Loan Repayment');
CREATE UNIQUE INDEX categories_builtin ON categories(household_id, builtin) WHERE builtin != '';
ALTER TABLE budget_amounts ADD COLUMN account_id INTEGER REFERENCES accounts(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE budget_amounts DROP COLUMN account_id;
DROP INDEX categories_builtin;
UPDATE categories SET name = 'Loan Repayment' WHERE builtin = 'debt_repayment' AND name = 'Debt Repayment';
ALTER TABLE categories DROP COLUMN builtin;
