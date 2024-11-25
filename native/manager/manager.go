package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/handler"
	"github.com/openmfp/crd-gql-gateway/native/gateway"
	"github.com/openmfp/crd-gql-gateway/native/resolver"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Provider interface {
	Start()
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type Service struct {
	log           *logger.Logger
	runtimeClient client.WithWatch
	restMapper    meta.RESTMapper
	resolver      resolver.Provider
	handlers      map[string]*graphqlHandler
	mu            sync.RWMutex
	watcher       *fsnotify.Watcher
	dir           string
}

type graphqlHandler struct {
	schema  *graphql.Schema
	handler http.Handler
}

func NewManager(log *logger.Logger, cfg *rest.Config, dir string) (*Service, error) {
	runtimeClient, err := setupK8sClients(cfg)
	if err != nil {
		return nil, err
	}

	restMapper, err := getRestMapper(cfg)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	m := &Service{
		log:           log,
		runtimeClient: runtimeClient,
		restMapper:    restMapper,
		resolver:      resolver.New(log, runtimeClient),
		handlers:      make(map[string]*graphqlHandler),
		watcher:       watcher,
		dir:           dir,
	}

	// Start watching the directory
	err = m.watcher.Add(dir)
	if err != nil {
		return nil, err
	}

	// Load existing files
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		filename := filepath.Base(file)
		m.OnFileChanged(filename)
	}

	m.Start()

	return m, nil
}

func (s *Service) Start() {
	go func() {
		for {
			select {
			case event, ok := <-s.watcher.Events:
				if !ok {
					return
				}
				s.handleEvent(event)
			case err, ok := <-s.watcher.Errors:
				if !ok {
					return
				}
				s.log.Error().Err(err).Msg("Error watching files")
			}
		}
	}()
}

func (s *Service) handleEvent(event fsnotify.Event) {
	s.log.Info().Str("event", event.String()).Msg("File event")

	filename := filepath.Base(event.Name)
	switch event.Op {
	case fsnotify.Create:
		s.OnFileChanged(filename)
	case fsnotify.Write:
		s.OnFileChanged(filename)
	case fsnotify.Rename:
		s.OnFileDeleted(filename)
	case fsnotify.Remove:
		s.OnFileDeleted(filename)
	default:
		s.log.Info().Str("file", filename).Msg("Unknown file event")
	}
}

func (s *Service) OnFileChanged(filename string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read the file and generate fullSchema
	schema, err := s.loadSchemaFromFile(filename)
	if err != nil {
		s.log.Error().Err(err).Str("file", filename).Msg("Error loading fullSchema from file")
		return
	}

	s.handlers[filename] = s.createHandler(schema)

	s.log.Info().Str("endpoint", fmt.Sprintf("http://localhost:3000/%s/graphql", filename)).Msg("Registered endpoint")
}

func (s *Service) OnFileDeleted(filename string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.handlers, filename)

}

func (s *Service) loadSchemaFromFile(filename string) (*graphql.Schema, error) {
	definitions, err := readDefinitionFromFile(filepath.Join(s.dir, filename))
	if err != nil {
		return nil, err
	}

	g, err := gateway.New(s.log, s.restMapper, definitions, s.resolver)
	if err != nil {
		return nil, err
	}

	return g.GetSchema(), nil
}

func (s *Service) createHandler(schema *graphql.Schema) *graphqlHandler {
	h := handler.New(&handler.Config{
		Schema:     schema,
		Pretty:     true,
		Playground: true,
	})
	return &graphqlHandler{
		schema:  schema,
		handler: h,
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Expected paths: /{filename}/graphql or /{filename}/subscriptions
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	filename := parts[0]
	endpoint := parts[1]

	s.mu.RLock()
	h, ok := s.handlers[filename]
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch endpoint {
	case "graphql": // Serve queries and mutations
		r.URL.Path = "/" + strings.Join(parts[1:], "/")
		h.handler.ServeHTTP(w, r)
	case "subscriptions": // Handle subscriptions over SSE
		s.handleSubscription(w, r, h.schema)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) handleSubscription(w http.ResponseWriter, r *http.Request, schema *graphql.Schema) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Parse the GraphQL query
	var params struct {
		Query         string                 `json:"query"`
		OperationName string                 `json:"operationName"`
		Variables     map[string]interface{} `json:"variables"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &params); err != nil {
		http.Error(w, "Error parsing JSON request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Execute the subscription
	subscriptionParams := graphql.Params{
		Schema:         *schema,
		RequestString:  params.Query,
		VariableValues: params.Variables,
		OperationName:  params.OperationName,
		Context:        ctx,
	}

	sub := graphql.Subscribe(subscriptionParams)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error executing subscription: %v", err), http.StatusInternalServerError)
		return
	}

	if sub == nil {
		http.Error(w, "No subscription found", http.StatusBadRequest)
		return
	}

	// Stream the results
	for res := range sub {
		if res == nil {
			continue
		}
		data, err := json.Marshal(res)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func readDefinitionFromFile(filePath string) (spec.Definitions, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var swagger spec.Swagger
	err = json.Unmarshal(data, &swagger)
	if err != nil {
		return nil, err
	}

	return swagger.Definitions, nil
}

// setupK8sClients initializes and returns the runtime client for Kubernetes.
func setupK8sClients(cfg *rest.Config) (client.WithWatch, error) {
	k8sCache, err := cache.New(cfg, cache.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, err
	}

	go func() {
		err = k8sCache.Start(context.Background())
		if err != nil {
			panic(err)
		}
	}()
	if !k8sCache.WaitForCacheSync(context.Background()) {
		return nil, fmt.Errorf("failed to sync cache")
	}

	runtimeClient, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme.Scheme,
		Cache: &client.CacheOptions{
			Reader: k8sCache,
		},
	})

	return runtimeClient, err
}

// restMapper is needed to derive plural names for resources.
func getRestMapper(cfg *rest.Config) (meta.RESTMapper, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error starting discovery client: %w", err)
	}

	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, err
	}

	return restmapper.NewDiscoveryRESTMapper(groupResources), nil
}
