package prompts

import (
	"fmt"
	"strings"
)

type Argument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type Prompt struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Arguments   []Argument `json:"arguments,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Message struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

type Registry struct {
	prompts map[string]Prompt
	order   []string
}

var legacyPromptNames = map[string]string{
	"create_integration_snippet": "create_integration_workflow",
}

func NewRegistry() *Registry {
	r := &Registry{prompts: map[string]Prompt{}}
	r.add(Prompt{
		Name:        "create_agent_workflow",
		Description: "Create an AI agent workflow using Mastra (bun) or LangGraph (python). Do not hand-roll custom agent loops.",
		Arguments: []Argument{
			{Name: "goal", Description: "What the agent should do.", Required: true},
			{Name: "language", Description: "bun (Mastra) or python (LangGraph). Default bun.", Required: false},
			{Name: "env", Description: "Validation environment, usually dev.", Required: false},
		},
	})
	r.add(Prompt{
		Name:        "create_integration_workflow",
		Description: "Create a Velane workflow that uses a connected integration and validate it in dev.",
		Arguments: []Argument{
			{Name: "provider", Description: "Integration provider slug, e.g. github, salesforce, slack.", Required: true},
			{Name: "goal", Description: "What the workflow should do.", Required: true},
			{Name: "language", Description: "Workflow language: bun or python.", Required: false},
			{Name: "env", Description: "Validation environment, usually dev.", Required: false},
		},
	})
	r.add(Prompt{
		Name:        "debug_failed_invocation",
		Description: "Inspect a failed invocation, explain the root cause, patch the draft, and rerun in dev.",
		Arguments: []Argument{
			{Name: "invocation_id", Description: "Invocation ID to inspect. Prefer this when available.", Required: false},
			{Name: "workflow_id", Description: "Workflow ID when invocation_id is not available.", Required: false},
			{Name: "snippet_id", Description: "Deprecated alias for workflow_id.", Required: false},
		},
	})
	r.add(Prompt{Name: "create_event_workflow", Description: "Create, test, publish, and explicitly enable a Nango sync event workflow.", Arguments: []Argument{{Name: "connection_id", Description: "Velane connection ID.", Required: true}, {Name: "goal", Description: "How to process model changes.", Required: true}, {Name: "language", Description: "bun or python.", Required: false}, {Name: "env", Description: "Target environment, default dev.", Required: false}}})
	r.add(Prompt{
		Name:        "publish_after_validation",
		Description: "Validate the latest workflow version and publish the exact version number to a target environment.",
		Arguments: []Argument{
			{Name: "workflow_id", Description: "Workflow ID to publish.", Required: true},
			{Name: "snippet_id", Description: "Deprecated alias for workflow_id.", Required: false},
			{Name: "target_env", Description: "Target environment: dev, staging, or prod.", Required: true},
		},
	})
	return r
}

func (r *Registry) List() []Prompt {
	out := make([]Prompt, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.prompts[name])
	}
	return out
}

func (r *Registry) Get(name string, args map[string]any) (Prompt, []Message, error) {
	if alias, ok := legacyPromptNames[name]; ok {
		name = alias
	}
	prompt, ok := r.prompts[name]
	if !ok {
		return Prompt{}, nil, fmt.Errorf("unknown prompt: %s", name)
	}

	var text string
	switch name {
	case "create_agent_workflow":
		text = createAgentWorkflowPrompt(args)
	case "create_integration_workflow":
		text = createIntegrationWorkflowPrompt(args)
	case "debug_failed_invocation":
		text = debugFailedInvocationPrompt(args)
	case "publish_after_validation":
		text = publishAfterValidationPrompt(args)
	case "create_event_workflow":
		text = createEventWorkflowPrompt(args)
	default:
		return Prompt{}, nil, fmt.Errorf("prompt not implemented: %s", name)
	}

	return prompt, []Message{{
		Role: "user",
		Content: Content{
			Type: "text",
			Text: text,
		},
	}}, nil
}

func createEventWorkflowPrompt(args map[string]any) string {
	return strings.TrimSpace(fmt.Sprintf(`Create a Velane Nango sync event workflow.

Connection: %s
Goal: %s
Language: %s
Target environment: %s

Follow this sequence exactly:
1. Read velane://runtime/event-triggers (or call get_event_trigger_docs).
2. Call list_integration_event_models for the connection and choose the deployed model; use a validated manual model only when manual_entry=true.
3. Create the handler with idempotent batch processing, deletion/partial-record handling, and integration(provider, { alias }).
4. Invoke in dev with a representative event envelope and fix all failures.
5. Publish the exact successfully tested version to the target environment.
6. Create the workflow trigger disabled with create_workflow_trigger.
7. Ask for explicit confirmation before update_workflow_trigger enables it.

Return the workflow/version, model, test invocation, trigger ID, and enabled state.`, argString(args, "connection_id", "<connection>"), argString(args, "goal", "<goal>"), argString(args, "language", "bun"), argString(args, "env", "dev")))
}

func (r *Registry) add(prompt Prompt) {
	r.prompts[prompt.Name] = prompt
	r.order = append(r.order, prompt.Name)
}

func createAgentWorkflowPrompt(args map[string]any) string {
	goal := argString(args, "goal", "<goal>")
	language := argString(args, "language", "bun")
	env := argString(args, "env", "dev")
	framework := "Mastra (@mastra/core/agent)"
	if language == "python" {
		framework = "LangGraph (langgraph)"
	}

	return strings.TrimSpace(fmt.Sprintf(`
Create a Velane %s AI agent workflow.

Goal: %s
Validation environment: %s
Required framework: %s

Follow this workflow:

1. Call get_agent_framework_docs before writing any code.
2. Use %s — do NOT hand-roll OpenAI/Anthropic fetch loops or custom agent classes.
3. create_workflow with language=%s (workflow ID is assigned automatically).
4. update_draft with framework-based handler code. Set runtime limits for agents (e.g. max_memory_mb 512, timeout_ms 120000).
5. Store provider API keys as tenant secrets (OPENAI_API_KEY, etc.).
6. invoke_workflow in %s and inspect output, stderr, and dev logs.
7. Do not publish until dev validation succeeds.

Return workflow ID, framework used, input/output shape, validation result, and next publish step.
`, language, goal, env, framework, framework, language, env))
}

func createIntegrationWorkflowPrompt(args map[string]any) string {
	provider := argString(args, "provider", "<provider>")
	goal := argString(args, "goal", "<goal>")
	language := argString(args, "language", "bun")
	env := argString(args, "env", "dev")

	return strings.TrimSpace(fmt.Sprintf(`
Create a Velane %s workflow for provider %q.

Goal: %s
Validation environment: %s

Follow this workflow:

1. Call list_connections with provider=%q to confirm the tenant has a matching connected integration.
2. Call get_integration_docs with provider=%q before writing code.
3. Create or update a workflow draft. Use the built-in integration helper; do not embed credentials.
4. Invoke the workflow in %s and inspect output, stderr, and live dev logs.
5. If the invocation fails, fix the draft and rerun in %s.
6. Do not publish to staging/prod until the validated version_number is known.

Return a concise summary of the workflow, required input shape, output shape, validation result, and next publish step.
`, language, provider, goal, env, provider, provider, env, env))
}

func debugFailedInvocationPrompt(args map[string]any) string {
	invocationID := argString(args, "invocation_id", "")
	workflowID := workflowIDFromArgs(args)

	target := "the failing Velane invocation"
	if invocationID != "" {
		target = "invocation_id=" + invocationID
	} else if workflowID != "" {
		target = "workflow_id=" + workflowID
	}

	return strings.TrimSpace(fmt.Sprintf(`
Debug %s.

Follow this workflow:

1. If invocation_id is available, call get_invocation and inspect status, error, stderr, output, duration_ms, and invoke_mode.
2. If only workflow_id is available, call get_logs with limit=5 and choose the most relevant failed run.
3. Call get_workflow before changing code so you have the latest code, versions, and active environments.
4. Identify the likely root cause. Be explicit about whether it is code, input, integration docs, credentials, egress policy, timeout, or runtime behavior.
5. Patch the draft with update_draft.
6. Invoke in dev. Debug logs are live-only and only forwarded in dev.
7. Repeat until the dev run succeeds, then summarize the fix and the exact version_number that was validated.
`, target))
}

func publishAfterValidationPrompt(args map[string]any) string {
	workflowID := workflowIDFromArgs(args, "<workflow_id>")
	targetEnv := argString(args, "target_env", "<target_env>")

	return strings.TrimSpace(fmt.Sprintf(`
Validate and publish Velane workflow %s to %s.

Follow this workflow:

1. Call get_workflow with workflow_id=%s.
2. Identify the latest version_number and current environment bindings.
3. Invoke the workflow in dev or staging before publishing to %s.
4. Inspect output, stderr, error, and duration. Do not ignore failures.
5. Publish exactly the validated version_number to %s using publish_workflow.
6. Return the workflow slug, published version_number, target environment, and validation evidence.
`, workflowID, targetEnv, workflowID, targetEnv, targetEnv))
}

func workflowIDFromArgs(args map[string]any, fallback ...string) string {
	id := argString(args, "workflow_id", "")
	if id != "" {
		return id
	}
	return argString(args, "snippet_id", fallbackFallback(fallback))
}

func fallbackFallback(fallback []string) string {
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}

func argString(args map[string]any, key, fallback string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return fallback
	}
	return strings.TrimSpace(text)
}
