package graphql

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func SendGraphQLRequest(url, query string) (map[string]interface{}, error) {
	reqBody := map[string]string{
		"query": query,
	}
	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBodyBytes))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, err
	}

	return result, nil
}
