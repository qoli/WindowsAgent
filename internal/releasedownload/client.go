// Package releasedownload fetches and verifies public WindowsAgent release
// assets over an explicitly HTTP/1.1-only transport.
package releasedownload

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/qoli/WindowsAgent/internal/releasecatalog"
)

type Client struct {
	HTTP *http.Client
}

func NewHTTP1Client() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		TLSNextProto:          make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" {
				return errors.New("release download redirect changed to a non-HTTPS URL")
			}
			if len(via) >= 10 {
				return errors.New("release download exceeded 10 redirects")
			}
			return nil
		},
	}
}

func (c Client) FetchCatalog(ctx context.Context, catalogURL string) (releasecatalog.Catalog, *url.URL, error) {
	parsed, err := url.Parse(catalogURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return releasecatalog.Catalog{}, nil, fmt.Errorf("catalog URL must be absolute HTTPS: %q", catalogURL)
	}
	response, err := c.http().Do(request(ctx, parsed.String()))
	if err != nil {
		return releasecatalog.Catalog{}, nil, fmt.Errorf("download release catalog: %w", err)
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL.Scheme != "https" {
		return releasecatalog.Catalog{}, nil, errors.New("release catalog response did not use HTTPS")
	}
	if response.ProtoMajor != 1 {
		return releasecatalog.Catalog{}, nil, fmt.Errorf("release catalog response used HTTP/%d, expected HTTP/1.1", response.ProtoMajor)
	}
	if response.StatusCode != http.StatusOK {
		return releasecatalog.Catalog{}, nil, fmt.Errorf("download release catalog returned HTTP %d", response.StatusCode)
	}
	catalog, err := releasecatalog.Load(response.Body)
	if err != nil {
		return releasecatalog.Catalog{}, nil, err
	}
	base := *parsed
	base.RawQuery, base.Fragment = "", ""
	base.Path = path.Dir(base.Path) + "/"
	if err := c.validateSums(ctx, &base, catalog); err != nil {
		return releasecatalog.Catalog{}, nil, err
	}
	return catalog, &base, nil
}

func (c Client) validateSums(ctx context.Context, base *url.URL, catalog releasecatalog.Catalog) error {
	target := base.ResolveReference(&url.URL{Path: "SHA256SUMS"})
	response, err := c.http().Do(request(ctx, target.String()))
	if err != nil {
		return fmt.Errorf("download SHA256SUMS: %w", err)
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL.Scheme != "https" {
		return errors.New("SHA256SUMS response did not use HTTPS")
	}
	if response.ProtoMajor != 1 {
		return fmt.Errorf("SHA256SUMS response used HTTP/%d, expected HTTP/1.1", response.ProtoMajor)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download SHA256SUMS returned HTTP %d", response.StatusCode)
	}
	if err := releasecatalog.ValidateSHA256Sums(response.Body, catalog); err != nil {
		return fmt.Errorf("validate SHA256SUMS: %w", err)
	}
	return nil
}

func (c Client) Stage(ctx context.Context, base *url.URL, catalog releasecatalog.Catalog, directory string, include func(releasecatalog.Artifact) bool) error {
	if base == nil || base.Scheme != "https" || base.Host == "" {
		return errors.New("release asset base URL must be absolute HTTPS")
	}
	if err := catalog.Validate(); err != nil {
		return err
	}
	if include == nil {
		return errors.New("release artifact selector is required")
	}
	if !filepath.IsAbs(directory) {
		return errors.New("release staging directory must be absolute")
	}
	if _, err := os.Stat(directory); err == nil {
		return fmt.Errorf("release staging destination already exists: %s", directory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create release staging parent: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".release-staging-*")
	if err != nil {
		return fmt.Errorf("create release transaction directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	selected := 0
	for _, artifact := range catalog.Artifacts {
		if !include(artifact) {
			continue
		}
		selected++
		assetURL := base.ResolveReference(&url.URL{Path: artifact.Name})
		if err := c.stageOne(ctx, assetURL.String(), temporary, artifact); err != nil {
			return err
		}
	}
	if selected == 0 {
		return errors.New("release artifact selector matched no artifacts")
	}
	if err := os.Rename(temporary, directory); err != nil {
		return fmt.Errorf("commit verified release staging directory: %w", err)
	}
	return nil
}

func (c Client) stageOne(ctx context.Context, assetURL, directory string, artifact releasecatalog.Artifact) error {
	response, err := c.http().Do(request(ctx, assetURL))
	if err != nil {
		return fmt.Errorf("download %s: %w", artifact.Name, err)
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL.Scheme != "https" {
		return fmt.Errorf("download %s response did not use HTTPS", artifact.Name)
	}
	if response.ProtoMajor != 1 {
		return fmt.Errorf("download %s used HTTP/%d, expected HTTP/1.1", artifact.Name, response.ProtoMajor)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned HTTP %d", artifact.Name, response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != artifact.Bytes {
		return fmt.Errorf("download %s content length is %d, expected %d", artifact.Name, response.ContentLength, artifact.Bytes)
	}
	temporary, err := os.CreateTemp(directory, ".download-*.exe")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, artifact.Bytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return fmt.Errorf("download %s body: %w", artifact.Name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staged %s: %w", artifact.Name, closeErr)
	}
	if written != artifact.Bytes {
		return fmt.Errorf("download %s size is %d, expected %d", artifact.Name, written, artifact.Bytes)
	}
	if digest := hex.EncodeToString(hash.Sum(nil)); digest != artifact.SHA256 {
		return fmt.Errorf("download %s sha256 mismatch", artifact.Name)
	}
	subsystem, err := releasecatalog.ReadPESubsystem(temporaryPath)
	if err != nil {
		return fmt.Errorf("verify downloaded %s PE: %w", artifact.Name, err)
	}
	if subsystem != artifact.Subsystem {
		return fmt.Errorf("download %s PE subsystem is %s, expected %s", artifact.Name, subsystem, artifact.Subsystem)
	}
	destination := filepath.Join(directory, artifact.Name)
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("staged release artifact already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("commit staged %s: %w", artifact.Name, err)
	}
	return nil
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return NewHTTP1Client()
}

func request(ctx context.Context, target string) *http.Request {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		panic(err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "WindowsAgent-AssistGUI")
	return request
}
