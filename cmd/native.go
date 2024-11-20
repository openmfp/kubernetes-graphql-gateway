package cmd

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/openmfp/crd-gql-gateway/native/manager"
	"github.com/openmfp/golang-commons/logger"
	"github.com/spf13/cobra"
	controllerruntime "sigs.k8s.io/controller-runtime"
)

var nativeCmd = &cobra.Command{
	Use: "native",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()

		log, err := setupLogger("INFO")
		if err != nil {
			return err
		}

		log.Info().Str("LogLevel", log.GetLevel().String()).Msg("Starting server...")

		cfg := controllerruntime.GetConfigOrDie()

		if len(os.Args) < 3 {
			fmt.Println("Usage: go run main.go native <watchedDirectory>")
			os.Exit(1) // Exit the program with a non-zero status code to indicate an error
		}
		dir := os.Args[2]

		managerInstance, err := manager.NewManager(log, cfg, dir)
		if err != nil {
			log.Error().Err(err).Msg("Error creating manager")
			return err
		}

		managerInstance.Start()

		http.Handle("/", managerInstance)

		log.Info().Float64("elapsed", time.Since(start).Seconds()).Msg("Setup took seconds")
		log.Info().Msg("Server is running on http://localhost:3000/{workspace}/graphql")

		return http.ListenAndServe(":3000", nil)
	},
}

func setupLogger(logLevel string) (*logger.Logger, error) {
	loggerCfg := logger.DefaultConfig()
	loggerCfg.Name = "crdGateway"
	loggerCfg.Level = logLevel
	return logger.New(loggerCfg)
}
