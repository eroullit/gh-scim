package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type keylessDWDTokenSource struct {
	ctx                 context.Context
	base                oauth2.TokenSource
	httpClient          *http.Client
	serviceAccountEmail string
	subject             string
	scope               string
	signURL             string
	tokenURL            string
	now                 func() time.Time
}

func googleHTTPClient(ctx context.Context, cfg config) (*http.Client, error) {
	scopes := []string{directoryUserScope, directoryGroupScope}
	if cfg.serviceAccountFile != "" {
		credentials, err := os.ReadFile(cfg.serviceAccountFile)
		if err != nil {
			return nil, fmt.Errorf("reading service account credentials: %w", err)
		}
		jwtConfig, err := google.JWTConfigFromJSON(credentials, scopes...)
		if err != nil {
			return nil, fmt.Errorf("parsing service account credentials: %w", err)
		}
		jwtConfig.Subject = cfg.adminSubject
		return jwtConfig.Client(ctx), nil
	}

	credentials, err := google.FindDefaultCredentials(ctx, cloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("finding Application Default Credentials: %w", err)
	}
	source := &keylessDWDTokenSource{
		ctx:                 ctx,
		base:                credentials.TokenSource,
		httpClient:          http.DefaultClient,
		serviceAccountEmail: cfg.serviceAccountEmail,
		subject:             cfg.adminSubject,
		scope:               strings.Join(scopes, " "),
		signURL:             iamCredentialsURL + url.PathEscape(cfg.serviceAccountEmail) + ":signJwt",
		tokenURL:            googleTokenURL,
		now:                 time.Now,
	}
	return oauth2.NewClient(ctx, oauth2.ReuseTokenSource(nil, source)), nil
}

func (s *keylessDWDTokenSource) Token() (*oauth2.Token, error) {
	baseToken, err := s.base.Token()
	if err != nil {
		return nil, fmt.Errorf("getting base Google credential: %w", err)
	}

	now := s.now()
	claims, err := json.Marshal(map[string]any{
		"iss":   s.serviceAccountEmail,
		"sub":   s.subject,
		"scope": s.scope,
		"aud":   s.tokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("encoding delegated JWT claims: %w", err)
	}
	signBody, err := json.Marshal(map[string]string{"payload": string(claims)})
	if err != nil {
		return nil, fmt.Errorf("encoding signJwt request: %w", err)
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.signURL, bytes.NewReader(signBody))
	if err != nil {
		return nil, fmt.Errorf("building signJwt request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+baseToken.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling signJwt: %w", err)
	}
	body, err := readResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("reading signJwt response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signJwt returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var signed struct {
		SignedJWT string `json:"signedJwt"`
	}
	if err := json.Unmarshal(body, &signed); err != nil {
		return nil, fmt.Errorf("decoding signJwt response: %w", err)
	}
	if signed.SignedJWT == "" {
		return nil, errors.New("signJwt returned an empty signedJwt")
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signed.SignedJWT},
	}
	req, err = http.NewRequestWithContext(s.ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building delegated token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging delegated JWT: %w", err)
	}
	body, err = readResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("reading delegated token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("delegated token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return nil, fmt.Errorf("decoding delegated token response: %w", err)
	}
	if tokenResponse.AccessToken == "" {
		return nil, errors.New("delegated token endpoint returned an empty access_token")
	}
	return &oauth2.Token{
		AccessToken: tokenResponse.AccessToken,
		TokenType:   tokenResponse.TokenType,
		Expiry:      now.Add(time.Duration(tokenResponse.ExpiresIn) * time.Second),
	}, nil
}

func readResponse(resp *http.Response) ([]byte, error) {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return body, nil
}
