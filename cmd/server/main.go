// Command server runs the Ledger: a gRPC settlement service backed by a
// double-entry PostgreSQL journal. It is the real implementation of the
// Ledger port the Service Constructor payment saga calls (freeze/capture/release).
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	ledgerv1 "github.com/nvsces/ledger/gen/ledger/v1"
	"github.com/nvsces/ledger/internal/config"
	"github.com/nvsces/ledger/internal/repository/postgres"
	"github.com/nvsces/ledger/internal/server"
	"github.com/nvsces/ledger/internal/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("ledger exited with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("applying migrations")
	if err := postgres.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := postgres.NewLedgerRepository(pool)
	svc := service.New(repo, cfg.PlatformWallet, cfg.TONAddress)
	ledgerSrv := server.NewLedgerServer(svc)

	grpcServer := grpc.NewServer()
	ledgerv1.RegisterLedgerServiceServer(grpcServer, ledgerSrv)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("gRPC server listening", "addr", cfg.GRPCAddr, "platformWallet", cfg.PlatformWallet)
		serveErr <- grpcServer.Serve(lis)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		stopped := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(cfg.ShutdownTimeout):
			grpcServer.Stop()
		}
		return nil
	}
}
