package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"

	"viceroy/internal/db"
)

// InviteTTL is how long a join or password reset link works.
const InviteTTL = 7 * 24 * time.Hour

var (
	ErrInviteInvalid = errors.New("this link is invalid, expired or already used")
	ErrEmailTaken    = errors.New("an account with this email already exists")
	ErrLastAdmin     = errors.New("the household needs at least one admin")
	ErrNotMember     = errors.New("not a member of this household")
)

// Invite is a freshly created link token (returned once) and its row.
type Invite struct {
	Token string
	Row   db.HouseholdInvite
}

// CreateInvite makes a one-time link. userID 0 = join the household; otherwise it's a
// password reset for that member, and any earlier reset link for them is revoked.
func (s *Service) CreateInvite(ctx context.Context, householdID, createdBy, userID int64, label string) (Invite, error) {
	q := s.q()
	if userID != 0 {
		if err := s.checkMember(ctx, q, householdID, userID); err != nil {
			return Invite{}, err
		}
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return Invite{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := s.Now()
	if userID != 0 {
		if err := q.RevokeUserResets(ctx, db.RevokeUserResetsParams{RevokedAt: nullInt(now.Unix()), UserID: nullInt(userID)}); err != nil {
			return Invite{}, err
		}
	}
	row, err := q.InsertInvite(ctx, db.InsertInviteParams{
		HouseholdID: householdID, UserID: sql.NullInt64{Int64: userID, Valid: userID != 0},
		Label: truncate(strings.TrimSpace(label), 80), TokenHash: hashToken(token),
		CreatedBy: nullInt(createdBy), CreatedAt: now.Unix(), ExpiresAt: now.Add(InviteTTL).Unix(),
	})
	return Invite{Token: token, Row: row}, err
}

// LookupInvite returns an open link by its token.
func (s *Service) LookupInvite(ctx context.Context, token string) (db.GetOpenInviteByHashRow, error) {
	if token == "" {
		return db.GetOpenInviteByHashRow{}, ErrInviteInvalid
	}
	inv, err := s.q().GetOpenInviteByHash(ctx, db.GetOpenInviteByHashParams{TokenHash: hashToken(token), ExpiresAt: s.Now().Unix()})
	if errors.Is(err, sql.ErrNoRows) {
		return inv, ErrInviteInvalid
	}
	return inv, err
}

type AcceptInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AcceptInvite uses a link: a join link creates a member (name, email, password), a reset
// link sets the member's password and signs out their other sessions. It returns the user.
func (s *Service) AcceptInvite(ctx context.Context, token string, in AcceptInput) (db.User, error) {
	inv, err := s.LookupInvite(ctx, token)
	if err != nil {
		return db.User{}, err
	}
	in.Name, in.Email = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email)
	if inv.UserID.Valid {
		if len(in.Password) < MinPasswordLength {
			return db.User{}, ValidationError{"Password must be at least 10 characters."}
		}
	} else if err := validateUser(in.Name, in.Email, in.Password); err != nil {
		return db.User{}, err
	}
	hash, err := argon2id.CreateHash(in.Password, argon2id.DefaultParams)
	if err != nil {
		return db.User{}, err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return db.User{}, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	now := s.Now().Unix()
	if n, err := q.UseInvite(ctx, db.UseInviteParams{UsedAt: nullInt(now), ID: inv.ID}); err != nil {
		return db.User{}, err
	} else if n == 0 {
		return db.User{}, ErrInviteInvalid
	}
	var u db.User
	if inv.UserID.Valid {
		if err := q.SetUserPassword(ctx, db.SetUserPasswordParams{PasswordHash: hash, ID: inv.UserID.Int64}); err != nil {
			return db.User{}, err
		}
		if err := q.DeleteUserSessions(ctx, inv.UserID.Int64); err != nil {
			return db.User{}, err
		}
		if u, err = q.GetUser(ctx, inv.UserID.Int64); err != nil {
			return db.User{}, err
		}
	} else {
		if _, err := q.GetUserByEmail(ctx, in.Email); err == nil {
			return db.User{}, ErrEmailTaken
		} else if !errors.Is(err, sql.ErrNoRows) {
			return db.User{}, err
		}
		if u, err = q.CreateUser(ctx, db.CreateUserParams{Email: in.Email, Name: in.Name, PasswordHash: hash, CreatedAt: now}); err != nil {
			return db.User{}, err
		}
		if err := q.AddHouseholdMember(ctx, db.AddHouseholdMemberParams{HouseholdID: inv.HouseholdID, UserID: u.ID, Role: "member", JoinedAt: now}); err != nil {
			return db.User{}, err
		}
	}
	return u, tx.Commit()
}

// SetAdmin grants or takes away admin rights. The last admin can't be demoted.
func (s *Service) SetAdmin(ctx context.Context, householdID, userID int64, admin bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	if err := s.checkMember(ctx, q, householdID, userID); err != nil {
		return err
	}
	role, flag := "member", int64(0)
	if admin {
		role, flag = "owner", 1
	}
	if err := q.SetUserAdmin(ctx, db.SetUserAdminParams{IsAdmin: flag, ID: userID}); err != nil {
		return err
	}
	if err := q.SetMemberRole(ctx, db.SetMemberRoleParams{Role: role, HouseholdID: householdID, UserID: userID}); err != nil {
		return err
	}
	if n, err := q.CountHouseholdAdmins(ctx, householdID); err != nil {
		return err
	} else if n == 0 {
		return ErrLastAdmin
	}
	return tx.Commit()
}

// RemoveMember deletes a member's login. Their transactions, accounts and wishlist items
// stay with the household (owner/added-by is cleared); their sessions, API keys, chats and
// notification settings go with them.
func (s *Service) RemoveMember(ctx context.Context, householdID, userID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	if err := s.checkMember(ctx, q, householdID, userID); err != nil {
		return err
	}
	if err := q.DeleteUser(ctx, userID); err != nil {
		return err
	}
	if n, err := q.CountHouseholdAdmins(ctx, householdID); err != nil {
		return err
	} else if n == 0 {
		return ErrLastAdmin
	}
	return tx.Commit()
}

func (s *Service) checkMember(ctx context.Context, q *db.Queries, householdID, userID int64) error {
	h, err := q.GetUserHousehold(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && h.ID != householdID) {
		return ErrNotMember
	}
	return err
}

func nullInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

// ChangePassword checks the current password, sets a new one and signs the user out of
// every other session (keepToken stays signed in).
func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next, keepToken string) error {
	q := s.q()
	u, err := q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if ok, err := argon2id.ComparePasswordAndHash(current, u.PasswordHash); err != nil {
		return err
	} else if !ok {
		return ErrInvalidCredentials
	}
	if len(next) < MinPasswordLength {
		return ValidationError{"Password must be at least 10 characters."}
	}
	hash, err := argon2id.CreateHash(next, argon2id.DefaultParams)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qt := db.New(tx)
	if err := qt.SetUserPassword(ctx, db.SetUserPasswordParams{PasswordHash: hash, ID: userID}); err != nil {
		return err
	}
	if err := qt.DeleteOtherSessions(ctx, db.DeleteOtherSessionsParams{UserID: userID, TokenHash: hashToken(keepToken)}); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeEmail checks the current password and changes the user's sign-in email. Sessions
// stay signed in.
func (s *Service) ChangeEmail(ctx context.Context, userID int64, current, email string) error {
	q := s.q()
	u, err := q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if ok, err := argon2id.ComparePasswordAndHash(current, u.PasswordHash); err != nil {
		return err
	} else if !ok {
		return ErrInvalidCredentials
	}
	email = strings.TrimSpace(email)
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return ValidationError{"Enter a valid email address."}
	}
	if other, err := q.GetUserByEmail(ctx, email); err == nil && other.ID != userID {
		return ErrEmailTaken
	}
	err = q.SetUserEmail(ctx, db.SetUserEmailParams{Email: email, ID: userID})
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrEmailTaken // lost a race with another signup
	}
	return err
}
