package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/budget"
	"viceroy/internal/budgetio"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
)

func (s *Server) budgetIORoutes(r chi.Router) {
	r.Get("/budget/export", s.handleBudgetExport)
	r.Post("/budget/import/preview", s.handleBudgetImportPreview)
	r.Post("/budget/import", s.handleBudgetImport)
}

// budgetSetup is the month's budget as it stands: groups with their categories (or goals),
// monthly amounts and schedules.
func budgetSetup(ctx context.Context, q *db.Queries, hh int64, month string) (budgetview.View, error) {
	m, err := budget.ParseMonth(month)
	if err != nil {
		return budgetview.View{}, err
	}
	return budgetview.Build(ctx, q, hh, budget.ViewMonth, m, budgetview.Today())
}

// importMonth reads a YYYY-MM month, defaulting to the current one.
func importMonth(v string) (string, bool) {
	if v == "" {
		return budget.MonthKey(budgetview.Today()), true
	}
	_, err := budget.ParseMonth(v)
	return v, err == nil
}

func (s *Server) handleBudgetExport(w http.ResponseWriter, r *http.Request) {
	month, ok := importMonth(r.URL.Query().Get("month"))
	if !ok {
		writeError(w, http.StatusBadRequest, "month must be YYYY-MM")
		return
	}
	v, err := budgetSetup(r.Context(), db.New(s.db), HouseholdID(r), month)
	if err != nil {
		s.internalError(w, err)
		return
	}
	var rows []budgetio.Row
	for _, g := range v.Groups {
		for _, l := range g.Lines {
			row := budgetio.Row{Group: g.Name, Category: l.Name, Icon: l.Icon, Amount: l.MonthBudget}
			if strings.HasPrefix(row.Icon, "/api/") { // an uploaded image doesn't travel in a CSV
				row.Icon = ""
			}
			if g.Kind != "goals" {
				row.Timing = &l.Chunk
			}
			rows = append(rows, row)
		}
	}
	var buf bytes.Buffer
	if err := budgetio.WriteCSV(&buf, rows); err != nil {
		s.internalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="viceroy-budget-%s.csv"`, month))
	w.Write(buf.Bytes())
}

// importRow is a preview line. Target says where it goes: "cat:<id>", "goal:<id>", a new
// category "new:<group id>", a new goal "new:goals", or "skip".
type importRow struct {
	budgetio.Row
	Target    string        `json:"target"`
	Status    string        `json:"status"` // new | changed | same | skip
	OldAmount int64         `json:"old_amount"`
	OldTiming *budget.Chunk `json:"old_timing"`
	Note      string        `json:"note,omitempty"`
}

// matchRows decides a target for each row: an existing category or goal with the same name
// (in any group), else a new one in the group named in the file.
func matchRows(v budgetview.View, rows []budgetio.Row) ([]importRow, []string) {
	type existing struct {
		target string
		line   budgetview.Line
		goal   bool
	}
	byName := map[string]existing{}
	groupByName := map[string]budgetview.Group{}
	groupByKind := map[string]budgetview.Group{}
	for _, g := range v.Groups {
		groupByName[strings.ToLower(g.Name)] = g
		if _, ok := groupByKind[g.Kind]; !ok {
			groupByKind[g.Kind] = g
		}
		for _, l := range g.Lines {
			t := fmt.Sprintf("cat:%d", l.ID)
			if g.Kind == "goals" {
				t = fmt.Sprintf("goal:%d", l.ID)
			}
			if _, dup := byName[strings.ToLower(l.Name)]; !dup {
				byName[strings.ToLower(l.Name)] = existing{t, l, g.Kind == "goals"}
			}
		}
	}
	var problems []string
	seen := map[string]int{}
	out := make([]importRow, 0, len(rows))
	for _, r := range rows {
		ir := importRow{Row: r}
		if e, ok := byName[strings.ToLower(r.Category)]; ok {
			ir.Target, ir.OldAmount = e.target, e.line.MonthBudget
			if !e.goal {
				c := e.line.Chunk
				ir.OldTiming = &c
			} else {
				ir.Timing = nil // goals have no schedule
			}
			if ir.Icon == "" {
				ir.Icon = e.line.Icon
			}
			if r.Source != "" {
				ir.Note = fmt.Sprintf("“%s” in your budget.", r.Source)
			}
			ir.Status = "same"
			if ir.Amount != ir.OldAmount || ir.Timing != nil && !sameTiming(*ir.Timing, *ir.OldTiming) {
				ir.Status = "changed"
			}
		} else {
			g, ok := groupByName[strings.ToLower(r.Group)]
			if !ok {
				g, ok = groupByKind[budgetio.GroupKind(r.Group)]
			}
			switch {
			case budgetio.GroupKind(r.Group) == "transfer":
				ir.Target, ir.Status, ir.Note = "skip", "skip", "Transfers aren't budgeted."
			case !ok:
				g = groupByKind["flexible"]
				ir.Target, ir.Status = fmt.Sprintf("new:%d", g.ID), "new"
				if r.Group != "" {
					ir.Note = fmt.Sprintf("No group called “%s”; added to %s.", r.Group, g.Name)
				}
			case g.Kind == "goals":
				ir.Target, ir.Status, ir.Timing = "new:goals", "new", nil
			default:
				ir.Target, ir.Status = fmt.Sprintf("new:%d", g.ID), "new"
			}
		}
		key := ir.Target
		if strings.HasPrefix(key, "new:") {
			key += ":" + strings.ToLower(r.Category)
		}
		if i, dup := seen[key]; dup && ir.Target != "skip" {
			problems = append(problems, fmt.Sprintf("%s is listed twice; the last amount is used.", r.Category))
			out[i].Target, out[i].Status, out[i].Note = "skip", "skip", "Listed again below."
		}
		seen[key] = len(out)
		out = append(out, ir)
	}
	return out, problems
}

// Screenshots and pasted text can be large: four images of up to 8 MB each, plus text.
const importBodyLimit = budgetio.MaxImages*budgetio.MaxImageBytes + 1<<20

func (s *Server) handleBudgetImportPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Month  string   `json:"month"`
		CSV    string   `json:"csv"`
		Text   string   `json:"text"`
		Images []string `json:"images"`
	}
	if !readJSONLimit(w, r, &in, importBodyLimit) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	bad := func(msg string) { writeError(w, http.StatusBadRequest, msg) }
	month, ok := importMonth(in.Month)
	if !ok {
		bad("month must be YYYY-MM")
		return
	}
	anchor := month + "-01"
	q := db.New(s.db)
	v, err := budgetSetup(ctx, q, hh, month)
	if err != nil {
		s.internalError(w, err)
		return
	}
	var (
		rows     []budgetio.Row
		problems []string
		source   = "csv"
		model    string
	)
	switch {
	case strings.TrimSpace(in.CSV) != "":
		if rows, problems, err = budgetio.ParseCSV(in.CSV, anchor); err != nil {
			bad(capitalize(err.Error()) + ".")
			return
		}
	case strings.TrimSpace(in.Text) != "" || len(in.Images) > 0:
		source = "ai"
		if len(in.Images) > budgetio.MaxImages {
			bad(fmt.Sprintf("Attach at most %d screenshots.", budgetio.MaxImages))
			return
		}
		for _, img := range in.Images {
			if !budgetio.ValidImage(img) {
				bad("Screenshots must be PNG, JPEG, WebP or GIF images under 8 MB.")
				return
			}
		}
		client, err := s.ai.VisionClient(ctx, hh)
		if err != nil {
			s.internalError(w, err)
			return
		}
		client.Feature = "budget_import"
		if !client.Configured() {
			bad("Add an OpenRouter API key in Settings → AI to read budgets with AI.")
			return
		}
		model = client.Model
		var groups []budgetio.Group
		for _, g := range v.Groups {
			bg := budgetio.Group{Name: g.Name, Kind: g.Kind}
			for _, l := range g.Lines {
				bg.Categories = append(bg.Categories, l.Name)
			}
			groups = append(groups, bg)
		}
		actx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		reply, err := client.CompleteJSON(actx, budgetio.Messages(groups, in.Text, in.Images))
		if err != nil {
			writeError(w, http.StatusBadGateway, "The AI couldn't read it: "+err.Error())
			return
		}
		if rows, problems, err = budgetio.ParseReply(reply, anchor); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": capitalize(err.Error()) + ".", "problems": problems})
			return
		}
	default:
		bad("Choose a CSV file, paste a budget or add a screenshot.")
		return
	}
	if len(rows) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "No budget lines found.", "problems": problems})
		return
	}
	out, dup := matchRows(v, rows)
	writeJSON(w, http.StatusOK, map[string]any{
		"month": month, "source": source, "model": model, "rows": out, "problems": append(problems, dup...),
	})
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type importApplyRow struct {
	Target   string        `json:"target"`
	Category string        `json:"category"`
	Icon     string        `json:"icon"`
	Amount   int64         `json:"amount"`
	Timing   *budget.Chunk `json:"timing"`
}

// handleBudgetImport writes the chosen rows: each amount applies from the month onward, a
// schedule (when given) replaces the category's, and new categories and goals are created
// (reusing one with the same name, so importing twice is safe).
func (s *Server) handleBudgetImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Month string           `json:"month"`
		Rows  []importApplyRow `json:"rows"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	bad := func(msg string) { writeError(w, http.StatusBadRequest, msg) }
	month, ok := importMonth(in.Month)
	if !ok {
		bad("month must be YYYY-MM")
		return
	}
	if len(in.Rows) > budgetio.MaxRows {
		bad(fmt.Sprintf("Import at most %d lines at once.", budgetio.MaxRows))
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	groups, err := q.ListCategoryGroups(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	cats, err := q.ListCategories(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	goals, err := q.ListGoals(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	idx, err := budgetview.LoadAmounts(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	budgetable := map[int64]bool{}
	for _, g := range groups {
		budgetable[g.ID] = budgetview.Kinds[g.Kind]
	}
	catByID := map[int64]db.Category{}
	for _, c := range cats {
		catByID[c.ID] = c
	}
	goalIDs := map[int64]bool{}
	for _, g := range goals {
		goalIDs[g.ID] = true
	}
	res := struct {
		Created   int `json:"created"`
		Updated   int `json:"updated"`
		Unchanged int `json:"unchanged"`
	}{}
	for _, row := range in.Rows {
		name := strings.Join(strings.Fields(row.Category), " ")
		if row.Amount < 0 || row.Amount > 1_000_000_000 {
			bad("Amounts must be between 0 and 10,000,000.")
			return
		}
		if row.Timing != nil {
			if err := row.Timing.Validate(); err != nil {
				bad(fmt.Sprintf("%s: spending schedule: %v.", name, err))
				return
			}
		}
		kind, idStr, _ := strings.Cut(row.Target, ":")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		var cat, goal sql.NullInt64
		created := false
		switch {
		case row.Target == "skip":
			continue
		case kind == "cat":
			c, ok := catByID[id]
			if !ok || !budgetable[c.GroupID] {
				bad("Unknown category for " + name + ".")
				return
			}
			cat = sql.NullInt64{Int64: id, Valid: true}
		case kind == "goal":
			if !goalIDs[id] {
				bad("Unknown goal for " + name + ".")
				return
			}
			goal = sql.NullInt64{Int64: id, Valid: true}
		case row.Target == "new:goals":
			if name == "" {
				bad("A new goal needs a name.")
				return
			}
			for _, g := range goals {
				if strings.EqualFold(g.Name, name) && g.Archived == 0 {
					goal = sql.NullInt64{Int64: g.ID, Valid: true}
				}
			}
			if !goal.Valid {
				icon := row.Icon
				if icon == "" {
					icon = "🎯"
				}
				g, err := q.CreateGoal(ctx, db.CreateGoalParams{HouseholdID: hh, Name: name, Icon: icon, CreatedAt: time.Now().Unix()})
				if err != nil {
					s.internalError(w, err)
					return
				}
				goals = append(goals, db.ListGoalsRow{ID: g.ID, Name: g.Name})
				goal, created = sql.NullInt64{Int64: g.ID, Valid: true}, true
			}
		case kind == "new":
			if !budgetable[id] {
				bad("Pick a budget group for " + name + ".")
				return
			}
			if name == "" {
				bad("A new category needs a name.")
				return
			}
			for _, c := range cats {
				if c.GroupID == id && strings.EqualFold(c.Name, name) && c.Archived == 0 {
					cat = sql.NullInt64{Int64: c.ID, Valid: true}
				}
			}
			if !cat.Valid {
				icon := row.Icon
				if icon == "" {
					icon = "📦"
				}
				c, err := q.CreateCategory(ctx, db.CreateCategoryParams{HouseholdID: hh, GroupID: id, Name: name, Icon: icon, Sort: int64(len(cats))})
				if err != nil {
					s.internalError(w, err)
					return
				}
				cats = append(cats, c)
				catByID[c.ID] = c
				cat, created = sql.NullInt64{Int64: c.ID, Valid: true}, true
			}
		default:
			bad("Unknown target " + strconv.Quote(row.Target) + ".")
			return
		}

		rows := idx.Cats[cat.Int64]
		if goal.Valid {
			rows = idx.Goals[goal.Int64]
		}
		changed := budget.Resolve(rows, month) != row.Amount
		rows = budget.SetAmount(rows, month, row.Amount, true)
		if goal.Valid {
			idx.Goals[goal.Int64] = rows
		} else {
			idx.Cats[cat.Int64] = rows
		}
		if err := q.DeleteBudgetAmountsFor(ctx, db.DeleteBudgetAmountsForParams{HouseholdID: hh, CategoryID: cat, GoalID: goal}); err != nil {
			s.internalError(w, err)
			return
		}
		for _, a := range rows {
			if err := q.InsertBudgetAmount(ctx, db.InsertBudgetAmountParams{
				HouseholdID: hh, CategoryID: cat, GoalID: goal, Month: a.Month, AmountCents: a.Amount, Forward: b2i(a.Forward),
			}); err != nil {
				s.internalError(w, err)
				return
			}
		}
		if cat.Valid && row.Timing != nil {
			// A timing in the file never turns off "leave out of pacing".
			row.Timing.NoPacing = row.Timing.NoPacing || budgetview.ParseChunk(catByID[cat.Int64].Chunk).NoPacing
			v := ""
			if row.Timing.Kind != budget.Even || row.Timing.NoPacing {
				b, _ := json.Marshal(row.Timing)
				v = string(b)
			}
			if c := catByID[cat.Int64]; c.Chunk != v {
				changed = true
				c.Chunk = v
				catByID[cat.Int64] = c
				if err := q.SetCategoryChunk(ctx, db.SetCategoryChunkParams{Chunk: v, ID: cat.Int64, HouseholdID: hh}); err != nil {
					s.internalError(w, err)
					return
				}
			}
		}
		switch {
		case created:
			res.Created++
		case changed:
			res.Updated++
		default:
			res.Unchanged++
		}
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// sameTiming compares schedules, ignoring the pacing flag (import files don't carry it).
func sameTiming(a, b budget.Chunk) bool {
	a.NoPacing, b.NoPacing = false, false
	return a == b
}
