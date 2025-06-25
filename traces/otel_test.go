package traces

import (
	"context"
	"net"
	"testing"
	"time"

	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

type mockExporter struct {
	sdkTrace.SpanExporter
}

func TestConfig_initProvider_Success(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
	}
	exporter := &mockExporter{}
	shutdown, err := cfg.initProvider(ctx, exporter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Error("expected shutdown function, got nil")
	}
}

func TestInitProvider_HappyPath(t *testing.T) {
	// Start a dummy gRPC server to simulate the OTLP collector
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(s, &dummyTraceServer{})
	go func() {
		err := s.Serve(lis)
		if err != nil {
			t.Logf("gRPC server exited: %v", err)
		}
	}()
	defer s.Stop()

	ctx := context.Background()
	cfg := Config{
		ServiceName:       "happy-service",
		ServiceVersion:    "1.2.3",
		CollectorEndpoint: lis.Addr().String(),
	}
	shutdown, err := InitProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function, got nil")
	}
	// Call shutdown to ensure it works
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

func TestInitLocalProvider(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "local-service",
		ServiceVersion: "0.1.0",
	}

	// Test with exportToConsole = false (should not error)
	shutdown, err := InitLocalProvider(ctx, cfg, false)
	if err != nil {
		t.Fatalf("expected no error with exportToConsole=false, got %v", err)
	}
	if shutdown == nil {
		t.Error("expected shutdown function, got nil")
	}

	// Test with exportToConsole = true (should not error)
	shutdown, err = InitLocalProvider(ctx, cfg, true)
	if err != nil {
		t.Fatalf("expected no error with exportToConsole=true, got %v", err)
	}
	if shutdown == nil {
		t.Error("expected shutdown function, got nil")
	}
}

// dummyTraceServer implements the OTLP TraceServiceServer interface with no-op methods.
type dummyTraceServer struct {
	collectortrace.UnimplementedTraceServiceServer
}
