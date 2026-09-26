// Package telegram delivers izci notifications through a Telegram bot and
// handles what users send back: chat and account linking commands and the
// triage buttons under notifications.
//
// The Bot API is called directly over HTTP; see
// https://core.telegram.org/bots/api for the methods used here.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the Telegram Bot API.
type Client struct {
	base string
	http *http.Client
}

// NewClient creates a client for the bot with token. apiURL is usually
// https://api.telegram.org.
func NewClient(apiURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{base: strings.TrimRight(apiURL, "/") + "/bot" + token + "/", http: hc}
}

// APIError is an unsuccessful Bot API response.
type APIError struct {
	Code        int
	Description string
	// RetryAfter is set for 429 responses (flood control).
	RetryAfter time.Duration
	// MigrateToChatID is set when a group was upgraded to a supergroup.
	MigrateToChatID int64
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}

// Permanent reports errors that retrying cannot fix: the bot was removed
// from the chat, the chat is gone or the request itself is invalid.
func (e *APIError) Permanent() bool {
	return e.Code == http.StatusForbidden || (e.Code == http.StatusBadRequest && e.MigrateToChatID == 0 && !e.NotModified())
}

// NotModified is the error Telegram returns when an edit changes nothing.
func (e *APIError) NotModified() bool {
	return e.Code == http.StatusBadRequest && strings.Contains(e.Description, "message is not modified")
}

type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter      int   `json:"retry_after"`
		MigrateToChatID int64 `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

// call posts params as JSON to method and decodes the result into out.
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		// The URL contains the token: never let it reach logs.
		var uerr interface{ Unwrap() error }
		if errors.As(err, &uerr) {
			return fmt.Errorf("telegram %s: %w", method, uerr.Unwrap())
		}
		return fmt.Errorf("telegram %s: request failed", method)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("telegram %s: unexpected response (HTTP %d)", method, res.StatusCode)
	}
	if !env.OK {
		e := &APIError{Code: env.ErrorCode, Description: env.Description}
		if e.Code == 0 {
			e.Code = res.StatusCode
		}
		if env.Parameters != nil {
			e.RetryAfter = time.Duration(env.Parameters.RetryAfter) * time.Second
			e.MigrateToChatID = env.Parameters.MigrateToChatID
		}
		return e
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}

// User is a Telegram user or bot.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Chat is a private chat, group, supergroup or channel.
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	IsForum   bool   `json:"is_forum,omitempty"`
}

// Name is a readable name of the chat.
func (c Chat) Name() string {
	switch {
	case c.Title != "":
		return c.Title
	case c.Username != "":
		return "@" + c.Username
	default:
		return c.FirstName
	}
}

// Message is a chat message.
type Message struct {
	MessageID       int64  `json:"message_id"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	IsTopicMessage  bool   `json:"is_topic_message,omitempty"`
	From            *User  `json:"from,omitempty"`
	Chat            Chat   `json:"chat"`
	Text            string `json:"text,omitempty"`
	MigrateToChatID int64  `json:"migrate_to_chat_id,omitempty"`
}

// CallbackQuery is a press on an inline keyboard button.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// Update is one incoming update.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// InlineKeyboard is an inline keyboard attached to a message.
type InlineKeyboard struct {
	Rows [][]Button `json:"inline_keyboard"`
}

// Button is an inline keyboard button: a link or a callback.
type Button struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

// OutgoingMessage is the content of a message to send or an edit.
type OutgoingMessage struct {
	ChatID   int64
	ThreadID int64
	Text     string
	Keyboard *InlineKeyboard
}

// GetMe returns the bot's own user.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var u User
	return &u, c.call(ctx, "getMe", struct{}{}, &u)
}

type linkPreview struct {
	IsDisabled bool `json:"is_disabled"`
}

// SendMessage posts an HTML message and returns it.
func (c *Client) SendMessage(ctx context.Context, m OutgoingMessage) (*Message, error) {
	params := map[string]any{
		"chat_id":              m.ChatID,
		"text":                 m.Text,
		"parse_mode":           "HTML",
		"link_preview_options": linkPreview{IsDisabled: true},
	}
	if m.ThreadID != 0 {
		params["message_thread_id"] = m.ThreadID
	}
	if m.Keyboard != nil {
		params["reply_markup"] = m.Keyboard
	}
	var out Message
	return &out, c.call(ctx, "sendMessage", params, &out)
}

// EditMessageText replaces the text and keyboard of a message. Unchanged
// content is not an error.
func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string, kb *InlineKeyboard) error {
	params := map[string]any{
		"chat_id":              chatID,
		"message_id":           messageID,
		"text":                 text,
		"parse_mode":           "HTML",
		"link_preview_options": linkPreview{IsDisabled: true},
	}
	if kb != nil {
		params["reply_markup"] = kb
	}
	err := c.call(ctx, "editMessageText", params, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.NotModified() {
		return nil
	}
	return err
}

// AnswerCallbackQuery confirms a button press with a short notice.
func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string, alert bool) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text, "show_alert": alert}, nil)
}

// GetUpdates long-polls for updates after offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var out []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         int(timeout.Seconds()),
		"allowed_updates": []string{"message", "callback_query"},
	}, &out)
	return out, err
}

// SetWebhook registers the webhook URL with its secret token.
func (c *Client) SetWebhook(ctx context.Context, url, secret string) error {
	return c.call(ctx, "setWebhook", map[string]any{
		"url":             url,
		"secret_token":    secret,
		"allowed_updates": []string{"message", "callback_query"},
	}, nil)
}

// DeleteWebhook removes a webhook so that getUpdates works.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.call(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
}
