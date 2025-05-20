package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/openmfp/golang-commons/sentry"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/handler"
	"github.com/kcp-dev/logicalcluster/v3"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	"github.com/openmfp/golang-commons/logger"

	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/schema"
)

var (
	ErrUnknownFileEvent = errors.New("unknown file event")
	ErrNoHandlerFound   = errors.New("no handler found for workspace")
)

type Provider interface {
	Start()
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type FileWatcher interface {
	OnFileChanged(filename string)
	OnFileDeleted(filename string)
}

type Service struct {
	appCfg   appConfig.Config
	handlers map[string]*graphqlHandler
	log      *logger.Logger
	mu       sync.RWMutex
	resolver resolver.Provider
	restCfg  *rest.Config
	watcher  *fsnotify.Watcher
}

type graphqlHandler struct {
	schema  *graphql.Schema
	handler http.Handler
}

func NewManager(log *logger.Logger, cfg *rest.Config, appCfg appConfig.Config) (*Service, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// lets ensure that kcp url points directly to kcp domain
	u, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, err
	}
	cfg.Host = fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return NewRoundTripper(log, rt, appCfg.Gateway.UsernameClaim, appCfg.Gateway.ShouldImpersonate)
	})

	runtimeClient, err := kcp.NewClusterAwareClientWithWatch(cfg, client.Options{})
	if err != nil {
		return nil, err
	}

	m := &Service{
		appCfg:   appCfg,
		handlers: make(map[string]*graphqlHandler),
		log:      log,
		resolver: resolver.New(log, runtimeClient),
		restCfg:  cfg,
		watcher:  watcher,
	}

	err = m.watcher.Add(appCfg.OpenApiDefinitionsPath)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(appCfg.OpenApiDefinitionsPath, "*"))
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
				sentry.CaptureError(err, nil)
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
		err := ErrUnknownFileEvent
		s.log.Error().Err(err).Str("filename", filename).Msg("Unknown file event")
		sentry.CaptureError(sentry.SentryError(err), nil, sentry.Extras{"filename": filename, "event": event.String()})
	}
}

func (s *Service) OnFileChanged(filename string) {
	schema, err := s.loadSchemaFromFile(filename)
	if err != nil {
		s.log.Error().Err(err).Str("filename", filename).Msg("failed to process the file's change")
		sentry.CaptureError(err, sentry.Tags{"filename": filename})

		return
	}

	s.mu.Lock()
	s.handlers[filename] = s.createHandler(schema)
	s.mu.Unlock()

	s.log.Info().Str("endpoint", fmt.Sprintf("http://localhost:%s/%s/graphql", s.appCfg.Gateway.Port, filename)).Msg("Registered endpoint")
}

func (s *Service) OnFileDeleted(filename string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.handlers, filename)
}

func (s *Service) loadSchemaFromFile(filename string) (*graphql.Schema, error) {
	definitions, err := ReadDefinitionFromFile(filepath.Join(s.appCfg.OpenApiDefinitionsPath, filename))
	if err != nil {
		return nil, err
	}

	g, err := schema.New(s.log, definitions, s.resolver)
	if err != nil {
		return nil, err
	}

	return g.GetSchema(), nil
}

func (s *Service) createHandler(schema *graphql.Schema) *graphqlHandler {
	h := handler.New(&handler.Config{
		Schema:     schema,
		Pretty:     s.appCfg.Gateway.HandlerCfg.Pretty,
		Playground: s.appCfg.Gateway.HandlerCfg.Playground,
		GraphiQL:   s.appCfg.Gateway.HandlerCfg.GraphiQL,
	})
	return &graphqlHandler{
		schema:  schema,
		handler: h,
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if s.appCfg.Gateway.Cors.Enabled {
		allowedOrigins := strings.Join(s.appCfg.Gateway.Cors.AllowedOrigins, ",")
		allowedHeaders := strings.Join(s.appCfg.Gateway.Cors.AllowedHeaders, ",")
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
		w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
		// setting cors allowed methods is not needed for this service,
		// as all graphql methods are part of the cors safelisted methods
		// https://fetch.spec.whatwg.org/#cors-safelisted-method

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	workspace, err := s.parsePath(r.URL.Path)
	if err != nil {
		s.log.Error().Err(err).Str("path", r.URL.Path).Msg("Error parsing path")
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	h, ok := s.handlers[workspace]
	s.mu.RUnlock()

	if !ok {
		s.log.Error().Err(ErrNoHandlerFound).Str("workspace", workspace)
		sentry.CaptureError(ErrNoHandlerFound, sentry.Tags{"workspace": workspace})
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodGet {
		h.handler.ServeHTTP(w, r)
		return
	}

	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")

	if !s.appCfg.LocalDevelopment {
		if token == "" {
			http.Error(w, "Authorization header is required", http.StatusUnauthorized)
			return
		}

		ok, err = s.validateToken(r.Context(), token)
		if err != nil {
			s.log.Error().Err(err).Msg("error validating token with k8s")
			http.Error(w, "error validating token", http.StatusInternalServerError)
			return
		}

		if !ok {
			http.Error(w, "Provided token is not authorized to access the cluster", http.StatusUnauthorized)
			return
		}
	}

	if s.appCfg.EnableKcp {
		r = r.WithContext(kontext.WithCluster(r.Context(), logicalcluster.Name(workspace)))
	}

	r = r.WithContext(context.WithValue(r.Context(), TokenKey{}, token))

	if r.Header.Get("Accept") == "text/event-stream" {
		s.handleSubscription(w, r, h.schema)
	} else {
		h.handler.ServeHTTP(w, r)
	}
}

// parsePath extracts filename and endpoint from the requested URL path.
func (s *Service) parsePath(path string) (workspace string, err error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid path")
	}

	return parts[0], nil
}

// validateToken uses the /version endpoint for a general authentication check.
func (s *Service) validateToken(ctx context.Context, token string) (bool, error) {
	cfg := &rest.Config{
		Host: s.restCfg.Host,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile: s.restCfg.TLSClientConfig.CAFile,
			CAData: s.restCfg.TLSClientConfig.CAData,
		},
		BearerToken: token,
	}

	transport, err := rest.TransportFor(cfg)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/version", cfg.Host), nil)
	if err != nil {
		return false, err
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return false, err
	}

	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	switch {
	case err != nil:
		return false, err
	case resp.StatusCode == http.StatusUnauthorized:
		return false, nil
	case resp.StatusCode == http.StatusOK:
		return true, nil
	default:
		return false, fmt.Errorf("unexpected status code from /version: %d", resp.StatusCode)
	}
}

func (s *Service) handleSubscription(w http.ResponseWriter, r *http.Request, schema *graphql.Schema) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var params struct {
		Query         string                 `json:"query"`
		OperationName string                 `json:"operationName"`
		Variables     map[string]interface{} `json:"variables"`
	}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Error parsing JSON request body", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	r.Body.Close()

	subscriptionParams := graphql.Params{
		Schema:         *schema,
		RequestString:  params.Query,
		VariableValues: params.Variables,
		OperationName:  params.OperationName,
		Context:        r.Context(),
	}

	subscriptionChannel := graphql.Subscribe(subscriptionParams)
	for res := range subscriptionChannel {
		if res == nil {
			continue
		}

		data, err := json.Marshal(res)
		if err != nil {
			s.log.Error().Err(err).Msg("Error marshalling subscription response")
			continue
		}

		fmt.Fprintf(w, "event: next\ndata: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprint(w, "event: complete\n\n")
}

func ReadDefinitionFromFile(filePath string) (spec.Definitions, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var swagger spec.Swagger
	err = json.NewDecoder(f).Decode(&swagger)
	if err != nil {
		return nil, err
	}

	return swagger.Definitions, nil
}
