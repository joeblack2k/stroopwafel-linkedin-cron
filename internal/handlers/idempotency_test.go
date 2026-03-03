package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIIdempotencyCreatePostReplay(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	handler := app.WithAPIIdempotency(app.APICreatePost)

	payload := map[string]any{
		"text":   "idempotent post",
		"status": "draft",
	}

	recorderOne := performJSONRequestWithHeaders(t, http.MethodPost, "/api/v1/posts", payload, map[string]string{
		idempotencyKeyHeader: "idem-create-1",
	}, handler)
	if recorderOne.Code != http.StatusCreated {
		t.Fatalf("expected first request status 201, got %d (%s)", recorderOne.Code, recorderOne.Body.String())
	}

	var first map[string]any
	if err := json.Unmarshal(recorderOne.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	firstPost, ok := first["post"].(map[string]any)
	if !ok {
		t.Fatalf("first response missing post payload: %+v", first)
	}
	firstID, ok := firstPost["id"].(float64)
	if !ok {
		t.Fatalf("first response post.id missing: %+v", firstPost)
	}

	recorderTwo := performJSONRequestWithHeaders(t, http.MethodPost, "/api/v1/posts", payload, map[string]string{
		idempotencyKeyHeader: "idem-create-1",
	}, handler)
	if recorderTwo.Code != http.StatusCreated {
		t.Fatalf("expected replay request status 201, got %d (%s)", recorderTwo.Code, recorderTwo.Body.String())
	}
	if replay := recorderTwo.Header().Get(idempotentReplayHeader); replay != "true" {
		t.Fatalf("expected replay header true, got %q", replay)
	}

	var second map[string]any
	if err := json.Unmarshal(recorderTwo.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	secondPost, ok := second["post"].(map[string]any)
	if !ok {
		t.Fatalf("second response missing post payload: %+v", second)
	}
	secondID, ok := secondPost["id"].(float64)
	if !ok {
		t.Fatalf("second response post.id missing: %+v", secondPost)
	}

	if firstID != secondID {
		t.Fatalf("expected identical post id for replay, got first=%v second=%v", firstID, secondID)
	}

	posts, err := app.Store.ListPosts(context.Background())
	if err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected one post after idempotent replay, got %d", len(posts))
	}
}

func TestAPIIdempotencyRejectsPayloadMismatch(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	handler := app.WithAPIIdempotency(app.APICreatePost)

	recorderOne := performJSONRequestWithHeaders(t, http.MethodPost, "/api/v1/posts", map[string]any{
		"text":   "first payload",
		"status": "draft",
	}, map[string]string{
		idempotencyKeyHeader: "idem-create-2",
	}, handler)
	if recorderOne.Code != http.StatusCreated {
		t.Fatalf("expected first request status 201, got %d (%s)", recorderOne.Code, recorderOne.Body.String())
	}

	recorderTwo := performJSONRequestWithHeaders(t, http.MethodPost, "/api/v1/posts", map[string]any{
		"text":   "second payload",
		"status": "draft",
	}, map[string]string{
		idempotencyKeyHeader: "idem-create-2",
	}, handler)
	if recorderTwo.Code != http.StatusConflict {
		t.Fatalf("expected mismatch request status 409, got %d (%s)", recorderTwo.Code, recorderTwo.Body.String())
	}

	if !strings.Contains(recorderTwo.Body.String(), "different request") {
		t.Fatalf("expected mismatch error message, got %s", recorderTwo.Body.String())
	}

	posts, err := app.Store.ListPosts(context.Background())
	if err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected one created post after mismatch, got %d", len(posts))
	}
}

func performJSONRequestWithHeaders(t *testing.T, method, target string, payload any, headers map[string]string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}
