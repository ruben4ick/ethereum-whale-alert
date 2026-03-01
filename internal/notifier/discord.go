package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Discord struct {
	webhookURL string
	httpClient *http.Client
}

func NewDiscord(webhookURL string) *Discord {
	return &Discord{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title     string         `json:"title"`
	Color     int            `json:"color"`
	Fields    []discordField `json:"fields"`
	Timestamp string         `json:"timestamp"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

func (d *Discord) Notify(ctx context.Context, event AlertEvent) error {
	payload := discordPayload{
		Embeds: []discordEmbed{
			{
				Title: "🐋 Whale Transaction Detected",
				Color: 3447003, // blue
				Fields: []discordField{
					{Name: "Tx Hash", Value: event.TxHash, Inline: false},
					{Name: "Block", Value: event.BlockNumber.String(), Inline: true},
					{Name: "Value", Value: event.ValueETH + " ETH", Inline: true},
					{Name: "To", Value: event.To, Inline: false},
				},
				Timestamp: event.Timestamp.UTC().Format(time.RFC3339),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send discord notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}

	return nil
}
