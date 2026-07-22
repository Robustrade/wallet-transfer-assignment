package service

import "time"

// Recorder is the metrics surface the service needs. It is satisfied by
// platform/metrics.Metrics; tests use NopRecorder. Keeping it here decouples the
// business logic from Prometheus.
type Recorder interface {
	TransferEnqueued()
	TransferProcessed(result string, d time.Duration)
}

// NopRecorder is a Recorder that does nothing, for tests and runs without metrics.
type NopRecorder struct{}

func (NopRecorder) TransferEnqueued()                       {}
func (NopRecorder) TransferProcessed(string, time.Duration) {}

// Result labels used with Recorder.TransferProcessed.
const (
	resultProcessed = "processed"
	resultFailed    = "failed"
)
