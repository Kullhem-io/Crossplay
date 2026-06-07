package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OpenAIBrain talks to any OpenAI-compatible chat endpoint (llama.cpp server,
// vLLM, etc.). Both the local Qwen and Gemma servers use it; they differ only
// in base URL, model id, and concurrency budget.
type OpenAIBrain struct {
	id      string
	baseURL string // e.g. http://127.0.0.1:8001/v1
	model   string
	max     int
	client  *http.Client
}

func NewOpenAIBrain(id, baseURL, model string, maxConcurrent int) *OpenAIBrain {
	return &OpenAIBrain{
		id:      id,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		max:     maxConcurrent,
		// No client-level timeout: streamed generations are long-lived and
		// bounded by the caller's context instead.
		client: &http.Client{},
	}
}

func (b *OpenAIBrain) ID() string         { return b.id }
func (b *OpenAIBrain) MaxConcurrent() int { return b.max }

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Stream         bool            `json:"stream"`
	Stop           []string        `json:"stop,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *schemaWrapper  `json:"json_schema,omitempty"`
}

type schemaWrapper struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func (b *OpenAIBrain) Stream(ctx context.Context, msgs []Message, opts CallOpts) (<-chan Token, error) {
	reqBody := chatRequest{
		Model:       b.model,
		Messages:    msgs,
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
		Stream:      true,
		Stop:        opts.Stop,
	}
	if len(opts.JSONSchema) > 0 {
		reqBody.ResponseFormat = &responseFormat{
			Type: "json_schema",
			JSONSchema: &schemaWrapper{
				Name:   "state",
				Schema: json.RawMessage(opts.JSONSchema),
				Strict: true,
			},
		}
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.id, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var snippet [512]byte
		n, _ := resp.Body.Read(snippet[:])
		return nil, fmt.Errorf("%s: status %d: %s", b.id, resp.StatusCode, strings.TrimSpace(string(snippet[:n])))
	}

	out := make(chan Token)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		emit := func(t Token) bool {
			select {
			case out <- t:
				return true
			case <-ctx.Done():
				return false
			}
		}

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // skip keep-alives / partial frames
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content != "" {
					if !emit(Token{Text: c.Delta.Content}) {
						return
					}
				}
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			emit(Token{Err: err})
		}
	}()
	return out, nil
}

// compile-time check
var _ Brain = (*OpenAIBrain)(nil)
