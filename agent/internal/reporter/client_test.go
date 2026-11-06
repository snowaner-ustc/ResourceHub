package reporter

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatal("nil should not be retryable")
	}
	if !IsRetryable(errors.New("connection refused")) {
		t.Fatal("network errors should be retryable")
	}
	if IsRetryable(&StatusError{Op: "submit job", StatusCode: 400, Status: "400 Bad Request"}) {
		t.Fatal("4xx should not be retryable")
	}
	if !IsRetryable(&StatusError{Op: "submit job", StatusCode: 500, Status: "500 Internal Server Error"}) {
		t.Fatal("5xx should be retryable")
	}
	if !IsRetryable(fmt.Errorf("wrap: %w", &StatusError{Op: "submit job", StatusCode: 503, Status: "503"})) {
		t.Fatal("wrapped 5xx should be retryable")
	}
}
