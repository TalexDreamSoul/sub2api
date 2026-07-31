package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *channelMonitorRepository) RecordIncidentObservation(ctx context.Context, observation service.ChannelMonitorIncidentObservation) error {
	if observation.MonitorID <= 0 {
		return fmt.Errorf("channel monitor observation requires monitor id")
	}
	failureTarget := observation.FailureTarget
	if failureTarget <= 0 {
		failureTarget = 3
	}
	checkedAt := observation.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO channel_monitor_health_states (monitor_id)
		VALUES ($1) ON CONFLICT (monitor_id) DO NOTHING`, observation.MonitorID); err != nil {
		return err
	}
	var streak int
	var open bool
	var version int64
	var lastObservedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT failure_streak,incident_open,incident_version,last_observed_at
		FROM channel_monitor_health_states WHERE monitor_id=$1 FOR UPDATE`, observation.MonitorID).
		Scan(&streak, &open, &version, &lastObservedAt); err != nil {
		return err
	}
	if lastObservedAt.Valid && !checkedAt.After(lastObservedAt.Time) {
		return tx.Commit()
	}

	eventKind := ""
	streak, open, version, eventKind = advanceChannelMonitorIncidentState(streak, open, version, observation.Failed, failureTarget)
	if _, err := tx.ExecContext(ctx, `UPDATE channel_monitor_health_states
		SET failure_streak=$2,incident_open=$3,incident_version=$4,last_observed_at=$5,updated_at=NOW()
		WHERE monitor_id=$1`, observation.MonitorID, streak, open, version, checkedAt); err != nil {
		return err
	}
	if eventKind != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO channel_monitor_notification_events (
			monitor_id,incident_version,event_kind,monitor_name,provider,model,
			observed_status,latency_ms,checked_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (monitor_id,incident_version,event_kind) DO NOTHING`,
			observation.MonitorID, version, eventKind, strings.TrimSpace(observation.MonitorName),
			strings.TrimSpace(observation.Provider), strings.TrimSpace(observation.Model),
			strings.TrimSpace(observation.Status), observation.LatencyMs, checkedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func advanceChannelMonitorIncidentState(streak int, open bool, version int64, failed bool, failureTarget int) (int, bool, int64, string) {
	if failureTarget <= 0 {
		failureTarget = 3
	}
	if failed {
		streak++
		if !open && streak >= failureTarget {
			return streak, true, version + 1, "incident"
		}
		return streak, open, version, ""
	}
	if open {
		return 0, false, version, "recovery"
	}
	return 0, false, version, ""
}

func (r *channelMonitorRepository) ClaimNotificationEvents(ctx context.Context, workerID string, limit int, lease time.Duration) ([]service.ChannelMonitorNotificationEvent, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 30 {
		leaseSeconds = 240
	}
	rows, err := r.db.QueryContext(ctx, `WITH candidates AS (
		SELECT e.id FROM channel_monitor_notification_events e
		WHERE ((e.status='pending' AND e.available_at<=NOW())
		   OR (e.status='processing' AND e.claimed_at<NOW()-($3*INTERVAL '1 second')))
		  AND NOT EXISTS (
			SELECT 1 FROM channel_monitor_notification_events prior
			WHERE prior.monitor_id=e.monitor_id AND prior.id<e.id
			  AND prior.status IN ('pending','processing')
		  )
		ORDER BY e.id LIMIT $2 FOR UPDATE OF e SKIP LOCKED
	)
	UPDATE channel_monitor_notification_events e
	SET status='processing',claimed_at=NOW(),claimed_by=$1,updated_at=NOW()
	FROM candidates WHERE e.id=candidates.id
	RETURNING e.id,e.monitor_id,e.incident_version,e.event_kind,
		e.monitor_name,e.provider,e.model,e.observed_status,e.latency_ms,
		e.checked_at,e.attempts`, strings.TrimSpace(workerID), limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]service.ChannelMonitorNotificationEvent, 0, limit)
	for rows.Next() {
		var event service.ChannelMonitorNotificationEvent
		var latency sql.NullInt64
		if err := rows.Scan(&event.ID, &event.MonitorID, &event.IncidentVersion, &event.EventKind,
			&event.MonitorName, &event.Provider, &event.Model, &event.ObservedStatus, &latency,
			&event.CheckedAt, &event.Attempts); err != nil {
			return nil, err
		}
		if latency.Valid {
			value := int(latency.Int64)
			event.LatencyMs = &value
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *channelMonitorRepository) MarkNotificationEventSent(ctx context.Context, id int64, workerID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE channel_monitor_notification_events
		SET status='sent',completed_at=NOW(),claimed_at=NULL,claimed_by=NULL,last_error=NULL,updated_at=NOW()
		WHERE id=$1 AND status='processing' AND claimed_by=$2`, id, strings.TrimSpace(workerID))
	return requireChannelMonitorNotificationClaim(result, err, id)
}

func (r *channelMonitorRepository) RetryNotificationEvent(ctx context.Context, id int64, workerID string, availableAt time.Time, errorCode string, dead bool) error {
	status := "pending"
	var completedAt any
	if dead {
		status = "dead"
		completedAt = time.Now()
	}
	result, err := r.db.ExecContext(ctx, `UPDATE channel_monitor_notification_events
		SET status=$3,attempts=attempts+1,available_at=$4,last_error=$5,
			completed_at=$6,claimed_at=NULL,claimed_by=NULL,updated_at=NOW()
		WHERE id=$1 AND status='processing' AND claimed_by=$2`, id, strings.TrimSpace(workerID),
		status, availableAt, truncateFeishuDeliveryError(errorCode), completedAt)
	return requireChannelMonitorNotificationClaim(result, err, id)
}

func (r *channelMonitorRepository) CleanupNotificationEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM channel_monitor_notification_events
		WHERE status IN ('sent','dead') AND completed_at<$1`, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func requireChannelMonitorNotificationClaim(result sql.Result, err error, id int64) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("channel monitor notification event %d is no longer claimed", id)
	}
	return nil
}
