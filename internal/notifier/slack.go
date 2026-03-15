package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Slack struct {
	webhookURL string
	httpClient *http.Client
}

func NewSlack(webhookURL string) *Slack {
	return &Slack{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type slackPayload struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string      `json:"type"`
	Text     *slackText  `json:"text,omitempty"`
	Fields   []slackText `json:"fields,omitempty"`
	Elements []slackText `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Slack) Notify(ctx context.Context, event AlertEvent) error {
	title := "🐋 Whale Transaction Detected"
	valueLabel := event.ValueETH + " ETH"
	if event.Type == TypeERC20 {
		title = "🐋 Whale ERC-20 Transfer Detected"
		valueLabel = event.TokenAmount + " tokens (≈ " + event.ValueETH + " ETH)"
	}

	fields := []slackText{
		{Type: "mrkdwn", Text: "*Tx Hash*\n" + event.TxHash},
		{Type: "mrkdwn", Text: "*Value*\n" + valueLabel},
		{Type: "mrkdwn", Text: "*Block*\n" + event.BlockNumber.String()},
		{Type: "mrkdwn", Text: "*To*\n" + event.To},
	}
	if event.From != "" {
		fields = append(fields, slackText{Type: "mrkdwn", Text: "*From*\n" + event.From})
	}
	if event.Token != "" {
		fields = append(fields, slackText{Type: "mrkdwn", Text: "*Token*\n" + event.Token})
	}

	payload := slackPayload{
		Blocks: []slackBlock{
			{
				Type: "header",
				Text: &slackText{Type: "plain_text", Text: title},
			},
			{
				Type:   "section",
				Fields: fields,
			},
			{
				Type: "context",
				Elements: []slackText{
					{Type: "mrkdwn", Text: event.Timestamp.UTC().Format(time.RFC1123)},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	return nil
}
