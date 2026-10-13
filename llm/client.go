package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the OpenRouter Chat Completions endpoint.
	DefaultBaseURL = "https://openrouter.ai/api/v1"

	// DefaultModel is used when no model is configured.
	DefaultModel = "openai/gpt-4o-mini"

	// DefaultHTTPTimeout bounds a single completion request.
	DefaultHTTPTimeout = 3 * time.Minute
)

// Config configures a Client.
type Config struct {
	// APIKey is the OpenRouter API key. Required.
	APIKey string

	// BaseURL is the API root, e.g. https://openrouter.ai/api/v1.
	// Empty falls back to DefaultBaseURL.
	BaseURL string

	// Model is the OpenRouter model slug, e.g. "anthropic/claude-sonnet-4".
	// Empty falls back to DefaultModel.
	Model string

	// HTTPClient is optional; a sensible default is used when nil.
	HTTPClient *http.Client

	// Referer and Title set the optional HTTP-Referer and X-Title headers
	// OpenRouter uses for app attribution/rankings.
	Referer string
	Title   string

	// MaxRetries is the number of retries for transient (429/5xx/network)
	// failures. Zero falls back to 3.
	MaxRetries int
}

// Client is a minimal OpenRouter Chat Completions client.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	http       *http.Client
	referer    string
	title      string
	maxRetries int
}

// NewClient builds a Client from cfg.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("llm: API key is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &Client{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		http:       httpClient,
		referer:    cfg.Referer,
		title:      cfg.Title,
		maxRetries: maxRetries,
	}, nil
}

// Model returns the model slug the client was configured with.
func (c *Client) Model() string { return c.model }

// FromEnv builds a Client from the SOLAI_LLM_MODEL, SOLAI_LLM_API_KEY, and
// SOLAI_LLM_BASE_URL environment variables injected into tool subprocesses.
func FromEnv() (*Client, error) {
	return NewClient(Config{
		APIKey:  os.Getenv("SOLAI_LLM_API_KEY"),
		BaseURL: os.Getenv("SOLAI_LLM_BASE_URL"),
		Model:   os.Getenv("SOLAI_LLM_MODEL"),
	})
}

// ---- wire types -------------------------------------------------------------

type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []toolDef `json:"tools,omitempty"`
	ToolChoice any       `json:"tool_choice,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

func (e *apiError) Error() string {
	if e == nil {
		return ""
	}
	if e.Type != "" {
		return fmt.Sprintf("llm: %s (%s)", e.Message, e.Type)
	}
	return "llm: " + e.Message
}

// ---- Chat -------------------------------------------------------------------

// Chat sends messages to the model with the given tools available and returns
// the assistant's reply. Transient failures (429, 5xx, network errors) are
// retried with exponential backoff.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    buildToolDefs(tools),
	}
	if len(reqBody.Tools) > 0 {
		reqBody.ToolChoice = "auto"
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("llm: marshalling request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		msg, err := c.doOnce(ctx, payload)
		if err == nil {
			return msg, nil
		}
		lastErr = err

		var apiErr *retryableError
		if !errors.As(err, &apiErr) {
			return nil, err
		}

		delay := time.Duration(1<<attempt) * time.Second
		if apiErr.retryAfter > 0 {
			delay = apiErr.retryAfter
		}
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

// retryableError marks an error that warrants a retry.
type retryableError struct {
	err        error
	retryAfter time.Duration
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func (c *Client) doOnce(ctx context.Context, payload []byte) (*Message, error) {
	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("llm: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.referer != "" {
		req.Header.Set("HTTP-Referer", c.referer)
	}
	if c.title != "" {
		req.Header.Set("X-Title", c.title)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &retryableError{err: fmt.Errorf("llm: request failed: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &retryableError{err: fmt.Errorf("llm: reading response: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		apiErr := parseAPIError(body)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, &retryableError{
				err:        fmt.Errorf("llm: HTTP %d: %s", resp.StatusCode, apiErr),
				retryAfter: retryAfterFrom(resp.Header),
			}
		}
		return nil, fmt.Errorf("llm: HTTP %d: %s", resp.StatusCode, apiErr)
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("llm: parsing response: %w (body: %s)", err, truncate(body, 500))
	}
	if parsed.Error != nil {
		return nil, parsed.Error
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("llm: response contained no choices (body: %s)", truncate(body, 500))
	}

	msg := parsed.Choices[0].Message
	if msg.Role == "" {
		msg.Role = RoleAssistant
	}
	return &msg, nil
}

func buildToolDefs(tools []Tool) []toolDef {
	if len(tools) == 0 {
		return nil
	}
	defs := make([]toolDef, 0, len(tools))
	for _, t := range tools {
		params := StringSchema("Free-form input for this tool. Provide plain text or a JSON object.")
		if pt, ok := t.(ParameterizedTool); ok {
			if p := pt.Parameters(); p != nil {
				params = p
			}
		}
		defs = append(defs, toolDef{
			Type: "function",
			Function: functionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  params,
			},
		})
	}
	return defs
}

func parseAPIError(body []byte) string {
	var resp chatResponse
	if err := json.Unmarshal(body, &resp); err == nil && resp.Error != nil {
		return resp.Error.Error()
	}
	return truncate(body, 500)
}

func retryAfterFrom(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
