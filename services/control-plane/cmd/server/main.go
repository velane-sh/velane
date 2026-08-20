package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/app"
	"go.uber.org/zap"
)

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := app.Bootstrap(ctx, log)
	if err != nil {
		log.Fatal("bootstrap failed", zap.Error(err))
	}
	defer a.Close()

	srv := &http.Server{
		Addr:         ":" + a.Port,
		Handler:      a.Router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	var hostSrv *http.Server
	if a.HostRouter != nil {
		hostSrv = &http.Server{Addr: a.SandboxHostListenAddr, Handler: a.HostRouter, TLSConfig: a.SandboxHostTLSConfig, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 120 * time.Second}
		go func() {
			log.Info("sandbox host server listening", zap.String("addr", hostSrv.Addr))
			if err := hostSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatal("sandbox host server error", zap.Error(err))
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("http server listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("server error", zap.Error(err))
		}
	}()

	<-quit
	log.Info("shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", zap.Error(err))
	}
	if hostSrv != nil {
		if err := hostSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("sandbox host graceful shutdown failed", zap.Error(err))
		}
	}
	log.Info("server stopped")
}
