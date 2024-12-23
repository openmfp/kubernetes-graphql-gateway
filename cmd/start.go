package cmd

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"time"

	"github.com/openmfp/crd-gql-gateway/internal/manager"
	"github.com/openmfp/golang-commons/logger"
	"github.com/spf13/cobra"
	restCfg "sigs.k8s.io/controller-runtime/pkg/client/config"

	appCfg "github.com/openmfp/crd-gql-gateway/internal/config"
)

var startCmd = &cobra.Command{
	Use:     "start",
	Short:   "Run the GQL Gateway",
	Example: "go run main.go start --watched-dir=./definitions",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()

		appCfg, err := appCfg.NewFromEnv()
		if err != nil {
			log.Fatal().Err(err).Msg("Error getting app restCfg, exiting")
		}

		log, err := setupLogger(appCfg.LogLevel)
		if err != nil {
			return fmt.Errorf("failed to setup logger: %w", err)
		}

		log.Info().Str("LogLevel", log.GetLevel().String()).Msg("Starting server...")

		// Get Kubernetes restCfg
		restCfg, err := restCfg.GetConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("Error getting Kubernetes restCfg, exiting")
		}

		// Initialize Manager
		managerInstance, err := manager.NewManager(log, restCfg, appCfg)
		if err != nil {
			log.Error().Err(err).Msg("Error creating manager")
			return fmt.Errorf("failed to create manager: %w", err)
		}

		// Set up HTTP handler
		http.Handle("/", managerInstance)

		// Start HTTP server
		err = http.ListenAndServe(fmt.Sprintf(":%s", appCfg.Port), nil)
		if err != nil {
			log.Error().Err(err).Msg("Error starting server")
			return fmt.Errorf("failed to start server: %w", err)
		}

		log.Info().Float64("elapsed_seconds", time.Since(start).Seconds()).Msg("Setup completed")

		return nil
	},
}

func init() {
	// Assuming rootCmd is defined in another file within the cmd package
	// Add startCmd as a subcommand to rootCmd
	rootCmd.AddCommand(startCmd)
}

// setupLogger initializes the logger with the given log level
func setupLogger(logLevel string) (*logger.Logger, error) {
	loggerCfg := logger.DefaultConfig()
	loggerCfg.Name = "crdGateway"
	loggerCfg.Level = logLevel
	return logger.New(loggerCfg)
}
