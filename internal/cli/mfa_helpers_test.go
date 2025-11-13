package cli

import (
	"testing"
	"time"
)

func TestIsStepUpFresh(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt *time.Time
		now       time.Time
		want      bool
	}{
		{
			name:      "nil expiration - not fresh",
			expiresAt: nil,
			now:       now,
			want:      false,
		},
		{
			name:      "expiration exactly 10 seconds in future - not fresh (buffer)",
			expiresAt: timePtr(now.Add(10 * time.Second)),
			now:       now,
			want:      false,
		},
		{
			name:      "expiration 9 seconds in future - not fresh",
			expiresAt: timePtr(now.Add(9 * time.Second)),
			now:       now,
			want:      false,
		},
		{
			name:      "expiration 11 seconds in future - fresh",
			expiresAt: timePtr(now.Add(11 * time.Second)),
			now:       now,
			want:      true,
		},
		{
			name:      "expiration 5 minutes in future - fresh",
			expiresAt: timePtr(now.Add(5 * time.Minute)),
			now:       now,
			want:      true,
		},
		{
			name:      "expiration 10 minutes in future - fresh",
			expiresAt: timePtr(now.Add(10 * time.Minute)),
			now:       now,
			want:      true,
		},
		{
			name:      "expiration in the past - not fresh",
			expiresAt: timePtr(now.Add(-1 * time.Second)),
			now:       now,
			want:      false,
		},
		{
			name:      "expiration 1 minute in past - not fresh",
			expiresAt: timePtr(now.Add(-1 * time.Minute)),
			now:       now,
			want:      false,
		},
		{
			name:      "expiration exactly now - not fresh",
			expiresAt: timePtr(now),
			now:       now,
			want:      false,
		},
		{
			name:      "expiration 1 second in future - not fresh (within buffer)",
			expiresAt: timePtr(now.Add(1 * time.Second)),
			now:       now,
			want:      false,
		},
		{
			name:      "expiration at boundary (10s + 1ns) - fresh",
			expiresAt: timePtr(now.Add(10*time.Second + 1*time.Nanosecond)),
			now:       now,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStepUpFresh(tt.expiresAt, tt.now)
			if got != tt.want {
				if tt.expiresAt != nil {
					t.Errorf("isStepUpFresh() = %v, want %v (expiresAt=%v, now=%v, diff=%v)",
						got, tt.want, *tt.expiresAt, tt.now, tt.expiresAt.Sub(tt.now))
				} else {
					t.Errorf("isStepUpFresh() = %v, want %v (expiresAt=nil)", got, tt.want)
				}
			}
		})
	}
}

// timePtr is a helper to create a pointer to a time.Time
func timePtr(t time.Time) *time.Time {
	return &t
}
