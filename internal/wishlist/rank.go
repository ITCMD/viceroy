package wishlist

import (
	"fmt"
	"sort"
)

// Item is what ranking needs to know about a wishlist item.
type Item struct {
	ID         int64
	Price      *int64 // cents; nil = unknown
	Stars      int
	Wanters    int
	SavesMoney bool
	CreatedAt  int64
}

// Sort orders.
const (
	SortAdded = "added" // newest first
	SortPrice = "price" // cheapest first
	SortScore = "score" // best value first
)

// Score = stars × people who want it ÷ price in dollars, × 1.5 when it saves money long term.
// Higher is better. Items without a price have no score.
func Score(it Item) (float64, bool) {
	if it.Price == nil || *it.Price <= 0 {
		return 0, false
	}
	s := float64(it.Stars) * float64(max(it.Wanters, 1)) / (float64(*it.Price) / 100)
	if it.SavesMoney {
		s *= 1.5
	}
	return s, true
}

// ScoreText explains a score: "4★ × 2 people ÷ $120 × 1.5".
func ScoreText(it Item) string {
	if it.Price == nil || *it.Price <= 0 {
		return "No price yet"
	}
	people := "1 person"
	if n := max(it.Wanters, 1); n > 1 {
		people = fmt.Sprintf("%d people", n)
	}
	d := *it.Price / 100
	price := fmt.Sprintf("$%d", d)
	if c := *it.Price % 100; c != 0 {
		price = fmt.Sprintf("$%d.%02d", d, c)
	}
	s := fmt.Sprintf("%d★ × %s ÷ %s", it.Stars, people, price)
	if it.SavesMoney {
		s += " × 1.5"
	}
	return s
}

// Sort orders items in place. Unknown prices go last for price and score; ties fall back to
// newest first.
func Sort(items []Item, by string) {
	newer := func(a, b Item) bool {
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt > b.CreatedAt
		}
		return a.ID > b.ID
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch by {
		case SortPrice:
			if (a.Price == nil) != (b.Price == nil) {
				return a.Price != nil
			}
			if a.Price != nil && *a.Price != *b.Price {
				return *a.Price < *b.Price
			}
		case SortScore:
			sa, oka := Score(a)
			sb, okb := Score(b)
			if oka != okb {
				return oka
			}
			if sa != sb {
				return sa > sb
			}
		}
		return newer(a, b)
	})
}

// When an item is affordable.
const (
	AffordNow      = "now"
	AffordMonthEnd = "month_end"
)

// Afford walks items in their sorted order and marks what the saved money covers, greedily:
// each item the money covers claims it, so higher-ranked items get it first. Items without a
// price are skipped. monthEnd is what will be saved by the end of the month (≥ now).
func Afford(items []Item, now, monthEnd int64) map[int64]string {
	out := map[int64]string{}
	monthEnd = max(monthEnd, now)
	for _, it := range items {
		if it.Price == nil || *it.Price <= 0 {
			continue
		}
		p := *it.Price
		switch {
		case p <= now:
			out[it.ID] = AffordNow
			now -= p
			monthEnd -= p
		case p <= monthEnd:
			out[it.ID] = AffordMonthEnd
			monthEnd -= p
			now = min(now, monthEnd) // money claimed for later can't buy something now
		}
	}
	return out
}
