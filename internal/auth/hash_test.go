package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHashSlotsBusy(t *testing.T) {
	defer func(w time.Duration) { hashWait = w }(hashWait)
	hashWait = 20 * time.Millisecond
	for i := 0; i < cap(hashSlots); i++ {
		hashSlots <- struct{}{}
	}
	_, err := checkPassword(context.Background(), "pw", dummyHash)
	for i := 0; i < cap(hashSlots); i++ {
		<-hashSlots
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if ok, err := checkPassword(context.Background(), "viceroy-timing-equalizer", dummyHash); !ok || err != nil {
		t.Fatalf("free slot = %v %v", ok, err)
	}
}
