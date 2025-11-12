// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package apigorowler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/itchyny/gojq"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v3"
)

type ProfileEventType int

const (
	// Root
	EVENT_ROOT_START ProfileEventType = iota

	// Request step container
	EVENT_REQUEST_STEP_START
	EVENT_REQUEST_STEP_END

	// Request step sub-events
	EVENT_CONTEXT_SELECTION
	EVENT_REQUEST_PAGE_START
	EVENT_REQUEST_PAGE_END
	EVENT_PAGINATION_EVAL
	EVENT_URL_COMPOSITION
	EVENT_REQUEST_DETAILS
	EVENT_REQUEST_RESPONSE
	EVENT_RESPONSE_TRANSFORM
	EVENT_CONTEXT_MERGE

	// ForEach step container
	EVENT_FOREACH_STEP_START
	EVENT_FOREACH_STEP_END

	// ForEach step sub-events
	EVENT_PARALLELISM_SETUP
	EVENT_ITEM_SELECTION

	// Authentication events
	EVENT_AUTH_START
	EVENT_AUTH_CACHED
	EVENT_AUTH_LOGIN_START
	EVENT_AUTH_LOGIN_END
	EVENT_AUTH_TOKEN_EXTRACT
	EVENT_AUTH_TOKEN_INJECT
	EVENT_AUTH_END

	// Result events
	EVENT_RESULT
	EVENT_STREAM_RESULT

	// Errors
	EVENT_ERROR
)

type StepProfilerData struct {
	// Core identification
	ID       string           `json:"id"`
	ParentID string           `json:"parentId,omitempty"`
	Type     ProfileEventType `json:"type"`
	Name     string           `json:"name"`
	Step     Step             `json:"step"`

	// Timeline
	Timestamp time.Time `json:"timestamp"`
	Duration  int64     `json:"durationMs,omitempty"` // Only in END events

	// Worker tracking (for parallel execution)
	WorkerID   int    `json:"workerId,omitempty"`
	WorkerPool string `json:"workerPool,omitempty"`

	// Flexible event-specific data
	Data map[string]any `json:"data"`
}

// Helper to create profiler events with UUID4
func newProfilerEvent(eventType ProfileEventType, name string, parentID string, step Step) StepProfilerData {
	return StepProfilerData{
		ID:        uuid.New().String(),
		ParentID:  parentID,
		Type:      eventType,
		Name:      name,
		Step:      step,
		Timestamp: time.Now(),
		Data:      make(map[string]any),
	}
}

// Helper to create profiler events with UUID4
func emitProfilerError(profiler chan StepProfilerData, name string, parentID string, err string) {
	if nil == profiler {
		return
	}
	profiler <- StepProfilerData{
		ID:        uuid.New().String(),
		ParentID:  parentID,
		Type:      EVENT_ERROR,
		Name:      name,
		Step:      Step{},
		Timestamp: time.Now(),
		Data: map[string]any{
			"error": err,
		},
	}
}

// Helper to create profiler events with worker tracking
func newProfilerEventWithWorker(eventType ProfileEventType, name string, parentID string, step Step, workerID int, workerPool string) StepProfilerData {
	event := newProfilerEvent(eventType, name, parentID, step)
	if workerID >= 0 {
		event.WorkerID = workerID
		event.WorkerPool = workerPool
	}
	return event
}

// serializeContextMap converts a context map to a safe serializable format
func serializeContextMap(contextMap map[string]*Context) map[string]any {
	result := make(map[string]any)
	for key, ctx := range contextMap {
		result[key] = map[string]any{
			"data":          copyDataSafe(ctx.Data),
			"parentContext": ctx.ParentContext,
			"depth":         ctx.depth,
			"key":           ctx.key,
		}
	}
	return result
}

// buildContextPath builds the hierarchical path from root to current context
func buildContextPath(contextMap map[string]*Context, currentKey string) string {
	if currentKey == "" || currentKey == "root" {
		return "root"
	}

	ctx, exists := contextMap[currentKey]
	if !exists {
		return currentKey
	}

	// Build path from root to current by traversing parents
	path := []string{currentKey}
	parentKey := ctx.ParentContext

	for parentKey != "" && parentKey != "root" {
		path = append([]string{parentKey}, path...)
		if parentCtx, ok := contextMap[parentKey]; ok {
			parentKey = parentCtx.ParentContext
		} else {
			break
		}
	}

	// Add root at the beginning
	path = append([]string{"root"}, path...)

	// Join with dots
	result := ""
	for i, p := range path {
		if i > 0 {
			result += "."
		}
		result += p
	}
	return result
}

// ContextData represents a single context in a snapshot
type ContextData struct {
	Data          any    `json:"data"`
	ParentContext string `json:"parentContext"`
	Depth         int    `json:"depth"`
	Key           string `json:"key"`
}

// ContextMapSnapshot captures the full context map state at a specific point
type ContextMapSnapshot struct {
	ID        string                 `json:"id"`
	Timestamp string                 `json:"timestamp"`
	EventID   string                 `json:"eventId"`  // Which event created this snapshot
	Contexts  map[string]ContextData `json:"contexts"` // Full context map
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warning(msg string, args ...any)
	Error(msg string, args ...any)
}

type stdLogger struct {
	logger *log.Logger
}

func NewDefaultLogger() Logger {
	return &stdLogger{
		logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

func (l *stdLogger) Info(msg string, args ...any) {
	l.logger.Println("[INFO]", fmt.Sprintf(msg, args...)+"\n")
}

func (l *stdLogger) Debug(msg string, args ...any) {
	l.logger.Println("[DEBUG]", fmt.Sprintf(msg, args...)+"\n")
}

func (l *stdLogger) Warning(msg string, args ...any) {
	l.logger.Println("[WARN]", fmt.Sprintf(msg, args...)+"\n")
}

func (l *stdLogger) Error(msg string, args ...any) {
	l.logger.Println("[ERROR]", fmt.Sprintf(msg, args...)+"\n")
}

const RES_KEY = "$res"

type RateLimitConfig struct {
	RequestsPerSecond float64 `yaml:"requestsPerSecond" json:"requestsPerSecond"`
	Burst             int     `yaml:"burst,omitempty" json:"burst,omitempty"`
}

type Config struct {
	Steps          []Step               `yaml:"steps" json:"steps"`
	RootContext    interface{}          `yaml:"rootContext" json:"rootContext"`
	Authentication *AuthenticatorConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
	Headers        map[string]string    `yaml:"headers,omitempty" json:"headers,omitempty"`
	Stream         bool                 `yaml:"stream,omitempty" json:"stream,omitempty"`
}

type Step struct {
	Type              string                `yaml:"type" json:"type"`
	Name              string                `yaml:"name,omitempty" json:"name,omitempty"`
	Path              string                `yaml:"path,omitempty" json:"path,omitempty"`
	As                string                `yaml:"as,omitempty" json:"as,omitempty"`
	Values            []interface{}         `yaml:"values,omitempty" json:"values,omitempty"`
	Steps             []Step                `yaml:"steps,omitempty" json:"steps,omitempty"`
	Request           *RequestConfig        `yaml:"request,omitempty" json:"request,omitempty"`
	ResultTransformer string                `yaml:"resultTransformer,omitempty" json:"resultTransformer,omitempty"`
	MergeWithParentOn string                `yaml:"mergeWithParentOn,omitempty" json:"mergeWithParentOn,omitempty"`
	MergeOn           string                `yaml:"mergeOn,omitempty" json:"mergeOn,omitempty"`
	MergeWithContext  *MergeWithContextRule `yaml:"mergeWithContext,omitempty" json:"mergeWithContext,omitempty"`
	NoopMerge         bool                  `yaml:"noopMerge,omitempty" json:"noopMerge,omitempty"`
	Parallel          bool                  `yaml:"parallel,omitempty" json:"parallel,omitempty"`
	MaxConcurrency    int                   `yaml:"maxConcurrency,omitempty" json:"maxConcurrency,omitempty"`
	RateLimit         *RateLimitConfig      `yaml:"rateLimit,omitempty" json:"rateLimit,omitempty"`
}

type RequestConfig struct {
	URL            string               `yaml:"url" json:"url"`
	Method         string               `yaml:"method" json:"method"`
	Headers        map[string]string    `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body           map[string]any       `yaml:"body,omitempty" json:"body,omitempty"`
	Pagination     Pagination           `yaml:"pagination,omitempty" json:"pagination,omitempty"`
	Authentication *AuthenticatorConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

type MergeWithContextRule struct {
	Name string `yaml:"name"`
	Rule string `yaml:"rule"`
}

type Context struct {
	Data          interface{}
	ParentContext string
	key           string
	depth         int
}

type stepExecution struct {
	step              Step
	currentContextKey string
	currentContext    *Context
	contextMap        map[string]*Context
	parentID          string // Parent step ID for profiler hierarchy
}

// mergeOperation encapsulates the parameters needed for a merge operation
type mergeOperation struct {
	step            Step
	currentContext  *Context
	contextMap      map[string]*Context
	result          any
	templateContext map[string]any
}

// httpRequestContext encapsulates HTTP request preparation parameters
type httpRequestContext struct {
	urlTemplate    string
	method         string
	requestID      string
	headers        map[string]string
	configuredBody map[string]any
	bodyParams     map[string]interface{}
	contentType    string
	queryParams    map[string]string
	nextPageURL    string
	authenticator  Authenticator
}

// forEachResult holds the result of a single forEach iteration
type forEachResult struct {
	index          int
	result         any
	profilerEvents []StepProfilerData
	err            error
	threadID       int
}

type ApiCrawler struct {
	Config              Config
	ContextMap          map[string]*Context
	globalAuthenticator Authenticator
	DataStream          chan any
	logger              Logger
	httpClient          HTTPClient
	profiler            chan StepProfilerData
	enableProfilation   bool
	templateCache       map[string]*template.Template
	jqCache             map[string]*gojq.Code
	mergeMutex          sync.Mutex // Protects concurrent merge operations

	// Phase 2: Snapshot system
	snapshotStore   map[string]ContextMapSnapshot
	snapshotMutex   sync.Mutex // Protects snapshot store
	snapshotCounter int
}

func NewApiCrawler(configPath string) (*ApiCrawler, []ValidationError, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, nil, err
	}

	errors := ValidateConfig(cfg)
	if len(errors) != 0 {
		return nil, errors, fmt.Errorf("validation failed")
	}

	c := &ApiCrawler{
		httpClient:      http.DefaultClient,
		Config:          cfg,
		ContextMap:      map[string]*Context{},
		logger:          NewDefaultLogger(),
		profiler:        nil,
		templateCache:   make(map[string]*template.Template),
		jqCache:         make(map[string]*gojq.Code),
		snapshotStore:   make(map[string]ContextMapSnapshot),
		snapshotCounter: 0,
	}

	// handle stream channel
	if cfg.Stream {
		c.DataStream = make(chan any)
	}

	// instantiate global authenticator
	if cfg.Authentication != nil {
		c.globalAuthenticator = NewAuthenticator(*cfg.Authentication, c.httpClient)
	} else {
		c.globalAuthenticator = NoopAuthenticator{
			BaseAuthenticator: &BaseAuthenticator{
				profiler: &AuthProfiler{authType: "cookie"},
			},
		}
	}
	return c, nil, nil
}

func (a *ApiCrawler) GetDataStream() chan interface{} {
	return a.DataStream
}

func (a *ApiCrawler) GetData() interface{} {
	return a.ContextMap["root"].Data
}

func (a *ApiCrawler) SetLogger(logger Logger) {
	a.logger = logger
}

func (a *ApiCrawler) SetClient(client HTTPClient) {
	a.httpClient = client
}

func (a *ApiCrawler) EnableProfiler() chan StepProfilerData {
	a.enableProfilation = true
	a.profiler = make(chan StepProfilerData)
	return a.profiler
}

// getOrCompileTemplate retrieves a pre-compiled template from the cache,
// or compiles, caches, and returns it if not found.
func (a *ApiCrawler) getOrCompileTemplate(tmplString string) (*template.Template, error) {
	if tmpl, ok := a.templateCache[tmplString]; ok {
		return tmpl, nil
	}

	tmpl, err := template.New("dynamic").Parse(tmplString)
	if err != nil {
		return nil, fmt.Errorf("error parsing template: %w", err)
	}

	a.templateCache[tmplString] = tmpl
	return tmpl, nil
}

// getOrCompileJQRule retrieves a pre-compiled JQ rule from the cache,
// or compiles, caches, and returns it if not found.
func (a *ApiCrawler) getOrCompileJQRule(ruleString string, variables ...string) (*gojq.Code, error) {
	cacheKey := ruleString
	if len(variables) > 0 {
		// Use a unique key for rules with variables
		// to avoid collisions with rules without variables.
		cacheKey += fmt.Sprintf("$$vars:%v", variables)
	}

	if code, ok := a.jqCache[cacheKey]; ok {
		return code, nil
	}

	query, err := gojq.Parse(ruleString)
	if err != nil {
		return nil, fmt.Errorf("invalid jq rule '%s': %w", ruleString, err)
	}

	code, err := gojq.Compile(query, gojq.WithVariables(variables))
	if err != nil {
		return nil, fmt.Errorf("failed to compile jq rule: %w", err)
	}

	a.jqCache[cacheKey] = code
	return code, nil
}

func deepCopy[T any](src T) (T, error) {
	// Use JSON marshaling for deep copy - more reliable for JSON data than gob
	var dst T

	jsonBytes, err := json.Marshal(src)
	if err != nil {
		var zero T
		return zero, err
	}

	if err := json.Unmarshal(jsonBytes, &dst); err != nil {
		var zero T
		return zero, err
	}

	return dst, nil
}

// captureSnapshot creates a snapshot of the current context map state
func (a *ApiCrawler) captureSnapshot(eventID string) string {
	if a.profiler == nil {
		return "" // No profiling enabled
	}

	a.snapshotMutex.Lock()
	defer a.snapshotMutex.Unlock()

	// Generate unique snapshot ID
	a.snapshotCounter++
	snapshotID := fmt.Sprintf("snapshot_%d", a.snapshotCounter)

	// Create context data map
	contexts := make(map[string]ContextData)
	for key, ctx := range a.ContextMap {
		// Deep copy the data to avoid race conditions
		dataCopy, _ := deepCopy(ctx.Data)
		contexts[key] = ContextData{
			Data:          dataCopy,
			ParentContext: ctx.ParentContext,
			Depth:         ctx.depth,
			Key:           ctx.key,
		}
	}

	// Store snapshot
	snapshot := ContextMapSnapshot{
		ID:        snapshotID,
		Timestamp: fmt.Sprintf("%d", a.snapshotCounter), // Simple incrementing timestamp
		EventID:   eventID,
		Contexts:  contexts,
	}

	a.snapshotStore[snapshotID] = snapshot
	return snapshotID
}

// getSnapshot retrieves a snapshot by ID
func (a *ApiCrawler) getSnapshot(snapshotID string) (ContextMapSnapshot, bool) {
	a.snapshotMutex.Lock()
	defer a.snapshotMutex.Unlock()

	snapshot, ok := a.snapshotStore[snapshotID]
	return snapshot, ok
}

// GetAllSnapshots returns all snapshots (for CLI to output)
func (a *ApiCrawler) GetAllSnapshots() map[string]ContextMapSnapshot {
	a.snapshotMutex.Lock()
	defer a.snapshotMutex.Unlock()

	// Return copy to avoid concurrent access
	snapshots := make(map[string]ContextMapSnapshot)
	for k, v := range a.snapshotStore {
		snapshots[k] = v
	}
	return snapshots
}

func newStepExecution(step Step, currentContextKey string, contextMap map[string]*Context, parentID string) *stepExecution {
	return &stepExecution{
		step:              step,
		currentContextKey: currentContextKey,
		contextMap:        contextMap,
		currentContext:    contextMap[currentContextKey],
		parentID:          parentID,
	}
}

func (c *ApiCrawler) Run(ctx context.Context) error {
	rootCtx := &Context{
		Data:          c.Config.RootContext,
		ParentContext: "",
		depth:         0,
		key:           "root",
	}

	c.ContextMap["root"] = rootCtx
	currentContext := "root"

	// Emit ROOT_START event
	var rootID string
	if c.profiler != nil {
		event := newProfilerEvent(EVENT_ROOT_START, "Root Start", "", Step{})
		event.Data = map[string]any{
			"contextMap": serializeContextMap(c.ContextMap),
			"config": map[string]any{
				"rootContext": c.Config.RootContext,
				"stream":      c.Config.Stream,
			},
		}
		c.profiler <- event
		rootID = event.ID
	}

	for _, step := range c.Config.Steps {
		ecxec := newStepExecution(step, currentContext, c.ContextMap, rootID)
		if err := c.ExecuteStep(ctx, ecxec); err != nil {
			return err
		}
	}

	// Emit final result if not streaming
	if c.profiler != nil && !c.Config.Stream {
		resultEvent := newProfilerEvent(EVENT_RESULT, "Final Result", rootID, Step{})
		resultEvent.Data = map[string]any{
			"result": copyDataSafe(rootCtx.Data),
		}
		c.profiler <- resultEvent
	}

	return nil
}

func (c *ApiCrawler) ExecuteStep(ctx context.Context, exec *stepExecution) error {
	switch exec.step.Type {
	case "request":
		return c.handleRequest(ctx, exec)
	case "forEach":
		return c.handleForEach(ctx, exec)
	default:
		return fmt.Errorf("unknown step type: %s", exec.step.Type)
	}
}

func (c *ApiCrawler) handleRequest(ctx context.Context, exec *stepExecution) error {
	c.logger.Info("[Request] Preparing %s", exec.step.Name)

	// Emit REQUEST_STEP_START event
	stepStartTime := time.Now()
	var stepID string
	if c.profiler != nil {
		event := newProfilerEvent(EVENT_REQUEST_STEP_START, exec.step.Name, exec.parentID, exec.step)
		event.Data = map[string]any{
			"stepName":   exec.step.Name,
			"stepConfig": exec.step,
		}
		c.profiler <- event
		stepID = event.ID
	}

	templateCtx := contextMapToTemplate(exec.contextMap)

	// Determine authenticator (request-specific overrides global)
	authenticator := c.globalAuthenticator
	if exec.step.Request.Authentication != nil {
		authenticator = NewAuthenticator(*exec.step.Request.Authentication, c.httpClient)
	}

	// Set profiler on authenticator
	if authenticator != nil && c.profiler != nil {
		authenticator.SetProfiler(c.profiler)
	}

	// Initialize paginator
	paginator, err := NewPaginator(ConfigP{exec.step.Request.Pagination})
	if err != nil {
		emitProfilerError(c.profiler, "Paginator Error", stepID, err.Error())
		return fmt.Errorf("error creating request paginator: %w", err)
	}

	stop := false
	next := paginator.NextFromCtx()

	// Track previous response for PAGINATION_EVAL event
	var previousResponseBody interface{}
	var previousResponseHeaders map[string]string

	// Pagination loop
	for !stop {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pageNum := paginator.PageNum()

		// Emit REQUEST_PAGE_START event
		pageStartTime := time.Now()
		var pageID string
		if c.profiler != nil {
			event := newProfilerEvent(EVENT_REQUEST_PAGE_START, fmt.Sprintf("Page %d", pageNum), stepID, exec.step)
			event.Data = map[string]any{
				"pageNumber": pageNum,
			}
			c.profiler <- event
			pageID = event.ID
		}

		// Prepare HTTP request
		reqCtx := httpRequestContext{
			requestID:      pageID,
			urlTemplate:    exec.step.Request.URL,
			method:         exec.step.Request.Method,
			headers:        exec.step.Request.Headers,
			configuredBody: exec.step.Request.Body,
			bodyParams:     next.BodyParams,
			contentType:    getContentType(exec.step.Request.Headers),
			queryParams:    next.QueryParams,
			nextPageURL:    next.NextPageUrl,
			authenticator:  authenticator,
		}

		req, urlObj, err := c.prepareHTTPRequest(reqCtx, templateCtx, c.Config.Headers)
		if err != nil {
			emitProfilerError(c.profiler, "Prepare Request Error", pageID, err.Error())
			return err
		}

		// Emit URL_COMPOSITION event
		if c.profiler != nil {
			event := newProfilerEvent(EVENT_URL_COMPOSITION, "URL Composition", pageID, exec.step)

			// Build result headers and body from request
			resultHeaders := make(map[string]string)
			for k, v := range req.Header {
				if len(v) > 0 {
					resultHeaders[k] = v[0]
				}
			}

			var resultBody interface{}
			if req.Body != nil {
				// Note: Request body is already set, we don't read it here to avoid consuming it
				resultBody = next.BodyParams
			}

			event.Data = map[string]any{
				"urlTemplate": exec.step.Request.URL,
				"paginationState": map[string]any{
					"pageNumber":  pageNum,
					"queryParams": next.QueryParams,
					"bodyParams":  next.BodyParams,
					"nextPageUrl": next.NextPageUrl,
				},
				"goTemplateContext": templateCtx,
				"resultUrl":         urlObj.String(),
				"resultHeaders":     resultHeaders,
				"resultBody":        resultBody,
			}
			c.profiler <- event
		}

		c.logger.Info("[Request] %s", urlObj.String())

		// Emit REQUEST_DETAILS event
		if c.profiler != nil {
			event := newProfilerEvent(EVENT_REQUEST_DETAILS, "Request Details", pageID, exec.step)

			// Build curl command
			curlCmd := fmt.Sprintf("curl -X %s '%s'", req.Method, urlObj.String())
			for k, v := range req.Header {
				if len(v) > 0 {
					curlCmd += fmt.Sprintf(" -H '%s: %s'", k, v[0])
				}
			}
			if req.Body != nil && len(next.BodyParams) > 0 {
				bodyJSON, _ := json.Marshal(next.BodyParams)
				curlCmd += fmt.Sprintf(" -d '%s'", string(bodyJSON))
			}

			// Build headers map
			headers := make(map[string]string)
			for k, v := range req.Header {
				if len(v) > 0 {
					headers[k] = v[0]
				}
			}

			event.Data = map[string]any{
				"curl":    curlCmd,
				"method":  req.Method,
				"url":     urlObj.String(),
				"headers": headers,
				"body":    next.BodyParams,
			}
			c.profiler <- event
		}

		c.logger.Debug("[Request] Got response: status pending")

		// Execute HTTP request with timing
		requestStartTime := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			emitProfilerError(c.profiler, "Request Error", pageID, err.Error())
			return fmt.Errorf("error performing HTTP request: %w", err)
		}
		defer resp.Body.Close()
		durationMs := time.Since(requestStartTime).Milliseconds()

		// Emit HTTP response event with metadata and duration
		responseSize := int(resp.ContentLength)
		if responseSize < 0 {
			responseSize = 0
		}

		// Capture pagination state BEFORE calling paginator.Next()
		previousPageState := map[string]any{
			"pageNumber": pageNum,
			"params": map[string]any{
				"queryParams": next.QueryParams,
				"bodyParams":  next.BodyParams,
			},
			"nextPageUrl": next.NextPageUrl,
		}

		// Update pagination state (reads and restores response body)
		next, stop, err = paginator.Next(resp)
		if err != nil {
			emitProfilerError(c.profiler, "Paginator Error", pageID, err.Error())
			return fmt.Errorf("paginator update error: %w", err)
		}

		// Decode JSON response
		var raw interface{}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			emitProfilerError(c.profiler, "Response Decode Error", pageID, err.Error())
			return fmt.Errorf("error decoding response JSON: %w", err)
		}

		// Emit PAGINATION_EVAL event (if pagination is configured and this is not the first page)
		if pageNum > 0 && c.profiler != nil && !stop {
			event := newProfilerEvent(EVENT_PAGINATION_EVAL, "Pagination Evaluation", pageID, exec.step)

			afterPageState := map[string]any{
				"pageNumber": paginator.PageNum(),
				"params": map[string]any{
					"queryParams": next.QueryParams,
					"bodyParams":  next.BodyParams,
				},
				"nextPageUrl": next.NextPageUrl,
			}

			event.Data = map[string]any{
				"pageNumber":       pageNum,
				"paginationConfig": exec.step.Request.Pagination,
				"previousResponse": map[string]any{
					"body":    copyDataSafe(previousResponseBody),
					"headers": previousResponseHeaders,
				},
				"previousState": previousPageState,
				"afterState":    afterPageState,
			}
			c.profiler <- event
		}

		// Emit REQUEST_RESPONSE event
		if c.profiler != nil {
			event := newProfilerEvent(EVENT_REQUEST_RESPONSE, "Request Response", pageID, exec.step)

			// Build response headers map
			responseHeaders := make(map[string]string)
			for k, v := range resp.Header {
				if len(v) > 0 {
					responseHeaders[k] = v[0]
				}
			}

			event.Data = map[string]any{
				"statusCode":   resp.StatusCode,
				"headers":      responseHeaders,
				"body":         copyDataSafe(raw),
				"responseSize": responseSize,
				"durationMs":   durationMs,
			}
			c.profiler <- event
		}

		// Store response for next PAGINATION_EVAL event
		previousResponseBody = raw
		previousResponseHeaders = make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				previousResponseHeaders[k] = v[0]
			}
		}

		// Transform response
		transformed, err := c.transformResult(raw, exec.step.ResultTransformer, templateCtx)
		if err != nil {
			emitProfilerError(c.profiler, "Response Transform Error", pageID, err.Error())
			return err
		}

		// Emit RESPONSE_TRANSFORM event
		if c.profiler != nil && exec.step.ResultTransformer != "" {
			event := newProfilerEvent(EVENT_RESPONSE_TRANSFORM, "Response Transform", pageID, exec.step)

			event.Data = map[string]any{
				"transformRule":  exec.step.ResultTransformer,
				"beforeResponse": copyDataSafe(raw),
				"afterResponse":  copyDataSafe(transformed),
				// TODO: Add diff computation
			}
			c.profiler <- event
		}

		// Execute nested steps on transformed result
		thisContextKey := exec.currentContextKey
		if exec.step.As != "" {
			thisContextKey = exec.step.As
		}

		childContextMap := childMapWith(exec.contextMap, exec.currentContext, thisContextKey, transformed)

		// Emit CONTEXT_SELECTION event (context created for nested steps)
		if c.profiler != nil && len(exec.step.Steps) > 0 {
			event := newProfilerEvent(EVENT_CONTEXT_SELECTION, "Context Selection", pageID, exec.step)
			contextPath := buildContextPath(childContextMap, thisContextKey)
			event.Data = map[string]any{
				"contextPath":        contextPath,
				"currentContextKey":  thisContextKey,
				"currentContextData": copyDataSafe(childContextMap[thisContextKey].Data),
				"fullContextMap":     serializeContextMap(childContextMap),
			}
			c.profiler <- event
		}

		for _, step := range exec.step.Steps {
			newExec := newStepExecution(step, thisContextKey, childContextMap, pageID)
			if err := c.ExecuteStep(ctx, newExec); err != nil {
				return err
			}
		}

		// Get final result after nested steps
		transformed = childContextMap[thisContextKey].Data

		// Apply merge strategy
		mergeOp := mergeOperation{
			step:            exec.step,
			currentContext:  exec.currentContext,
			contextMap:      exec.contextMap,
			result:          transformed,
			templateContext: templateCtx,
		}

		dataBefore := exec.currentContext.Data

		mergeStepName, err := c.performMerge(mergeOp)
		if err != nil {
			emitProfilerError(c.profiler, "Merge Error", pageID, err.Error())
			return err
		}

		// Emit CONTEXT_MERGE event if merge happened
		if mergeStepName != "" && c.profiler != nil {
			event := newProfilerEvent(EVENT_CONTEXT_MERGE, "Context Merge", pageID, exec.step)

			// Determine merge rule and target context
			mergeRule := ""
			currentContextKey := exec.currentContext.key
			targetContextKey := exec.currentContext.key

			if exec.step.MergeOn != "" {
				mergeRule = exec.step.MergeOn
			} else if exec.step.MergeWithParentOn != "" {
				mergeRule = exec.step.MergeWithParentOn
				targetContextKey = exec.currentContext.ParentContext
			} else if exec.step.MergeWithContext != nil {
				mergeRule = exec.step.MergeWithContext.Rule
				targetContextKey = exec.step.MergeWithContext.Name
			}

			// Get target context data
			targetContext := exec.contextMap[targetContextKey]
			var targetContextBefore interface{}
			if targetContext != nil {
				targetContextBefore = dataBefore
			}

			event.Data = map[string]any{
				"currentContextKey":   currentContextKey,
				"targetContextKey":    targetContextKey,
				"mergeRule":           mergeRule,
				"targetContextBefore": copyDataSafe(targetContextBefore),
				"targetContextAfter":  copyDataSafe(exec.currentContext.Data),
				"fullContextMap":      serializeContextMap(exec.contextMap),
				// TODO: Add diff computation
			}
			c.profiler <- event
		}

		// Handle streaming at root level
		if exec.currentContext.depth == 0 && c.Config.Stream {
			array_data := exec.currentContext.Data.([]interface{})
			for _, d := range array_data {
				c.DataStream <- d

				// Emit STREAM_RESULT event
				if c.profiler != nil {
					streamEvent := newProfilerEvent(EVENT_STREAM_RESULT, "Stream Result", pageID, exec.step)
					streamEvent.Data = map[string]any{
						"entity": copyDataSafe(d),
						"index":  len(array_data),
					}
					c.profiler <- streamEvent
				}
			}
			exec.currentContext.Data = []interface{}{}
		}

		// Emit REQUEST_PAGE_END event (reuse START event ID)
		if c.profiler != nil && pageID != "" {
			event := StepProfilerData{
				ID:        pageID, // Reuse START event ID
				ParentID:  stepID,
				Type:      EVENT_REQUEST_PAGE_END,
				Name:      fmt.Sprintf("Page %d End", pageNum),
				Step:      exec.step,
				Timestamp: time.Now(),
				Duration:  time.Since(pageStartTime).Milliseconds(),
				Data:      make(map[string]any),
			}
			c.profiler <- event
		}
	}

	// Emit REQUEST_STEP_END event (reuse START event ID)
	if c.profiler != nil && stepID != "" {
		event := StepProfilerData{
			ID:        stepID, // Reuse START event ID
			ParentID:  exec.parentID,
			Type:      EVENT_REQUEST_STEP_END,
			Name:      exec.step.Name + " End",
			Step:      exec.step,
			Timestamp: time.Now(),
			Duration:  time.Since(stepStartTime).Milliseconds(),
			Data:      make(map[string]any),
		}
		c.profiler <- event
	}

	return nil
}

// createRateLimiter creates a rate limiter from config, returns nil if no rate limiting
func createRateLimiter(stepLimit *RateLimitConfig) *rate.Limiter {
	var limitConfig *RateLimitConfig

	// Step-level rate limit takes precedence
	if stepLimit != nil {
		limitConfig = stepLimit
	}

	if limitConfig == nil || limitConfig.RequestsPerSecond <= 0 {
		return nil
	}

	burst := limitConfig.Burst
	if burst <= 0 {
		burst = 1 // Default burst size
	}

	return rate.NewLimiter(rate.Limit(limitConfig.RequestsPerSecond), burst)
}

// executeForEachIteration executes a single forEach iteration
func (c *ApiCrawler) executeForEachIteration(
	ctx context.Context,
	index int,
	item any,
	exec *stepExecution,
	profilerEnabled bool,
	stepID string,
	workerID int,
	workerPoolID string,
) forEachResult {
	result := forEachResult{
		index:          index,
		profilerEvents: make([]StepProfilerData, 0),
	}

	c.logger.Info("[ForEach] Iteration %d as '%s'", index, exec.step.As, "item", item)

	childContextMap := childMapWith(exec.contextMap, exec.currentContext, exec.step.As, item)

	// Emit CONTEXT_SELECTION event with worker tracking (context created for iteration)
	if c.profiler != nil {
		event := newProfilerEventWithWorker(EVENT_CONTEXT_SELECTION, "Context Selection", stepID, exec.step, workerID, workerPoolID)
		contextPath := buildContextPath(childContextMap, exec.step.As)
		event.Data = map[string]any{
			"contextPath":        contextPath,
			"currentContextKey":  exec.step.As,
			"currentContextData": copyDataSafe(childContextMap[exec.step.As].Data),
			"fullContextMap":     serializeContextMap(childContextMap),
		}
		c.profiler <- event
	}

	// Emit ITEM_SELECTION event with worker tracking
	var itemID string
	if c.profiler != nil {
		event := newProfilerEventWithWorker(EVENT_ITEM_SELECTION, fmt.Sprintf("Item %d", index), stepID, exec.step, workerID, workerPoolID)
		event.Data = map[string]any{
			"iterationIndex":     index,
			"itemValue":          copyDataSafe(item),
			"currentContextKey":  exec.step.As,
			"currentContextData": copyDataSafe(childContextMap[exec.step.As].Data),
		}
		c.profiler <- event
		itemID = event.ID
	}

	// Execute nested steps
	for _, nested := range exec.step.Steps {
		newExec := newStepExecution(nested, exec.step.As, childContextMap, itemID)
		if err := c.ExecuteStep(ctx, newExec); err != nil {
			result.err = err
			return result
		}
	}

	result.result = childContextMap[exec.step.As].Data

	return result
}

// executeForEachParallel executes forEach iterations in parallel
func (c *ApiCrawler) executeForEachParallel(
	ctx context.Context,
	exec *stepExecution,
	items []interface{},
	maxConcurrency int,
	rateLimiter *rate.Limiter,
	stepID string,
) ([]interface{}, error) {
	profilerEnabled := c.profiler != nil
	numItems := len(items)

	// Results channel sized to hold all results
	resultsChan := make(chan forEachResult, numItems)

	// Error group for managing goroutines
	var wg sync.WaitGroup

	// Semaphore for concurrency control
	semaphore := make(chan struct{}, maxConcurrency)

	// Launch workers for each item
	for i, item := range items {
		wg.Add(1)

		go func(index int, item any, threadID int) {
			defer wg.Done()

			// Acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check context cancellation
			select {
			case <-ctx.Done():
				resultsChan <- forEachResult{index: index, err: ctx.Err()}
				return
			default:
			}

			// Apply rate limiting if configured
			if rateLimiter != nil {
				if err := rateLimiter.Wait(ctx); err != nil {
					resultsChan <- forEachResult{index: index, err: err}
					return
				}
			}

			// Execute iteration
			workerPoolID := stepID + "-pool"
			result := c.executeForEachIteration(ctx, index, item, exec, profilerEnabled, stepID, threadID, workerPoolID)
			result.threadID = threadID
			resultsChan <- result
		}(i, item, i%maxConcurrency)
	}

	// Close results channel when all workers complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and maintain order
	results := make([]forEachResult, numItems)
	for result := range resultsChan {
		if result.err != nil {
			return nil, result.err
		}
		results[result.index] = result
	}

	// Emit profiler events in order if profiling is enabled
	if profilerEnabled {
		for _, result := range results {
			for _, event := range result.profilerEvents {
				c.profiler <- event
			}
		}
	}

	// Extract execution results in order
	executionResults := make([]interface{}, numItems)
	for i, result := range results {
		executionResults[i] = result.result
	}

	return executionResults, nil
}

func (c *ApiCrawler) handleForEach(ctx context.Context, exec *stepExecution) error {
	c.logger.Info("[Foreach] Preparing %s", exec.step.Name)

	// Emit FOREACH_STEP_START event
	stepStartTime := time.Now()
	var stepID string
	if c.profiler != nil {
		event := newProfilerEvent(EVENT_FOREACH_STEP_START, exec.step.Name, exec.parentID, exec.step)
		event.Data = map[string]any{
			"stepName":   exec.step.Name,
			"stepConfig": exec.step,
		}
		c.profiler <- event
		stepID = event.ID
	}

	results := []interface{}{}

	// Extract items to iterate over
	if len(exec.step.Path) != 0 && exec.step.Values == nil {
		c.logger.Debug("[Foreach] Extracting from parent context with rule: %s", exec.step.Path)

		code, err := c.getOrCompileJQRule(exec.step.Path)
		if err != nil {
			emitProfilerError(c.profiler, "Path Extraction Error", stepID, err.Error())
			return fmt.Errorf("failed to get/compile jq path: %w", err)
		}

		iter := code.Run(exec.currentContext.Data)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, isErr := v.(error); isErr {
				return fmt.Errorf("jq error: %w", err)
			}
			results = append(results, v)
		}

		// Make sure the result is an array (jq might emit one-by-one items)
		if len(results) == 1 {
			if arr, ok := results[0].([]interface{}); ok {
				results = arr
			}
		}
	} else if exec.step.Values != nil {
		c.logger.Debug("[Foreach] using values over path: %s, values %+v", exec.step.Path, exec.step.Values)

		for _, v := range exec.step.Values {
			results = append(results, map[string]interface{}{"value": v})
		}
	}

	// Determine execution mode and parameters
	var executionResults []interface{}
	var err error

	if exec.step.Parallel {
		// Determine max concurrency (step > global > default)
		maxConcurrency := exec.step.MaxConcurrency
		if maxConcurrency == 0 {
			maxConcurrency = 10 // Default concurrency
		}

		// Create rate limiter if configured
		rateLimiter := createRateLimiter(exec.step.RateLimit)

		// Emit PARALLELISM_SETUP event
		if c.profiler != nil {
			workerPoolID := stepID + "-pool"
			workerIDs := make([]int, maxConcurrency)
			for i := 0; i < maxConcurrency; i++ {
				workerIDs[i] = i
			}

			event := newProfilerEvent(EVENT_PARALLELISM_SETUP, "Parallelism Setup", stepID, exec.step)
			event.Data = map[string]any{
				"maxConcurrency": maxConcurrency,
				"workerPoolId":   workerPoolID,
				"workerIds":      workerIDs,
			}
			if rateLimiter != nil {
				if exec.step.RateLimit != nil {
					event.Data["rateLimit"] = exec.step.RateLimit.RequestsPerSecond
					event.Data["burst"] = exec.step.RateLimit.Burst
				}
			}
			c.profiler <- event
		}

		c.logger.Info("[ForEach] Executing %d iterations in parallel (max concurrency: %d)", len(results), maxConcurrency)

		// Execute in parallel
		executionResults, err = c.executeForEachParallel(ctx, exec, results, maxConcurrency, rateLimiter, stepID)
		if err != nil {
			return err
		}
	} else {
		// Execute sequentially (original behavior)
		executionResults = make([]interface{}, 0)
		for i, item := range results {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			c.logger.Info("[ForEach] Iteration %d as '%s'", i, exec.step.As, "item", item)

			childContextMap := childMapWith(exec.contextMap, exec.currentContext, exec.step.As, item)

			// Emit CONTEXT_SELECTION event (context created for iteration)
			if c.profiler != nil {
				event := newProfilerEvent(EVENT_CONTEXT_SELECTION, "Context Selection", stepID, exec.step)
				contextPath := buildContextPath(childContextMap, exec.step.As)
				event.Data = map[string]any{
					"contextPath":        contextPath,
					"currentContextKey":  exec.step.As,
					"currentContextData": copyDataSafe(childContextMap[exec.step.As].Data),
					"fullContextMap":     serializeContextMap(childContextMap),
				}
				c.profiler <- event
			}

			// Emit ITEM_SELECTION event
			var itemID string
			if c.profiler != nil {
				event := newProfilerEvent(EVENT_ITEM_SELECTION, fmt.Sprintf("Item %d", i), stepID, exec.step)
				event.Data = map[string]any{
					"iterationIndex":     i,
					"itemValue":          copyDataSafe(item),
					"currentContextKey":  exec.step.As,
					"currentContextData": copyDataSafe(childContextMap[exec.step.As].Data),
				}
				c.profiler <- event
				itemID = event.ID
			}

			for _, nested := range exec.step.Steps {
				newExec := newStepExecution(nested, exec.step.As, childContextMap, itemID)
				if err := c.ExecuteStep(ctx, newExec); err != nil {
					return err
				}
			}

			executionResults = append(executionResults, childContextMap[exec.step.As].Data)
		}
	}

	// Determine merge strategy
	templateCtx := contextMapToTemplate(exec.contextMap)

	// Check if custom merge rules are specified
	hasCustomMerge := exec.step.MergeOn != "" || exec.step.MergeWithParentOn != "" || exec.step.MergeWithContext != nil || exec.step.NoopMerge

	if hasCustomMerge {
		// Use custom merge logic (same as request steps)
		mergeOp := mergeOperation{
			step:            exec.step,
			currentContext:  exec.currentContext,
			contextMap:      exec.contextMap,
			result:          executionResults,
			templateContext: templateCtx,
		}

		_, err := c.performMerge(mergeOp)
		if err != nil {
			emitProfilerError(c.profiler, "Merge Error", stepID, err.Error())
			return err
		}

	} else {
		// Default: patch the array at exec.step.Path with new results
		code, err := c.getOrCompileJQRule(exec.step.Path+" = $new", "$new")
		if err != nil {
			emitProfilerError(c.profiler, "Merge Error", stepID, err.Error())
			return fmt.Errorf("failed to get/compile merge rule: %w", err)
		}

		iter := code.Run(exec.currentContext.Data, executionResults)

		v, ok := iter.Next()
		if !ok {
			return fmt.Errorf("patch yielded nothing")
		}
		if err, isErr := v.(error); isErr {
			return err
		}

		exec.currentContext.Data = v
	}

	// Handle streaming at root level
	if exec.currentContext.depth <= 1 && c.Config.Stream {
		array_data := exec.currentContext.Data.([]interface{})
		for _, d := range array_data {
			c.DataStream <- d

			// Emit STREAM_RESULT event
			if c.profiler != nil {
				streamEvent := newProfilerEvent(EVENT_STREAM_RESULT, "Stream Result", stepID, exec.step)
				streamEvent.Data = map[string]any{
					"entity": copyDataSafe(d),
					"index":  len(array_data),
				}
				c.profiler <- streamEvent
			}
		}
		exec.currentContext.Data = []interface{}{}
	}

	// Emit FOREACH_STEP_END event (reuse START event ID)
	if c.profiler != nil && stepID != "" {
		event := StepProfilerData{
			ID:        stepID, // Reuse START event ID
			ParentID:  exec.parentID,
			Type:      EVENT_FOREACH_STEP_END,
			Name:      exec.step.Name + " End",
			Step:      exec.step,
			Timestamp: time.Now(),
			Duration:  time.Since(stepStartTime).Milliseconds(),
			Data:      make(map[string]any),
		}
		c.profiler <- event
	}

	return nil
}

func applyMergeRule(c *ApiCrawler, contextData any, rule string, result any, templateCtx map[string]any) (interface{}, error) {
	// Parse the JQ expression
	code, err := c.getOrCompileJQRule(rule, "$res", "$ctx")
	if err != nil {
		return nil, fmt.Errorf("failed to get/compile merge rule: %w", err)
	}

	// Run the query against contextData, passing $res as a variable
	iter := code.Run(contextData, result, templateCtx)

	// Collect the results, expecting exactly one
	var values []interface{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if errVal, isErr := v.(error); isErr {
			return nil, fmt.Errorf("error running JQ: %w", errVal)
		}
		values = append(values, v)
	}

	// Enforce exactly one result
	if len(values) != 1 {
		return nil, fmt.Errorf("merge rule must produce exactly one result, got %d", len(values))
	}

	return values[0], nil
}

// performMerge applies the appropriate merge strategy based on step configuration
// Returns the profiler step name and error if any. The actual context is modified in place.
// Thread-safe: Uses mutex to protect concurrent access to contexts.
func (c *ApiCrawler) performMerge(op mergeOperation) (profilerStepName string, err error) {
	// Lock for thread-safe merge operations
	c.mergeMutex.Lock()
	defer c.mergeMutex.Unlock()

	// Check for noop merge (skip merging entirely)
	if op.step.NoopMerge {
		c.logger.Debug("[Merge] noop merge - skipping")
		return "", nil
	}

	// 1. Explicit merge rule (merge with ancestor context)
	if op.step.MergeOn != "" {
		c.logger.Debug("[Merge] merging-on with expression: %s", op.step.MergeOn)
		updated, err := applyMergeRule(c, op.currentContext.Data, op.step.MergeOn, op.result, op.templateContext)
		if err != nil {
			return "", fmt.Errorf("mergeOn failed: %w", err)
		}
		op.currentContext.Data = updated
		return "Merge-On", nil
	}

	// 2. Merge with parent context
	if op.step.MergeWithParentOn != "" {
		c.logger.Debug("[Merge] merging-with-parent with expression: %s", op.step.MergeWithParentOn)
		parentCtx := op.contextMap[op.currentContext.ParentContext]
		updated, err := applyMergeRule(c, parentCtx.Data, op.step.MergeWithParentOn, op.result, op.templateContext)
		if err != nil {
			return "", fmt.Errorf("mergeWithParentOn failed: %w", err)
		}
		parentCtx.Data = updated
		return "Merge-Parent", nil
	}

	// 3. Named context merge (cross-scope update)
	if op.step.MergeWithContext != nil {
		c.logger.Debug("[Merge] merging-with-context with expression: %s:%s",
			op.step.MergeWithContext.Name, op.step.MergeWithContext.Rule)

		targetCtx, ok := op.contextMap[op.step.MergeWithContext.Name]
		if !ok {
			return "", fmt.Errorf("context '%s' not found", op.step.MergeWithContext.Name)
		}
		updated, err := applyMergeRule(c, targetCtx.Data, op.step.MergeWithContext.Rule, op.result, op.templateContext)
		if err != nil {
			return "", fmt.Errorf("mergeWithContext failed: %w", err)
		}
		targetCtx.Data = updated
		return "Merge-Context", nil
	}

	// 4. Default merge (shallow merge for maps/arrays)
	c.logger.Debug("[Merge] default merge")
	switch data := op.currentContext.Data.(type) {
	case []interface{}:
		op.currentContext.Data = append(data, op.result.([]interface{})...)
	case map[string]interface{}:
		if transformedMap, ok := op.result.(map[string]interface{}); ok {
			for k, v := range transformedMap {
				data[k] = v
			}
		}
	default:
		op.currentContext.Data = op.result
	}
	return "Merge-Default", nil
}

// prepareHTTPRequest builds an HTTP request from the context and pagination parameters
func (c *ApiCrawler) prepareHTTPRequest(ctx httpRequestContext, templateCtx map[string]any, globalHeaders map[string]string) (*http.Request, *url.URL, error) {
	var urlObj *url.URL
	var err error

	// Determine base URL
	if ctx.nextPageURL != "" {
		urlObj, err = url.Parse(ctx.nextPageURL)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid nextPageURL %s: %w", ctx.nextPageURL, err)
		}
	} else {
		// Template URL expansion
		tmpl, err := c.getOrCompileTemplate(ctx.urlTemplate)
		if err != nil {
			return nil, nil, fmt.Errorf("error getting/compiling URL template: %w", err)
		}

		var urlBuf bytes.Buffer
		if err := tmpl.Execute(&urlBuf, templateCtx); err != nil {
			return nil, nil, fmt.Errorf("error executing URL template: %w", err)
		}

		urlObj, err = url.Parse(urlBuf.String())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid URL %s: %w", urlBuf.String(), err)
		}
	}

	// Add query parameters
	query := urlObj.Query()
	for k, v := range ctx.queryParams {
		query.Set(k, v)
	}
	urlObj.RawQuery = query.Encode()

	// Merge configured body with pagination body params
	mergedBody := make(map[string]any)
	for k, v := range ctx.configuredBody {
		mergedBody[k] = v
	}
	for k, v := range ctx.bodyParams {
		mergedBody[k] = v
	}

	// Determine content type
	contentType := ctx.contentType

	// Prepare request body based on content type
	var reqBody io.Reader
	if len(mergedBody) > 0 {
		switch contentType {
		case "application/json":
			bodyJSON, err := json.Marshal(mergedBody)
			if err != nil {
				return nil, nil, fmt.Errorf("error encoding JSON body: %w", err)
			}
			reqBody = bytes.NewReader(bodyJSON)

		case "application/x-www-form-urlencoded":
			formData := url.Values{}
			for k, v := range mergedBody {
				formData.Set(k, fmt.Sprintf("%v", v))
			}
			reqBody = bytes.NewReader([]byte(formData.Encode()))

		default:
			return nil, nil, fmt.Errorf("unsupported content type: %s", contentType)
		}
	}

	// Create HTTP request
	req, err := http.NewRequest(ctx.method, urlObj.String(), reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Apply headers (priority: global < request-specific < pagination)
	for k, v := range globalHeaders {
		req.Header.Set(k, v)
	}
	for k, v := range ctx.headers {
		req.Header.Set(k, v)
	}

	// Set Content-Type header if body is present
	if len(mergedBody) > 0 && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Apply authentication
	ctx.authenticator.PrepareRequest(req, ctx.requestID)

	return req, urlObj, nil
}

// transformResult applies the jq transformer to the raw response
func (c *ApiCrawler) transformResult(raw any, transformer string, templateCtx map[string]any) (any, error) {
	if transformer == "" {
		return raw, nil
	}

	c.logger.Debug("[Transform] transforming with expression: %s", transformer)

	code, err := c.getOrCompileJQRule(transformer, "$ctx")
	if err != nil {
		return nil, fmt.Errorf("failed to get/compile transform rule: %w", err)
	}

	iter := code.Run(raw, templateCtx)
	var singleResult interface{}
	count := 0

	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			return nil, fmt.Errorf("jq error: %w", err)
		}

		count++
		if count > 1 {
			return nil, fmt.Errorf("resultTransformer yielded more than one value")
		}

		singleResult = v
	}
	return singleResult, nil
}

func childMapWith(base map[string]*Context, currentCotnext *Context, key string, value interface{}) map[string]*Context {
	newMap := make(map[string]*Context, len(base)+1)
	for k, v := range base {
		newMap[k] = v
	}
	newMap[key] = &Context{
		Data:          value,
		ParentContext: currentCotnext.key,
		key:           key,
		depth:         currentCotnext.depth + 1,
	}
	return newMap
}

func contextMapToTemplate(base map[string]*Context) map[string]interface{} {
	result := make(map[string]interface{})
	// root special case
	if rootMap, ok := base["root"].Data.(map[string]interface{}); ok {
		for k, v := range rootMap {
			result[k] = v
		}
	}

	for k, c := range base {
		if k == "root" {
			continue
		}
		result[k] = c.Data
	}
	return result
}

// getTypeDescription returns a human-readable type description for profiler events
func getTypeDescription(v interface{}) string {
	if v == nil {
		return "null"
	}

	switch val := v.(type) {
	case []interface{}:
		return fmt.Sprintf("array[%d]", len(val))
	case map[string]interface{}:
		return fmt.Sprintf("object{%d}", len(val))
	case string:
		return "string"
	case float64, int, int64:
		return "number"
	case bool:
		return "boolean"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// copyDataSafe creates a safe copy of data for profiler events (non-JSON approach)
func copyDataSafe(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case []interface{}:
		copy := make([]interface{}, len(val))
		for i, item := range val {
			copy[i] = copyDataSafe(item)
		}
		return copy
	case map[string]any:
		copy := make(map[string]any, len(val))
		for k, item := range val {
			copy[k] = copyDataSafe(item)
		}
		return copy
	default:
		// Primitives and other types are safe to share
		return v
	}
}

// copyContextSafe creates a safe copy of Context for profiler events
func copyContextSafe(ctx Context) Context {
	return Context{
		Data:          copyDataSafe(ctx.Data),
		ParentContext: ctx.ParentContext,
		key:           ctx.key,
		depth:         ctx.depth,
	}
}
