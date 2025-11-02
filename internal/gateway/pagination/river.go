package pagination

import "github.com/riverqueue/river/rivertype"

// GetRiverPageInfo creates PageInfo for River jobs without expensive COUNT(*)
// Fetches limit+1 jobs and uses the extra to detect hasNextPage
func GetRiverPageInfo(jobs []*rivertype.JobRow, limit int, hasBefore bool) PageInfo {
	if len(jobs) == 0 {
		return PageInfo{
			HasNextPage:     false,
			HasPreviousPage: hasBefore,
		}
	}

	hasMore := len(jobs) > limit
	items := jobs
	if hasMore {
		items = jobs[:limit]
	}

	// Create cursors from first and last items
	var startCursor, endCursor *string
	if len(items) > 0 {
		// ID-only cursors for jobs (no timestamp needed)
		start := Cursor{ID: items[0].ID}
		end := Cursor{ID: items[len(items)-1].ID}
		startEnc := start.Encode()
		endEnc := end.Encode()
		startCursor = &startEnc
		endCursor = &endEnc
	}

	return PageInfo{
		HasNextPage:     hasMore,
		HasPreviousPage: hasBefore,
		StartCursor:     startCursor,
		EndCursor:       endCursor,
		TotalCount:      nil, // Never include count for River
	}
}
