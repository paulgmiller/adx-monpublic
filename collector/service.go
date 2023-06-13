package collector

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/promremote"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_model/go"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
)

type Service struct {
	opts   *ServiceOpts
	K8sCli kubernetes.Interface

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	remoteClient *promremote.Client
	watcher      watch.Interface

	mu            sync.RWMutex
	targets       []scrapeTarget
	factory       informers.SharedInformerFactory
	pl            v12.PodLister
	srv           *http.Server
	seriesCreator *seriesCreator
}

type ServiceOpts struct {
	ListentAddr    string
	K8sCli         kubernetes.Interface
	NodeName       string
	Targets        []string
	Endpoints      []string
	AddLabels      map[string]string
	DropLabels     map[string]struct{}
	ScrapeInterval time.Duration
	DropMetrics    map[string]struct{}

	// InsecureSkipVerify skips the verification of the remote write endpoint certificate chain and host name.
	InsecureSkipVerify bool
}

type scrapeTarget struct {
	Addr      string
	Namespace string
	Pod       string
	Container string
}

func (t scrapeTarget) path() string {
	path := fmt.Sprintf("%s/%s", t.Namespace, t.Pod)
	if t.Container != "" {
		path = fmt.Sprintf("%s/%s", path, t.Container)
	}
	return path
}

func NewService(opts *ServiceOpts) (*Service, error) {
	return &Service{
		opts:   opts,
		K8sCli: opts.K8sCli,
		seriesCreator: &seriesCreator{
			AddLabels:  opts.AddLabels,
			DropLabels: opts.DropLabels,
		},
	}, nil
}

func (s *Service) Open(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	var err error
	s.remoteClient, err = promremote.NewClient(10*time.Second, s.opts.InsecureSkipVerify)
	if err != nil {
		return fmt.Errorf("failed to create prometheus remote client: %w", err)
	}

	// Add static targets
	for _, target := range s.opts.Targets {
		logger.Info("Adding static target %s", target)
		s.targets = append(s.targets, scrapeTarget{
			Addr: target,
		})
	}

	// Discover the initial targets running on the node
	pods, err := s.K8sCli.CoreV1().Pods("").List(s.ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=" + s.opts.NodeName),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != s.opts.NodeName {
			continue
		}

		targets := makeTargets(&pod)
		for _, target := range targets {
			logger.Info("Adding target %s %s", target.path(), target)
			s.targets = append(s.targets, target)
		}
	}

	factory := informers.NewSharedInformerFactory(s.K8sCli, time.Minute)
	podsInformer := factory.Core().V1().Pods().Informer()

	factory.Start(s.ctx.Done()) // Start processing these informers.
	factory.WaitForCacheSync(s.ctx.Done())
	s.factory = factory

	pl := factory.Core().V1().Pods().Lister()
	s.pl = pl

	if _, err := podsInformer.AddEventHandler(s); err != nil {
		return err
	}

	logger.Info("Listening at %s", s.opts.ListentAddr)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	s.srv = &http.Server{Addr: s.opts.ListentAddr, Handler: mux}

	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	go s.scrape()
	return nil
}

func (s *Service) Close() error {
	s.cancel()
	s.srv.Shutdown(s.ctx)
	s.factory.Shutdown()
	s.wg.Wait()
	return nil
}

func (s *Service) scrape() {
	s.wg.Add(1)
	defer s.wg.Done()

	t := time.NewTicker(s.opts.ScrapeInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.scrapeTargets()
		}
	}
}

func (s *Service) scrapeTargets() {
	targets := s.Targets()
	for _, target := range targets {
		fams, err := FetchMetrics(target.Addr)
		if err != nil {
			logger.Error("Failed to scrape metrics for %s: %s", target, err.Error())
			continue
		}

		wr := &prompb.WriteRequest{}
		for name, val := range fams {
			// Drop metrics that are in the drop list
			var drop bool
			for prefix := range s.opts.DropMetrics {
				if strings.HasPrefix(name, prefix) {
					drop = true
					break
				}
			}
			if drop {
				continue
			}

			for _, m := range val.Metric {
				ts := s.newSeries(name, target, m)

				timestamp := m.GetTimestampMs()
				if timestamp == 0 {
					timestamp = time.Now().UnixNano() / 1e6
				}
				sample := prompb.Sample{
					Timestamp: timestamp,
				}

				if m.GetCounter() != nil {
					sample.Value = m.GetCounter().GetValue()
				} else if m.GetGauge() != nil {
					sample.Value = m.GetGauge().GetValue()
				} else if m.GetUntyped() != nil {
					sample.Value = m.GetUntyped().GetValue()
				} else if m.GetSummary() != nil {
					sum := m.GetSummary()

					// Add the quantile series
					for _, q := range sum.GetQuantile() {
						ts := s.newSeries(name, target, m)
						ts.Labels = append(ts.Labels, prompb.Label{
							Name:  []byte("quantile"),
							Value: []byte(fmt.Sprintf("%f", q.GetQuantile())),
						})
						ts.Samples = []prompb.Sample{
							{
								Timestamp: timestamp,
								Value:     q.GetValue(),
							},
						}
						wr.Timeseries = append(wr.Timeseries, ts)
					}

					// Add sum series
					ts := s.newSeries(fmt.Sprintf("%s_sum", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     sum.GetSampleSum(),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)

					// Add sum series
					ts = s.newSeries(fmt.Sprintf("%s_count", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     float64(sum.GetSampleCount()),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)
				} else if m.GetHistogram() != nil {
					hist := m.GetHistogram()
					// Add the quantile series
					for _, q := range hist.GetBucket() {
						ts := s.newSeries(name, target, m)
						ts.Labels = append(ts.Labels, prompb.Label{
							Name:  []byte("le"),
							Value: []byte(fmt.Sprintf("%f", q.GetUpperBound())),
						})
						ts.Samples = []prompb.Sample{
							{
								Timestamp: timestamp,
								Value:     q.GetCumulativeCountFloat(),
							},
						}
						wr.Timeseries = append(wr.Timeseries, ts)
					}

					// Add sum series
					ts := s.newSeries(fmt.Sprintf("%s_sum", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     hist.GetSampleSum(),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)

					// Add sum series
					ts = s.newSeries(fmt.Sprintf("%s_count", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     float64(hist.GetSampleCount()),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)
				}

				ts.Samples = append(ts.Samples, sample)
				wr.Timeseries = append(wr.Timeseries, ts)
			}
		}

		if len(s.opts.Endpoints) == 0 || logger.IsDebug() {
			var sb strings.Builder
			for _, ts := range wr.Timeseries {
				sb.Reset()
				for i, l := range ts.Labels {
					sb.Write(l.Name)
					sb.WriteString("=")
					sb.Write(l.Value)
					if i < len(ts.Labels)-1 {
						sb.Write([]byte(","))
					}
				}
				sb.Write([]byte(" "))
				for _, s := range ts.Samples {
					logger.Debug("%s %d %f", sb.String(), s.Timestamp, s.Value)
				}

			}
		}

		// TODO: Send write requests to separate goroutines
		for _, endpoint := range s.opts.Endpoints {
			if err := s.remoteClient.Write(s.ctx, endpoint, wr); err != nil {
				logger.Error(err.Error())
			}
		}
	}
}

func (s *Service) newSeries(name string, scrapeTarget scrapeTarget, m *io_prometheus_client.Metric) prompb.TimeSeries {
	return s.seriesCreator.newSeries(name, scrapeTarget, m)
}

func (s *Service) OnAdd(obj interface{}) {
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targets, exists := s.isScrapeable(p)

	// Not a scrape-able pod
	if len(targets) == 0 {
		return
	}

	// We're already scraping this pod, nothing to do
	if exists {
		return
	}

	for _, target := range targets {
		logger.Info("Adding target %s %s", target.path(), target)
		s.targets = append(s.targets, target)
	}
}

func (s *Service) OnUpdate(oldObj, newObj interface{}) {
	s.OnAdd(newObj)
}

func (s *Service) OnDelete(obj interface{}) {
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targets, exists := s.isScrapeable(p)

	// Not a scrapeable pod
	if len(targets) == 0 {
		return
	}

	// We're not currently scraping this pod, nothing to do
	if !exists {
		return
	}

	var remainingTargets []scrapeTarget
	for _, target := range targets {
		logger.Info("Removing target %s %s", target.path(), target)
		for _, v := range s.targets {
			if v.Addr == target.Addr {
				continue
			}
			remainingTargets = append(remainingTargets, v)
		}
	}
	s.targets = remainingTargets
}

// isScrapeable returns the scrape target endpoints and true if the pod is currently a target, false otherwise
func (s *Service) isScrapeable(p *v1.Pod) ([]scrapeTarget, bool) {
	// If this pod is not schedule to this node, skip it
	if strings.ToLower(p.Spec.NodeName) != strings.ToLower(s.opts.NodeName) {
		return nil, false
	}

	targets := makeTargets(p)
	if len(targets) == 0 {
		return nil, false
	}

	// See if any of the pods targets are already being scraped
	for _, v := range s.targets {
		for _, target := range targets {
			if v.Addr == target.Addr {
				return targets, true
			}
		}
	}

	// Not scraping this pod, return all the targets
	return targets, false
}

func (s *Service) Targets() []scrapeTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a := make([]scrapeTarget, len(s.targets))
	for i, v := range s.targets {
		a[i] = v
	}
	return a
}

func makeTargets(p *v1.Pod) []scrapeTarget {
	var targets []scrapeTarget

	// Skip the pod if it has not opted in to scraping
	if p.Annotations["adx-mon/scrape"] != "true" {
		return nil
	}

	podIP := p.Status.PodIP
	if podIP == "" {
		return nil
	}

	scheme := "http"
	if p.Annotations["adx-mon/scheme"] != "" {
		scheme = p.Annotations["adx-mon/scheme"]
	}

	path := "/metrics"
	if p.Annotations["adx-mon/path"] != "" {
		path = p.Annotations["adx-mon/path"]
	}

	// Just scrape this one port
	port := p.Annotations["adx-mon/port"]

	// If a port is specified, only scrape that port on the pod
	if port != "" {
		targets = append(targets,
			scrapeTarget{
				Addr:      fmt.Sprintf("%s://%s:%s%s", scheme, podIP, port, path),
				Namespace: p.Namespace,
				Pod:       p.Name,
			})
		return targets
	}

	// Otherwise, scrape all the ports on the pod
	for _, c := range p.Spec.Containers {
		for _, cp := range c.Ports {
			targets = append(targets,
				scrapeTarget{
					Addr:      fmt.Sprintf("%s://%s:%d%s", scheme, podIP, cp.ContainerPort, path),
					Namespace: p.Namespace,
					Pod:       p.Name,
					Container: c.Name,
				})
		}
	}

	return targets
}
