package auth

import (
	"context"
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
)

// Each argon2id hash takes 64 MiB, so a burst of sign-in attempts could exhaust memory.
// hashSlots caps how many run at once; callers wait up to hashWait for a slot.
var (
	hashSlots = make(chan struct{}, 4)
	hashWait  = 10 * time.Second

	// ErrBusy means too many password checks are already running.
	ErrBusy = errors.New("too many sign-in attempts in progress")
)

func withHashSlot(ctx context.Context, fn func()) error {
	t := time.NewTimer(hashWait)
	defer t.Stop()
	select {
	case hashSlots <- struct{}{}:
	case <-t.C:
		return ErrBusy
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-hashSlots }()
	fn()
	return nil
}

func checkPassword(ctx context.Context, password, hash string) (ok bool, err error) {
	if serr := withHashSlot(ctx, func() { ok, err = argon2id.ComparePasswordAndHash(password, hash) }); serr != nil {
		return false, serr
	}
	return ok, err
}

func hashPassword(ctx context.Context, password string) (hash string, err error) {
	if serr := withHashSlot(ctx, func() { hash, err = argon2id.CreateHash(password, argon2id.DefaultParams) }); serr != nil {
		return "", serr
	}
	return hash, err
}
