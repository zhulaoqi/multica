package auth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func forEachCLIDeviceStore(t *testing.T, fn func(*testing.T, CLIDeviceStore)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) {
		fn(t, NewMemoryCLIDeviceStore())
	})
	t.Run("redis", func(t *testing.T) {
		rawURL := os.Getenv("REDIS_TEST_URL")
		if rawURL == "" {
			t.Skip("REDIS_TEST_URL not set")
		}
		opts, err := redis.ParseURL(rawURL)
		if err != nil {
			t.Fatalf("parse REDIS_TEST_URL: %v", err)
		}
		opts.DB = 13
		rdb := redis.NewClient(opts)
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			t.Skipf("REDIS_TEST_URL unreachable: %v", err)
		}
		if err := rdb.FlushDB(context.Background()).Err(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = rdb.FlushDB(context.Background()).Err()
			_ = rdb.Close()
		})
		fn(t, NewRedisCLIDeviceStore(rdb))
	})
}

func TestCLIDeviceStoreApprovalAndIdempotentCompletion(t *testing.T) {
	forEachCLIDeviceStore(t, func(t *testing.T, store CLIDeviceStore) {
		ctx := context.Background()
		if err := store.Create(ctx, "digest-1", "ABCD-EFGH", time.Minute); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Get(ctx, "ABCD-EFGH", "wrong-digest"); !errors.Is(err, ErrCLIDeviceNotFound) {
			t.Fatalf("wrong secret error = %v", err)
		}
		if _, err := store.Get(ctx, "ABCD-EFGH", "digest-1"); !errors.Is(err, ErrCLIDevicePending) {
			t.Fatalf("get before approval error = %v", err)
		}
		if err := store.Approve(ctx, "ABCD-EFGH", "user-1"); err != nil {
			t.Fatal(err)
		}
		approval, err := store.Get(ctx, "ABCD-EFGH", "digest-1")
		if err != nil {
			t.Fatal(err)
		}
		if approval.UserID != "user-1" || approval.Token != "" {
			t.Fatalf("approval = %+v", approval)
		}
		token, err := store.Complete(ctx, "ABCD-EFGH", "digest-1", "token-1", 30*time.Second)
		if err != nil || token != "token-1" {
			t.Fatalf("first complete = %q, %v", token, err)
		}
		token, err = store.Complete(ctx, "ABCD-EFGH", "digest-1", "token-2", 30*time.Second)
		if err != nil || token != "token-1" {
			t.Fatalf("second complete = %q, %v", token, err)
		}
		approval, err = store.Get(ctx, "ABCD-EFGH", "digest-1")
		if err != nil || approval.Token != "token-1" {
			t.Fatalf("completed approval = %+v, %v", approval, err)
		}
	})
}

func TestCLIDeviceStoreFirstApprovalWins(t *testing.T) {
	forEachCLIDeviceStore(t, func(t *testing.T, store CLIDeviceStore) {
		ctx := context.Background()
		if err := store.Create(ctx, "digest-2", "JKLM-NPQR", time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := store.Approve(ctx, "JKLM-NPQR", "user-1"); err != nil {
			t.Fatal(err)
		}
		if err := store.Approve(ctx, "JKLM-NPQR", "user-2"); !errors.Is(err, ErrCLIDeviceAlreadyApproved) {
			t.Fatalf("second approval error = %v", err)
		}
		approval, err := store.Get(ctx, "JKLM-NPQR", "digest-2")
		if err != nil || approval.UserID != "user-1" {
			t.Fatalf("approval = %+v, %v", approval, err)
		}
	})
}

func TestMemoryCLIDeviceStoreExpiresSessions(t *testing.T) {
	store := NewMemoryCLIDeviceStore()
	now := time.Now()
	store.now = func() time.Time { return now }
	if err := store.Create(context.Background(), "digest-3", "STUV-WXYZ", time.Second); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := store.Approve(context.Background(), "STUV-WXYZ", "user-1"); !errors.Is(err, ErrCLIDeviceNotFound) {
		t.Fatalf("approval after expiry error = %v", err)
	}
	if _, err := store.Get(context.Background(), "STUV-WXYZ", "digest-3"); !errors.Is(err, ErrCLIDeviceNotFound) {
		t.Fatalf("get after expiry error = %v", err)
	}
}

func TestMemoryCLIDeviceStoreBoundsPendingSessions(t *testing.T) {
	store := NewMemoryCLIDeviceStore()
	store.maxSessions = 1
	if err := store.Create(context.Background(), "digest-1", "ABCD-EFGH", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), "digest-2", "JKLM-NPQR", time.Minute); !errors.Is(err, ErrCLIDeviceCapacity) {
		t.Fatalf("second create error = %v", err)
	}
}
