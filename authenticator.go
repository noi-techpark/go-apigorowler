// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package apigorowler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/itchyny/gojq"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// getContentType retrieves the Content-Type from headers (case-insensitive)
func getContentType(headers map[string]string) string {
	if headers == nil {
		return ""
	}
	// Check for exact match first
	if ct, ok := headers["Content-Type"]; ok {
		return ct
	}
	// Case-insensitive search
	for key, value := range headers {
		if strings.ToLower(key) == "content-type" {
			return value
		}
	}
	return ""
}

type Authenticator interface {
	PrepareRequest(req *http.Request, requestID string) error
	SetProfiler(profiler chan StepProfilerData)
}

// AuthProfiler is a helper for emitting authentication profiling events
type AuthProfiler struct {
	profiler chan StepProfilerData
	authType string
}

func (ap *AuthProfiler) emit(eventType ProfileEventType, name, requestID string, data map[string]any) string {
	if ap.profiler == nil {
		return ""
	}

	event := StepProfilerData{
		ID:        uuid.New().String(),
		ParentID:  requestID,
		Type:      eventType,
		Name:      name,
		Step:      Step{},
		Timestamp: time.Now(),
		Data:      data,
	}

	if event.Data == nil {
		event.Data = make(map[string]any)
	}
	event.Data["authType"] = ap.authType

	ap.profiler <- event
	return event.ID
}

func (ap *AuthProfiler) emitEnd(eventType ProfileEventType, name, parent string, duration int64, data map[string]any) string {
	if ap.profiler == nil {
		return ""
	}

	event := StepProfilerData{
		ID:        uuid.New().String(),
		ParentID:  parent,
		Type:      eventType,
		Name:      name,
		Step:      Step{},
		Timestamp: time.Now(),
		Data:      data,
		Duration:  duration,
	}

	if event.Data == nil {
		event.Data = make(map[string]any)
	}
	event.Data["authType"] = ap.authType

	ap.profiler <- event
	return event.ID
}

type BaseAuthenticator struct {
	profiler *AuthProfiler
}

func (a *BaseAuthenticator) SetProfiler(profiler chan StepProfilerData) {
	a.profiler.profiler = profiler
}

func (a *BaseAuthenticator) GetProfiler() *AuthProfiler {
	return a.profiler
}

// NoopAuthenticator - no authentication
type NoopAuthenticator struct {
	*BaseAuthenticator
}

func (np NoopAuthenticator) PrepareRequest(req *http.Request, requestID string) error {
	return nil
}

type AuthenticatorConfig struct {
	Type string `yaml:"type,omitempty" json:"type,omitempty"` // basic | bearer | oauth | cookie | jwt | custom

	// Basic auth
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`

	// Bearer auth
	Token string `yaml:"token,omitempty" json:"token,omitempty"`

	// OAuth (inlined for backward compatibility)
	OAuthConfig `yaml:"oauth,omitempty" json:"oauth,omitempty"`

	// Cookie/JWT/Custom auth
	LoginRequest    *RequestConfig `yaml:"loginRequest,omitempty" json:"loginRequest,omitempty"`
	ExtractFrom     string         `yaml:"extractFrom,omitempty" json:"extractFrom,omitempty"`         // cookie | header | body
	ExtractSelector string         `yaml:"extractSelector,omitempty" json:"extractSelector,omitempty"` // jq for body, name for cookie/header
	InjectInto      string         `yaml:"injectInto,omitempty" json:"injectInto,omitempty"`           // cookie | header | bearer | body | query
	InjectKey       string         `yaml:"injectKey,omitempty" json:"injectKey,omitempty"`             // name for cookie/header/query/body field

	// Refresh settings
	MaxAgeSeconds int  `yaml:"maxAgeSeconds,omitempty" json:"maxAgeSeconds,omitempty"` // 0 = no refresh
	OnePerRun     bool `yaml:"onePerRun,omitempty" json:"onePerRun,omitempty"`
}

// BasicAuthenticator - HTTP Basic Authentication
type BasicAuthenticator struct {
	*BaseAuthenticator
	username string
	password string
}

func (a *BasicAuthenticator) PrepareRequest(req *http.Request, requestID string) error {
	a.profiler.emit(EVENT_AUTH_START, "Basic Auth", requestID, map[string]any{
		"username": a.username,
	})

	req.SetBasicAuth(a.username, a.password)

	a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "Basic Auth Injected", requestID, map[string]any{
		"location": "Authorization header",
		"format":   "Basic",
	})

	a.profiler.emit(EVENT_AUTH_END, "Basic Auth Complete", requestID, nil)
	return nil
}

// BearerAuthenticator - Bearer token authentication
type BearerAuthenticator struct {
	*BaseAuthenticator
	token string
}

func (a *BearerAuthenticator) PrepareRequest(req *http.Request, requestID string) error {
	a.profiler.emit(EVENT_AUTH_START, "Bearer Auth", requestID, nil)

	a.profiler.emit(EVENT_AUTH_CACHED, "Using Provided Token", requestID, map[string]any{
		"token": maskToken(a.token),
	})

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))

	a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "Bearer Token Injected", requestID, map[string]any{
		"location": "Authorization header",
		"format":   "Bearer",
		"token":    maskToken(a.token),
	})

	a.profiler.emit(EVENT_AUTH_END, "Bearer Auth Complete", requestID, nil)
	return nil
}

func (a *BearerAuthenticator) SetProfiler(profiler chan StepProfilerData) {
	a.profiler.profiler = profiler
}

// maskToken masks a token for display, showing only first and last 4 characters
func maskToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

type OAuthConfig struct {
	Method       string `yaml:"method,omitempty" json:"method,omitempty"` // password | client_credentials
	TokenURL     string `yaml:"tokenUrl,omitempty" json:"tokenUrl,omitempty"`
	ClientID     string `yaml:"clientId,omitempty" json:"clientId,omitempty"`
	ClientSecret string `yaml:"clientSecret,omitempty" json:"clientSecret,omitempty"`
	// usernam and password inherited from AuthenticatorConfig
	Scopes []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

// OAuthAuthenticator - OAuth2 authentication
type OAuthAuthenticator struct {
	*BaseAuthenticator
	provider *OAuthProvider
	profiler *AuthProfiler
}

func (a *OAuthAuthenticator) PrepareRequest(req *http.Request, requestID string) error {
	a.profiler.emit(EVENT_AUTH_START, "OAuth2 Auth", requestID, nil)

	token, fromCache, err := a.provider.GetTokenWithCache()
	if err != nil {
		a.profiler.emit(EVENT_AUTH_END, "OAuth2 Auth Failed", requestID, map[string]any{
			"error": err.Error(),
		})
		return fmt.Errorf("could not get oauth token: %w", err)
	}

	if fromCache {
		a.profiler.emit(EVENT_AUTH_CACHED, "Using Cached OAuth Token", requestID, map[string]any{
			"token":  maskToken(token),
			"source": "cached",
		})
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "OAuth Token Injected", requestID, map[string]any{
		"location": "Authorization header",
		"format":   "Bearer",
		"token":    maskToken(token),
	})

	a.profiler.emit(EVENT_AUTH_END, "OAuth2 Auth Complete", requestID, nil)
	return nil
}

func (a *OAuthAuthenticator) SetProfiler(profiler chan StepProfilerData) {
	a.profiler.profiler = profiler
	a.provider.profiler = profiler
}

// OAuthProvider struct
type OAuthProvider struct {
	conf        *oauth2.Config
	clientCreds *clientcredentials.Config
	token       *oauth2.Token
	mu          sync.Mutex
	username    string
	password    string
	profiler    chan StepProfilerData
	method      string // password or client_credentials
}

func NewOAuthProvider(cfg OAuthConfig, username, password string) *OAuthProvider {
	authMethod := cfg.Method
	tokenURL := cfg.TokenURL
	clientID := cfg.ClientID
	clientSecret := cfg.ClientSecret

	wrapper := &OAuthProvider{
		username: username,
		password: password,
		method:   authMethod,
	}

	switch authMethod {
	case "password":
		wrapper.conf = &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint: oauth2.Endpoint{
				TokenURL: tokenURL,
			},
			Scopes: cfg.Scopes,
		}
	case "client_credentials":
		wrapper.clientCreds = &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
			Scopes:       cfg.Scopes,
		}
	default:
		slog.Error("Unsupported OAUTH_METHOD. Use 'password' or 'client_credentials'")
		panic("Unsupported OAUTH_METHOD. Use 'password' or 'client_credentials'")
	}

	return wrapper
}

// GetToken retrieves a valid access token (refreshing if necessary)
func (w *OAuthProvider) GetToken() (string, error) {
	token, _, err := w.GetTokenWithCache()
	return token, err
}

// GetTokenWithCache retrieves a valid access token and returns whether it was cached
func (w *OAuthProvider) GetTokenWithCache() (string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	ctx := context.Background()

	// If token exists and is still valid, return it as cached
	if w.token != nil && w.token.Valid() {
		return w.token.AccessToken, true, nil
	}

	// Need to fetch new token - emit login start event
	loginID := ""
	if w.profiler != nil {
		event := StepProfilerData{
			ID:        uuid.New().String(),
			Type:      EVENT_AUTH_LOGIN_START,
			Name:      "OAuth2 Login Request",
			Timestamp: time.Now(),
			Data: map[string]any{
				"authType": "oauth",
				"method":   w.method,
			},
		}
		if w.method == "password" {
			event.Data["username"] = w.username
		} else if w.method == "client_credentials" {
			if w.clientCreds != nil {
				event.Data["clientId"] = w.clientCreds.ClientID
			}
		}
		w.profiler <- event
		loginID = event.ID
	}

	// Fetch new token
	var token *oauth2.Token
	var err error

	loginStart := time.Now()
	if w.conf != nil { // Password flow
		token, err = w.conf.PasswordCredentialsToken(ctx, w.username, w.password)
	} else { // Client Credentials flow
		token, err = w.clientCreds.Token(ctx)
	}
	loginDuration := time.Since(loginStart)

	// Emit login end event
	if w.profiler != nil {
		event := StepProfilerData{
			ID:        uuid.New().String(),
			ParentID:  loginID,
			Type:      EVENT_AUTH_LOGIN_END,
			Name:      "OAuth2 Login Complete",
			Timestamp: time.Now(),
			Duration:  loginDuration.Milliseconds(),
			Data: map[string]any{
				"authType": "oauth",
				"method":   w.method,
			},
		}
		if err != nil {
			event.Data["error"] = err.Error()
		} else {
			event.Data["token"] = maskToken(token.AccessToken)
			if !token.Expiry.IsZero() {
				event.Data["expiresAt"] = token.Expiry.Format(time.RFC3339)
			}
		}
		w.profiler <- event
	}

	if err != nil {
		return "", false, err
	}

	// Store new token
	w.token = token
	return token.AccessToken, false, nil
}

// CookieAuthenticator - performs login via POST, extracts cookie, injects it
type CookieAuthenticator struct {
	*BaseAuthenticator
	loginRequest  *RequestConfig
	cookieName    string
	cookie        *http.Cookie
	maxAge        time.Duration
	acquiredAt    time.Time
	onePerRun     bool
	authenticated bool
	mu            sync.Mutex
	httpClient    HTTPClient
}

func (a *CookieAuthenticator) PrepareRequest(req *http.Request, requestID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	authID := a.profiler.emit(EVENT_AUTH_START, "Cookie Auth", requestID, nil)

	// Check if we need to authenticate
	needsAuth := false
	if !a.authenticated || (a.cookie == nil && !a.onePerRun) {
		needsAuth = true
	} else if a.maxAge > 0 && time.Since(a.acquiredAt) > a.maxAge {
		needsAuth = true
	}

	if needsAuth {
		if err := a.performLogin(authID); err != nil {
			a.profiler.emit(EVENT_AUTH_END, "Cookie Auth Failed", authID, map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("cookie authentication failed: %w", err)
		}
		a.authenticated = true
	} else {
		a.profiler.emit(EVENT_AUTH_CACHED, "Using Cached Cookie", authID, map[string]any{
			"cookieName": a.cookieName,
			"age":        time.Since(a.acquiredAt).String(),
		})
	}

	// Inject cookie
	if a.cookie != nil {
		req.AddCookie(a.cookie)
		a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "Cookie Injected", authID, map[string]any{
			"location":    "Cookie header",
			"cookieName":  a.cookieName,
			"cookieValue": maskToken(a.cookie.Value),
		})
	}

	a.profiler.emit(EVENT_AUTH_END, "Cookie Auth Complete", authID, nil)
	return nil
}

func (a *CookieAuthenticator) performLogin(requestID string) error {
	loginID := a.profiler.emit(EVENT_AUTH_LOGIN_START, "Cookie Login Request", requestID, map[string]any{
		"url":    a.loginRequest.URL,
		"method": a.loginRequest.Method,
	})

	loginStart := time.Now()

	// Build login request
	loginReq, err := a.buildLoginRequest()
	if err != nil {
		a.profiler.emit(EVENT_AUTH_LOGIN_END, "Cookie Login Failed", loginID, map[string]any{
			"error": err.Error(),
		})
		return err
	}

	// Execute login request
	resp, err := a.httpClient.Do(loginReq)
	if err != nil {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Cookie Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType": "cookie",
			"error":    err.Error(),
		})
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Cookie Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType":   "cookie",
			"error":      fmt.Sprintf("login request failed with status %d", resp.StatusCode),
			"statusCode": resp.StatusCode,
		})
		return fmt.Errorf("login request failed with status %d", resp.StatusCode)
	}

	// Extract cookie
	cookies := resp.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == a.cookieName {
			a.cookie = cookie
			a.acquiredAt = time.Now()

			loginDuration := time.Since(loginStart)
			a.profiler.emit(EVENT_AUTH_TOKEN_EXTRACT, "Cookie Extracted", loginID, map[string]any{
				"cookieName":  a.cookieName,
				"cookieValue": maskToken(cookie.Value),
			})

			a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Cookie Login Complete", loginID, loginDuration.Milliseconds(), map[string]any{
				"authType":    "cookie",
				"cookieName":  a.cookieName,
				"cookieValue": maskToken(cookie.Value),
				"statusCode":  resp.StatusCode,
			})
			return nil
		}
	}

	loginDuration := time.Since(loginStart)
	a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Cookie Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
		"authType": "cookie",
		"error":    fmt.Sprintf("cookie '%s' not found in login response", a.cookieName),
	})
	return fmt.Errorf("cookie '%s' not found in login response", a.cookieName)
}

func (a *CookieAuthenticator) buildLoginRequest() (*http.Request, error) {
	// Build URL
	urlStr := a.loginRequest.URL
	urlObj, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid login URL: %w", err)
	}

	// Build body
	var bodyReader *bytes.Reader
	if len(a.loginRequest.Body) > 0 {
		contentType := getContentType(a.loginRequest.Headers)
		switch contentType {

		case "application/json":
			bodyJSON, err := json.Marshal(a.loginRequest.Body)
			if err != nil {
				return nil, fmt.Errorf("error encoding JSON body: %w", err)
			}
			bodyReader = bytes.NewReader(bodyJSON)

		case "application/x-www-form-urlencoded":
			formData := url.Values{}
			for k, v := range a.loginRequest.Body {
				formData.Set(k, fmt.Sprintf("%v", v))
			}
			bodyReader = bytes.NewReader([]byte(formData.Encode()))

		default:
			return nil, fmt.Errorf("unsupported content type: %s", contentType)
		}
	}

	// Create request
	var req *http.Request
	if bodyReader != nil {
		req, err = http.NewRequest(a.loginRequest.Method, urlObj.String(), bodyReader)
	} else {
		req, err = http.NewRequest(a.loginRequest.Method, urlObj.String(), nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}

	// Set headers
	for k, v := range a.loginRequest.Headers {
		req.Header.Set(k, v)
	}
	if bodyReader != nil {
		contentType := getContentType(a.loginRequest.Headers)
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

// JWTAuthenticator - performs login via POST, extracts JWT from response
type JWTAuthenticator struct {
	*BaseAuthenticator
	loginRequest    *RequestConfig
	extractFrom     string // header | body
	extractSelector string // jq expression for body, header name for header
	token           string
	maxAge          time.Duration
	acquiredAt      time.Time
	onePerRun       bool
	authenticated   bool
	mu              sync.Mutex
	httpClient      HTTPClient
	jqCache         map[string]*gojq.Code
}

func (a *JWTAuthenticator) PrepareRequest(req *http.Request, requestID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.profiler.emit(EVENT_AUTH_START, "JWT Auth", requestID, nil)

	// Check if we need to authenticate
	needsAuth := false
	if !a.authenticated || (a.token == "" && !a.onePerRun) {
		needsAuth = true
	} else if a.maxAge > 0 && time.Since(a.acquiredAt) > a.maxAge {
		needsAuth = true
	}

	if needsAuth {
		if err := a.performLogin(requestID); err != nil {
			a.profiler.emit(EVENT_AUTH_END, "JWT Auth Failed", requestID, map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("jwt authentication failed: %w", err)
		}
		a.authenticated = true
	} else {
		a.profiler.emit(EVENT_AUTH_CACHED, "Using Cached JWT Token", requestID, map[string]any{
			"token": maskToken(a.token),
			"age":   time.Since(a.acquiredAt).String(),
		})
	}

	// Inject token as Bearer
	if a.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))
		a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "JWT Token Injected", requestID, map[string]any{
			"location": "Authorization header",
			"format":   "Bearer",
			"token":    maskToken(a.token),
		})
	}

	a.profiler.emit(EVENT_AUTH_END, "JWT Auth Complete", requestID, nil)
	return nil
}

func (a *JWTAuthenticator) performLogin(requestID string) error {
	loginID := a.profiler.emit(EVENT_AUTH_LOGIN_START, "JWT Login Request", requestID, map[string]any{
		"url":    a.loginRequest.URL,
		"method": a.loginRequest.Method,
	})

	loginStart := time.Now()

	// Build login request
	loginReq, err := a.buildLoginRequest()
	if err != nil {
		a.profiler.emit(EVENT_AUTH_LOGIN_END, "JWT Login Failed", requestID, map[string]any{
			"error": err.Error(),
		})
		return err
	}

	// Execute login request
	resp, err := a.httpClient.Do(loginReq)
	if err != nil {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "JWT Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType": "jwt",
			"error":    err.Error(),
		})
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "JWT Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType":   "jwt",
			"error":      fmt.Sprintf("login request failed with status %d", resp.StatusCode),
			"statusCode": resp.StatusCode,
		})
		return fmt.Errorf("login request failed with status %d", resp.StatusCode)
	}

	// Extract token
	token, err := a.extractToken(resp)
	if err != nil {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "JWT Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType": "jwt",
			"error":    err.Error(),
		})
		return err
	}

	a.token = token
	a.acquiredAt = time.Now()

	loginDuration := time.Since(loginStart)
	a.profiler.emit(EVENT_AUTH_TOKEN_EXTRACT, "JWT Token Extracted", requestID, map[string]any{
		"extractFrom":     a.extractFrom,
		"extractSelector": a.extractSelector,
		"token":           maskToken(token),
	})

	a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "JWT Login Complete", loginID, loginDuration.Milliseconds(), map[string]any{
		"authType":    "jwt",
		"token":       maskToken(token),
		"statusCode":  resp.StatusCode,
		"extractFrom": a.extractFrom,
	})
	return nil
}

func (a *JWTAuthenticator) extractToken(resp *http.Response) (string, error) {
	switch a.extractFrom {
	case "header":
		token := resp.Header.Get(a.extractSelector)
		if token == "" {
			return "", fmt.Errorf("header '%s' not found in login response", a.extractSelector)
		}
		return token, nil

	case "body":
		var body interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return "", fmt.Errorf("failed to decode login response: %w", err)
		}

		// Compile or get cached jq expression
		code, err := a.getOrCompileJQ(a.extractSelector)
		if err != nil {
			return "", err
		}

		// Execute jq expression
		iter := code.Run(body)
		v, ok := iter.Next()
		if !ok {
			return "", fmt.Errorf("jq selector '%s' yielded no results", a.extractSelector)
		}
		if err, isErr := v.(error); isErr {
			return "", fmt.Errorf("jq error: %w", err)
		}

		// Convert to string
		token, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("token extracted is not a string: %T", v)
		}
		return token, nil

	default:
		return "", fmt.Errorf("unsupported extractFrom: %s", a.extractFrom)
	}
}

func (a *JWTAuthenticator) getOrCompileJQ(expression string) (*gojq.Code, error) {
	if code, ok := a.jqCache[expression]; ok {
		return code, nil
	}

	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid jq expression '%s': %w", expression, err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("failed to compile jq expression: %w", err)
	}

	a.jqCache[expression] = code
	return code, nil
}

func (a *JWTAuthenticator) buildLoginRequest() (*http.Request, error) {
	// Build URL
	urlStr := a.loginRequest.URL
	urlObj, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid login URL: %w", err)
	}

	// Build body
	var bodyReader *bytes.Reader
	if len(a.loginRequest.Body) > 0 {
		bodyJSON, err := json.Marshal(a.loginRequest.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal login body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyJSON)
	}

	// Create request
	var req *http.Request
	if bodyReader != nil {
		req, err = http.NewRequest(a.loginRequest.Method, urlObj.String(), bodyReader)
	} else {
		req, err = http.NewRequest(a.loginRequest.Method, urlObj.String(), nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}

	// Set headers
	for k, v := range a.loginRequest.Headers {
		req.Header.Set(k, v)
	}
	if bodyReader != nil {
		contentType := getContentType(a.loginRequest.Headers)
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

// CustomAuthenticator - fully configurable authenticator
type CustomAuthenticator struct {
	*BaseAuthenticator
	loginRequest    *RequestConfig
	extractFrom     string // cookie | header | body
	extractSelector string
	injectInto      string // cookie | header | bearer | body | query
	injectKey       string
	token           string
	cookie          *http.Cookie
	maxAge          time.Duration
	acquiredAt      time.Time
	onePerRun       bool
	authenticated   bool
	mu              sync.Mutex
	httpClient      HTTPClient
	jqCache         map[string]*gojq.Code
}

func (a *CustomAuthenticator) PrepareRequest(req *http.Request, requestID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.profiler.emit(EVENT_AUTH_START, "Custom Auth", requestID, map[string]any{
		"injectInto":  a.injectInto,
		"extractFrom": a.extractFrom,
	})

	// Check if we need to authenticate
	needsAuth := false
	if !a.authenticated {
		needsAuth = true
	} else if !a.onePerRun && a.maxAge > 0 && time.Since(a.acquiredAt) > a.maxAge {
		needsAuth = true
	}

	if needsAuth {
		if err := a.performLogin(requestID); err != nil {
			a.profiler.emit(EVENT_AUTH_END, "Custom Auth Failed", requestID, map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("custom authentication failed: %w", err)
		}
		a.authenticated = true
	} else {
		cacheData := map[string]any{
			"age": time.Since(a.acquiredAt).String(),
		}
		if a.token != "" {
			cacheData["token"] = maskToken(a.token)
		}
		if a.cookie != nil {
			cacheData["cookieName"] = a.cookie.Name
			cacheData["cookieValue"] = maskToken(a.cookie.Value)
		}
		a.profiler.emit(EVENT_AUTH_CACHED, "Using Cached Credential", requestID, cacheData)
	}

	// Inject token/cookie based on injectInto
	switch a.injectInto {
	case "cookie":
		if a.cookie != nil {
			req.AddCookie(a.cookie)
			a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "Credential Injected", requestID, map[string]any{
				"location":    "Cookie header",
				"cookieName":  a.cookie.Name,
				"cookieValue": maskToken(a.cookie.Value),
			})
		}
	case "header":
		if a.token != "" {
			req.Header.Set(a.injectKey, a.token)
			a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "Credential Injected", requestID, map[string]any{
				"location":  "Header",
				"headerKey": a.injectKey,
				"token":     maskToken(a.token),
			})
		}
	case "bearer":
		if a.token != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))
			a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "Credential Injected", requestID, map[string]any{
				"location": "Authorization header",
				"format":   "Bearer",
				"token":    maskToken(a.token),
			})
		}
	case "query":
		if a.token != "" {
			q := req.URL.Query()
			q.Set(a.injectKey, a.token)
			req.URL.RawQuery = q.Encode()
			a.profiler.emit(EVENT_AUTH_TOKEN_INJECT, "Credential Injected", requestID, map[string]any{
				"location": "Query parameter",
				"queryKey": a.injectKey,
				"token":    maskToken(a.token),
			})
		}
	case "body":
		// Note: This modifies the request body which may be tricky
		// For now, we'll skip this case or implement it later
		a.profiler.emit(EVENT_AUTH_END, "Custom Auth Failed", requestID, map[string]any{
			"error": "injectInto=body not yet implemented",
		})
		return fmt.Errorf("injectInto=body not yet implemented")
	default:
		a.profiler.emit(EVENT_AUTH_END, "Custom Auth Failed", requestID, map[string]any{
			"error": fmt.Sprintf("unsupported injectInto: %s", a.injectInto),
		})
		return fmt.Errorf("unsupported injectInto: %s", a.injectInto)
	}

	a.profiler.emit(EVENT_AUTH_END, "Custom Auth Complete", requestID, nil)
	return nil
}

func (a *CustomAuthenticator) performLogin(requestID string) error {
	loginID := a.profiler.emit(EVENT_AUTH_LOGIN_START, "Custom Login Request", requestID, map[string]any{
		"url":    a.loginRequest.URL,
		"method": a.loginRequest.Method,
	})

	loginStart := time.Now()

	// Build login request
	loginReq, err := a.buildLoginRequest()
	if err != nil {
		a.profiler.emit(EVENT_AUTH_LOGIN_END, "Custom Login Failed", requestID, map[string]any{
			"error": err.Error(),
		})
		return err
	}

	// Execute login request
	resp, err := a.httpClient.Do(loginReq)
	if err != nil {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Custom Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType": "custom",
			"error":    err.Error(),
		})
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Custom Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType":   "custom",
			"error":      fmt.Sprintf("login request failed with status %d", resp.StatusCode),
			"statusCode": resp.StatusCode,
		})
		return fmt.Errorf("login request failed with status %d", resp.StatusCode)
	}

	// Extract token/cookie
	if err := a.extractCredential(resp, loginID); err != nil {
		loginDuration := time.Since(loginStart)
		a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Custom Login Failed", loginID, loginDuration.Milliseconds(), map[string]any{
			"authType": "custom",
			"error":    err.Error(),
		})
		return err
	}

	a.acquiredAt = time.Now()

	loginDuration := time.Since(loginStart)

	a.profiler.emitEnd(EVENT_AUTH_LOGIN_END, "Custom Login Complete", loginID, loginDuration.Milliseconds(), map[string]any{
		"authType":    "custom",
		"statusCode":  resp.StatusCode,
		"extractFrom": a.extractFrom,
	})
	return nil
}

func (a *CustomAuthenticator) extractCredential(resp *http.Response, requestID string) error {
	switch a.extractFrom {
	case "cookie":
		cookies := resp.Cookies()
		for _, cookie := range cookies {
			if cookie.Name == a.extractSelector {
				a.cookie = cookie
				// If we're not injecting as cookie, store value as token
				if a.injectInto != "cookie" {
					a.token = cookie.Value
				}
				a.profiler.emit(EVENT_AUTH_TOKEN_EXTRACT, "Credential Extracted from Cookie", requestID, map[string]any{
					"cookieName":  a.extractSelector,
					"cookieValue": maskToken(cookie.Value),
				})
				return nil
			}
		}
		return fmt.Errorf("cookie '%s' not found in login response", a.extractSelector)

	case "header":
		token := resp.Header.Get(a.extractSelector)
		if token == "" {
			return fmt.Errorf("header '%s' not found in login response", a.extractSelector)
		}
		a.token = token
		a.profiler.emit(EVENT_AUTH_TOKEN_EXTRACT, "Credential Extracted from Header", requestID, map[string]any{
			"headerName": a.extractSelector,
			"token":      maskToken(token),
		})
		return nil

	case "body":
		var body interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return fmt.Errorf("failed to decode login response: %w", err)
		}

		// Compile or get cached jq expression
		code, err := a.getOrCompileJQ(a.extractSelector)
		if err != nil {
			return err
		}

		// Execute jq expression
		iter := code.Run(body)
		v, ok := iter.Next()
		if !ok {
			return fmt.Errorf("jq selector '%s' yielded no results", a.extractSelector)
		}
		if err, isErr := v.(error); isErr {
			return fmt.Errorf("jq error: %w", err)
		}

		// Convert to string
		token, ok := v.(string)
		if !ok {
			return fmt.Errorf("token extracted is not a string: %T", v)
		}
		a.token = token
		a.profiler.emit(EVENT_AUTH_TOKEN_EXTRACT, "Credential Extracted from Body", requestID, map[string]any{
			"jqSelector": a.extractSelector,
			"token":      maskToken(token),
		})
		return nil

	default:
		return fmt.Errorf("unsupported extractFrom: %s", a.extractFrom)
	}
}

func (a *CustomAuthenticator) getOrCompileJQ(expression string) (*gojq.Code, error) {
	if code, ok := a.jqCache[expression]; ok {
		return code, nil
	}

	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid jq expression '%s': %w", expression, err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("failed to compile jq expression: %w", err)
	}

	a.jqCache[expression] = code
	return code, nil
}

func (a *CustomAuthenticator) buildLoginRequest() (*http.Request, error) {
	// Build URL
	urlStr := a.loginRequest.URL
	urlObj, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid login URL: %w", err)
	}

	// Build body
	var bodyReader *bytes.Reader
	if len(a.loginRequest.Body) > 0 {
		bodyJSON, err := json.Marshal(a.loginRequest.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal login body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyJSON)
	}

	// Create request
	var req *http.Request
	if bodyReader != nil {
		req, err = http.NewRequest(a.loginRequest.Method, urlObj.String(), bodyReader)
	} else {
		req, err = http.NewRequest(a.loginRequest.Method, urlObj.String(), nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}

	// Set headers
	for k, v := range a.loginRequest.Headers {
		req.Header.Set(k, v)
	}
	if bodyReader != nil {
		contentType := getContentType(a.loginRequest.Headers)
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

// NewAuthenticator creates an authenticator based on the configuration
func NewAuthenticator(config AuthenticatorConfig, httpClient HTTPClient) Authenticator {
	if config.Type == "" {
		return &NoopAuthenticator{}
	}

	switch config.Type {
	case "basic":
		return &BasicAuthenticator{
			username: config.Username,
			password: config.Password,
			BaseAuthenticator: &BaseAuthenticator{
				profiler: &AuthProfiler{authType: "basic"},
			},
		}

	case "bearer":
		return &BearerAuthenticator{
			token: config.Token,
			BaseAuthenticator: &BaseAuthenticator{
				profiler: &AuthProfiler{authType: "bearer"},
			},
		}

	case "oauth":
		return &OAuthAuthenticator{
			provider: NewOAuthProvider(config.OAuthConfig, config.Username, config.Password),
			BaseAuthenticator: &BaseAuthenticator{
				profiler: &AuthProfiler{authType: "oauth"},
			},
		}

	case "cookie":
		maxAge := time.Duration(config.MaxAgeSeconds) * time.Second
		return &CookieAuthenticator{
			loginRequest: config.LoginRequest,
			cookieName:   config.ExtractSelector,
			maxAge:       maxAge,
			onePerRun:    config.OnePerRun,
			httpClient:   httpClient,
			BaseAuthenticator: &BaseAuthenticator{
				profiler: &AuthProfiler{authType: "cookie"},
			},
		}

	case "jwt":
		maxAge := time.Duration(config.MaxAgeSeconds) * time.Second
		extractFrom := config.ExtractFrom
		if extractFrom == "" {
			extractFrom = "body" // Default to body extraction
		}
		return &JWTAuthenticator{
			loginRequest:    config.LoginRequest,
			extractFrom:     extractFrom,
			extractSelector: config.ExtractSelector,
			maxAge:          maxAge,
			onePerRun:       config.OnePerRun,
			httpClient:      httpClient,
			jqCache:         make(map[string]*gojq.Code),
			BaseAuthenticator: &BaseAuthenticator{
				profiler: &AuthProfiler{authType: "jwt"},
			},
		}

	case "custom":
		maxAge := time.Duration(config.MaxAgeSeconds) * time.Second
		return &CustomAuthenticator{
			loginRequest:    config.LoginRequest,
			extractFrom:     config.ExtractFrom,
			extractSelector: config.ExtractSelector,
			injectInto:      config.InjectInto,
			injectKey:       config.InjectKey,
			maxAge:          maxAge,
			onePerRun:       config.OnePerRun,
			httpClient:      httpClient,
			jqCache:         make(map[string]*gojq.Code),
			BaseAuthenticator: &BaseAuthenticator{
				profiler: &AuthProfiler{authType: "custom"},
			},
		}

	default:
		slog.Error(fmt.Sprintf("Unsupported authentication type: %s", config.Type))
		panic(fmt.Sprintf("Unsupported authentication type: %s", config.Type))
	}
}
