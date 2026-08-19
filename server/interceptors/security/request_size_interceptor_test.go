package security

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestSizeInterceptor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		method         string
		body           []byte
		contentLength  int64
		maxBodyBytes   int64
		expectedStatus int
		expectedCalled bool
		handler        func(http.ResponseWriter, *http.Request)
	}{
		{
			name:           "read only method bypasses body checks",
			method:         http.MethodGet,
			body:           bytes.Repeat([]byte("x"), 32),
			contentLength:  32,
			maxBodyBytes:   8,
			expectedStatus: http.StatusOK,
			expectedCalled: true,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:           "rejects payload based on content length",
			method:         http.MethodPost,
			body:           []byte("small"),
			contentLength:  11,
			maxBodyBytes:   10,
			expectedStatus: http.StatusRequestEntityTooLarge,
			expectedCalled: false,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:           "allows payload within limit",
			method:         http.MethodPost,
			body:           []byte("small"),
			contentLength:  5,
			maxBodyBytes:   10,
			expectedStatus: http.StatusOK,
			expectedCalled: true,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:           "limits streaming body when content length is unknown",
			method:         http.MethodPost,
			body:           bytes.Repeat([]byte("x"), 11),
			contentLength:  -1,
			maxBodyBytes:   10,
			expectedStatus: http.StatusRequestEntityTooLarge,
			expectedCalled: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				w.WriteHeader(http.StatusOK)
			},
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			called := false
			handler := NewRequestSizeInterceptor(testCase.maxBodyBytes).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				testCase.handler(w, r)
			}))

			req := httptest.NewRequestWithContext(t.Context(), testCase.method, "/api/v1/test", bytes.NewReader(testCase.body))
			req.ContentLength = testCase.contentLength

			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			if res.Code != testCase.expectedStatus {
				t.Fatalf("expected status %d, got %d", testCase.expectedStatus, res.Code)
			}

			if called != testCase.expectedCalled {
				t.Fatalf("expected handler called=%t, got %t", testCase.expectedCalled, called)
			}
		})
	}
}
