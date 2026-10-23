package db

import (
	"database/sql"
	"fmt"
	"time"
)

// CheckedBuildMaxAge is how long a checked build holds up env changes when the rack never reports it finished.
// The rack's build reads the app's env as it starts, so an hour-old build has read it.
const CheckedBuildMaxAge = time.Hour

// CheckedBuild is a build whose manifest the gateway checked with the app's env (see the checked_builds migration).
// BuildID is empty until the rack answers with the build.
type CheckedBuild struct {
	ID        int64
	App       string
	BuildID   string
	CheckedAt time.Time
}

// RecordCheckedBuild records a build of app whose manifest was checked with the app's env, before it's forwarded.
// The caller holds the app's env lock.
func (d *Database) RecordCheckedBuild(app string) (int64, error) {
	var id int64
	if err := d.queryRow(`INSERT INTO checked_builds (app) VALUES (?) RETURNING id`, app).Scan(&id); err != nil {
		return 0, fmt.Errorf("failed to record the checked build: %w", err)
	}
	return id, nil
}

// SetCheckedBuildID records the ID the rack gave a checked build.
func (d *Database) SetCheckedBuildID(id int64, buildID string) error {
	if _, err := d.exec(`UPDATE checked_builds SET build_id = ? WHERE id = ?`, buildID, id); err != nil {
		return fmt.Errorf("failed to record the checked build's ID: %w", err)
	}
	return nil
}

// FinishCheckedBuild forgets a checked build that has finished, or never reached the rack.
func (d *Database) FinishCheckedBuild(id int64) error {
	if _, err := d.exec(`DELETE FROM checked_builds WHERE id = ?`, id); err != nil {
		return fmt.Errorf("failed to forget the checked build: %w", err)
	}
	return nil
}

// UnfinishedCheckedBuilds returns the app's checked builds younger than CheckedBuildMaxAge, oldest first, and
// forgets the older ones.
func (d *Database) UnfinishedCheckedBuilds(app string) ([]CheckedBuild, error) {
	_, err := d.exec(`DELETE FROM checked_builds WHERE checked_at < NOW() - ? * INTERVAL '1 second'`,
		int64(CheckedBuildMaxAge/time.Second))
	if err != nil {
		return nil, fmt.Errorf("failed to forget old checked builds: %w", err)
	}
	rows, err := d.query(`SELECT id, app, build_id, checked_at FROM checked_builds
		WHERE app = ? ORDER BY checked_at, id`, app)
	if err != nil {
		return nil, fmt.Errorf("failed to list checked builds: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var builds []CheckedBuild
	for rows.Next() {
		var build CheckedBuild
		var buildID sql.NullString
		if err := rows.Scan(&build.ID, &build.App, &buildID, &build.CheckedAt); err != nil {
			return nil, fmt.Errorf("failed to read a checked build: %w", err)
		}
		build.BuildID = buildID.String
		builds = append(builds, build)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list checked builds: %w", err)
	}
	return builds, nil
}
