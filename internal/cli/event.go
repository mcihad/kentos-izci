package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/sample"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// ingestClient builds a client for the given protocol ("grpc" uses HTTP/2:
// h2c for http:// URLs, TLS ALPN for https://).
func ingestClient(baseURL, protocol string) (ingestv1connect.IngestServiceClient, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	switch protocol {
	case "connect", "":
		return ingestv1connect.NewIngestServiceClient(&http.Client{Timeout: 30 * time.Second}, baseURL, connect.WithSendGzip()), nil
	case "grpc", "grpcweb":
		tr := &http.Transport{Protocols: new(http.Protocols), MaxIdleConnsPerHost: 256, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
		if strings.HasPrefix(baseURL, "http://") {
			tr.Protocols.SetUnencryptedHTTP2(true)
		} else {
			tr.Protocols.SetHTTP2(true)
		}
		opt := connect.WithGRPC()
		if protocol == "grpcweb" {
			opt = connect.WithGRPCWeb()
		}
		return ingestv1connect.NewIngestServiceClient(&http.Client{Transport: tr, Timeout: 30 * time.Second}, baseURL, opt, connect.WithSendGzip()), nil
	}
	return nil, fmt.Errorf("unknown protocol %q (connect, grpc or grpcweb)", protocol)
}

func newEventCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "event", Short: "Send events to an izci server (testing and demos)"}
	var url, key, protocol, message, release string
	var count, tmpl int
	send := &cobra.Command{
		Use:   "send",
		Short: "Send realistic sample events (or one message event)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if key == "" {
				return errors.New("--key is required (an izk_ project key)")
			}
			client, err := ingestClient(url, protocol)
			if err != nil {
				return err
			}
			r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7))
			out := cmd.OutOrStdout()
			for range count {
				ev := sample.Event(r, sample.Options{Template: tmpl, Release: release})
				if message != "" {
					ev.Exceptions = nil
					ev.Message = &ingestv1.Message{Formatted: message}
				}
				req := connect.NewRequest(&ingestv1.SendEventRequest{Event: ev})
				req.Header().Set("Authorization", "Bearer "+key)
				resp, err := client.SendEvent(cmd.Context(), req)
				if err != nil {
					return err
				}
				res := resp.Msg.GetResult()
				fmt.Fprintf(out, "%s  %s  %s  new=%v  %s\n", strings.TrimPrefix(res.GetOutcome().String(), "EVENT_OUTCOME_"),
					res.GetEventId(), res.GetIssueShortId(), res.GetIssueIsNew(), res.GetIssueUrl())
			}
			return nil
		},
	}
	send.Flags().StringVar(&url, "url", "http://localhost:5300", "izci base URL")
	send.Flags().StringVar(&key, "key", "", "project API key (required)")
	send.Flags().StringVar(&protocol, "protocol", "connect", "connect, grpc or grpcweb")
	send.Flags().IntVar(&count, "count", 1, "number of events")
	send.Flags().IntVar(&tmpl, "template", -1, fmt.Sprintf("sample error template 0..%d (-1 = random)", sample.Count()-1))
	send.Flags().StringVar(&message, "message", "", "send a message event with this text instead of an error")
	send.Flags().StringVar(&release, "release", "", "release to report (default: random demo release)")
	cmd.AddCommand(send)
	return cmd
}

func newBenchCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bench", Short: "Load-test an izci server"}
	var url, key, protocol string
	var duration time.Duration
	var concurrency, batch, rate int
	ingestCmd := &cobra.Command{
		Use:   "ingest",
		Short: "Send events as fast as possible (or at --rate) and report throughput and latency",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if key == "" {
				return errors.New("--key is required")
			}
			client, err := ingestClient(url, protocol)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), duration)
			defer cancel()

			var sent, accepted, failed, limited atomic.Int64
			var mu sync.Mutex
			var latencies []time.Duration
			var ticker <-chan time.Time
			if rate > 0 {
				interval := time.Second * time.Duration(batch) / time.Duration(rate)
				t := time.NewTicker(max(interval, time.Microsecond))
				defer t.Stop()
				ticker = t.C
			}

			var wg sync.WaitGroup
			start := time.Now()
			for w := range concurrency {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r := rand.New(rand.NewPCG(uint64(w), uint64(time.Now().UnixNano())))
					local := make([]time.Duration, 0, 4096)
					defer func() {
						mu.Lock()
						latencies = append(latencies, local...)
						mu.Unlock()
					}()
					for ctx.Err() == nil {
						if ticker != nil {
							select {
							case <-ticker:
							case <-ctx.Done():
								return
							}
						}
						events := make([]*ingestv1.Event, batch)
						for i := range events {
							events[i] = sample.Event(r, sample.Options{Template: -1})
						}
						t0 := time.Now()
						var ok, bad, lim int64
						if batch == 1 {
							req := connect.NewRequest(&ingestv1.SendEventRequest{Event: events[0]})
							req.Header().Set("Authorization", "Bearer "+key)
							_, err := client.SendEvent(ctx, req)
							switch {
							case err == nil:
								ok = 1
							case connect.CodeOf(err) == connect.CodeResourceExhausted:
								lim = 1
							case ctx.Err() != nil:
								return
							default:
								bad = 1
							}
						} else {
							req := connect.NewRequest(&ingestv1.SendEventsRequest{Events: events})
							req.Header().Set("Authorization", "Bearer "+key)
							resp, err := client.SendEvents(ctx, req)
							if err != nil {
								if ctx.Err() != nil {
									return
								}
								bad = int64(batch)
							} else {
								for _, res := range resp.Msg.GetResults() {
									switch res.GetOutcome() {
									case ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED, ingestv1.EventOutcome_EVENT_OUTCOME_DUPLICATE:
										ok++
									case ingestv1.EventOutcome_EVENT_OUTCOME_RATE_LIMITED:
										lim++
									default:
										bad++
									}
								}
							}
						}
						local = append(local, time.Since(t0))
						sent.Add(int64(batch))
						accepted.Add(ok)
						failed.Add(bad)
						limited.Add(lim)
					}
				}()
			}
			wg.Wait()
			elapsed := time.Since(start)
			slices.Sort(latencies)
			pct := func(p float64) time.Duration {
				if len(latencies) == 0 {
					return 0
				}
				return latencies[min(len(latencies)-1, int(float64(len(latencies))*p))]
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "duration     %s (%d workers, batch %d, %s)\n", elapsed.Round(time.Millisecond), concurrency, batch, protocol)
			fmt.Fprintf(out, "events       %d sent, %d accepted, %d rate-limited, %d failed\n", sent.Load(), accepted.Load(), limited.Load(), failed.Load())
			fmt.Fprintf(out, "throughput   %.0f events/s accepted\n", float64(accepted.Load())/elapsed.Seconds())
			fmt.Fprintf(out, "latency      p50 %s  p95 %s  p99 %s  max %s (per request)\n",
				pct(0.50).Round(time.Microsecond*100), pct(0.95).Round(time.Microsecond*100), pct(0.99).Round(time.Microsecond*100), pct(1).Round(time.Microsecond*100))
			if limited.Load() > 0 {
				fmt.Fprintln(out, "note: raise INGEST_RATE_LIMIT_PER_MINUTE (or the project limit) to measure raw throughput")
			}
			return nil
		},
	}
	ingestCmd.Flags().StringVar(&url, "url", "http://localhost:5300", "izci base URL")
	ingestCmd.Flags().StringVar(&key, "key", "", "project API key (required)")
	ingestCmd.Flags().StringVar(&protocol, "protocol", "grpc", "connect, grpc or grpcweb")
	ingestCmd.Flags().DurationVar(&duration, "duration", 20*time.Second, "test length")
	ingestCmd.Flags().IntVar(&concurrency, "concurrency", 32, "parallel workers")
	ingestCmd.Flags().IntVar(&batch, "batch", 1, "events per request (1 = SendEvent, >1 = SendEvents)")
	ingestCmd.Flags().IntVar(&rate, "rate", 0, "target events per second across workers (0 = unlimited)")
	cmd.AddCommand(ingestCmd)
	return cmd
}
