package mpesa

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Flying-Tea-Squad/paykit-go"
)

// AccessTokenProvider supplies OAuth access tokens for M-Pesa API requests.
type AccessTokenProvider interface {
	GetAccessToken(ctx context.Context) (string, error)
}

// Client sends requests to the M-Pesa API.
type Client struct {
	baseURL string
	paykit.HTTPClient
	passkey      string
	tokenManager AccessTokenProvider
}

// NewMpesaClient creates an M-Pesa API client.
func NewMpesaClient(
	baseURL string,
	httpClient paykit.HTTPClient,
	passkey string,
	tokenManager AccessTokenProvider,
) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		baseURL:      baseURL,
		HTTPClient:   httpClient,
		passkey:      passkey,
		tokenManager: tokenManager,
	}
}

// STKPush sends an STK Push payment request to the M-Pesa API.
func (c *Client) STKPush(ctx context.Context, req STKPushRequest) (*STKPushResponse, error) {
	req.Timestamp = generateTimestamp()
	req.Password = generatePassword(
		req.BusinessShortCode,
		c.passkey,
		req.Timestamp,
	)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/mpesa/stkpush/v1/processrequest",
		bytes.NewReader(body),
	)

	if err != nil {
		return nil, err
	}

	if c.tokenManager == nil {
		return nil, fmt.Errorf("mpesa: token manager is required")
	}

	token, err := c.tokenManager.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)

	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var stkResp STKPushResponse

	err = json.NewDecoder(resp.Body).Decode(&stkResp)
	if err != nil {
		return nil, err
	}

	return &stkResp, nil
}

// STKPushQuery checks the status of an STK Push transaction using CheckoutRequestID
// via the /mpesa/stkpushquery/v1/query endpoint and maps the response to a paykit.Response.
func (c *Client) STKPushQuery(ctx context.Context, req STKPushQueryRequest) (*paykit.Response, error) {
	if req.Timestamp == "" {
		req.Timestamp = generateTimestamp()
	}
	if req.Password == "" && c.passkey != "" {
		req.Password = generatePassword(
			req.BusinessShortCode,
			c.passkey,
			req.Timestamp,
		)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mpesa: marshal STKPushQueryRequest: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/mpesa/stkpushquery/v1/query",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("mpesa: build STKPushQuery request: %w", err)
	}

	if c.tokenManager == nil {
		return nil, fmt.Errorf("mpesa: token manager is required")
	}

	token, err := c.tokenManager.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)

	if req.IdempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mpesa: STKPushQuery HTTP request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mpesa: read response body: %w", err)
	}

	var queryResp STKPushQueryResponse
	if err := json.Unmarshal(bodyBytes, &queryResp); err != nil {
		return nil, fmt.Errorf("mpesa: decode STKPushQueryResponse: %w", err)
	}

	return MapSTKPushQueryResponse(queryResp, json.RawMessage(bodyBytes)), nil
}

// MapSTKPushQueryResponse converts an STKPushQueryResponse into the shared
// paykit.Response struct, mapping gateway error codes and extracting metadata.
func MapSTKPushQueryResponse(r STKPushQueryResponse, raw json.RawMessage) *paykit.Response {
	metadata := map[string]any{
		"merchant_request_id":  r.MerchantRequestID,
		"checkout_request_id":  r.CheckoutRequestID,
		"response_code":        r.ResponseCode,
		"response_description": r.ResponseDescription,
		"result_code":          r.ResultCode,
		"result_desc":          r.ResultDesc,
	}

	// Gateway rejected the query (e.g. invalid credentials or transaction not found)
	if r.ResponseCode != "0" {
		msg := r.ResponseDescription
		if msg == "" {
			msg = r.ResultDesc
		}
		return &paykit.Response{
			Success:       false,
			Message:       msg,
			TransactionID: r.CheckoutRequestID,
			Raw:           raw,
			ErrorCode:     paykit.MapGatewayErrorCode(r.ResponseCode),
			Metadata:      metadata,
		}
	}

	// Transaction completed successfully
	if r.ResultCode == "0" {
		msg := r.ResultDesc
		if msg == "" {
			msg = r.ResponseDescription
		}
		return &paykit.Response{
			Success:       true,
			Message:       msg,
			TransactionID: r.CheckoutRequestID,
			Raw:           raw,
			Metadata:      metadata,
		}
	}

	// Transaction failed, cancelled by user, or timed out
	msg := r.ResultDesc
	if msg == "" {
		msg = r.ResponseDescription
	}
	return &paykit.Response{
		Success:       false,
		Message:       msg,
		TransactionID: r.CheckoutRequestID,
		Raw:           raw,
		ErrorCode:     paykit.MapGatewayErrorCode(r.ResultCode),
		Metadata:      metadata,
	}
}

func generateTimestamp() string {
	return time.Now().Format("20060102150405")
}

func generatePassword(shortcode, passkey, timeStamp string) string {
	raw := shortcode + passkey + timeStamp

	return base64.StdEncoding.EncodeToString([]byte(raw))
}
