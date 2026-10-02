package reports

import (
	"fmt"
	"sort"
	"strings"
)

// TreeRow is one total per category, merchant and goal over a date range. GoalID is set
// for money put toward a goal; its Total is then already positive.
type TreeRow struct {
	CategoryID int64
	Kind       string
	Merchant   string
	GoalID     int64
	Total      int64
	Count      int64
}

type Goal struct {
	ID   int64
	Name string
	Icon string
}

// Node is one box in the spending tree or cash flow diagram: a category group (or the
// Contributions and Uncategorized sections), a category or goal, or a merchant.
type Node struct {
	Key      string  `json:"key"`
	ID       int64   `json:"id"` // group, category or goal id; 0 otherwise
	Name     string  `json:"name"`
	Icon     string  `json:"icon"`
	Kind     string  `json:"kind"`  // group | category | goal | uncategorized | contributions | merchant | income
	Group    string  `json:"group"` // the category group kind (fixed, flexible, ...) on groups and categories
	Total    int64   `json:"total"`
	Count    int64   `json:"count"`
	Children []*Node `json:"children,omitempty"`
}

// Tree splits a range's money into income (category → merchant) and spending
// (group → category → merchant, plus Contributions → goal → merchant and Uncategorized → merchant).
// Totals are positive; refunds net against their category. Transfers are left out unless put toward a goal.
type Tree struct {
	Income   *Node `json:"income"`
	Spending *Node `json:"spending"`
}

func BuildTree(rows []TreeRow, cats map[int64]Category, goals map[int64]Goal) Tree {
	income := &Node{Key: "income", Name: "Income", Kind: "income"}
	spending := &Node{Key: "spending", Name: "Spending", Kind: "spending"}
	child := func(parent *Node, key string, mk func() *Node) *Node {
		for _, c := range parent.Children {
			if c.Key == key {
				return c
			}
		}
		n := mk()
		n.Key = key
		parent.Children = append(parent.Children, n)
		return n
	}
	add := func(path []*Node, v, count int64) {
		for _, n := range path {
			n.Total += v
			n.Count += count
		}
	}
	merchant := func(parent *Node, name string) *Node {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "Unknown"
		}
		return child(parent, parent.Key+"/m:"+strings.ToLower(name), func() *Node { return &Node{Name: name, Kind: "merchant"} })
	}

	for _, r := range rows {
		if r.GoalID != 0 {
			sec := child(spending, "contributions", func() *Node { return &Node{Name: "Contributions", Kind: "contributions", Icon: "🎯"} })
			g := goals[r.GoalID]
			gn := child(sec, fmt.Sprintf("goal%d", r.GoalID), func() *Node {
				name := g.Name
				if name == "" {
					name = "Goal"
				}
				return &Node{ID: r.GoalID, Name: name, Icon: g.Icon, Kind: "goal"}
			})
			add([]*Node{spending, sec, gn, merchant(gn, r.Merchant)}, r.Total, r.Count)
			continue
		}
		isIncome, v, counted := Classify(Row{CategoryID: r.CategoryID, Kind: r.Kind, Total: r.Total})
		if !counted {
			continue
		}
		c, known := cats[r.CategoryID]
		if isIncome {
			if !known {
				cn := child(income, "c0", func() *Node { return &Node{Name: "Uncategorized", Kind: "uncategorized"} })
				add([]*Node{income, cn, merchant(cn, r.Merchant)}, v, r.Count)
				continue
			}
			cn := child(income, fmt.Sprintf("c%d", c.ID), func() *Node {
				return &Node{ID: c.ID, Name: c.Name, Icon: c.Icon, Kind: "category", Group: "income"}
			})
			add([]*Node{income, cn, merchant(cn, r.Merchant)}, v, r.Count)
			continue
		}
		if !known {
			sec := child(spending, "uncategorized", func() *Node { return &Node{Name: "Uncategorized", Kind: "uncategorized", Icon: "❔"} })
			add([]*Node{spending, sec, merchant(sec, r.Merchant)}, v, r.Count)
			continue
		}
		gk := c.GroupKind
		sec := child(spending, fmt.Sprintf("g%d", c.GroupID), func() *Node {
			return &Node{ID: c.GroupID, Name: c.GroupName, Kind: "group", Group: gk}
		})
		cn := child(sec, fmt.Sprintf("c%d", c.ID), func() *Node {
			return &Node{ID: c.ID, Name: c.Name, Icon: c.Icon, Kind: "category", Group: gk}
		})
		add([]*Node{spending, sec, cn, merchant(cn, r.Merchant)}, v, r.Count)
	}
	sortTree(income)
	sortTree(spending)
	return Tree{Income: income, Spending: spending}
}

// sortTree drops zero nodes and orders children largest first (ties by name).
func sortTree(n *Node) {
	kept := n.Children[:0]
	for _, c := range n.Children {
		if c.Total != 0 {
			sortTree(c)
			kept = append(kept, c)
		}
	}
	n.Children = kept
	sort.Slice(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Name < b.Name
	})
}
