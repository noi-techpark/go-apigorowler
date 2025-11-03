// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package apigorowler

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"
)

type StepProfileType int

const (
	STEP_PROFILER_TYPE_START      StepProfileType = 0
	STEP_PROFILER_TYPE_NONE       StepProfileType = 1
	STEP_PROFILER_TYPE_END        StepProfileType = 2
	STEP_PROFILER_TYPE_END_SILENT StepProfileType = 3
)

type StepProfilerData struct {
	Type       StepProfileType
	Name       string
	Config     Step
	Data       any
	DataBefore any
	DataString string
	Context    Context
	Extra      map[string]any
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
}

type RequestConfig struct {
	URL            string               `yaml:"url" json:"url"`
	Method         string               `yaml:"method" json:"method"`
	Headers        map[string]string    `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body           string               `yaml:"body,omitempty" json:"body,omitempty"`
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
	urlTemplate   string
	method        string
	headers       map[string]string
	bodyParams    map[string]interface{}
	queryParams   map[string]string
	nextPageURL   string
	authenticator Authenticator
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
		httpClient:    http.DefaultClient,
		Config:        cfg,
		ContextMap:    map[string]*Context{},
		logger:        NewDefaultLogger(),
		profiler:      nil,
		templateCache: make(map[string]*template.Template),
		jqCache:       make(map[string]*gojq.Code),
	}

	// handle stream channel
	if cfg.Stream {
		c.DataStream = make(chan any)
	}

	// instantiate global authenticator
	if cfg.Authentication != nil {
		c.globalAuthenticator = NewAuthenticator(*cfg.Authentication)
	} else {
		c.globalAuthenticator = NoopAuthenticator{}
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
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	dec := gob.NewDecoder(&buf)

	if err := enc.Encode(src); err != nil {
		var zero T
		return zero, err
	}

	var dst T
	if err := dec.Decode(&dst); err != nil {
		return dst, err
	}

	return dst, nil
}

func (a *ApiCrawler) pushProfilerData(dataType StepProfileType, name string, exec *stepExecution, data any, dataBefore any, extra ...any) {
	if a.profiler == nil {
		return
	}

	cleanConfig := Step{}
	context := Context{}
	if exec != nil {
		// Defensive copy of step, with Steps cleared
		cleanConfig, _ = deepCopy(exec.step)
		cleanConfig.Steps = make([]Step, 0)

		context = *exec.currentContext
	}

	// Convert variadic args into map[string]any
	extraMap := make(map[string]any)
	for i := 0; i+1 < len(extra); i += 2 {
		key, ok := extra[i].(string)
		if !ok {
			continue // skip invalid key
		}
		extraMap[key] = extra[i+1]
	}

	d := StepProfilerData{
		Type:       dataType,
		Name:       name,
		Context:    context,
		Data:       data,
		DataBefore: dataBefore,
		Config:     cleanConfig,
		Extra:      extraMap,
	}

	a.profiler <- d
}

func newStepExecution(step Step, currentContextKey string, contextMap map[string]*Context) *stepExecution {
	return &stepExecution{
		step:              step,
		currentContextKey: currentContextKey,
		contextMap:        contextMap,
		currentContext:    contextMap[currentContextKey],
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

	for _, step := range c.Config.Steps {
		ecxec := newStepExecution(step, currentContext, c.ContextMap)
		if err := c.ExecuteStep(ctx, ecxec); err != nil {
			return err
		}
	}

	c.pushProfilerData(STEP_PROFILER_TYPE_NONE, "Result", nil, c.GetData(), c.Config.RootContext)
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

	templateCtx := contextMapToTemplate(exec.contextMap)

	// Determine authenticator (request-specific overrides global)
	authenticator := c.globalAuthenticator
	if exec.step.Request.Authentication != nil {
		authenticator = NewAuthenticator(*exec.step.Request.Authentication)
	}

	// Initialize paginator
	paginator, err := NewPaginator(ConfigP{exec.step.Request.Pagination})
	if err != nil {
		return fmt.Errorf("error creating request paginator: %w", err)
	}

	stop := false
	next := paginator.NextFromCtx()

	// Pagination loop
	for !stop {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Prepare HTTP request
		reqCtx := httpRequestContext{
			urlTemplate:   exec.step.Request.URL,
			method:        exec.step.Request.Method,
			headers:       exec.step.Request.Headers,
			bodyParams:    next.BodyParams,
			queryParams:   next.QueryParams,
			nextPageURL:   next.NextPageUrl,
			authenticator: authenticator,
		}

		req, urlObj, err := c.prepareHTTPRequest(reqCtx, templateCtx, c.Config.Headers)
		if err != nil {
			return err
		}

		c.logger.Info("[Request] %s", urlObj.String())
		c.logger.Debug("[Request] Got response: status pending")

		// Execute HTTP request
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("error performing HTTP request: %w", err)
		}
		defer resp.Body.Close()

		// Update pagination state
		next, stop, err = paginator.Next(resp)
		if err != nil {
			return fmt.Errorf("paginator update error: %w", err)
		}

		// Decode JSON response
		var raw interface{}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return fmt.Errorf("error decoding JSON: %w", err)
		}

		profileStepName := fmt.Sprintf("Request '%s' | page#%d", exec.step.Name, paginator.PageNum())
		c.pushProfilerData(STEP_PROFILER_TYPE_START, profileStepName, exec, raw, nil, "url", urlObj.String())

		// Transform response
		transformed, err := c.transformResult(raw, exec.step.ResultTransformer, templateCtx)
		if err != nil {
			return err
		}

		c.pushProfilerData(STEP_PROFILER_TYPE_NONE, "Response Transformation", exec, transformed, raw, "url", urlObj.String())

		// Execute nested steps on transformed result
		thisContextKey := exec.currentContextKey
		if exec.step.As != "" {
			thisContextKey = exec.step.As
		}

		childContextMap := childMapWith(exec.contextMap, exec.currentContext, thisContextKey, transformed)

		for _, step := range exec.step.Steps {
			newExec := newStepExecution(step, thisContextKey, childContextMap)
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
			return err
		}

		// Profile merge if it happened
		if mergeStepName != "" {
			c.pushProfilerData(STEP_PROFILER_TYPE_NONE, "Response "+mergeStepName, exec, exec.currentContext.Data, dataBefore, "url", urlObj.String())
		}

		c.pushProfilerData(STEP_PROFILER_TYPE_END_SILENT, "", nil, nil, nil)

		// Handle streaming at root level
		if exec.currentContext.depth == 0 && c.Config.Stream {
			array_data := exec.currentContext.Data.([]interface{})
			for i, d := range array_data {
				c.DataStream <- d
				c.pushProfilerData(STEP_PROFILER_TYPE_NONE, fmt.Sprintf("Stream result #%d", i), exec, d, nil, "url", urlObj.String())
			}
			exec.currentContext.Data = []interface{}{}
		}
	}

	return nil
}

func (c *ApiCrawler) handleForEach(ctx context.Context, exec *stepExecution) error {
	c.logger.Info("[Foreach] Preparing %s", exec.step.Name)

	results := []interface{}{}

	// Extract items to iterate over
	if len(exec.step.Path) != 0 && exec.step.Values == nil {
		c.logger.Debug("[Foreach] Extracting from parent context with rule: %s", exec.step.Path)

		code, err := c.getOrCompileJQRule(exec.step.Path)
		if err != nil {
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

	profileStepName := fmt.Sprintf("Foreach Extract '%s'", exec.step.Name)
	c.pushProfilerData(STEP_PROFILER_TYPE_START, profileStepName, exec, results, nil)

	// Execute nested steps for each item
	executionResults := make([]interface{}, 0)
	for i, item := range results {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c.logger.Info("[ForEach] Iteration %d as '%s'", i, exec.step.As, "item", item)

		childContextMap := childMapWith(exec.contextMap, exec.currentContext, exec.step.As, item)

		c.pushProfilerData(STEP_PROFILER_TYPE_NONE, fmt.Sprintf("Selection #%d", i), exec, item, nil)

		for _, nested := range exec.step.Steps {
			newExec := newStepExecution(nested, exec.step.As, childContextMap)
			if err := c.ExecuteStep(ctx, newExec); err != nil {
				return err
			}
		}

		c.pushProfilerData(STEP_PROFILER_TYPE_NONE, fmt.Sprintf("Result #%d", i), exec, childContextMap[exec.step.As].Data, nil)
		executionResults = append(executionResults, childContextMap[exec.step.As].Data)
	}

	// Determine merge strategy
	templateCtx := contextMapToTemplate(exec.contextMap)
	dataBefore := exec.currentContext.Data

	// Check if custom merge rules are specified
	hasCustomMerge := exec.step.MergeOn != "" || exec.step.MergeWithParentOn != "" || exec.step.MergeWithContext != nil

	if hasCustomMerge {
		// Use custom merge logic (same as request steps)
		mergeOp := mergeOperation{
			step:            exec.step,
			currentContext:  exec.currentContext,
			contextMap:      exec.contextMap,
			result:          executionResults,
			templateContext: templateCtx,
		}

		mergeStepName, err := c.performMerge(mergeOp)
		if err != nil {
			return err
		}

		profileStepName = fmt.Sprintf("Foreach Merge '%s'", exec.step.Name)
		if mergeStepName == "" {
			// noop merge - don't update profiler
			c.pushProfilerData(STEP_PROFILER_TYPE_END, profileStepName+" (noop)", exec, nil, dataBefore)
		} else {
			c.pushProfilerData(STEP_PROFILER_TYPE_END, profileStepName, exec, exec.currentContext.Data, dataBefore)
		}
	} else {
		// Default: patch the array at exec.step.Path with new results
		code, err := c.getOrCompileJQRule(exec.step.Path+" = $new", "$new")
		if err != nil {
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

		profileStepName = fmt.Sprintf("Foreach Merge '%s'", exec.step.Name)
		c.pushProfilerData(STEP_PROFILER_TYPE_END, profileStepName, exec, v, dataBefore)

		exec.currentContext.Data = v
	}

	// Handle streaming at root level
	if exec.currentContext.depth <= 1 && c.Config.Stream {
		array_data := exec.currentContext.Data.([]interface{})
		for i, d := range array_data {
			c.DataStream <- d
			c.pushProfilerData(STEP_PROFILER_TYPE_NONE, fmt.Sprintf("Stream result #%d", i), exec, d, nil)
		}
		exec.currentContext.Data = []interface{}{}
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
func (c *ApiCrawler) performMerge(op mergeOperation) (profilerStepName string, err error) {
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

	// Prepare request body
	var reqBody io.Reader
	if len(ctx.bodyParams) > 0 {
		bodyJSON, err := json.Marshal(ctx.bodyParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error encoding body params: %w", err)
		}
		reqBody = bytes.NewReader(bodyJSON)
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

	// Apply authentication
	ctx.authenticator.PrepareRequest(req)

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
