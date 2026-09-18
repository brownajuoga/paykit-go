package mpesa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

type fakeTokenProvider struct{}

func TestSTKPushUsesCorrectendpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mpesa/stkpush/v1/processrequest" {
			t.Errorf("expected path %q, got %q", "/mpesa/stkpush/v1/processrequest", r.URL.Path)
		}

		if r.Method != http.MethodPost {
			t.Errorf("expected method %q, got %q", http.MethodPost, r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type %q, got %q", "application/json", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{
		"SellerRequestID": "123",
		"OutRequestID":"456",
		"Response":"0",
		"ResponseCode":"0",
		"CustomerPrompt":"Accepted"
		}`))
	}))

	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	req := STKPushRequest{
		Amount:           1,
		PartyA:           "254700000001",
		PartyB:           "174379",
		PhoneNumber:      "254700000001",
		CallBackURL:      "https://example.com/callback",
		AccountReference: "INV001",
		TransactionDesc:  "Payment",
	}

	_, err := client.STKPush(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSTKPushParsesSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{
			"MerchantRequestID":"123",
			"CheckoutRequestID":"456",
			"ResponseCode":"0",
			"ResponseDescription":"Success",
			"CustomerMessage":"Accepted"
		}`))
	}))

	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	req := STKPushRequest{
		Amount:           1,
		PartyA:           "254700000001",
		PartyB:           "174379",
		PhoneNumber:      "254700000001",
		CallBackURL:      "https://example.com/callback",
		AccountReference: "INV001",
		TransactionDesc:  "Payment",
	}

	resp, err := client.STKPush(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.MerchantRequestID != "123" {
		t.Errorf("expected MerchantRequestID %q, got %q", "123", resp.MerchantRequestID)
	}

	if resp.CheckoutRequestID != "456" {
		t.Errorf("expected CheckoutRequestID   %q, got %q", "456", resp.CheckoutRequestID)
	}

	if resp.ResponseCode != "0" {
		t.Errorf("expected ResponseCode %q, got %q", "0", resp.ResponseCode)
	}
	if resp.CustomerMessage != "Accepted" {
		t.Errorf("expected CustomerMessage%q, got %q", "Accepted", resp.CustomerMessage)
	}
}

func TestSTKPushIncludesIdempotencyHeader(t *testing.T) {
	var (
		authorization  string
		idempotencyKey string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		idempotencyKey = r.Header.Get("Idempotency-Key")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"MerchantRequestID":"123",
			"CheckoutRequestID":"456",
			"ResponseCode":"0",
			"ResponseDescription":"Success",
			"CustomerMessage":"Accepted"
		}`))
	}))

	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	req := STKPushRequest{
		IdempotencyKey:    "stk-push-001",
		BusinessShortCode: "174379",
		Amount:            1,
		PartyA:            "254700000001",
		PartyB:            "174379",
		PhoneNumber:       "254700000001",
		CallBackURL:       "https://example.com/callback",
		AccountReference:  "INV001",
		TransactionDesc:   "Payment",
	}

	_, err := client.STKPush(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if idempotencyKey != "stk-push-001" {
		t.Errorf("expected Idempotency-Key header %q, got %q", "stk-push-001", idempotencyKey)
	}

	if authorization != "Bearer test-token" {
		t.Errorf("expected Authorization header %q, got %q", "Bearer test-token", authorization)
	}
}

func TestSTKPushGeneratesTimestampPassword(t *testing.T) {
	const passkey = "known-passkey"

	var received STKPushRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"MerchantRequestID":"123",
			"CheckoutRequestID":"456",
			"ResponseCode":"0",
			"ResponseDescription":"Success",
			"CustomerMessage":"Accepted"
		}`))
	}))

	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		passkey:      passkey,
		tokenManager: fakeTokenProvider{},
	}

	req := STKPushRequest{
		BusinessShortCode: "174379",
		Amount:            1,
		PartyA:            "254700000001",
		PartyB:            "174379",
		PhoneNumber:       "254700000001",
		CallBackURL:       "https://example.com/callback",
		AccountReference:  "INV001",
		TransactionDesc:   "Payment",
	}

	_, err := client.STKPush(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsedTime, err := time.Parse("20060102150405", received.Timestamp)
	if err != nil {
		t.Fatalf("expected a valid timestamp, got %q: %v", received.Timestamp, err)
	}

	if parsedTime.IsZero() {
		t.Fatal("expected parsed timestamp to be non-zero")
	}

	wantPassword := base64.StdEncoding.EncodeToString([]byte(
		req.BusinessShortCode + passkey + received.Timestamp,
	))
	if received.Password != wantPassword {
		t.Errorf("expected password %q, got %q", wantPassword, received.Password)
	}
}

func TestSTKPushReturnsErrorForNon2xxResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "client error", statusCode: http.StatusBadRequest},
		{name: "server error", statusCode: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := &Client{
				baseURL:      server.URL,
				HTTPClient:   server.Client(),
				tokenManager: fakeTokenProvider{},
			}

			resp, err := client.STKPush(context.Background(), STKPushRequest{})
			if resp != nil {
				t.Fatalf("expected nil response, got %#v", resp)
			}

			wantErr := fmt.Sprintf("unexpected status code: %d", tt.statusCode)
			if err == nil || err.Error() != wantErr {
				t.Fatalf("expected error %q, got %v", wantErr, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// STK Push Query Tests
// ---------------------------------------------------------------------------

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("fixtures/" + name)
	if err != nil {
		t.Fatalf("loadFixture: %v", err)
	}
	return data
}

func TestSTKPushQuery_UsesCorrectEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mpesa/stkpushquery/v1/query" {
			t.Errorf("expected path /mpesa/stkpushquery/v1/query, got %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %q", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization Bearer test-token, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ResponseCode": "0",
			"ResponseDescription": "Success",
			"MerchantRequestID": "m-1",
			"CheckoutRequestID": "c-1",
			"ResultCode": "0",
			"ResultDesc": "The service request is processed successfully."
		}`))
	}))
	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	resp, err := client.STKPushQuery(context.Background(), STKPushQueryRequest{
		BusinessShortCode: "174379",
		Password:          "pass",
		Timestamp:         "20260918120000",
		CheckoutRequestID: "c-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected Success=true, got false")
	}
}

func TestSTKPushQuery_Success(t *testing.T) {
	fixture := loadFixture(t, "stk_push_query_success.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	resp, err := client.STKPushQuery(context.Background(), STKPushQueryRequest{
		BusinessShortCode: "174379",
		Password:          "dGVzdA==",
		Timestamp:         "20191113130211",
		CheckoutRequestID: "ws_CO_13112019130211790_358936185",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected Success=true, got false; message: %s", resp.Message)
	}
	if resp.TransactionID != "ws_CO_13112019130211790_358936185" {
		t.Errorf("unexpected TransactionID: %s", resp.TransactionID)
	}
	if resp.Raw == nil {
		t.Error("expected Raw to be populated")
	}

	checkoutID, ok := resp.Metadata["checkout_request_id"]
	if !ok || checkoutID != "ws_CO_13112019130211790_358936185" {
		t.Errorf("unexpected checkout_request_id in metadata: %v", checkoutID)
	}
}

func TestSTKPushQuery_CustomerCancelled(t *testing.T) {
	fixture := loadFixture(t, "stk_push_query_cancelled.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	resp, err := client.STKPushQuery(context.Background(), STKPushQueryRequest{
		BusinessShortCode: "174379",
		Password:          "dGVzdA==",
		Timestamp:         "20191113130211",
		CheckoutRequestID: "ws_CO_13112019130211790_358936186",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected Success=false for cancelled transaction")
	}
	if resp.Message != "Request cancelled by user" {
		t.Errorf("unexpected message: %s", resp.Message)
	}
	// ResultCode "1032" maps to ErrCardDeclined
	if resp.ErrorCode != "card_declined" {
		t.Errorf("expected error code card_declined, got %s", resp.ErrorCode)
	}
}

func TestSTKPushQuery_QueryFailed(t *testing.T) {
	fixture := loadFixture(t, "stk_push_query_failure.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	resp, err := client.STKPushQuery(context.Background(), STKPushQueryRequest{
		BusinessShortCode: "174379",
		Password:          "dGVzdA==",
		Timestamp:         "20191113130211",
		CheckoutRequestID: "ws_CO_INVALID",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected Success=false for failed query")
	}
	if resp.Message != "The initiator information is invalid." {
		t.Errorf("unexpected message: %s", resp.Message)
	}
}

func TestSTKPushQuery_GeneratesTimestampPassword(t *testing.T) {
	const passkey = "query-passkey"
	var received STKPushQueryRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ResponseCode": "0",
			"ResponseDescription": "Success",
			"MerchantRequestID": "m-1",
			"CheckoutRequestID": "c-1",
			"ResultCode": "0",
			"ResultDesc": "Success"
		}`))
	}))
	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		passkey:      passkey,
		tokenManager: fakeTokenProvider{},
	}

	req := STKPushQueryRequest{
		BusinessShortCode: "174379",
		CheckoutRequestID: "c-1",
	}

	_, err := client.STKPushQuery(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsedTime, err := time.Parse("20060102150405", received.Timestamp)
	if err != nil {
		t.Fatalf("expected a valid timestamp, got %q: %v", received.Timestamp, err)
	}
	if parsedTime.IsZero() {
		t.Fatal("expected parsed timestamp to be non-zero")
	}

	wantPassword := base64.StdEncoding.EncodeToString([]byte(
		req.BusinessShortCode + passkey + received.Timestamp,
	))
	if received.Password != wantPassword {
		t.Errorf("expected password %q, got %q", wantPassword, received.Password)
	}
}

func TestSTKPushQuery_IdempotencyKeyHeader(t *testing.T) {
	const idempotencyKey = "idem-key-xyz"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Idempotency-Key")
		if got != idempotencyKey {
			t.Errorf("Idempotency-Key header: got %q, want %q", got, idempotencyKey)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ResponseCode": "0",
			"ResponseDescription": "Success",
			"MerchantRequestID": "m-1",
			"CheckoutRequestID": "c-1",
			"ResultCode": "0",
			"ResultDesc": "Success"
		}`))
	}))
	defer server.Close()

	client := &Client{
		baseURL:      server.URL,
		HTTPClient:   server.Client(),
		tokenManager: fakeTokenProvider{},
	}

	_, err := client.STKPushQuery(context.Background(), STKPushQueryRequest{
		IdempotencyKey:    idempotencyKey,
		BusinessShortCode: "174379",
		Password:          "pass",
		Timestamp:         "20260918120000",
		CheckoutRequestID: "c-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSTKPushQuery_ReturnsErrorForNon2xxResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "client error", statusCode: http.StatusBadRequest},
		{name: "server error", statusCode: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := &Client{
				baseURL:      server.URL,
				HTTPClient:   server.Client(),
				tokenManager: fakeTokenProvider{},
			}

			resp, err := client.STKPushQuery(context.Background(), STKPushQueryRequest{})
			if resp != nil {
				t.Fatalf("expected nil response, got %#v", resp)
			}

			wantErr := fmt.Sprintf("unexpected status code: %d", tt.statusCode)
			if err == nil || err.Error() != wantErr {
				t.Fatalf("expected error %q, got %v", wantErr, err)
			}
		})
	}
}

func TestSTKPushQueryRequest_Serialization(t *testing.T) {
	req := STKPushQueryRequest{
		IdempotencyKey:    "key-should-be-omitted",
		BusinessShortCode: "174379",
		Password:          "MTc0Mzc5...",
		Timestamp:         "20191113130211",
		CheckoutRequestID: "ws_CO_test",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal STKPushQueryRequest: %v", err)
	}

	jsonStr := string(data)

	// IdempotencyKey must be excluded from the JSON body sent to Daraja
	for _, forbidden := range []string{"IdempotencyKey", "key-should-be-omitted"} {
		if contains(jsonStr, forbidden) {
			t.Errorf("JSON body must not contain %q, got: %s", forbidden, jsonStr)
		}
	}

	// Required PascalCase keys per Daraja schema
	for _, key := range []string{`"BusinessShortCode"`, `"Password"`, `"Timestamp"`, `"CheckoutRequestID"`} {
		if !contains(jsonStr, key) {
			t.Errorf("expected JSON key %s in body, got: %s", key, jsonStr)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestMapSTKPushQueryResponse(t *testing.T) {
	tests := []struct {
		name          string
		input         STKPushQueryResponse
		wantSuccess   bool
		wantMessage   string
		wantTxID      string
		wantErrorCode string
	}{
		{
			name: "successful transaction",
			input: STKPushQueryResponse{
				ResponseCode:        "0",
				ResponseDescription: "Accepted",
				MerchantRequestID:   "m-1",
				CheckoutRequestID:   "c-1",
				ResultCode:          "0",
				ResultDesc:          "The service request is processed successfully.",
			},
			wantSuccess:   true,
			wantMessage:   "The service request is processed successfully.",
			wantTxID:      "c-1",
			wantErrorCode: "",
		},
		{
			name: "cancelled by user (1032)",
			input: STKPushQueryResponse{
				ResponseCode:        "0",
				ResponseDescription: "Accepted",
				MerchantRequestID:   "m-2",
				CheckoutRequestID:   "c-2",
				ResultCode:          "1032",
				ResultDesc:          "Request cancelled by user",
			},
			wantSuccess:   false,
			wantMessage:   "Request cancelled by user",
			wantTxID:      "c-2",
			wantErrorCode: "card_declined",
		},
		{
			name: "timeout (1025)",
			input: STKPushQueryResponse{
				ResponseCode:        "0",
				ResponseDescription: "Accepted",
				MerchantRequestID:   "m-3",
				CheckoutRequestID:   "c-3",
				ResultCode:          "1025",
				ResultDesc:          "Transaction timed out",
			},
			wantSuccess:   false,
			wantMessage:   "Transaction timed out",
			wantTxID:      "c-3",
			wantErrorCode: "request_timeout",
		},
		{
			name: "invalid number (2001)",
			input: STKPushQueryResponse{
				ResponseCode:        "0",
				ResponseDescription: "Accepted",
				MerchantRequestID:   "m-4",
				CheckoutRequestID:   "c-4",
				ResultCode:          "2001",
				ResultDesc:          "The initiator information is invalid.",
			},
			wantSuccess:   false,
			wantMessage:   "The initiator information is invalid.",
			wantTxID:      "c-4",
			wantErrorCode: "invalid_number",
		},
		{
			name: "unknown result code defaults to processing_error",
			input: STKPushQueryResponse{
				ResponseCode:        "0",
				ResponseDescription: "Accepted",
				MerchantRequestID:   "m-5",
				CheckoutRequestID:   "c-5",
				ResultCode:          "9999",
				ResultDesc:          "Some unknown internal error",
			},
			wantSuccess:   false,
			wantMessage:   "Some unknown internal error",
			wantTxID:      "c-5",
			wantErrorCode: "processing_error",
		},
		{
			name: "gateway error response code",
			input: STKPushQueryResponse{
				ResponseCode:        "1",
				ResponseDescription: "The initiator information is invalid.",
				MerchantRequestID:   "",
				CheckoutRequestID:   "",
			},
			wantSuccess:   false,
			wantMessage:   "The initiator information is invalid.",
			wantTxID:      "",
			wantErrorCode: "processing_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(`{}`)
			resp := MapSTKPushQueryResponse(tt.input, raw)

			if resp.Success != tt.wantSuccess {
				t.Errorf("Success: got %v, want %v", resp.Success, tt.wantSuccess)
			}
			if resp.Message != tt.wantMessage {
				t.Errorf("Message: got %q, want %q", resp.Message, tt.wantMessage)
			}
			if resp.TransactionID != tt.wantTxID {
				t.Errorf("TransactionID: got %q, want %q", resp.TransactionID, tt.wantTxID)
			}
			if resp.ErrorCode != tt.wantErrorCode {
				t.Errorf("ErrorCode: got %q, want %q", resp.ErrorCode, tt.wantErrorCode)
			}
			if resp.Metadata["merchant_request_id"] != tt.input.MerchantRequestID {
				t.Errorf("metadata merchant_request_id mismatch")
			}
		})
	}
}

func (f fakeTokenProvider) GetAccessToken(ctx context.Context) (string, error) {
	return "test-token", nil
}
