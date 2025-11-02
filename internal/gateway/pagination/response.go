package pagination

// PageInfo contains cursor-based pagination metadata
type PageInfo struct {
	HasNextPage     bool    `json:"has_next_page"`
	HasPreviousPage bool    `json:"has_previous_page"`
	StartCursor     *string `json:"start_cursor,omitempty"`
	EndCursor       *string `json:"end_cursor,omitempty"`
	TotalCount      *int    `json:"total_count,omitempty"` // Optional - omit for expensive counts (River)
}

// PaginatedResponse wraps items with cursor pagination info
type PaginatedResponse[T any] struct {
	Items    []T      `json:"items"`
	PageInfo PageInfo `json:"page_info"`
}
