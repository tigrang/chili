package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

type handler struct {
	mu             sync.Mutex
	app            *app
	proxy          *httputil.ReverseProxy
	connectTimeout time.Duration
	notifyRoute    string
	proxyBind      string
	proxyUrl       string
}

func newProxy(proxyBind string, notifyRoute string, connectTimeout time.Duration, app *app) (*handler, error) {
	remote, err := url.Parse("http://" + app.url)
	if err != nil {
		return nil, err
	}

	return &handler{
		proxy:          httputil.NewSingleHostReverseProxy(remote),
		proxyUrl:       app.url,
		proxyBind:      proxyBind,
		notifyRoute:    notifyRoute,
		connectTimeout: connectTimeout,
		app:            app,
	}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Info("Serving request", "url", r.URL)

	if r.URL.Path == h.notifyRoute {
		h.mu.Lock()
		h.app.markAsDirty()
		h.mu.Unlock()
		return
	}

	h.mu.Lock()
	err := h.app.rebuildIfDirty(h.connectTimeout)
	h.mu.Unlock()

	if err != nil {
		h.respondWithError(w, h.app.buildOutput, err)
		return
	}

	slog.Info("Proxying request", "url", r.URL)
	h.proxy.ServeHTTP(w, r)
}

func (h *handler) notify() error {
	if err := waitForConnection(h.proxyBind, h.connectTimeout); err != nil {
		return fmt.Errorf("failed to connect to proxy: %w", err)
	}

	_, err := http.Get("http://" + h.proxyBind + h.notifyRoute)
	if err != nil {
		return fmt.Errorf("failed to notify proxy: %w", err)
	}

	return nil
}

func (h *handler) respondWithError(w http.ResponseWriter, output []byte, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	if err := tmpl.Execute(w, map[string]any{"output": string(output), "err": err.Error()}); err != nil {
		slog.Error("Failed to execute template", "err", err)
	}
}
