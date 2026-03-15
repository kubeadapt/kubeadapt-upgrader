package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/KimMachineGun/automemlimit"
	_ "go.uber.org/automaxprocs"

	"github.com/kubeadapt/kubeadapt-upgrader/internal/backend"
	"github.com/kubeadapt/kubeadapt-upgrader/internal/config"
	"github.com/kubeadapt/kubeadapt-upgrader/internal/upgrader"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	// Initialize logger
	logger := initLogger()
	defer func() {
		_ = logger.Sync()
	}()

	logger.Info("Starting kubeadapt-upgrader")

	// Load configuration from environment
	cfg, err := config.LoadFromEnv()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	logger.Info("Configuration loaded",
		zap.String("backend_endpoint", cfg.BackendAPIEndpoint),
		zap.String("pod_name", cfg.PodName),
		zap.String("pod_namespace", cfg.PodNamespace),
		zap.String("chart_version", cfg.ChartVersion),
		zap.Bool("upgrade_enabled", cfg.UpgradeEnabled),
		zap.Duration("check_interval", cfg.UpgradeCheckInterval),
		zap.String("policy", cfg.UpgradePolicy),
		zap.String("channel", cfg.UpgradeChannel),
		zap.Bool("dry_run", cfg.UpgradeDryRun))

	// Create in-cluster Kubernetes client
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to create in-cluster config", zap.Error(err))
	}

	k8sClientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		logger.Fatal("Failed to create Kubernetes clientset", zap.Error(err))
	}

	logger.Info("Kubernetes client initialized")

	// Create backend HTTP client
	backendClient := backend.NewClient(
		cfg.BackendAPIEndpoint,
		cfg.AgentToken,
		logger,
	)

	logger.Info("Backend client initialized")

	// Create upgrader instance
	upg := upgrader.New(cfg, backendClient, k8sClientset, logger)

	// Setup context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the upgrader
	if err := upg.Start(ctx); err != nil {
		logger.Fatal("Failed to start upgrader", zap.Error(err))
	}

	logger.Info("Upgrader started successfully")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigCh
	logger.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Cancel context to signal all goroutines to stop
	cancel()

	// Stop the upgrader gracefully
	if err := upg.Stop(); err != nil {
		logger.Error("Error stopping upgrader", zap.Error(err))
	}

	logger.Info("kubeadapt-upgrader shutdown complete")
}

// initLogger initializes a zap logger based on LOG_LEVEL environment variable
func initLogger() *zap.Logger {
	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		level = "info"
	}

	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapLevel),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, err := config.Build()
	if err != nil {
		panic(err)
	}

	return logger
}
