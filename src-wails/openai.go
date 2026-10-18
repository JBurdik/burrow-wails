package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// OpenAIConfig is persisted per provider instance. The API key never enters
// the transcript, tool environment, or an error's request URL.
type OpenAIConfig struct {
	BaseURL         string `json:"baseUrl"`
	APIKey          string `json:"apiKey"`
	Model           string `json:"model"`
	ContextWindow   int    `json:"contextWindow"`
	MaxOutputTokens int    `json:"maxOutputTokens"`
}

func normalizeOpenAIConfig(c OpenAIConfig, requireModel bool) (OpenAIConfig, error) {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("base URL must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if u.Path == "" || u.Path == "/" {
		c.BaseURL += "/v1"
	}
	c.Model = strings.TrimSpace(c.Model)
	if requireModel && c.Model == "" {
		return c, fmt.Errorf("choose a model in Settings > Providers")
	}
	if c.ContextWindow == 0 {
		c.ContextWindow = 32768
	}
	if c.MaxOutputTokens == 0 {
		c.MaxOutputTokens = 4096
	}
	if c.ContextWindow < 2048 || c.MaxOutputTokens < 128 || c.MaxOutputTokens > c.ContextWindow/2 {
		return c, fmt.Errorf("context must be at least 2048 tokens; output must be 128 tokens to half the context window")
	}
	return c, nil
}

// No credential-bearing redirects to a different origin, including discovery.
func openAIHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("model server redirect to another origin was refused")
		}
		return nil
	}}
}

func openAIRequest(ctx context.Context, c OpenAIConfig, method, path string, body any) (*http.Response, error) {
	var input io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := openAIHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("model server request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := string(b)
		if c.APIKey != "" {
			message = strings.ReplaceAll(message, c.APIKey, "[redacted]")
		}
		return nil, fmt.Errorf("model server HTTP %d: %s", resp.StatusCode, message)
	}
	return resp, nil
}

type OpenAIModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// OpenAIListModels doubles as the explicit connection test. No requests are
// sent when editing credentials or merely opening settings.
func (a *App) OpenAIListModels(config OpenAIConfig) ([]OpenAIModel, error) {
	c, err := normalizeOpenAIConfig(config, false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := openAIRequest(ctx, c, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid model catalog: %w", err)
	}
	out := []OpenAIModel{}
	seen := map[string]bool{}
	for _, m := range result.Data {
		if m.ID != "" && !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, OpenAIModel{ID: m.ID, Label: m.ID})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the server returned no models; enter a model ID manually")
	}
	return out, nil
}

type openAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type openAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}
type openAIMessage struct {
	InputTokens  int              `json:"-"`
	OutputTokens int              `json:"-"`
	Role         string           `json:"role"`
	Content      any              `json:"content"`
	ToolCalls    []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID   string           `json:"tool_call_id,omitempty"`
}

type openAICompletion struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
			ToolCalls []struct {
				Index    int            `json:"index"`
				ID       string         `json:"id"`
				Function openAIFunction `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// completion reassembles fragmented tool arguments before any execution. A
// truncated/failed stream cannot execute a partial command or partial edit.
func openAIComplete(ctx context.Context, c OpenAIConfig, messages []openAIMessage, tools bool, delta func(string, bool)) (openAIMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	body := map[string]any{"model": c.Model, "messages": messages, "stream": true, "stream_options": map[string]any{"include_usage": true}, "max_tokens": c.MaxOutputTokens}
	if tools {
		body["tools"] = openAITools()
		body["tool_choice"] = "auto"
	}
	resp, err := openAIRequest(ctx, c, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return openAIMessage{}, err
	}
	defer resp.Body.Close()
	result := openAIMessage{Role: "assistant", Content: ""}
	calls := map[int]*openAIToolCall{}
	finished := false
	consume := func(b []byte) error {
		var chunk openAICompletion
		if err := json.Unmarshal(b, &chunk); err != nil {
			return fmt.Errorf("invalid completion stream: %w", err)
		}
		if chunk.Error != nil {
			return errors.New(chunk.Error.Message)
		}
		if chunk.Usage.PromptTokens > 0 {
			result.InputTokens = chunk.Usage.PromptTokens
			result.OutputTokens = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) == 0 {
			return nil
		}
		choice := chunk.Choices[0]
		text := choice.Delta.Content
		if choice.Message.Role != "" {
			text, _ = choice.Message.Content.(string)
			result.ToolCalls = choice.Message.ToolCalls
		}
		if text != "" {
			result.Content = result.Content.(string) + text
			if delta != nil {
				delta(text, false)
			}
		}
		if choice.Delta.Reasoning != "" && delta != nil {
			delta(choice.Delta.Reasoning, true)
		}
		for _, part := range choice.Delta.ToolCalls {
			if part.Index < 0 || part.Index > 127 {
				return fmt.Errorf("invalid tool call index")
			}
			call := calls[part.Index]
			if call == nil {
				call = &openAIToolCall{Type: "function"}
				calls[part.Index] = call
			}
			if part.ID != "" {
				call.ID = part.ID
			}
			call.Function.Name += part.Function.Name
			call.Function.Arguments += part.Function.Arguments
		}
		if choice.FinishReason != "" {
			if choice.FinishReason == "length" {
				return fmt.Errorf("model reached the output limit; increase Maximum output tokens or retry")
			}
			if choice.FinishReason != "stop" && choice.FinishReason != "tool_calls" {
				return fmt.Errorf("model stopped with reason %q", choice.FinishReason)
			}
			finished = true
		}
		return nil
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			return result, err
		}
		if err = consume(b); err != nil {
			return result, err
		}
	} else {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 65536), 4<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				break
			}
			if err := consume([]byte(data)); err != nil {
				return result, err
			}
		}
		if err := sc.Err(); err != nil {
			return result, err
		}
	}
	if !finished {
		return result, fmt.Errorf("model server closed the stream before completing the response")
	}
	indexes := []int{}
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		result.ToolCalls = append(result.ToolCalls, *calls[index])
	}
	ids := map[string]bool{}
	for _, call := range result.ToolCalls {
		if call.ID == "" || ids[call.ID] || call.Type != "function" || !json.Valid([]byte(call.Function.Arguments)) {
			return result, fmt.Errorf("model returned an invalid tool call; use a model and chat template with tool calling support")
		}
		ids[call.ID] = true
	}
	if result.Content == "" && len(result.ToolCalls) == 0 {
		return result, fmt.Errorf("model returned an empty response")
	}
	return result, nil
}
