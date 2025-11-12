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
	PrepareRequest(req *http.Request) error
}

// NoopAuthenticator - no authentication
type NoopAuthenticator struct{}

func (np NoopAuthenticator) PrepareRequest(req *http.Request) error {
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
	OAuthConfig `yaml:",inline" json:",inline"`

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
	username string
	password string
}

func (a *BasicAuthenticator) PrepareRequest(req *http.Request) error {
	req.SetBasicAuth(a.username, a.password)
	return nil
}

// BearerAuthenticator - Bearer token authentication
type BearerAuthenticator struct {
	token string
}

func (a *BearerAuthenticator) PrepareRequest(req *http.Request) error {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))
	return nil
}

type OAuthConfig struct {
	Method       string   `yaml:"method,omitempty" json:"method,omitempty"` // password | client_credentials
	TokenURL     string   `yaml:"tokenUrl,omitempty" json:"tokenUrl,omitempty"`
	ClientID     string   `yaml:"clientId,omitempty" json:"clientId,omitempty"`
	ClientSecret string   `yaml:"clientSecret,omitempty" json:"clientSecret,omitempty"`
	Username     string   `yaml:"username,omitempty" json:"username,omitempty"`
	Password     string   `yaml:"password,omitempty" json:"password,omitempty"`
	Scopes       []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

// OAuthAuthenticator - OAuth2 authentication
type OAuthAuthenticator struct {
	provider *OAuthProvider
}

func (a *OAuthAuthenticator) PrepareRequest(req *http.Request) error {
	token, err := a.provider.GetToken()
	if err != nil {
		return fmt.Errorf("could not get oauth token: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	return nil
}

// OAuthProvider struct
type OAuthProvider struct {
	conf        *oauth2.Config
	clientCreds *clientcredentials.Config
	token       *oauth2.Token
	mu          sync.Mutex
	username    string
	password    string
}

func NewOAuthProvider(cfg OAuthConfig) *OAuthProvider {
	authMethod := cfg.Method
	tokenURL := cfg.TokenURL
	clientID := cfg.ClientID
	clientSecret := cfg.ClientSecret

	wrapper := &OAuthProvider{
		username: cfg.Username,
		password: cfg.Password,
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
	w.mu.Lock()
	defer w.mu.Unlock()

	ctx := context.Background()

	// If token exists and is still valid, return it
	if w.token != nil && w.token.Valid() {
		return w.token.AccessToken, nil
	}

	// Fetch new token
	var token *oauth2.Token
	var err error

	if w.conf != nil { // Password flow
		token, err = w.conf.PasswordCredentialsToken(ctx, w.username, w.password)
	} else { // Client Credentials flow
		token, err = w.clientCreds.Token(ctx)
	}

	if err != nil {
		return "", err
	}

	// Store new token
	w.token = token
	return token.AccessToken, nil
}

// CookieAuthenticator - performs login via POST, extracts cookie, injects it
type CookieAuthenticator struct {
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

func (a *CookieAuthenticator) PrepareRequest(req *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check if we need to authenticate
	needsAuth := false
	if !a.authenticated || (a.cookie == nil && !a.onePerRun) {
		needsAuth = true
	} else if a.maxAge > 0 && time.Since(a.acquiredAt) > a.maxAge {
		needsAuth = true
	}

	if needsAuth {
		if err := a.performLogin(); err != nil {
			return fmt.Errorf("cookie authentication failed: %w", err)
		}
		a.authenticated = true
	}

	// Inject cookie
	if a.cookie != nil {
		req.AddCookie(a.cookie)
	}

	return nil
}

func (a *CookieAuthenticator) performLogin() error {
	// Build login request
	loginReq, err := a.buildLoginRequest()
	if err != nil {
		return err
	}

	// Execute login request
	resp, err := a.httpClient.Do(loginReq)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("login request failed with status %d", resp.StatusCode)
	}

	// Extract cookie
	cookies := resp.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == a.cookieName {
			a.cookie = cookie
			a.acquiredAt = time.Now()
			return nil
		}
	}

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

// JWTAuthenticator - performs login via POST, extracts JWT from response
type JWTAuthenticator struct {
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

func (a *JWTAuthenticator) PrepareRequest(req *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check if we need to authenticate
	needsAuth := false
	if !a.authenticated || (a.token == "" && !a.onePerRun) {
		needsAuth = true
	} else if a.maxAge > 0 && time.Since(a.acquiredAt) > a.maxAge {
		needsAuth = true
	}

	if needsAuth {
		if err := a.performLogin(); err != nil {
			return fmt.Errorf("jwt authentication failed: %w", err)
		}
		a.authenticated = true
	}

	// Inject token as Bearer
	if a.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))
	}

	return nil
}

func (a *JWTAuthenticator) performLogin() error {
	// Build login request
	loginReq, err := a.buildLoginRequest()
	if err != nil {
		return err
	}

	// Execute login request
	resp, err := a.httpClient.Do(loginReq)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("login request failed with status %d", resp.StatusCode)
	}

	// Extract token
	token, err := a.extractToken(resp)
	if err != nil {
		return err
	}

	a.token = token
	a.acquiredAt = time.Now()
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

func (a *CustomAuthenticator) PrepareRequest(req *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check if we need to authenticate
	needsAuth := false
	if !a.authenticated {
		needsAuth = true
	} else if !a.onePerRun && a.maxAge > 0 && time.Since(a.acquiredAt) > a.maxAge {
		needsAuth = true
	}

	if needsAuth {
		if err := a.performLogin(); err != nil {
			return fmt.Errorf("custom authentication failed: %w", err)
		}
		a.authenticated = true
	}

	// Inject token/cookie based on injectInto
	switch a.injectInto {
	case "cookie":
		if a.cookie != nil {
			req.AddCookie(a.cookie)
		}
	case "header":
		if a.token != "" {
			req.Header.Set(a.injectKey, a.token)
		}
	case "bearer":
		if a.token != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))
		}
	case "query":
		if a.token != "" {
			q := req.URL.Query()
			q.Set(a.injectKey, a.token)
			req.URL.RawQuery = q.Encode()
		}
	case "body":
		// Note: This modifies the request body which may be tricky
		// For now, we'll skip this case or implement it later
		return fmt.Errorf("injectInto=body not yet implemented")
	default:
		return fmt.Errorf("unsupported injectInto: %s", a.injectInto)
	}

	return nil
}

func (a *CustomAuthenticator) performLogin() error {
	// Build login request
	loginReq, err := a.buildLoginRequest()
	if err != nil {
		return err
	}

	// Execute login request
	resp, err := a.httpClient.Do(loginReq)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("login request failed with status %d", resp.StatusCode)
	}

	// Extract token/cookie
	if err := a.extractCredential(resp); err != nil {
		return err
	}

	a.acquiredAt = time.Now()
	return nil
}

func (a *CustomAuthenticator) extractCredential(resp *http.Response) error {
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
		}

	case "bearer":
		return &BearerAuthenticator{
			token: config.Token,
		}

	case "oauth":
		return &OAuthAuthenticator{
			provider: NewOAuthProvider(config.OAuthConfig),
		}

	case "cookie":
		maxAge := time.Duration(config.MaxAgeSeconds) * time.Second
		return &CookieAuthenticator{
			loginRequest: config.LoginRequest,
			cookieName:   config.ExtractSelector,
			maxAge:       maxAge,
			onePerRun:    config.OnePerRun,
			httpClient:   httpClient,
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
		}

	default:
		slog.Error(fmt.Sprintf("Unsupported authentication type: %s", config.Type))
		panic(fmt.Sprintf("Unsupported authentication type: %s", config.Type))
	}
}
