package routing

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err    error
		expect ErrorAction
	}{
		{errors.New("429 Too Many Requests"), ActionFailover},
		{errors.New("project rate limit exceeded"), ActionFailover},
		{errors.New("quota exceeded"), ActionFailover},
		{errors.New("daily request count exceeded"), ActionFailover},
		{errors.New("403 Forbidden"), ActionFailover},
		{errors.New("Invalid JSON-RPC request -32600"), ActionFatal},
		{errors.New("Method not found -32601"), ActionFatal},
		{errors.New("Parse error -32700"), ActionFatal},
		{errors.New("connection reset by peer"), ActionRetry},
		{errors.New("timeout"), ActionRetry},
		{errors.New("500 Internal Server Error"), ActionRetry},
	}

	for _, tt := range tests {
		if got := ClassifyError(tt.err); got != tt.expect {
			t.Errorf("ClassifyError(%q) = %v, want %v", tt.err, got, tt.expect)
		}
	}
}
