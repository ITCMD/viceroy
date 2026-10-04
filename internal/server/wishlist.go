package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"viceroy/internal/ai"
	"viceroy/internal/budget"
	"viceroy/internal/budgetview"
	"viceroy/internal/db"
	"viceroy/internal/money"
	"viceroy/internal/wishlist"
)

// wishlistGoal is goals.builtin for the household's Wishlist goal.
const wishlistGoal = "wishlist"

func (s *Server) wishlistRoutes(r chi.Router) {
	r.Get("/wishlist", s.handleListWishlist)
	r.Post("/wishlist/preview", s.handleWishlistPreview)
	r.Post("/wishlist", s.handleCreateWishlistItem)
	r.Patch("/wishlist/{id}", s.handleUpdateWishlistItem)
	r.Delete("/wishlist/{id}", s.handleDeleteWishlistItem)
	r.Get("/wishlist/{id}/image", s.handleGetWishlistImage)
	r.Put("/wishlist/{id}/image", s.handlePutWishlistImage)
	r.Delete("/wishlist/{id}/image", s.handleDeleteWishlistImage)
	r.Post("/wishlist/{id}/bought", s.handleWishlistBought)
	r.Delete("/wishlist/{id}/bought", s.handleWishlistUnbought)
}

// ensureWishlistGoal returns the household's Wishlist goal, creating it the first time.
func ensureWishlistGoal(ctx context.Context, q *db.Queries, hh int64) (db.Goal, error) {
	g, err := q.GetBuiltinGoal(ctx, db.GetBuiltinGoalParams{HouseholdID: hh, Builtin: wishlistGoal})
	if errors.Is(err, sql.ErrNoRows) {
		return q.CreateBuiltinGoal(ctx, db.CreateBuiltinGoalParams{
			HouseholdID: hh, Name: "Wishlist", Icon: "🌠", Builtin: wishlistGoal, CreatedAt: time.Now().Unix(),
		})
	}
	return g, err
}

type wishMember struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type wishItemDTO struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Store       string  `json:"store"`
	Price       *int64  `json:"price_cents"`
	PriceSource string  `json:"price_source"`
	ImageURL    string  `json:"image_url"` // served by /wishlist/{id}/image; "" = none
	Stars       int64   `json:"stars"`
	SavesMoney  bool    `json:"saves_money"`
	Notes       string  `json:"notes"`
	WantedBy    []int64 `json:"wanted_by"`
	AddedBy     *int64  `json:"added_by"`
	CreatedAt   int64   `json:"created_at"`
	Score       float64 `json:"score"` // 0 without a price
	ScoreText   string  `json:"score_text"`
	Afford      string  `json:"afford"` // now | month_end | ""
	BoughtAt    *string `json:"bought_at"`
	BoughtTxnID *int64  `json:"bought_txn_id"`
	TxnName     string  `json:"txn_name"`
	TxnAmount   int64   `json:"txn_amount"`
	TxnDate     string  `json:"txn_date"`
}

type affordDTO struct {
	GoalID           int64  `json:"goal_id"`
	GoalName         string `json:"goal_name"`
	GoalIcon         string `json:"goal_icon"`
	SavedNow         int64  `json:"saved_now"`
	MonthEnd         int64  `json:"month_end"`         // saved by the end of the month
	MonthBudget      int64  `json:"month_budget"`      // this month's Wishlist contribution
	MonthContributed int64  `json:"month_contributed"` // put in so far this month
	OnBudget         bool   `json:"on_budget"`         // spending is within the budget this month
	MonthEndDate     string `json:"month_end_date"`
	Month            string `json:"month"`
}

// GET /wishlist?sort=added|price|score&person=<user id>: items in order with what the
// Wishlist goal can afford, bought items, and household members.
func (s *Server) handleListWishlist(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	q := db.New(s.db)
	goal, err := ensureWishlistGoal(ctx, q, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	sortBy := r.URL.Query().Get("sort")
	if sortBy != wishlist.SortPrice && sortBy != wishlist.SortScore {
		sortBy = wishlist.SortAdded
	}
	person, _ := queryInt(r, "person")

	rows, err := q.ListWishlistItems(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	wanterRows, err := q.ListWishlistWanters(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	wanters := map[int64][]int64{}
	for _, wr := range wanterRows {
		wanters[wr.ItemID] = append(wanters[wr.ItemID], wr.UserID)
	}
	users, err := q.ListHouseholdUsers(ctx, hh)
	if err != nil {
		s.internalError(w, err)
		return
	}
	members := make([]wishMember, len(users))
	for i, u := range users {
		members[i] = wishMember{u.ID, u.Name}
	}
	afford, err := s.wishlistAfford(ctx, q, hh, goal)
	if err != nil {
		s.internalError(w, err)
		return
	}

	byID := map[int64]wishItemDTO{}
	var open []wishlist.Item
	bought := []wishItemDTO{}
	for _, it := range rows {
		d := wishItemDTO{
			ID: it.ID, Title: it.Title, URL: it.Url, Store: it.Store, Price: nullCents(it.PriceCents), PriceSource: it.PriceSource,
			Stars: it.Stars, SavesMoney: it.SavesMoney == 1, Notes: it.Notes, WantedBy: wanters[it.ID], AddedBy: ptr(it.AddedBy),
			CreatedAt: it.CreatedAt, BoughtTxnID: ptr(it.BoughtTxnID), TxnName: it.TxnName, TxnAmount: it.TxnAmount, TxnDate: it.TxnDate,
		}
		if d.WantedBy == nil {
			d.WantedBy = []int64{}
		}
		if it.ImageUpdatedAt != 0 {
			d.ImageURL = fmt.Sprintf("/api/wishlist/%d/image?v=%d", it.ID, it.ImageUpdatedAt)
		}
		ri := wishlist.Item{ID: it.ID, Price: d.Price, Stars: int(it.Stars), Wanters: len(d.WantedBy), SavesMoney: d.SavesMoney, CreatedAt: it.CreatedAt}
		d.Score, _ = wishlist.Score(ri)
		d.ScoreText = wishlist.ScoreText(ri)
		if it.BoughtAt.Valid {
			d.BoughtAt = &it.BoughtAt.String
			bought = append(bought, d)
			continue
		}
		byID[it.ID] = d
		open = append(open, ri)
	}
	// Affordability is for the whole household's list in this order, whoever it's filtered to.
	wishlist.Sort(open, sortBy)
	marks := wishlist.Afford(open, afford.SavedNow, afford.MonthEnd)
	items := []wishItemDTO{}
	for _, ri := range open {
		d := byID[ri.ID]
		d.Afford = marks[ri.ID]
		if person != 0 && !containsID(d.WantedBy, person) {
			continue
		}
		items = append(items, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "bought": bought, "members": members, "afford": afford, "sort": sortBy,
	})
}

func containsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func nullCents(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// wishlistAfford works out what the Wishlist goal holds now and by the end of the month. The
// rest of this month's contribution counts only while the month's spending is on budget.
func (s *Server) wishlistAfford(ctx context.Context, q *db.Queries, hh int64, goal db.Goal) (affordDTO, error) {
	out := affordDTO{GoalID: goal.ID, GoalName: goal.Name, GoalIcon: goal.Icon}
	goals, err := q.ListGoals(ctx, hh)
	if err != nil {
		return out, err
	}
	for _, g := range goals {
		if g.ID == goal.ID {
			out.SavedNow = g.StartingCents + g.ContributedCents - g.WithdrawnCents
		}
	}
	today := budgetview.Today()
	v, err := budgetview.Build(ctx, q, hh, budget.ViewMonth, today, today)
	if err != nil {
		return out, err
	}
	out.Month, out.MonthEndDate = v.Month, v.End
	for _, g := range v.Groups {
		if g.Kind != "goals" {
			continue
		}
		for _, l := range g.Lines {
			if l.ID == goal.ID {
				out.MonthBudget, out.MonthContributed = l.Budget, l.Actual
			}
		}
	}
	out.OnBudget = v.Summary.ExpenseActual <= v.Summary.ExpenseBudget
	out.MonthEnd = out.SavedNow
	if out.OnBudget && out.MonthBudget > out.MonthContributed {
		out.MonthEnd += out.MonthBudget - out.MonthContributed
	}
	return out, nil
}

// POST /wishlist/preview {url}: what the product page says (title, price, image). Falls back
// to the light AI model for the price when the page has none in its markup.
func (s *Server) handleWishlistPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.wish.Fetch(r.Context(), in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, capitalize(err.Error())+".")
		return
	}
	if p.Price == nil && !p.Refused {
		if client, err := s.ai.EmailClient(r.Context(), HouseholdID(r)); err == nil && client.Configured() {
			client.Feature = "wishlist"
			if err := wishlist.AIFill(r.Context(), client, &p); err != nil && !errors.Is(err, ai.ErrNotConfigured) {
				s.log.Warn("wishlist AI price", "err", err)
			}
		}
	}
	writeJSON(w, http.StatusOK, p)
}

type wishItemIn struct {
	Title        *string  `json:"title"`
	URL          *string  `json:"url"`
	Price        *string  `json:"price"`         // dollars; "" = unknown
	PriceFetched bool     `json:"price_fetched"` // the price came from the page preview unchanged
	Stars        *int64   `json:"stars"`
	SavesMoney   *bool    `json:"saves_money"`
	Notes        *string  `json:"notes"`
	WantedBy     *[]int64 `json:"wanted_by"`
	// Image: a data URL the app already shrank, or ImageURL to download (from the preview).
	Image    string `json:"image"`
	ImageURL string `json:"image_url"`
}

// apply merges in into it; it returns a user-facing error message.
func (in *wishItemIn) apply(it *db.WishlistItem) string {
	if in.Title != nil {
		it.Title = strings.TrimSpace(*in.Title)
	}
	if it.Title == "" || utf8.RuneCountInString(it.Title) > 200 {
		return "Give it a name (up to 200 characters)."
	}
	if in.URL != nil {
		raw := strings.TrimSpace(*in.URL)
		it.Url, it.Store = "", ""
		if raw != "" {
			u, err := wishlist.CheckURL(raw)
			if err != nil || len(raw) > 2000 {
				return "Enter a link starting with http:// or https://."
			}
			it.Url, it.Store = wishlist.CleanURL(u).String(), wishlist.StoreName(u.Hostname())
		}
	}
	if in.Price != nil {
		old := it.PriceCents
		it.PriceCents = sql.NullInt64{}
		if p := strings.TrimSpace(*in.Price); p != "" {
			c, err := money.ParseCents(p)
			if err != nil || c <= 0 {
				return "Enter a price like 49.99, or leave it empty."
			}
			it.PriceCents = sql.NullInt64{Int64: c, Valid: true}
		}
		switch {
		case !it.PriceCents.Valid:
			it.PriceSource = ""
		case in.PriceFetched:
			it.PriceSource = "fetched"
			it.PriceCheckedAt = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
		case old != it.PriceCents || it.PriceSource == "":
			it.PriceSource = "user"
		}
	}
	if in.Stars != nil {
		if *in.Stars < 1 || *in.Stars > 5 {
			return "Stars go from 1 to 5."
		}
		it.Stars = *in.Stars
	}
	if in.SavesMoney != nil {
		it.SavesMoney = b2i(*in.SavesMoney)
	}
	if in.Notes != nil {
		it.Notes = strings.TrimSpace(*in.Notes)
		if utf8.RuneCountInString(it.Notes) > 2000 {
			return "Notes can be up to 2000 characters."
		}
	}
	if in.ImageURL != "" {
		it.ImageSourceUrl = in.ImageURL
	}
	return ""
}

// saveWanters replaces who wants the item with household members from ids.
func saveWanters(ctx context.Context, q *db.Queries, hh, item int64, ids []int64) error {
	users, err := q.ListHouseholdUsers(ctx, hh)
	if err != nil {
		return err
	}
	if err := q.ClearWishlistWanters(ctx, item); err != nil {
		return err
	}
	for _, u := range users {
		if containsID(ids, u.ID) {
			if err := q.AddWishlistWanter(ctx, db.AddWishlistWanterParams{ItemID: item, UserID: u.ID}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) handleCreateWishlistItem(w http.ResponseWriter, r *http.Request) {
	var in wishItemIn
	if !readJSONLimit(w, r, &in, wishlist.MaxImageBytes*2) {
		return
	}
	ctx, hh, me := r.Context(), HouseholdID(r), CurrentUser(r)
	it := db.WishlistItem{Stars: 3}
	if msg := in.apply(&it); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	img, msg := s.wishlistImage(ctx, in)
	if msg != "" && in.Image != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	if _, err := ensureWishlistGoal(ctx, q, hh); err != nil {
		s.internalError(w, err)
		return
	}
	created, err := q.CreateWishlistItem(ctx, db.CreateWishlistItemParams{
		HouseholdID: hh, Title: it.Title, Url: it.Url, Store: it.Store, PriceCents: it.PriceCents, PriceSource: it.PriceSource,
		PriceCheckedAt: it.PriceCheckedAt, ImageSourceUrl: it.ImageSourceUrl, Stars: it.Stars, SavesMoney: it.SavesMoney,
		Notes: it.Notes, AddedBy: sql.NullInt64{Int64: me.ID, Valid: me.ID != 0}, CreatedAt: time.Now().UnixNano(),
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	wanted := []int64{me.ID}
	if in.WantedBy != nil {
		wanted = *in.WantedBy
	}
	if err := saveWanters(ctx, q, hh, created.ID, wanted); err != nil {
		s.internalError(w, err)
		return
	}
	if img != nil {
		if err := q.SetWishlistImage(ctx, db.SetWishlistImageParams{ItemID: created.ID, Mime: img.mime, Data: img.data, UpdatedAt: time.Now().UnixNano()}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	out := map[string]any{"id": created.ID}
	if msg != "" {
		out["image_problem"] = msg // the item is saved; the picture can be added later
	}
	writeJSON(w, http.StatusCreated, out)
}

type wishImage struct {
	data []byte
	mime string
}

// wishlistImage reads the image given with an item: an uploaded data URL, or a link to
// download (from the page preview). Returns nil without one; msg explains a bad one.
func (s *Server) wishlistImage(ctx context.Context, in wishItemIn) (*wishImage, string) {
	switch {
	case in.Image != "":
		_, b64, found := strings.Cut(in.Image, ";base64,")
		data, err := base64.StdEncoding.DecodeString(b64)
		if !found || err != nil || len(data) == 0 {
			return nil, "Upload a PNG, JPEG, WebP or GIF image."
		}
		if len(data) > wishlist.MaxImageBytes {
			return nil, "That image is too large (256 KB at most)."
		}
		mime := http.DetectContentType(data) // what's served, whatever the data URL claimed
		if !wishlist.AllowedImage(mime) {
			return nil, "Upload a PNG, JPEG, WebP or GIF image."
		}
		return &wishImage{data, mime}, ""
	case in.ImageURL != "":
		raw, _, err := s.wish.Image(ctx, in.ImageURL)
		if err != nil {
			return nil, "Couldn't download the picture: " + err.Error()
		}
		data, mime, err := wishlist.Shrink(raw)
		if err != nil {
			return nil, "Couldn't use the picture: " + err.Error() + "."
		}
		return &wishImage{data, mime}, ""
	}
	return nil, ""
}

func (s *Server) loadWishlistItem(w http.ResponseWriter, r *http.Request, q *db.Queries) (db.WishlistItem, bool) {
	it, err := q.GetWishlistItem(r.Context(), db.GetWishlistItemParams{ID: txnID(r), HouseholdID: HouseholdID(r)})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Wishlist item not found.")
		return it, false
	}
	if err != nil {
		s.internalError(w, err)
		return it, false
	}
	return it, true
}

func (s *Server) handleUpdateWishlistItem(w http.ResponseWriter, r *http.Request) {
	var in wishItemIn
	if !readJSONLimit(w, r, &in, wishlist.MaxImageBytes*2) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	img, msg := s.wishlistImage(ctx, in)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	it, ok := s.loadWishlistItem(w, r, q)
	if !ok {
		return
	}
	if msg := in.apply(&it); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := q.UpdateWishlistItem(ctx, db.UpdateWishlistItemParams{
		Title: it.Title, Url: it.Url, Store: it.Store, PriceCents: it.PriceCents, PriceSource: it.PriceSource,
		PriceCheckedAt: it.PriceCheckedAt, ImageSourceUrl: it.ImageSourceUrl, Stars: it.Stars, SavesMoney: it.SavesMoney,
		Notes: it.Notes, ID: it.ID, HouseholdID: hh,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	if in.WantedBy != nil {
		if err := saveWanters(ctx, q, hh, it.ID, *in.WantedBy); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if img != nil {
		if err := q.SetWishlistImage(ctx, db.SetWishlistImageParams{ItemID: it.ID, Mime: img.mime, Data: img.data, UpdatedAt: time.Now().UnixNano()}); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /wishlist/{id}. A bought item's purchase stays spent from the goal.
func (s *Server) handleDeleteWishlistItem(w http.ResponseWriter, r *http.Request) {
	if err := db.New(s.db).DeleteWishlistItem(r.Context(), db.DeleteWishlistItemParams{ID: txnID(r), HouseholdID: HouseholdID(r)}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetWishlistImage(w http.ResponseWriter, r *http.Request) {
	img, err := db.New(s.db).GetWishlistImage(r.Context(), db.GetWishlistImageParams{ItemID: txnID(r), HouseholdID: HouseholdID(r)})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", img.Mime)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // the URL carries ?v=
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Write(img.Data)
}

// PUT /wishlist/{id}/image {image: data URL} or {image_url}.
func (s *Server) handlePutWishlistImage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Image    string `json:"image"`
		ImageURL string `json:"image_url"`
	}
	if !readJSONLimit(w, r, &in, wishlist.MaxImageBytes*2) {
		return
	}
	q := db.New(s.db)
	it, ok := s.loadWishlistItem(w, r, q)
	if !ok {
		return
	}
	img, msg := s.wishlistImage(r.Context(), wishItemIn{Image: in.Image, ImageURL: in.ImageURL})
	if img == nil {
		if msg == "" {
			msg = "Upload a PNG, JPEG, WebP or GIF image."
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := q.SetWishlistImage(r.Context(), db.SetWishlistImageParams{ItemID: it.ID, Mime: img.mime, Data: img.data, UpdatedAt: time.Now().UnixNano()}); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteWishlistImage(w http.ResponseWriter, r *http.Request) {
	q := db.New(s.db)
	it, ok := s.loadWishlistItem(w, r, q)
	if !ok {
		return
	}
	if err := q.DeleteWishlistImage(r.Context(), it.ID); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /wishlist/{id}/bought {transaction_id | date}: moves the item to Bought. A purchase
// transaction is assigned to the Wishlist goal as money spent from it.
func (s *Server) handleWishlistBought(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TransactionID int64  `json:"transaction_id"`
		Date          string `json:"date"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	it, ok := s.loadWishlistItem(w, r, q)
	if !ok {
		return
	}
	if it.BoughtTxnID.Valid {
		if err := releasePurchase(ctx, q, hh, it.BoughtTxnID.Int64); err != nil {
			s.internalError(w, err)
			return
		}
	}
	txnRef := sql.NullInt64{}
	date := in.Date
	if in.TransactionID != 0 {
		t, err := q.GetTransactionByID(ctx, in.TransactionID)
		if err != nil || t.HouseholdID != hh {
			writeError(w, http.StatusBadRequest, "Unknown transaction.")
			return
		}
		if t.AmountCents >= 0 {
			writeError(w, http.StatusBadRequest, "Pick the purchase (money out).")
			return
		}
		goal, err := ensureWishlistGoal(ctx, q, hh)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if err := q.SetTransactionGoalWithdrawal(ctx, db.SetTransactionGoalWithdrawalParams{
			GoalID: sql.NullInt64{Int64: goal.ID, Valid: true}, GoalWithdrawal: 1, ID: t.ID, HouseholdID: hh,
		}); err != nil {
			s.internalError(w, err)
			return
		}
		txnRef, date = sql.NullInt64{Int64: t.ID, Valid: true}, t.Date
	}
	if date == "" {
		date = budgetview.Today().Format(time.DateOnly)
	}
	if _, err := budget.ParseDate(date); err != nil {
		writeError(w, http.StatusBadRequest, "Pick a date.")
		return
	}
	if err := q.SetWishlistBought(ctx, db.SetWishlistBoughtParams{
		BoughtAt: sql.NullString{String: date, Valid: true}, BoughtTxnID: txnRef, ID: it.ID, HouseholdID: hh,
	}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// releasePurchase takes a purchase transaction off the Wishlist goal (when it's still there).
func releasePurchase(ctx context.Context, q *db.Queries, hh, txnID int64) error {
	t, err := q.GetTransactionByID(ctx, txnID)
	if err != nil || t.HouseholdID != hh || t.GoalWithdrawal != 1 {
		return nil
	}
	goal, err := ensureWishlistGoal(ctx, q, hh)
	if err != nil || t.GoalID.Int64 != goal.ID {
		return err
	}
	return q.SetTransactionGoalWithdrawal(ctx, db.SetTransactionGoalWithdrawalParams{ID: t.ID, HouseholdID: hh})
}

// DELETE /wishlist/{id}/bought: back on the list; the purchase leaves the goal.
func (s *Server) handleWishlistUnbought(w http.ResponseWriter, r *http.Request) {
	ctx, hh := r.Context(), HouseholdID(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer tx.Rollback()
	q := db.New(tx)
	it, ok := s.loadWishlistItem(w, r, q)
	if !ok {
		return
	}
	if it.BoughtTxnID.Valid {
		if err := releasePurchase(ctx, q, hh, it.BoughtTxnID.Int64); err != nil {
			s.internalError(w, err)
			return
		}
	}
	if err := q.SetWishlistBought(ctx, db.SetWishlistBoughtParams{ID: it.ID, HouseholdID: hh}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
