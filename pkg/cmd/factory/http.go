package factory

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/cli/cli/v2/internal/ghinstance"
	"github.com/cli/cli/v2/utils"
	"github.com/cli/go-gh"
	ghAPI "github.com/cli/go-gh/pkg/api"
)

type configGetter interface {
	Get(string, string) (string, error)
}

type HTTPClientOptions struct {
	AppVersion        string
	CacheTTL          time.Duration
	Config            configGetter
	EnableCache       bool
	Log               io.Writer
	SkipAcceptHeaders bool
}

func NewHTTPClient(opts HTTPClientOptions) (*http.Client, error) {
	clientOpts := &ghAPI.ClientOptions{}

	if debugEnabled, _ := utils.IsDebugEnabled(); debugEnabled {
		clientOpts.Log = opts.Log
	}

	headers := map[string]string{
		"User-Agent": fmt.Sprintf("GitHub CLI %s", opts.AppVersion),
	}
	if opts.SkipAcceptHeaders {
		headers["Accept"] = ""
	}
	clientOpts.Headers = headers

	if opts.EnableCache {
		clientOpts.EnableCache = opts.EnableCache
		clientOpts.CacheTTL = opts.CacheTTL
	}

	client, err := gh.HTTPClient(clientOpts)
	if err != nil {
		return nil, err
	}

	client.Transport = AddAuthTokenHeader(opts.Config, client.Transport)
	client.Transport = ExtractHeader("X-GitHub-SSO", &ssoHeader)(client.Transport)

	return client, nil
}

// AddAuthToken adds an authentication token header for the host specified by the request.
func AddAuthTokenHeader(cfg configGetter, rt http.RoundTripper) http.RoundTripper {
	return &funcTripper{roundTrip: func(req *http.Request) (*http.Response, error) {
		hostname := ghinstance.NormalizeHostname(getHost(req))
		if token, err := cfg.Get(hostname, "oauth_token"); err == nil && token != "" {
			req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
		}
		return rt.RoundTrip(req)
	}}
}

// ExtractHeader extracts a named header from any response received by this client and,
// if non-blank, saves it to dest.
func ExtractHeader(name string, dest *string) func(http.RoundTripper) http.RoundTripper {
	return func(tr http.RoundTripper) http.RoundTripper {
		return &funcTripper{roundTrip: func(req *http.Request) (*http.Response, error) {
			res, err := tr.RoundTrip(req)
			if err == nil {
				if value := res.Header.Get(name); value != "" {
					*dest = value
				}
			}
			return res, err
		}}
	}
}

type funcTripper struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (tr funcTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return tr.roundTrip(req)
}

var ssoHeader string
var ssoURLRE = regexp.MustCompile(`\burl=([^;]+)`)

// SSOURL returns the URL of a SAML SSO challenge received by the server for clients that use ExtractHeader
// to extract the value of the "X-GitHub-SSO" response header.
func SSOURL() string {
	if ssoHeader == "" {
		return ""
	}
	m := ssoURLRE.FindStringSubmatch(ssoHeader)
	if m == nil {
		return ""
	}
	return m[1]
}

func getHost(r *http.Request) string {
	if r.Host != "" {
		return r.Host
	}
	return r.URL.Hostname()
}
