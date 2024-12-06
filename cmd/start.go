package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/openmfp/crd-gql-gateway/internal/manager"
	"github.com/openmfp/golang-commons/logger"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// Variable to hold the watched directory flag
var watchedDir string

var startCmd = &cobra.Command{
	Use:     "start",
	Short:   "Run the GQL Gateway",
	Example: "go run main.go start --watched-dir=./definitions",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()

		// watchedDir is already required by MarkFlagRequired, additional check is redundant
		// But if you want to ensure it's not empty, you can keep it
		if watchedDir == "" {
			return fmt.Errorf("the --watched-dir flag is required")
		}

		// Setup Logger
		log, err := setupLogger("INFO")
		if err != nil {
			return fmt.Errorf("failed to setup logger: %w", err)
		}

		log.Info().Str("LogLevel", log.GetLevel().String()).Msg("Starting server...")

		// Get Kubernetes config
		cfg := config.GetConfigOrDie()

		// Initialize Manager
		managerInstance, err := manager.NewManager(log, cfg, watchedDir)
		if err != nil {
			log.Error().Err(err).Msg("Error creating manager")
			return fmt.Errorf("failed to create manager: %w", err)
		}

		// Set up HTTP handler
		http.Handle("/", managerInstance)

		// Start HTTP server
		err = http.ListenAndServe(":3000", nil)
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

	// Define the --watched-dir flag for startCmd
	startCmd.Flags().StringVar(&watchedDir, "watched-dir", "",
		"The directory to watch for changes.")

	// Mark the --watched-dir flag as required
	startCmd.MarkFlagRequired("watched-dir") // nolint:errcheck
}

// setupLogger initializes the logger with the given log level
func setupLogger(logLevel string) (*logger.Logger, error) {
	loggerCfg := logger.DefaultConfig()
	loggerCfg.Name = "crdGateway"
	loggerCfg.Level = logLevel
	return logger.New(loggerCfg)
}
