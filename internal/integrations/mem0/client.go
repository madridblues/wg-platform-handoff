package mem0

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultMem0Endpoint = "https://api.mem0.ai/v1/memories"

type Client struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	endpoint := strings.TrimSpace(os.Getenv("MEM0_API_URL"))
	if endpoint == "" {
		endpoint = defaultMem0Endpoint
	}

	return &Client{
		apiKey:   strings.TrimSpace(apiKey),
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
}

func (c *Client) WriteEvent(ctx context.Context, event map[string]any) error {
	if event == nil {
		return errors.New("nil event")
	}

	if c.apiKey == "" {
		// Mem0 is optional and non-canonical. If no key is configured, keep this best-effort no-op.
		return nil
	}

	eventBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal mem0 event: %w", err)
	}

	payload := map[string]any{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": string(eventBytes),
			},
		},
		"metadata": map[string]any{
			"source": "wg-platform-handoff",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal mem0 payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create mem0 request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post mem0 event: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return fmt.Errorf("mem0 write failed: status=%d body=%s", res.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return nil
}
