//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type feishuChannelMonitorRepoStub struct {
	ChannelMonitorRepository
	events   []ChannelMonitorNotificationEvent
	sentID   int64
	monitors []*ChannelMonitor
	latest   map[int64][]*ChannelMonitorLatest
}

func (r *feishuChannelMonitorRepoStub) ClaimNotificationEvents(context.Context, string, int, time.Duration) ([]ChannelMonitorNotificationEvent, error) {
	events := r.events
	r.events = nil
	return events, nil
}
func (r *feishuChannelMonitorRepoStub) MarkNotificationEventSent(_ context.Context, id int64, _ string) error {
	r.sentID = id
	return nil
}
func (r *feishuChannelMonitorRepoStub) RetryNotificationEvent(context.Context, int64, string, time.Time, string, bool) error {
	return nil
}
func (r *feishuChannelMonitorRepoStub) CleanupNotificationEvents(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *feishuChannelMonitorRepoStub) ListEnabled(context.Context) ([]*ChannelMonitor, error) {
	return r.monitors, nil
}
func (r *feishuChannelMonitorRepoStub) ListLatestForMonitorIDs(context.Context, []int64) (map[int64][]*ChannelMonitorLatest, error) {
	return r.latest, nil
}

func TestFeishuChannelsCommandReturnsGlobalSanitizedStatus(t *testing.T) {
	repo := &feishuChannelMonitorRepoStub{
		monitors: []*ChannelMonitor{{ID: 3, Name: "Primary OpenAI", PrimaryModel: "gpt-test"}},
		latest:   map[int64][]*ChannelMonitorLatest{3: {{Model: "gpt-test", Status: MonitorStatusOperational}}},
	}
	svc := &FeishuNotificationService{channelMonitorRepo: repo}

	reply, err := svc.renderFeishuChannels(context.Background())

	require.NoError(t, err)
	require.Contains(t, reply, "Primary OpenAI · gpt-test · operational")
}

func TestChannelMonitorNotificationEventFansOutThroughOutbox(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{"code": 0}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	monitorRepo := &feishuChannelMonitorRepoStub{events: []ChannelMonitorNotificationEvent{{
		ID: 7, MonitorID: 3, IncidentVersion: 2, EventKind: "incident",
		MonitorName: "Primary OpenAI", Provider: "openai", Model: "gpt-test",
		ObservedStatus: "failed", CheckedAt: time.Now(), Attempts: 0,
	}}}
	svc.outboxRepo = outbox
	svc.channelMonitorRepo = monitorRepo
	svc.workerID = "worker-test"

	svc.processChannelMonitorNotificationsOnce(context.Background())

	require.EqualValues(t, 7, monitorRepo.sentID)
	require.NotNil(t, outbox.enqueued)
	require.Equal(t, "channel", outbox.enqueued.Category)
	require.Equal(t, "feishu:channel:user:1", outbox.enqueued.OrderingKey)
	require.Contains(t, outbox.enqueued.DedupeKey, "user:1:channel-monitor:3:2:incident")
	require.NotContains(t, string(outbox.enqueued.Payload), "error detail")
}
