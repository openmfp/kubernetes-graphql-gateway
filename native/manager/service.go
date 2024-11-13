package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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

type Manager struct {
	log           *logger.Logger
	runtimeClient client.WithWatch
	restMapper    meta.RESTMapper
	resolver      resolver.Provider
	handlers      map[string]*handler.Handler
	mu            sync.RWMutex
	watcher       *fsnotify.Watcher
	dir           string
}

func NewManager(log *logger.Logger, cfg *rest.Config, dir string) (*Manager, error) {
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

	m := &Manager{
		log:           log,
		runtimeClient: runtimeClient,
		restMapper:    restMapper,
		resolver:      resolver.New(log, runtimeClient),
		handlers:      make(map[string]*handler.Handler),
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
		m.OnFileEvent(filename, fsnotify.Create)
	}

	return m, nil
}

func (m *Manager) Start() {
	go func() {
		for {
			select {
			case event, ok := <-m.watcher.Events:
				if !ok {
					return
				}
				m.handleEvent(event)
			case err, ok := <-m.watcher.Errors:
				if !ok {
					return
				}
				m.log.Error().Err(err).Msg("Error watching files")
			}
		}
	}()
}

func (m *Manager) handleEvent(event fsnotify.Event) {
	m.log.Info().Str("event", event.String()).Msg("File event")

	filename := filepath.Base(event.Name)
	switch event.Op {
	case fsnotify.Create:
		m.log.Info().Str("file", filename).Msg("File added")
		m.OnFileEvent(filename, event.Op)
	case fsnotify.Write:
		m.log.Info().Str("file", filename).Msg("File modified")
		m.OnFileEvent(filename, event.Op)
	case fsnotify.Rename:
		m.OnFileDeleted(filename)
	case fsnotify.Remove:
		m.OnFileDeleted(filename)
	default:
		m.log.Info().Str("file", filename).Msg("Unknown file event")
	}
}

func (m *Manager) OnFileEvent(filename string, eventType fsnotify.Op) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Read the file and generate schema
	schema, err := m.loadSchemaFromFile(filename)
	if err != nil {
		m.log.Error().Err(err).Str("file", filename).Msg("Error loading schema from file")
		return
	}

	m.handlers[filename] = m.createHandler(schema)
}

func (m *Manager) OnFileDeleted(filename string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.Info().Str("file", filename).Msg("File deleted")

	delete(m.handlers, filename)
}

func (m *Manager) loadSchemaFromFile(filename string) (*graphql.Schema, error) {
	// Read the file
	filePath := filepath.Join(m.dir, filename)
	// Read and parse the OpenAPI spec
	definitions, err := readDefinitionFromFile(filePath)
	if err != nil {
		return nil, err
	}

	g, err := gateway.New(m.log, m.restMapper, definitions, m.resolver)
	if err != nil {
		return nil, err
	}

	schema := g.GetSchema()

	return schema, nil
}

func (m *Manager) createHandler(schema *graphql.Schema) *handler.Handler {
	h := handler.New(&handler.Config{
		Schema:     schema,
		Pretty:     true,
		Playground: true,
	})

	return h
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Expected path is /{filename}/graphql
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	filename := parts[0]
	m.mu.RLock()
	h, ok := m.handlers[filename]
	m.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Adjust the URL path to pass to the handler
	r.URL.Path = "/" + strings.Join(parts[1:], "/")

	// Serve the request using the handler
	h.ServeHTTP(w, r)
}

func readDefinitionFromFile(filePath string) (spec.Definitions, error) {
	data, err := ioutil.ReadFile(filePath)
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

	go k8sCache.Start(context.Background())
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
