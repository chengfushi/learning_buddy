package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxParseErrorRunes = 512

type parseFailureAlerter interface {
	AlertParseFailure(ctx context.Context, materialID int64, parseError string) error
}

type noopParseFailureAlerter struct{}

func (noopParseFailureAlerter) AlertParseFailure(context.Context, int64, string) error { return nil }

type webhookParseFailureAlerter struct {
	url    string
	client *http.Client
}

type parseFailureAlert struct {
	Event      string    `json:"event"`
	MaterialID int64     `json:"material_id"`
	Error      string    `json:"error"`
	OccurredAt time.Time `json:"occurred_at"`
}

func newParseFailureAlerter(webhookURL string) parseFailureAlerter {
	if strings.TrimSpace(webhookURL) == "" {
		return noopParseFailureAlerter{}
	}
	return &webhookParseFailureAlerter{
		url: webhookURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (a *webhookParseFailureAlerter) AlertParseFailure(
	ctx context.Context,
	materialID int64,
	parseError string,
) error {
	payload, err := json.Marshal(parseFailureAlert{
		Event:      "material_parse_failed",
		MaterialID: materialID,
		Error:      parseError,
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("marshal parse failure alert: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create parse failure alert request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("post parse failure alert: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("parse failure alert returned %s", resp.Status)
	}
	return nil
}

func boundedParseError(err error) string {
	message := []rune(err.Error())
	if len(message) <= maxParseErrorRunes {
		return string(message)
	}
	return string(message[:maxParseErrorRunes-3]) + "..."
}
