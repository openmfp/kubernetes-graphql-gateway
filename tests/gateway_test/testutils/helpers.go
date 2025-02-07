package testutils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/openmfp/golang-commons/logger"
	"io"
	"net/http"
	"strings"
)

type CoreData struct {
	CreatePod *Pod   `json:"createPod,omitempty"`
	Pod       *Pod   `json:"Pod,omitempty"`
	Pods      []*Pod `json:"Pods,omitempty"`
	UpdatePod *Pod   `json:"updatePod,omitempty"`

	Service *ServiceData `json:"Service,omitempty"`

	CreateAccount *Account `json:"createAccount,omitempty"`
	Account       *Account `json:"Account,omitempty"`
	UpdateAccount *Account `json:"updateAccount,omitempty"`
	DeleteAccount *bool    `json:"deleteAccount,omitempty"`
}

type SubscriptionResponse struct {
	Data struct {
		Accounts []Account `json:"core_openmfp_io_accounts"`
		Account  Account   `json:"core_openmfp_io_account"`
	} `json:"data"`
}

type Metadata struct {
	Name      string
	Namespace string
	Labels    map[string]string
}

type GraphQLResponse struct {
	Data   *GraphQLData   `json:"data,omitempty"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

type GraphQLData struct {
	Core          *CoreData `json:"core,omitempty"`
	CoreOpenmfpIO *CoreData `json:"core_openmfp_io,omitempty"`
}

type GraphQLError struct {
	Message   string                 `json:"message"`
	Locations []GraphQLErrorLocation `json:"locations,omitempty"`
	Path      []interface{}          `json:"path,omitempty"`
}

type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func SendRequest(url, query string) (*GraphQLResponse, int, error) {
	reqBody := map[string]string{
		"query": query,
	}
	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBodyBytes))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	v := resp.Body

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	var bodyResp GraphQLResponse
	err = json.Unmarshal(respBytes, &bodyResp)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("response body is not json, but %s", respBytes)
	}

	return &bodyResp, resp.StatusCode, err
}

func Subscribe(url, query string, log *logger.Logger) (<-chan SubscriptionResponse, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan SubscriptionResponse)

	go func() {
		defer close(events)
		defer cancel()

		payload, err := json.Marshal(map[string]string{"query": query})
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal payload")
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			log.Error().Err(err).Msg("Failed to create request")
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Error().Err(err).Msg("Failed to send request")
			return
		}
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		var eventData strings.Builder

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Error().Err(err).Msg("Failed to read response")
				}
				break
			}
			line = strings.TrimSpace(line)

			// If empty line, we've reached event's end.
			if line == "" && eventData.Len() > 0 {
				rawMsg := strings.TrimPrefix(eventData.String(), "event: next\ndata: ")
				var msg SubscriptionResponse
				if err := json.Unmarshal([]byte(rawMsg), &msg); err != nil {
					log.Error().Err(err).Msg("Failed to unmarshal response")
					return
				}
				events <- msg
				eventData.Reset()
			} else {
				eventData.WriteString(line)
				eventData.WriteString("\n")
			}
		}
	}()

	return events, cancel, nil
}
