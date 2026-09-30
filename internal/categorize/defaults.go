package categorize

import (
	"context"

	"viceroy/internal/db"
)

type defaultGroup struct {
	name, kind string
	cats       [][2]string // name, icon
}

// Monarch-like starter categories, seeded once per household.
var defaults = []defaultGroup{
	{"Income", "income", [][2]string{
		{"Paychecks", "💰"}, {"Interest", "🏦"}, {"Business Income", "💼"}, {"Other Income", "💵"},
	}},
	{"Fixed", "fixed", [][2]string{
		{"Rent", "🏠"}, {"Mortgage", "🏡"}, {"Insurance", "🛡️"}, {"Gas & Electric", "⚡"}, {"Water", "💧"},
		{"Internet & Cable", "🌐"}, {"Phone", "📱"}, {"Loan Repayment", "🏦"}, {"Student Loans", "🎓"}, {"Childcare", "👶"},
	}},
	{"Flexible", "flexible", [][2]string{
		{"Groceries", "🛒"}, {"Restaurants & Bars", "🍽️"}, {"Coffee Shops", "☕"}, {"Gas", "⛽"},
		{"Auto Maintenance", "🔧"}, {"Parking & Tolls", "🅿️"}, {"Public Transit", "🚆"}, {"Taxi & Ride Shares", "🚕"},
		{"Shopping", "🛍️"}, {"Clothing", "👕"}, {"Entertainment & Recreation", "🎟️"}, {"Personal Care", "💅"},
		{"Medical", "💊"}, {"Fitness", "🏋️"}, {"Pets", "🐾"}, {"Subscriptions", "📺"}, {"Gifts", "🎁"},
		{"Charity", "🤲"}, {"Home Improvement", "🔨"}, {"Miscellaneous", "📦"},
	}},
	{"Non-monthly", "non_monthly", [][2]string{
		{"Travel & Vacation", "✈️"}, {"Taxes", "🧾"}, {"Education", "📚"}, {"Holidays", "🎄"}, {"Auto Registration", "🚗"},
	}},
	{"Transfers", "transfer", [][2]string{
		{"Transfer", "🔁"}, {"Credit Card Payment", "💳"}, {"Balance Adjustments", "⚖️"},
	}},
}

// SeedDefaults creates the starter categories when the household has none.
func SeedDefaults(ctx context.Context, q *db.Queries, householdID int64) error {
	n, err := q.CountCategoryGroups(ctx, householdID)
	if err != nil || n > 0 {
		return err
	}
	for gi, g := range defaults {
		grp, err := q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{HouseholdID: householdID, Name: g.name, Kind: g.kind, Sort: int64(gi)})
		if err != nil {
			return err
		}
		for ci, c := range g.cats {
			if _, err := q.CreateCategory(ctx, db.CreateCategoryParams{
				HouseholdID: householdID, GroupID: grp.ID, Name: c[0], Icon: c[1], Sort: int64(ci),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// SeedAll seeds every household that has no categories (households created before phase 3).
func SeedAll(ctx context.Context, q *db.Queries) error {
	ids, err := q.ListHouseholdIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := SeedDefaults(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}
