package cmd

import (
	openmfpconfig "github.com/openmfp/golang-commons/config"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"time"
)

var (
	rootCmd = &cobra.Command{
		Use: "listener or gateway",
	}

	appCfg     config.Config
	defaultCfg *openmfpconfig.CommonServiceConfig
	v          *viper.Viper
)

func initConfig() {
	v.SetDefault("shutdown-timeout", 5*time.Second)

	// Top-level defaults
	v.SetDefault("openapi-definitions-path", "./bin/definitions")
	v.SetDefault("enable-kcp", true)
	v.SetDefault("local-development", false)

	// Listener
	v.SetDefault("listener-apiexport-workspace", ":root")
	v.SetDefault("listener-apiexport-name", "kcp.io")

	// Gateway
	v.SetDefault("gateway-port", "8080")
	v.SetDefault("gateway-username-claim", "email")
	v.SetDefault("gateway-should-impersonate", true)
	// Gateway Handler config
	v.SetDefault("gateway-handler-pretty", true)
	v.SetDefault("gateway-handler-playground", true)
	v.SetDefault("gateway-handler-graphiql", true)
	// Gateway CORS
	v.SetDefault("gateway-cors-enabled", false)
	v.SetDefault("gateway-cors-allowed-origins", []string{"*"})
	v.SetDefault("gateway-cors-allowed-headers", []string{"*"})

	var cfg config.Config
	if err := v.Unmarshal(&cfg); err != nil {
		panic("Unable to unmarshal config: " + err.Error())
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
