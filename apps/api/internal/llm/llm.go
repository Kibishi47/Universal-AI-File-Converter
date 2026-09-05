package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type FeasibilityResult struct {
	Convertible bool   `json:"convertible"`
	Reason      string `json:"reason"`
}

type ToolsResult struct {
	Tools       []string `json:"tools"`
	PackageHint string   `json:"package_hint"`
}

type ExecutionStep struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type ExecutionPlan struct {
	Steps []ExecutionStep `json:"steps"`
}

type DecisionResponse struct {
	DetectedMime    string  `json:"detected_mime"`
	IsConvertible   bool    `json:"is_convertible"`
	RejectionReason *string `json:"rejection_reason"`
	RequiredTool    string  `json:"required_tool"`
	ToolInstalled   bool    `json:"tool_installed"`
	CommandTemplate string  `json:"command_template"`
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	model      string
	mu         sync.Mutex
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		model: model,
	}
}

// CompleteWithGrammar queries llama.cpp with a GBNF grammar constraint and enforces a 20s timeout and token limit
func (c *Client) CompleteWithGrammar(ctx context.Context, systemPrompt, userPrompt, grammar string, maxTokens int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Enforce 20-second contextual timeout per inference request to never lock mutex indefinitely
	infCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if maxTokens <= 0 {
		maxTokens = 256
	}

	// First try llama.cpp native endpoint (/completion) which natively takes grammar
	nativeReqBody := map[string]interface{}{
		"prompt":      fmt.Sprintf("<|im_start|>system\n%s<|im_end|>\n<|im_start|>user\n%s<|im_end|>\n<|im_start|>assistant\n", systemPrompt, userPrompt),
		"grammar":     grammar,
		"temperature": 0.1,
		"n_predict":   maxTokens,
	}

	bodyBytes, err := json.Marshal(nativeReqBody)
	if err == nil {
		endpoint := fmt.Sprintf("%s/completion", c.baseURL)
		req, reqErr := http.NewRequestWithContext(infCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr == nil {
			req.Header.Set("Content-Type", "application/json")
			resp, doErr := c.httpClient.Do(req)
			if doErr == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var comp struct {
						Content string `json:"content"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&comp); err == nil && comp.Content != "" {
						return strings.TrimSpace(comp.Content), nil
					}
				}
			}
		}
	}

	// Fallback to OpenAI-compatible /v1/chat/completions with grammar parameter
	chatReqBody := map[string]interface{}{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"grammar":     grammar,
		"temperature": 0.1,
		"max_tokens":  maxTokens,
		"n_predict":   maxTokens,
	}

	chatBytes, err := json.Marshal(chatReqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chat grammar request: %w", err)
	}

	chatEndpoint := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	req, err := http.NewRequestWithContext(infCtx, http.MethodPost, chatEndpoint, bytes.NewReader(chatBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create chat grammar request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm grammar request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("llm grammar response status %d: %s", resp.StatusCode, string(respData))
	}

	var chatCompletion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&chatCompletion); err != nil {
		return "", fmt.Errorf("failed to decode chat grammar response: %w", err)
	}

	if len(chatCompletion.Choices) == 0 {
		return "", fmt.Errorf("empty choices from llm grammar response")
	}

	return strings.TrimSpace(chatCompletion.Choices[0].Message.Content), nil
}

// CheckFeasibility (Phase 1) checks if the file can be converted to the target format
func (c *Client) CheckFeasibility(ctx context.Context, originalName string, size int64, detectedMime, detectedExt, targetExt string) (*FeasibilityResult, error) {
	systemPrompt := `You are an autonomous cognitive file format conversion evaluator.
Determine if source format can technically and meaningfully be directly converted to target format while preserving the intrinsic nature of the data.
Conversions that require hallucinating data or lack direct technical pipelines (e.g. still image to video stream without explicit animation/audio, arbitrary binary/executable to media, still image to audio) MUST return convertible=false.
Any technically meaningful conversion across 2D/3D/CAD/scientific/media/document/archive formats (e.g. .blend, .dxf, .fits, .epub, .wasm, .heic, etc.) with existing open-source/CLI conversion pipelines is convertible=true.
If convertible=false, explain concisely and clearly in 'reason' why this conversion is impossible for the user.
Respond strictly using the JSON grammar.`
	userPrompt := fmt.Sprintf(`Source File: %s
File Size: %d bytes
Detected MIME: %s
Source Extension: %s
Target Extension: %s

Can this source format technically and meaningfully be directly converted to the target extension?`, originalName, size, detectedMime, detectedExt, targetExt)

	raw, err := c.CompleteWithGrammar(ctx, systemPrompt, userPrompt, GrammarFeasibility, 256)
	if err != nil {
		return nil, fmt.Errorf("llm feasibility check failed: %w", err)
	}

	var res FeasibilityResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("failed to parse feasibility json: %w (raw: %s)", err, raw)
	}
	return &res, nil
}

// DiscoverTools (Phase 2) discovers required CLI tools and package hint via GBNF
func (c *Client) DiscoverTools(ctx context.Context, sourceExt, targetExt string) (*ToolsResult, error) {
	systemPrompt := `You are an expert system administrator for CLI file conversion. Identify the required CLI binary names in 'tools' and the distribution package name hint (apt/apk) in 'package_hint' to convert source to target format. Return strictly a JSON object adhering to the grammar.`
	userPrompt := fmt.Sprintf(`Source Extension: %s
Target Extension: %s

What CLI tools are needed to perform this conversion, and what is the system package name to install them?`, sourceExt, targetExt)

	raw, err := c.CompleteWithGrammar(ctx, systemPrompt, userPrompt, GrammarTools, 256)
	if err != nil {
		return nil, fmt.Errorf("llm tool discovery failed: %w", err)
	}

	var res ToolsResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("failed to parse tools json: %w (raw: %s)", err, raw)
	}
	return &res, nil
}

// SynthesizePlan (Phase 3) generates the exact bash command steps
func (c *Client) SynthesizePlan(ctx context.Context, sourceExt, targetExt string, availableTools []string, errorFeedback string) (*ExecutionPlan, error) {
	systemPrompt := `You are an expert command line synthesizer. Generate the exact execution steps to convert a file from source to target. Use $INPUT as placeholder for the source file and $OUTPUT for the destination file. Return strictly a JSON object adhering to the grammar.`
	userPrompt := fmt.Sprintf(`Source Extension: %s
Target Extension: %s
Available CLI tools on system: [%s]
Previous error feedback (if any): %s

Generate steps with command (the binary name) and args array.`, sourceExt, targetExt, strings.Join(availableTools, ", "), errorFeedback)

	raw, err := c.CompleteWithGrammar(ctx, systemPrompt, userPrompt, GrammarExecution, 512)
	if err != nil {
		return nil, fmt.Errorf("llm plan synthesis failed: %w", err)
	}

	var res ExecutionPlan
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("failed to parse execution plan json: %w (raw: %s)", err, raw)
	}
	return &res, nil
}

// PlanConversion backward compatibility method
func (c *Client) PlanConversion(ctx context.Context, originalName, detectedMime, detectedExt, targetExt string, installedTools []string) (*DecisionResponse, error) {
	feas, err := c.CheckFeasibility(ctx, originalName, 0, detectedMime, detectedExt, targetExt)
	if err != nil || !feas.Convertible {
		reason := "Format conversion not supported"
		if feas != nil && feas.Reason != "" {
			reason = feas.Reason
		}
		return &DecisionResponse{
			DetectedMime:    detectedMime,
			IsConvertible:   false,
			RejectionReason: &reason,
		}, nil
	}

	tools, err := c.DiscoverTools(ctx, detectedExt, targetExt)
	reqTool := ""
	if err == nil && len(tools.Tools) > 0 {
		reqTool = tools.Tools[0]
	}

	toolInstalled := false
	for _, it := range installedTools {
		if strings.EqualFold(it, reqTool) {
			toolInstalled = true
			break
		}
	}

	plan, err := c.SynthesizePlan(ctx, detectedExt, targetExt, installedTools, "")
	cmdTemplate := fmt.Sprintf("%s {{input}} {{output}}", reqTool)
	if err == nil && len(plan.Steps) > 0 {
		step := plan.Steps[0]
		cmdTemplate = step.Command + " " + strings.Join(step.Args, " ")
		cmdTemplate = strings.ReplaceAll(cmdTemplate, "$INPUT", "{{input}}")
		cmdTemplate = strings.ReplaceAll(cmdTemplate, "$OUTPUT", "{{output}}")
	}

	return &DecisionResponse{
		DetectedMime:    detectedMime,
		IsConvertible:   true,
		RequiredTool:    reqTool,
		ToolInstalled:   toolInstalled,
		CommandTemplate: cmdTemplate,
	}, nil
}

// ExplainFailure asks the LLM using GrammarFeasibility to formulate a clear, user-friendly failure cause from stderr logs
func (c *Client) ExplainFailure(ctx context.Context, sourceExt, targetExt, stderr string) string {
	systemPrompt := `You are an expert technical debugger for file conversions. Formulate a concise, clear and user-friendly explanation in one sentence of why the conversion failed based on the error logs. Output JSON only matching the grammar.`
	userPrompt := fmt.Sprintf(`Source Extension: %s
Target Extension: %s
Process Error Output (stderr):
%s

Formulate why this conversion failed. Set convertible=false and reason to the user-friendly explanation.`, sourceExt, targetExt, stderr)

	raw, err := c.CompleteWithGrammar(ctx, systemPrompt, userPrompt, GrammarFeasibility, 256)
	if err == nil {
		var res FeasibilityResult
		if err := json.Unmarshal([]byte(raw), &res); err == nil && res.Reason != "" {
			return res.Reason
		}
	}

	return "La conversion a échoué lors de l'exécution des commandes."
}
