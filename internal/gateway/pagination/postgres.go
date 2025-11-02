package pagination

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BuildCursorWhere generates a WHERE clause for cursor-based pagination
// order should be "ASC" or "DESC"
func BuildCursorWhere(cursor *Cursor, order string, timeColumn, idColumn string) (clause string, args []interface{}) {
	if cursor == nil {
		return "", nil
	}

	hasTimestamp := cursor.Timestamp != nil && timeColumn != ""
	if hasTimestamp {
		return buildCompositeWhere(cursor, order, timeColumn, idColumn)
	}
	return buildIDOnlyWhere(cursor, order, idColumn)
}

func buildCompositeWhere(cursor *Cursor, order, timeColumn, idColumn string) (string, []interface{}) {
	argPos := 1
	args := []interface{}{cursor.Timestamp, cursor.Timestamp, cursor.ID}

	if order == "DESC" {
		// For DESC order: (timestamp < cursor_time) OR (timestamp = cursor_time AND id < cursor_id)
		return fmt.Sprintf(
			"((%s < $%d) OR (%s = $%d AND %s < $%d))",
			timeColumn, argPos, timeColumn, argPos+1, idColumn, argPos+2,
		), args
	}

	// For ASC order: (timestamp > cursor_time) OR (timestamp = cursor_time AND id > cursor_id)
	return fmt.Sprintf(
		"((%s > $%d) OR (%s = $%d AND %s > $%d))",
		timeColumn, argPos, timeColumn, argPos+1, idColumn, argPos+2,
	), args
}

func buildIDOnlyWhere(cursor *Cursor, order, idColumn string) (string, []interface{}) {
	argPos := 1
	args := []interface{}{cursor.ID}

	operator := ">"
	if order == "DESC" {
		operator = "<"
	}

	return fmt.Sprintf("%s %s $%d", idColumn, operator, argPos), args
}

// CountWithFilters executes a COUNT query with optional WHERE clause
func CountWithFilters(
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	whereClause string,
	args []interface{},
) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if whereClause != "" {
		query += " WHERE " + whereClause
	}

	var count int
	err := pool.QueryRow(ctx, query, args...).Scan(&count)
	return count, err
}
