package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/chrome"
	_ "github.com/browserutils/kooky/browser/edge"
	firefoxkooky "github.com/browserutils/kooky/browser/firefox"
	_ "github.com/browserutils/kooky/browser/safari"
)

type cookieCandidate struct {
	value   string
	expires time.Time
}

var (
	cookieMu     sync.Mutex
	cachedCookie string
)

// extractCookieFromBrowser searches installed browsers for Zendesk session cookies.
// Supports Chrome, Edge, Firefox, Safari, and Zen (Firefox-based).
// When multiple browsers have the same cookie, the one with the latest expiry wins.
func extractCookieFromBrowser(subdomain string) (string, error) {
	domain := subdomain + ".zendesk.com"

	best := map[string]cookieCandidate{}

	validFilters := []kooky.Filter{
		kooky.Valid,
		kooky.DomainHasSuffix(domain),
	}

	var totalFound int

	// Phase 1: Zen and Firefox (no Keychain prompt)
	for _, c := range extractFirefoxLikeCookies(domain, zenProfileRoots()) {
		totalFound++
		if prev, ok := best[c.Name]; !ok || c.Expires.After(prev.expires) {
			best[c.Name] = cookieCandidate{value: c.Name + "=" + c.Value, expires: c.Expires}
		}
	}
	for _, c := range extractFirefoxLikeCookies(domain, firefoxProfileRoots()) {
		totalFound++
		if prev, ok := best[c.Name]; !ok || c.Expires.After(prev.expires) {
			best[c.Name] = cookieCandidate{value: c.Name + "=" + c.Value, expires: c.Expires}
		}
	}

	if len(best) > 0 {
		return buildCookieString(best), nil
	}

	// Phase 2: Safari and Chrome via registered finders (Chrome may trigger Keychain prompt on macOS)
	for c := range kooky.TraverseCookies(context.TODO(), validFilters...).OnlyCookies() {
		totalFound++
		if prev, ok := best[c.Name]; !ok || c.Expires.After(prev.expires) {
			best[c.Name] = cookieCandidate{value: c.Name + "=" + c.Value, expires: c.Expires}
		}
	}

	if len(best) > 0 {
		return buildCookieString(best), nil
	}

	// No valid cookies - check for expired ones to give a better error
	if totalFound == 0 {
		var expiredCount int
		domainOnly := []kooky.Filter{kooky.DomainHasSuffix(domain)}
		for range kooky.TraverseCookies(context.TODO(), domainOnly...).OnlyCookies() {
			expiredCount++
		}
		if expiredCount > 0 {
			return "", fmt.Errorf(
				"found %d Zendesk cookies for %s but all are expired - log into Zendesk in your browser to refresh your session",
				expiredCount, domain,
			)
		}
	}

	return "", fmt.Errorf(
		"no Zendesk cookies found in any browser for %s - ensure you are logged into Zendesk in your browser (Zen, Firefox, Safari, Chrome, or Edge)",
		domain,
	)
}

func buildCookieString(best map[string]cookieCandidate) string {
	var parts []string
	for _, c := range best {
		parts = append(parts, c.value)
	}
	fmt.Fprintf(os.Stderr, "zendesk-oauth-mcp: extracted %d cookies from browser\n", len(parts))
	return strings.Join(parts, "; ")
}

func zenProfileRoots() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "zen", "Profiles")}
	case "linux":
		return []string{filepath.Join(home, ".zen", "Profiles")}
	}
	return nil
}

func firefoxProfileRoots() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")}
	case "linux":
		return []string{
			filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
			filepath.Join(home, ".mozilla", "firefox"),
		}
	}
	return nil
}

// extractFirefoxLikeCookies reads cookies from Firefox-format SQLite databases
// found under the given profile root directories.
func extractFirefoxLikeCookies(domain string, roots []string) []*kooky.Cookie {
	var found []*kooky.Cookie
	for _, root := range roots {
		profiles, err := filepath.Glob(filepath.Join(root, "*", "cookies.sqlite"))
		if err != nil {
			continue
		}
		for _, dbPath := range profiles {
			for c := range firefoxkooky.TraverseCookies(dbPath, kooky.Valid, kooky.DomainHasSuffix(domain)).OnlyCookies() {
				found = append(found, c)
			}
		}
	}
	return found
}

// refreshCookie re-extracts the cookie from the browser and updates the cache.
func refreshCookie() (string, error) {
	if zendeskSubdomain == "" {
		return "", fmt.Errorf("ZENDESK_SUBDOMAIN is required to extract cookies")
	}

	cookie, err := extractCookieFromBrowser(zendeskSubdomain)
	if err != nil {
		return "", err
	}

	cookieMu.Lock()
	cachedCookie = cookie
	zendeskCookie = cookie
	cookieMu.Unlock()

	return cookie, nil
}

// initCookie sets up the cookie, either from env or browser extraction.
func initCookie() error {
	if zendeskCookie != "" {
		return nil
	}

	_, err := refreshCookie()
	return err
}
