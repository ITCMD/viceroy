package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"viceroy/internal/bills"
	"viceroy/internal/db"
	"viceroy/internal/email"
	"viceroy/internal/money"
)

// handleEmailAction acts on a notice email from the bell ("From your bank"):
//   - balance: set the account's balance to the one Viceroy verified in the email; with always,
//     also add a filter that does this for every email like it (no AI needed).
//   - bill_paid: record a payment for the account, which clears its "due" badge.
//   - ignore: add a filter that ignores emails from this sender (whose subject contains
//     subject_match), and ignore this one.
func (s *Server) handleEmailAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action       string `json:"action"`
		Always       bool   `json:"always"`
		AccountID    int64  `json:"account_id"`
		SubjectMatch string `json:"subject_match"`
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
	m, err := q.GetEmailMessage(ctx, db.GetEmailMessageParams{ID: txnID(r), HouseholdID: hh})
	if err != nil {
		writeError(w, http.StatusNotFound, "Email not found.")
		return
	}
	if m.Status == email.StatusParsed {
		writeError(w, http.StatusConflict, "This email already created a transaction.")
		return
	}
	now := time.Now()
	out := struct {
		Applied  string `json:"applied"`
		FilterID *int64 `json:"filter_id"`
	}{}
	status, filterID := m.Status, m.FilterID
	newFilter := func(name string, f db.InsertEmailFilterParams) error {
		prio, err := q.NextEmailFilterPriority(ctx, hh)
		if err != nil {
			return err
		}
		f.HouseholdID, f.Name, f.Priority, f.Enabled, f.Sender, f.CreatedAt = hh, name, prio, 1, strings.ToLower(m.FromAddr), now.Unix()
		if f.Parser == "" {
			f.Parser = "generic"
		}
		if f.Sign == "" {
			f.Sign = "debit"
		}
		row, err := q.InsertEmailFilter(ctx, f)
		if err != nil {
			return err
		}
		out.FilterID, filterID = &row.ID, sql.NullInt64{Int64: row.ID, Valid: true}
		return nil
	}

	switch in.Action {
	case email.ActionBalance:
		var facts email.Facts
		if m.AiFacts == "" || json.Unmarshal([]byte(m.AiFacts), &facts) != nil || facts.Kind != "balance" {
			writeError(w, http.StatusBadRequest, "Viceroy didn't find a balance in this email.")
			return
		}
		if facts.Problem != "" || facts.AccountID == 0 {
			writeError(w, http.StatusBadRequest, "Viceroy couldn't confirm this balance: "+facts.Problem+".")
			return
		}
		if in.Always && !facts.CanAlways {
			writeError(w, http.StatusBadRequest, "Viceroy can't read this balance without AI, so it can't do this automatically.")
			return
		}
		msg, err := email.ApplyBalance(ctx, q, hh, facts.AccountID, facts.AmountCents, facts.AsOf, time.Unix(m.ReceivedAt, 0), now)
		if errors.Is(err, email.ErrStaleBalance) {
			writeError(w, http.StatusConflict, "Your bank sync already has a newer balance for this account.")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "That account no longer exists.")
			return
		}
		if err != nil {
			s.internalError(w, err)
			return
		}
		out.Applied, status = msg, email.StatusApplied
		if in.Always {
			acct, err := q.GetAccount(ctx, db.GetAccountParams{ID: facts.AccountID, HouseholdID: hh})
			if err != nil {
				s.internalError(w, err)
				return
			}
			subject := matchedSubject(m.Subject, in.SubjectMatch)
			if err := newFilter(firstNonEmpty(subject, m.FromAddr)+" → "+acct.Name+" balance", db.InsertEmailFilterParams{
				SubjectMatch: subject, BodyMatch: facts.AccountText, AccountID: acct.ID, Action: email.ActionBalance,
			}); err != nil {
				s.internalError(w, err)
				return
			}
			out.Applied += ", and will for every email like this"
		}

	case "bill_paid":
		acct, err := q.GetAccount(ctx, db.GetAccountParams{ID: in.AccountID, HouseholdID: hh})
		if err != nil {
			writeError(w, http.StatusBadRequest, "Choose an account.")
			return
		}
		amount := sql.NullInt64{}
		if b, err := q.GetBillForEmail(ctx, sql.NullInt64{Int64: m.ID, Valid: true}); err == nil && b.Kind == bills.Due {
			amount = b.AmountCents
		}
		if _, err := q.InsertAccountBill(ctx, db.InsertAccountBillParams{
			HouseholdID: hh, AccountID: sql.NullInt64{Int64: acct.ID, Valid: true}, Kind: bills.Paid, AmountCents: amount,
			Date: sql.NullString{String: now.Format(time.DateOnly), Valid: true}, Summary: "Marked paid",
			EmailMessageID: sql.NullInt64{Int64: m.ID, Valid: true}, CreatedAt: now.Unix(),
		}); err != nil {
			s.internalError(w, err)
			return
		}
		out.Applied = "Marked " + acct.Name + " paid"
		if amount.Valid {
			out.Applied += " (" + "$" + money.Format(amount.Int64) + ")"
		}

	case email.ActionIgnore:
		// A filter needs an account; use the one the email is about, else any open account.
		acctID := in.AccountID
		if acctID == 0 {
			if id, _, err := email.SuggestAccount(ctx, q, hh, email.FromRow(m)); err == nil {
				acctID = id
			}
		}
		if acctID == 0 {
			accts, err := q.ListAccounts(ctx, hh)
			if err != nil {
				s.internalError(w, err)
				return
			}
			for _, a := range accts {
				if a.Status != "closed" && a.Status != "ignored" {
					acctID = a.ID
					break
				}
			}
		}
		if acctID == 0 {
			writeError(w, http.StatusBadRequest, "Add an account first.")
			return
		}
		subject := matchedSubject(m.Subject, in.SubjectMatch)
		if subject == "" { // a sender alone would also swallow its transaction alerts
			writeError(w, http.StatusBadRequest, "Enter part of this email's subject to match.")
			return
		}
		if err := newFilter("Ignore "+firstNonEmpty(subject, m.FromAddr), db.InsertEmailFilterParams{
			SubjectMatch: subject, AccountID: acctID, Action: email.ActionIgnore,
		}); err != nil {
			s.internalError(w, err)
			return
		}
		status, out.Applied = email.StatusIgnored, "Ignoring emails like this"

	default:
		writeError(w, http.StatusBadRequest, "Unknown action.")
		return
	}

	if err := q.SetEmailApplied(ctx, db.SetEmailAppliedParams{Status: status, FilterID: filterID, Applied: out.Applied, ID: m.ID}); err != nil {
		s.internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, err)
		return
	}
	if out.FilterID != nil { // waiting emails like this one are handled too
		if _, err := s.mail.Reroute(ctx, hh); err != nil {
			s.internalError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// matchedSubject keeps the user's subject phrase only when it's really in the subject.
func matchedSubject(subject, part string) string {
	part = strings.TrimSpace(part)
	if part == "" || !strings.Contains(strings.ToLower(subject), strings.ToLower(part)) {
		return ""
	}
	return part
}
