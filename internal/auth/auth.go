// Package auth handles first-run setup, password login and sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"

	"viceroy/internal/categorize"
	"viceroy/internal/db"
)

const (
	SessionTTL        = 30 * 24 * time.Hour
	MinPasswordLength = 10
)

var (
	ErrAlreadySetUp       = errors.New("viceroy is already set up")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNoSession          = errors.New("no valid session")
)

// ValidationError is a user-facing input problem.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

type Service struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(conn *sql.DB) *Service {
	return &Service{DB: conn, Now: time.Now}
}

func (s *Service) q() *db.Queries { return db.New(s.DB) }

func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := s.q().CountUsers(ctx)
	return n == 0, err
}

type SetupInput struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	HouseholdName string `json:"household_name"`
}

// Setup creates the first (admin) user and their household. It fails once any user exists.
func (s *Service) Setup(ctx context.Context, in SetupInput) (db.User, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	in.HouseholdName = strings.TrimSpace(in.HouseholdName)
	if in.HouseholdName == "" {
		in.HouseholdName = in.Name + "'s household"
	}
	if err := validateUser(in.Name, in.Email, in.Password); err != nil {
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
	if n, err := q.CountUsers(ctx); err != nil {
		return db.User{}, err
	} else if n > 0 {
		return db.User{}, ErrAlreadySetUp
	}
	now := s.Now().Unix()
	u, err := q.CreateUser(ctx, db.CreateUserParams{
		Email: in.Email, Name: in.Name, PasswordHash: hash, IsAdmin: 1, CreatedAt: now,
	})
	if err != nil {
		return db.User{}, err
	}
	h, err := q.CreateHousehold(ctx, db.CreateHouseholdParams{Name: in.HouseholdName, CreatedAt: now})
	if err != nil {
		return db.User{}, err
	}
	if err := q.AddHouseholdMember(ctx, db.AddHouseholdMemberParams{HouseholdID: h.ID, UserID: u.ID, Role: "owner"}); err != nil {
		return db.User{}, err
	}
	if err := categorize.SeedDefaults(ctx, q, h.ID); err != nil {
		return db.User{}, err
	}
	return u, tx.Commit()
}

func validateUser(name, email, password string) error {
	if name == "" {
		return ValidationError{"Name is required."}
	}
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return ValidationError{"Enter a valid email address."}
	}
	if len(password) < MinPasswordLength {
		return ValidationError{"Password must be at least 10 characters."}
	}
	return nil
}

// dummyHash keeps login timing similar whether or not the email exists.
var dummyHash, _ = argon2id.CreateHash("viceroy-timing-equalizer", argon2id.DefaultParams)

func (s *Service) Login(ctx context.Context, email, password string) (db.User, error) {
	u, err := s.q().GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, sql.ErrNoRows) {
		argon2id.ComparePasswordAndHash(password, dummyHash)
		return db.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return db.User{}, err
	}
	ok, err := argon2id.ComparePasswordAndHash(password, u.PasswordHash)
	if err != nil {
		return db.User{}, err
	}
	if !ok {
		return db.User{}, ErrInvalidCredentials
	}
	return u, nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// CreateSession returns an opaque token; only its hash is stored.
func (s *Service) CreateSession(ctx context.Context, userID int64, userAgent, ip string) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := s.Now()
	exp := now.Add(SessionTTL)
	err := s.q().CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hashToken(token), UserID: userID,
		CreatedAt: now.Unix(), ExpiresAt: exp.Unix(), LastSeenAt: now.Unix(),
		UserAgent: truncate(userAgent, 256), Ip: ip,
	})
	return token, exp, err
}

// Authenticate resolves a session token to its user, sliding the expiry forward
// at most once an hour.
func (s *Service) Authenticate(ctx context.Context, token string) (db.User, error) {
	if token == "" {
		return db.User{}, ErrNoSession
	}
	now := s.Now()
	q := s.q()
	sess, err := q.GetSession(ctx, db.GetSessionParams{TokenHash: hashToken(token), ExpiresAt: now.Unix()})
	if errors.Is(err, sql.ErrNoRows) {
		return db.User{}, ErrNoSession
	}
	if err != nil {
		return db.User{}, err
	}
	if now.Unix()-sess.LastSeenAt > 3600 {
		if err := q.TouchSession(ctx, db.TouchSessionParams{
			LastSeenAt: now.Unix(), ExpiresAt: now.Add(SessionTTL).Unix(), TokenHash: sess.TokenHash,
		}); err != nil {
			return db.User{}, err
		}
	}
	return q.GetUser(ctx, sess.UserID)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.q().DeleteSession(ctx, hashToken(token))
}

func (s *Service) PruneSessions(ctx context.Context) error {
	return s.q().DeleteExpiredSessions(ctx, s.Now().Unix())
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
