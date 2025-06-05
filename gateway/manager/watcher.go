package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"

	"github.com/openmfp/golang-commons/sentry"

	"github.com/openmfp/kubernetes-graphql-gateway/gateway/schema"
)

var (
	ErrUnknownFileEvent = errors.New("unknown file event")
)

type FileWatcher interface {
	OnFileChanged(filename string)
	OnFileDeleted(filename string)
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
	// Re-initialize target clusters when files change to pick up any new ClusterAccess resources
	err := s.initializeTargetClusters()
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to re-initialize target clusters")
	}

	schema, err := s.loadSchemaFromFile(filename)
	if err != nil {
		s.log.Error().Err(err).Str("filename", filename).Msg("failed to process the file's change")
		sentry.CaptureError(err, sentry.Tags{"filename": filename})

		return
	}

	s.handlers.mu.Lock()
	s.handlers.registry[filename] = s.createHandler(schema)
	s.handlers.mu.Unlock()

	s.log.Info().Str("endpoint", fmt.Sprintf("http://localhost:%s/%s/graphql", s.AppCfg.Gateway.Port, filename)).Msg("Registered endpoint")
}

func (s *Service) OnFileDeleted(filename string) {
	s.handlers.mu.Lock()
	defer s.handlers.mu.Unlock()

	delete(s.handlers.registry, filename)
}

func (s *Service) loadSchemaFromFile(filename string) (*graphql.Schema, error) {
	definitions, err := ReadDefinitionFromFile(filepath.Join(s.AppCfg.OpenApiDefinitionsPath, filename))
	if err != nil {
		return nil, err
	}

	// Get the resolver for this specific cluster
	resolver, exists := s.getResolverForCluster(filename)
	if !exists {
		return nil, fmt.Errorf("no resolver found for cluster '%s'. Available clusters: %v", filename, s.getAvailableClusters())
	}

	g, err := schema.New(s.log, definitions, resolver)
	if err != nil {
		return nil, err
	}

	return g.GetSchema(), nil
}

// getAvailableClusters returns a list of available cluster names for debugging
func (s *Service) getAvailableClusters() []string {
	clusters := make([]string, 0, len(s.resolvers))
	for clusterName := range s.resolvers {
		clusters = append(clusters, clusterName)
	}
	return clusters
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
