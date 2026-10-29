package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

type codedTestError int

func (e codedTestError) Error() string { return "database busy" }
func (e codedTestError) Code() int     { return int(e) }

func TestBeginWriteRetriesOnlyBusyBeforeTransactionStarts(t *testing.T) {
	attempts := 0
	_, err := beginWrite(context.Background(), func() (*sql.Tx, error) {
		attempts++
		if attempts == 1 {
			return nil, fmt.Errorf("wrapped: %w", codedTestError(5))
		}
		return nil, nil
	})
	if err != nil || attempts != 2 {
		t.Fatal(attempts, err)
	}
}
func TestBeginWriteBoundsRetriesAndDoesNotRetryOtherFailures(t *testing.T) {
	for _, code := range []codedTestError{5, 6} {
		attempts := 0
		_, err := beginWrite(context.Background(), func() (*sql.Tx, error) { attempts++; return nil, code })
		want := 1
		if code == 5 {
			want = 3
		}
		if !errors.Is(err, code) || attempts != want {
			t.Fatal(code, attempts, err)
		}
	}
}
func TestBeginWriteStopsWhenRequestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	_, err := beginWrite(ctx, func() (*sql.Tx, error) { attempts++; cancel(); return nil, codedTestError(5) })
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatal(attempts, err)
	}
}
