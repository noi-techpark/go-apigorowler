// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package apigorowler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	crawler_testing "github.com/noi-techpark/go-apigorowler/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicAuthenticator(t *testing.T) {
	config := AuthenticatorConfig{
		Type:     "basic",
		Username: "testuser",
		Password: "testpass",
	}

	auth := NewAuthenticator(config, nil)
	req, _ := http.NewRequest("GET", "https://example.com", nil)

	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	username, password, ok := req.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "testuser", username)
	assert.Equal(t, "testpass", password)
}

func TestBearerAuthenticator(t *testing.T) {
	config := AuthenticatorConfig{
		Type:  "bearer",
		Token: "my-secret-token",
	}

	auth := NewAuthenticator(config, nil)
	req, _ := http.NewRequest("GET", "https://example.com", nil)

	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	authHeader := req.Header.Get("Authorization")
	assert.Equal(t, "Bearer my-secret-token", authHeader)
}

func TestNoopAuthenticator(t *testing.T) {
	config := AuthenticatorConfig{
		Type: "",
	}

	auth := NewAuthenticator(config, nil)
	req, _ := http.NewRequest("GET", "https://example.com", nil)

	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	// Should not add any authentication headers
	assert.Empty(t, req.Header.Get("Authorization"))
}

func TestCookieAuthenticator(t *testing.T) {
	// Setup mock HTTP client that returns a cookie
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/login": "testdata/auth/cookie_login_response.json",
	})

	// Mock needs to intercept and set cookies
	mockTransport.InterceptFunc = func(req *http.Request, resp *http.Response) {
		if req.URL.Path == "/login" {
			// Simulate cookie being set
			cookie := &http.Cookie{
				Name:  "session_id",
				Value: "abc123xyz",
			}
			resp.Header.Add("Set-Cookie", cookie.String())
		}
	}

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "cookie",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/login",
			Method: "POST",
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
		},
		ExtractSelector: "session_id",
		MaxAgeSeconds:   3600,
	}

	auth := NewAuthenticator(config, client)

	// First request should perform login
	req1, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req1, "")
	require.Nil(t, err)

	// Check that cookie is set
	cookies := req1.Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "session_id", cookies[0].Name)
	assert.Equal(t, "abc123xyz", cookies[0].Value)

	// Second request should reuse the same cookie without login
	req2, _ := http.NewRequest("GET", "https://api.example.com/data2", nil)
	err = auth.PrepareRequest(req2, "")
	require.Nil(t, err)

	cookies2 := req2.Cookies()
	require.Len(t, cookies2, 1)
	assert.Equal(t, "session_id", cookies2[0].Name)
}

func TestCookieAuthenticatorOnePerRun(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/login": "testdata/auth/cookie_login_response.json",
	})

	loginCount := 0
	mockTransport.InterceptFunc = func(req *http.Request, resp *http.Response) {
		if req.URL.Path == "/login" {
			loginCount++
			cookie := &http.Cookie{
				Name:  "session_id",
				Value: "abc123xyz",
			}
			resp.Header.Add("Set-Cookie", cookie.String())
		}
	}

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "cookie",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/login",
			Method: "POST",
		},
		ExtractSelector: "session_id",
		OnePerRun:       true,
	}

	auth := NewAuthenticator(config, client)

	// Multiple requests should only login once
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
		err := auth.PrepareRequest(req, "")
		require.Nil(t, err)
	}

	assert.Equal(t, 1, loginCount, "Should only login once with onePerRun=true")
}

func TestJWTAuthenticatorFromBody(t *testing.T) {
	// Create login response with JWT in body
	loginResponse := map[string]interface{}{
		"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test",
		"user":  "testuser",
	}

	mockTransport := crawler_testing.NewMockRoundTripperWithResponse(map[string]interface{}{
		"https://api.example.com/login": loginResponse,
	})

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "jwt",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/login",
			Method: "POST",
			Body: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
		},
		ExtractFrom:     "body",
		ExtractSelector: ".token",
		MaxAgeSeconds:   3600,
	}

	auth := NewAuthenticator(config, client)

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	authHeader := req.Header.Get("Authorization")
	assert.Equal(t, "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test", authHeader)
}

func TestJWTAuthenticatorFromHeader(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/login": "testdata/auth/jwt_login_response.json",
	})

	mockTransport.InterceptFunc = func(req *http.Request, resp *http.Response) {
		if req.URL.Path == "/login" {
			resp.Header.Set("X-Auth-Token", "jwt-token-from-header")
		}
	}

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "jwt",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/login",
			Method: "POST",
		},
		ExtractFrom:     "header",
		ExtractSelector: "X-Auth-Token",
		OnePerRun:       true,
	}

	auth := NewAuthenticator(config, client)

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	authHeader := req.Header.Get("Authorization")
	assert.Equal(t, "Bearer jwt-token-from-header", authHeader)
}

func TestJWTAuthenticatorRefresh(t *testing.T) {
	loginResponse := map[string]interface{}{
		"token": "initial-token",
	}

	mockTransport := crawler_testing.NewMockRoundTripperWithResponse(map[string]interface{}{
		"https://api.example.com/login": loginResponse,
	})

	loginCount := 0
	mockTransport.InterceptFunc = func(req *http.Request, resp *http.Response) {
		if req.URL.Path == "/login" {
			loginCount++
			// Change token on subsequent logins (after first)
			if loginCount > 1 {
				newBody := map[string]interface{}{
					"token": "refreshed-token",
				}
				bodyBytes, _ := json.Marshal(newBody)
				resp.Body = crawler_testing.CreateResponseBody(string(bodyBytes))
			}
		}
	}

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "jwt",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/login",
			Method: "POST",
		},
		ExtractFrom:     "body",
		ExtractSelector: ".token",
		MaxAgeSeconds:   1, // 1 second max age
	}

	auth := NewAuthenticator(config, client)

	// First request
	req1, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req1, "")
	require.Nil(t, err)
	assert.Contains(t, req1.Header.Get("Authorization"), "initial-token")

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	// Second request should refresh
	req2, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = auth.PrepareRequest(req2, "")
	require.Nil(t, err)
	assert.Contains(t, req2.Header.Get("Authorization"), "refreshed-token")

	assert.Equal(t, 2, loginCount, "Should have logged in twice (initial + refresh)")
}

func TestCustomAuthenticatorCookieToHeader(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/login": "testdata/auth/custom_login_response.json",
	})

	mockTransport.InterceptFunc = func(req *http.Request, resp *http.Response) {
		if req.URL.Path == "/login" {
			cookie := &http.Cookie{
				Name:  "auth_cookie",
				Value: "cookie-value-123",
			}
			resp.Header.Add("Set-Cookie", cookie.String())
		}
	}

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "custom",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/login",
			Method: "POST",
		},
		ExtractFrom:     "cookie",
		ExtractSelector: "auth_cookie",
		InjectInto:      "header",
		InjectKey:       "X-Custom-Auth",
		OnePerRun:       true,
	}

	auth := NewAuthenticator(config, client)

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	authHeader := req.Header.Get("X-Custom-Auth")
	assert.Equal(t, "cookie-value-123", authHeader)
}

func TestCustomAuthenticatorBodyToquery(t *testing.T) {
	loginResponse := map[string]interface{}{
		"access_token": "query-param-token-456",
		"expires_in":   3600,
	}

	mockTransport := crawler_testing.NewMockRoundTripperWithResponse(map[string]interface{}{
		"https://api.example.com/auth": loginResponse,
	})

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "custom",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/auth",
			Method: "POST",
		},
		ExtractFrom:     "body",
		ExtractSelector: ".access_token",
		InjectInto:      "query",
		InjectKey:       "api_key",
		OnePerRun:       true,
	}

	auth := NewAuthenticator(config, client)

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	query := req.URL.Query().Get("api_key")
	assert.Equal(t, "query-param-token-456", query)
}

func TestCustomAuthenticatorHeaderToBearer(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/authenticate": "testdata/auth/custom_login_response.json",
	})

	mockTransport.InterceptFunc = func(req *http.Request, resp *http.Response) {
		if req.URL.Path == "/authenticate" {
			resp.Header.Set("Authorization", "Bearer header-token-789")
		}
	}

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "custom",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/authenticate",
			Method: "POST",
		},
		ExtractFrom:     "header",
		ExtractSelector: "Authorization",
		InjectInto:      "bearer",
		OnePerRun:       true,
	}

	auth := NewAuthenticator(config, client)

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	authHeader := req.Header.Get("Authorization")
	assert.Equal(t, "Bearer Bearer header-token-789", authHeader) // Note: doubled "Bearer" because we extract full header value
}

func TestCustomAuthenticatorCookieToCookie(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/login": "testdata/auth/custom_login_response.json",
	})

	mockTransport.InterceptFunc = func(req *http.Request, resp *http.Response) {
		if req.URL.Path == "/login" {
			cookie := &http.Cookie{
				Name:  "session",
				Value: "session-abc-123",
			}
			resp.Header.Add("Set-Cookie", cookie.String())
		}
	}

	client := &http.Client{Transport: mockTransport}

	config := AuthenticatorConfig{
		Type: "custom",
		LoginRequest: &RequestConfig{
			URL:    "https://api.example.com/login",
			Method: "POST",
		},
		ExtractFrom:     "cookie",
		ExtractSelector: "session",
		InjectInto:      "cookie",
		MaxAgeSeconds:   3600,
	}

	auth := NewAuthenticator(config, client)

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err := auth.PrepareRequest(req, "")
	require.Nil(t, err)

	cookies := req.Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "session", cookies[0].Name)
	assert.Equal(t, "session-abc-123", cookies[0].Value)
}
