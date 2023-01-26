package metrics

import (
	"context"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/prompb"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"os"
	"strings"
	"time"
)

type TimeSeriesWriter interface {
	Write(ctx context.Context, wr prompb.WriteRequest) error
}

type ServiceOpts struct {
	Coordinator TimeSeriesWriter
}

// Service manages the collection of metrics for ingestors.
type Service struct {
	Coordinator TimeSeriesWriter
	closing     chan struct{}

	hostname string
}

func NewService(opts ServiceOpts) *Service {
	return &Service{
		closing:     make(chan struct{}),
		Coordinator: opts.Coordinator,
	}
}

func (s *Service) Open() error {
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	s.hostname = hostname
	go s.collect()
	return nil
}

func (s *Service) Close() error {
	close(s.closing)
	return nil
}

func (s *Service) collect() {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()

	var lastCount float64
	for {
		select {
		case <-s.closing:
			return
		case <-t.C:
			mets, err := prometheus.DefaultGatherer.Gather()
			if err != nil {
				logger.Error("Failed to gather metrics: %s", err)
				continue
			}

			timestamp := time.Now().UTC().UnixMilli()
			req := prompb.WriteRequest{}
			for _, v := range mets {
				switch *v.Type {
				case io_prometheus_client.MetricType_COUNTER:
					for _, vv := range v.Metric {
						if !strings.HasPrefix(v.GetName(), Namespace) {
							continue
						}

						if strings.Contains(v.GetName(), "samples_stored_total") {
							logger.Info("Rate %0.2f, %f %f", (vv.Counter.GetValue()-lastCount)/10, lastCount, vv.Counter.GetValue())
							lastCount = vv.Counter.GetValue()
						}

						ts := prompb.TimeSeries{}
						ts.Labels = append(ts.Labels, prompb.Label{
							Name:  []byte("__name__"),
							Value: []byte(v.GetName()),
						})
						for _, label := range vv.Label {
							ts.Labels = append(ts.Labels, prompb.Label{
								[]byte(label.GetName()),
								[]byte(label.GetValue()),
							})
						}

						ts.Samples = append(ts.Samples, prompb.Sample{
							Value:     vv.Counter.GetValue(),
							Timestamp: int64(timestamp),
						})

						req.Timeseries = append(req.Timeseries, ts)
					}
				}
			}
			if err := s.Coordinator.Write(context.Background(), req); err != nil {
				logger.Error("Failed to write metrics: %s", err)
			}
		}
	}
}
