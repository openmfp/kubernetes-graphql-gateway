package main

import (
	"net/http"
	_ "net/http/pprof"

	"github.com/openmfp/kubernetes-graphql-gateway/cmd"
)

func main() {
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	cmd.Execute()
}
